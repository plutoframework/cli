package project

import (
	"strings"
	"testing"
)

// authEventsFixture is the seam fixture plus the hook operation, declared in
// [auth.events], with every role holding the hook permission.
func authEventsFixture(t *testing.T) Manifest {
	t.Helper()

	m := generatableSeamFixture(t)

	m.Memberships.ActiveColumn = "state"
	m.Memberships.ActiveValues = []string{"active"}
	m.Models = append(m.Models, Model{
		ID:     "audit.AuthEvent",
		Fields: []string{"event:string"},
	})

	for index := range m.Roles {
		m.Roles[index].Permissions = append(
			m.Roles[index].Permissions,
			"audit.record",
		)
	}

	m.Operations = append(m.Operations, Operation{
		Group:          "audit",
		Action:         "RecordAuthEvent",
		Permission:     "audit.record",
		Write:          true,
		Idempotency:    "exempt",
		ReplayReason:   "Recorded once per sign-in.",
		Body:           BodyAuthored,
		Resource:       "audit.events",
		ResourcePolicy: "tenant",
		Ports:          []string{"audit.Events"},
		Input:          "audit.AuthEvent",
	})
	m.Auth = &AuthSection{Events: &AuthEvents{
		Operation: "audit.RecordAuthEvent",
	}}

	return m
}

func TestAuthEventsValidatesAndDefaultsClosed(t *testing.T) {
	m := authEventsFixture(t)

	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}

	if !m.Auth.Events.Closed() {
		t.Fatal("the default failure policy must be closed")
	}

	m.Auth.Events.OnError = AuthEventsBestEffort
	if err := m.Validate(); err != nil || m.Auth.Events.Closed() {
		t.Fatalf("best effort rejected or ignored: %v", err)
	}
}

