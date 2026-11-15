package project

import (
	"regexp"
	"strings"
	"testing"
)

// generatableSeamFixture is the seam fixture minus the pagination-reserved
// query name that only validation tolerates.
func generatableSeamFixture(t *testing.T) Manifest {
	t.Helper()

	m := seamFixture(t)

	for index := range m.Operations {
		var kept []OperationQuery

		for _, param := range m.Operations[index].Query {
			if param.Name != "limit" {
				kept = append(kept, param)
			}
		}

		m.Operations[index].Query = kept
	}

	return m
}

// systemFixture extends the seam fixture with a tenant root, a provisioning
// operation and a slug lookup, each exposed by one endpoint.
func systemFixture(t *testing.T) Manifest {
	t.Helper()

	m := generatableSeamFixture(t)

	m.Tables = append(m.Tables, Table{
		Group:        "orgs",
		Name:         "orgs",
		TenantColumn: "id",
		RowSecurity:  true,
		TenantRoot:   true,
	})
	m.Queries = append(m.Queries, QuerySet{
		Group:  "orgs",
		Files:  []string{"db/queries/orgs/*.sql"},
		Tables: []string{"orgs"},
	})
	m.Ports = append(m.Ports, Port{
		ID:      "orgs.Tenants",
		Adapter: "orgs.TenantStore",
		Methods: []string{
			"Provision(scope.Provisioning, models.Signup) " +
				"(models.Org, error)",
		},
	})
	m.Models = append(
		m.Models,
		Model{ID: "orgs.Signup", Fields: []string{"slug:string"}},
		Model{
			ID:     "orgs.Org",
			Fields: []string{"id:string", "slug:string"},
		},
	)
	m.Lookups = []Lookup{{
		ID:      "orgs.BySlug",
		Table:   "orgs",
		Key:     "slug",
		Columns: []string{"id", "slug"},
	}}
	m.Operations = append(
		m.Operations,
		Operation{
			Group:        "orgs",
			Action:       "Provision",
			Scope:        ScopeProvision,
			Write:        true,
			Idempotency:  "exempt",
			ReplayReason: "A signup mints a new tenant.",
			RateLimit:    "3/1h",
			Body:         BodyAuthored,
			Ports:        []string{"orgs.Tenants"},
			Response:     ResponseObject,
			Input:        "orgs.Signup",
			Output:       "orgs.Org",
		},
		Operation{
			Group:       "orgs",
			Action:      "Lookup",
			Scope:       ScopeLookup,
			Lookup:      "orgs.BySlug",
			Public:      true,
			Idempotency: "none",
			RateLimit:   "30/1m",
			Body:        BodySystem,
			Response:    ResponseObject,
			Output:      "orgs.Org",
			Query: []OperationQuery{
				{Name: "slug", Type: "string", Max: 63},
			},
		},
	)
	m.Endpoints = append(
		m.Endpoints,
		Endpoint{
			Group:       "orgs",
			Action:      "Provision",
			Operation:   "orgs.Provision",
			Method:      "POST",
			Path:        "/api/orgs",
			Name:        "Provision",
			Description: "Create an organization.",
		},
		Endpoint{
			Group:       "orgs",
			Action:      "Lookup",
			Operation:   "orgs.Lookup",
			Method:      "GET",
			Path:        "/api/orgs/lookup",
			Name:        "Lookup",
			Description: "Resolve an organization by slug.",
		},
	)

	return m
}

func TestSystemManifestValidatesAndRoundTrips(t *testing.T) {
	m := systemFixture(t)
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}

	data, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(decoded.Lookups) != 1 || !decoded.Tables[2].TenantRoot ||
		decoded.Operations[len(decoded.Operations)-1].Scope != ScopeLookup {
		t.Fatalf("system declarations were lost: %+v", decoded)
	}

	endpoint, err := decoded.OperationEndpoint(decoded.Endpoints[len(
		decoded.Endpoints,
	)-2])
	if err != nil || endpoint.Scope != ScopeProvision {
		t.Fatalf("endpoint scope was not resolved: %v %+v", err, endpoint)
	}
}