func TestAuthEventsRejections(t *testing.T) {
	operation := func(m *Manifest) *Operation {
		return &m.Operations[len(m.Operations)-1]
	}

	cases := []struct {
		name   string
		mutate func(*Manifest)
		want   string
	}{
		{"empty section", func(m *Manifest) {
			m.Auth = &AuthSection{}
		}, "declares nothing"},
		{"published profile", func(m *Manifest) {
			m.Boundary.Mode = ""
		}, "candidate"},
		{"no local credentials", func(m *Manifest) {
			m.Capabilities.LocalCredentials = false
		}, "local credentials"},
		{"no memberships", func(m *Manifest) {
			m.Memberships = nil
		}, "[memberships]"},
		{"bad policy", func(m *Manifest) {
			m.Auth.Events.OnError = "open"
		}, "on_error"},
		{"undeclared operation", func(m *Manifest) {
			m.Auth.Events.Operation = "audit.Missing"
		}, "not declared"},
		{"read operation", func(m *Manifest) {
			operation(m).Write = false
			operation(m).Idempotency = "none"
		}, "protected authored write"},
		{"public operation", func(m *Manifest) {
			operation(m).Public = true
		}, "protected authored write"},
		{"response", func(m *Manifest) {
			operation(m).Response = ResponseObject
			operation(m).Output = "audit.Verification"
		}, "no response"},
		{"input with an actor", func(m *Manifest) {
			m.Models[len(m.Models)-1].Fields = []string{
				"event:string", "actor_id:string",
			}
		}, "exactly one field"},
		{"input with a tenant", func(m *Manifest) {
			m.Models[len(m.Models)-1].Fields = []string{"tenant:string"}
		}, "exactly one field"},
		{"no input", func(m *Manifest) {
			operation(m).Input = ""
		}, "input model"},
		{"exposed by an endpoint", func(m *Manifest) {
			m.Endpoints = append(m.Endpoints, Endpoint{
				Group:       "audit",
				Action:      "RecordAuthEvent",
				Operation:   "audit.RecordAuthEvent",
				Method:      "POST",
				Path:        "/api/audit/events",
				Name:        "Record",
				Description: "Record.",
			})
		}, "endpoint"},
		{"role without the permission", func(m *Manifest) {
			m.Roles[len(m.Roles)-1].Permissions = []string{"audit.read"}
		}, "lacks permission"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			m := authEventsFixture(t)
			test.mutate(&m)

			err := m.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want error containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestAuthEventsMemberIndexSQL(t *testing.T) {
	m := authEventsFixture(t)
	users := m.Tables[1]

	sql := memberIndexSQL(m, users)

	for _, want := range []string{
		"CREATE POLICY pluto_auth_event_members ON users FOR SELECT",
		"USING (id = current_setting('app.auth_event_subject', true))",
		"CREATE OR REPLACE VIEW pluto_auth_event_members WITH " +
			"(security_invoker = true)",
		"SELECT org_id::text AS tenant_id FROM users WHERE state::text = " +
			"ANY (ARRAY['active'])",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("member index lacks %q:\n%s", want, sql)
		}
	}

	if got := memberIndexSQL(m, m.Tables[0]); got != "" {
		t.Fatalf("another table got the member index: %s", got)
	}

	m.Memberships.ActiveValues = nil
	if got := memberIndexSQL(m, users); !strings.Contains(
		got, "WHERE state IS TRUE",
	) {
		t.Fatalf("boolean active predicate missing:\n%s", got)
	}

	m.Memberships.ActiveColumn = ""
	if got := memberIndexSQL(m, users); strings.Contains(got, "WHERE") {
		t.Fatalf("an unconditional membership gained a predicate:\n%s", got)
	}

	m.Auth = nil
	if got := memberIndexSQL(m, users); got != "" {
		t.Fatalf("the index exists without [auth.events]: %s", got)
	}
}

func TestAuthEventsManagedOutput(t *testing.T) {
	m := authEventsFixture(t)
	m.Capabilities.OIDC = true

	files, err := candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	contains := func(path string, wants ...string) {
		t.Helper()

		source, ok := files[path]
		if !ok {
			t.Fatalf("missing managed file %s", path)
		}

		for _, want := range wants {
			if !strings.Contains(string(source), want) {
				t.Fatalf("%s lacks %q:\n%s", path, want, source)
			}
		}
	}

	contains(
		"internal/app/candidate_auth_events_gen.go",
		"const authEventsClosed = true",
		"if authEventsClosed {\n\t\t\t\t_ = e.manager.Revoke(ctx, token)",
		"if authEventsClosed {\n\t\t\t\t\tauthEventUnavailable(w)",
		"e.tenants.Tenants(ctx, subject)",
		"access.WithSession(",
		`"logout"`,
	)
	contains(
		"internal/app/candidate_credentials_gen.go",
		`events.signedIn("login", handler)`,
		`events.signedIn("oidc_login", oidcHandler)`,
		"events.signingOut(handler)",
	)
	contains(
		"internal/app/candidate_operations_gen.go",
		"newAuthEvents(",
		"authEventModels.RecordAuthEventRequest{Event: event}",
		"registerCandidateCredentials(router, authentication, events)",
	)
	contains(
		"internal/storage/identity/memberships_gen.go",
		"func (m *Memberships) Tenants(",
		"set_config('app.auth_event_subject'",
		"FROM pluto_auth_event_members",
	)

	m.Auth.Events.OnError = AuthEventsBestEffort

	files, err = candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	contains(
		"internal/app/candidate_auth_events_gen.go",
		"const authEventsClosed = false",
	)

	// Without the declaration none of the hook exists.
	m.Auth = nil

	files, err = candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"internal/app/candidate_auth_events_gen.go"} {
		if _, ok := files[path]; ok {
			t.Fatalf("%s generated without [auth.events]", path)
		}
	}

	// Applications without the declaration keep their exact prior render.
	credentials := string(files["internal/app/candidate_credentials_gen.go"])
	if strings.Contains(credentials, "events") ||
		!strings.Contains(
			credentials,
			"\t\t\trouter.Method(\n\t\t\t\tendpoint.Method,\n"+
				"\t\t\t\tendpoint.Path,\n\t\t\t\thttp.StripPrefix("+
				"\"/auth\", authentication.Credentials),\n\t\t\t)\n",
		) {
		t.Fatalf(
			"credential render changed without [auth.events]:\n%s",
			credentials,
		)
	}

	if strings.Contains(
		string(files["internal/storage/identity/memberships_gen.go"]),
		"Tenants(",
	) {
		t.Fatal("subject index read generated without [auth.events]")
	}
}