func TestSystemManifestRejections(t *testing.T) {
	provision := func(m *Manifest) *Operation {
		return &m.Operations[len(m.Operations)-2]
	}
	lookup := func(m *Manifest) *Operation {
		return &m.Operations[len(m.Operations)-1]
	}

	cases := []struct {
		name   string
		mutate func(*Manifest)
		want   string
	}{
		{"no tenant root", func(m *Manifest) {
			m.Tables[2].TenantRoot = false
		}, "tenant_root"},
		{"two tenant roots", func(m *Manifest) {
			m.Tables[1].TenantRoot = true
		}, "exactly one tenant root"},
		{"append only root", func(m *Manifest) {
			m.Tables[2].AppendOnly = true
		}, "cannot be append_only"},
		{"no local credentials", func(m *Manifest) {
			m.Capabilities.LocalCredentials = false
		}, "local credentials"},
		{"provision with permission", func(m *Manifest) {
			provision(m).Permission = "members.read"
		}, "takes no permission"},
		{"provision with resource", func(m *Manifest) {
			provision(m).Resource = "orgs.orgs"
			provision(m).ResourcePolicy = "tenant"
		}, "takes no permission"},
		{"public provision", func(m *Manifest) {
			provision(m).Public = true
		}, "protected"},
		{"read provision", func(m *Manifest) {
			provision(m).Write = false
			provision(m).Idempotency = "none"
		}, "write = true"},
		{"provision without reason", func(m *Manifest) {
			provision(m).ReplayReason = ""
		}, "reason"},
		{"provision without rate limit", func(m *Manifest) {
			provision(m).RateLimit = ""
		}, "rate_limit"},
		{"provision with bad rate limit", func(m *Manifest) {
			provision(m).RateLimit = "0/1h"
		}, "attempts"},
		{"provision without ports", func(m *Manifest) {
			provision(m).Ports = nil
		}, "at least one port"},
		{"provision without input", func(m *Manifest) {
			provision(m).Input = ""
		}, "input model"},
		{"provision with query", func(m *Manifest) {
			provision(m).Query = []OperationQuery{{
				Name: "x", Type: "string", Max: 1,
			}}
		}, "JSON body"},
		{"provision without endpoint", func(m *Manifest) {
			m.Endpoints = append(m.Endpoints[:len(m.Endpoints)-2],
				m.Endpoints[len(m.Endpoints)-1])
		}, "exactly one endpoint"},
		{"provision on a read endpoint", func(m *Manifest) {
			m.Endpoints[len(m.Endpoints)-2].Method = "GET"
		}, "must use POST"},
		{"authored scope", func(m *Manifest) {
			provision(m).Scope = "system"
		}, "scope must be"},
		{"lookup with authored body", func(m *Manifest) {
			lookup(m).Body = BodyAuthored
		}, "body = \"system\""},
		{"lookup not public", func(m *Manifest) {
			lookup(m).Public = false
		}, "public read"},
		{"lookup with ports", func(m *Manifest) {
			lookup(m).Ports = []string{"orgs.Tenants"}
		}, "no ports"},
		{"lookup with unknown declaration", func(m *Manifest) {
			lookup(m).Lookup = "orgs.Missing"
		}, "undeclared lookup"},
		{"lookup query name differs from key", func(m *Manifest) {
			lookup(m).Query[0].Name = "name"
		}, "named slug"},
		{"lookup query is not a string", func(m *Manifest) {
			lookup(m).Query[0] = OperationQuery{Name: "slug", Type: "int64"}
		}, "named slug"},
		{"lookup output has extra fields", func(m *Manifest) {
			m.Models[len(m.Models)-1].Fields = append(
				m.Models[len(m.Models)-1].Fields,
				"billing_email:string",
			)
		}, "exactly the lookup columns"},
		{"lookup output has json field", func(m *Manifest) {
			m.Models[len(m.Models)-1].Fields = []string{
				"id:string", "slug:json",
			}
		}, "type"},
		{"lookup key outside the columns", func(m *Manifest) {
			m.Lookups[0].Columns = []string{"id"}
		}, "including the key"},
		{"lookup on the tenant column", func(m *Manifest) {
			m.Lookups[0].Key = "id"
		}, "other than id"},
		{"lookup on a non-root table", func(m *Manifest) {
			m.Lookups[0].Table = "missing"
		}, "tenant_root table"},
		{"unused lookup", func(m *Manifest) {
			m.Lookups = append(m.Lookups, Lookup{
				ID:      "orgs.ByName",
				Table:   "orgs",
				Key:     "name",
				Columns: []string{"name"},
			})
		}, "not used by any"},
		{"lookup shared by two operations", func(m *Manifest) {
			clone := *lookup(m)
			clone.Action = "Lookup2"
			m.Operations = append(m.Operations, clone)
		}, "share lookup"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			m := systemFixture(t)
			test.mutate(&m)

			err := m.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want error containing %q, got %v", test.want, err)
			}
		})
	}
}

// A provision body may join another group's port only through declared tx
// methods, and only when that port lists it as a caller.
func TestSystemProvisionForeignTxPorts(t *testing.T) {
	build := func(callers []string) Manifest {
		m := systemFixture(t)

		audit := &m.Ports[0]
		audit.Methods = append(
			audit.Methods,
			"AppendTx(scope.Tx, models.Event) error",
		)
		audit.TxMethods = []string{"AppendTx"}
		audit.TxCallers = callers

		operation := &m.Operations[len(m.Operations)-2]
		operation.Ports = append(operation.Ports, audit.ID)

		return m
	}

	if err := build([]string{"orgs.Provision"}).Validate(); err != nil {
		t.Fatalf("declared tx caller rejected: %v", err)
	}

	for name, callers := range map[string][]string{
		"unlisted": {"audit.Append"},
		"foreign":  {"audit.Missing"},
	} {
		err := build(callers).Validate()
		if err == nil {
			t.Fatalf("%s tx caller accepted a foreign provision port", name)
		}
	}

	m := systemFixture(t)
	operation := &m.Operations[len(m.Operations)-2]
	operation.Ports = append(operation.Ports, m.Ports[0].ID)

	err := m.Validate()
	if err == nil || !strings.Contains(err.Error(), "tx_callers") {
		t.Fatalf("plain foreign port accepted by provision: %v", err)
	}
}

func TestSystemScopeRequiresCandidateProfile(t *testing.T) {
	m := systemFixture(t)
	m.Boundary.Mode = ""

	if err := m.Validate(); err == nil {
		t.Fatal("system declarations validated outside the candidate profile")
	}
}

func TestLookupMigrationSQL(t *testing.T) {
	m := systemFixture(t)
	sql := lookupMigrationSQL(m, m.Tables[2])

	for _, want := range []string{
		"CREATE POLICY pluto_lookup_orgs_byslug ON orgs FOR SELECT",
		"USING (slug = current_setting('app.lookup_orgs_byslug', true))",
		"CREATE OR REPLACE VIEW pluto_lookup_orgs_byslug WITH " +
			"(security_invoker = true)",
		"SELECT id, slug FROM orgs",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("lookup migration lacks %q:\n%s", want, sql)
		}
	}

	if got := lookupMigrationSQL(m, m.Tables[0]); got != "" {
		t.Fatalf("a non-root table got lookup SQL: %s", got)
	}
}

func TestSystemManagedOutput(t *testing.T) {
	m := systemFixture(t)

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
		"internal/scope/scope_gen.go",
		"type Provisioning struct",
		"func (d DB) Provision(",
		"func BindProvisioning(",
		"ErrProvisioned",
		"pgx.ReadWrite",
	)
	contains(
		"internal/operations/orgs/provision_gate_gen.go",
		"UseIdentity",
		"Limiter.Allow",
		`"orgs.Provision"+":"+subject`,
	)
	contains(
		"internal/operations/orgs/provision_workflow_gen.go",
		"scope.BindProvisioning(",
		"logic.Provision(",
	)
	contains(
		"internal/logic/orgs/contract_gen.go",
		"scope.Provisioning,",
	)
	contains(
		"internal/handlers/orgs/provision.go",
		"httpauth.IdentityFromContext",
		"transport.Decode",
		"rate_limited",
	)
	contains(
		"internal/operations/orgs/lookup_gate_gen.go",
		"ErrLookupNotFound",
		"Limiter.Allow",
	)
	contains(
		"internal/storage/orgs/lookup_lookup_gen.go",
		`"SELECT set_config('app.lookup_orgs_byslug', $1, true)"`,
		`FROM \"pluto_lookup_orgs_byslug\" WHERE \"slug\" = $1 LIMIT 2`,
		"pgx.ReadOnly",
	)

	if !regexp.MustCompile(`SessionOnly:\s+true`).Match(
		files["internal/paths/orgs_gen.go"],
	) {
		t.Fatalf("provision endpoint is not session-only:\n%s",
			files["internal/paths/orgs_gen.go"])
	}

	contains(
		"internal/app/candidate_operations_gen.go",
		"identity.NewAttemptLimiter(",
		"orgsoperations.NewProvision(",
		"orgslookup.NewLookupReader(pool)",
	)

	// A project without system operations keeps the scope surface unchanged.
	plain := generatableSeamFixture(t)

	scope, err := candidateManaged(plain)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(scope["internal/scope/scope_gen.go"]),
		"Provisioning") {
		t.Fatal("scope exposes provisioning without a provision operation")
	}
}
