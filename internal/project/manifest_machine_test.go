package project

import (
	"strings"
	"testing"
)

// machineFixture extends the seam fixture with a gateway principal, its
// enrollment operation and one machine-authenticated write, each exposed by
// one endpoint.
func machineFixture(t *testing.T) Manifest {
	t.Helper()

	m := generatableSeamFixture(t)

	m.Tables = append(m.Tables, Table{
		Group:        "gateways",
		Name:         "gateways",
		TenantColumn: "org_id",
		RowSecurity:  true,
	})
	m.Queries = append(m.Queries, QuerySet{
		Group:  "gateways",
		Files:  []string{"db/queries/gateways/*.sql"},
		Tables: []string{"gateways"},
	})
	m.Ports = append(m.Ports, Port{
		ID:      "gateways.Registry",
		Adapter: "gateways.Store",
		Methods: []string{
			"Beat(scope.Execution, models.Beat) (models.Ack, error)",
		},
	})
	m.Models = append(
		m.Models,
		Model{ID: "gateways.EnrollInput", Fields: []string{
			"token:string", "public_key:string",
		}},
		Model{ID: "gateways.Enrolled", Fields: []string{"machine_id:string"}},
		Model{ID: "gateways.Beat", Fields: []string{"seq:int64"}},
		Model{ID: "gateways.Ack", Fields: []string{"seq:int64"}},
	)
	m.Principals = []Principal{{
		ID:           "gateways.Gateway",
		Kind:         PrincipalKindMachine,
		Table:        "gateways",
		IDColumn:     "id",
		KeyColumn:    "public_key",
		StateColumn:  "state",
		PendingValue: "pending",
		ActiveValue:  "active",
		Permissions:  []string{"gateway.heartbeat"},
	}}
	m.Operations = append(
		m.Operations,
		Operation{
			Group:        "gateways",
			Action:       "Enroll",
			Scope:        ScopeEnroll,
			Principal:    "gateways.Gateway",
			Public:       true,
			Write:        true,
			Idempotency:  "exempt",
			ReplayReason: "A single-use token is the replay protection.",
			RateLimit:    "5/1h",
			Body:         BodySystem,
			Response:     ResponseObject,
			Input:        "gateways.EnrollInput",
			Output:       "gateways.Enrolled",
		},
		Operation{
			Group:          "gateways",
			Action:         "Heartbeat",
			Auth:           "machine:gateways.Gateway",
			Permission:     "gateway.heartbeat",
			Write:          true,
			Idempotency:    "exempt",
			ReplayReason:   "Signed nonces reject replays.",
			Body:           BodyAuthored,
			Resource:       "gateways.heartbeat",
			ResourcePolicy: "tenant",
			Ports:          []string{"gateways.Registry"},
			Response:       ResponseObject,
			Input:          "gateways.Beat",
			Output:         "gateways.Ack",
		},
	)
	m.Endpoints = append(
		m.Endpoints,
		Endpoint{
			Group:       "gateways",
			Action:      "Enroll",
			Operation:   "gateways.Enroll",
			Method:      "POST",
			Path:        "/api/gateways/enroll",
			Name:        "Enroll",
			Description: "Enroll a gateway.",
		},
		Endpoint{
			Group:       "gateways",
			Action:      "Heartbeat",
			Operation:   "gateways.Heartbeat",
			Method:      "POST",
			Path:        "/api/gateway/heartbeat",
			Name:        "Heartbeat",
			Description: "A gateway reports liveness.",
		},
	)

	return m
}

func TestMachineManifestValidatesAndRoundTrips(t *testing.T) {
	m := machineFixture(t)
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

	if len(decoded.Principals) != 1 ||
		decoded.Principals[0].ID != "gateways.Gateway" {
		t.Fatalf("principals were lost: %+v", decoded.Principals)
	}

	entry := decoded.Endpoints[len(decoded.Endpoints)-1]

	view, err := decoded.OperationEndpoint(entry)
	if err != nil || view.Machine != "gateways.Gateway" ||
		view.Scope != ScopeMachine || view.Permission != "" {
		t.Fatalf("machine endpoint was not resolved: %v %+v", err, view)
	}
}

func TestMachineManifestRejections(t *testing.T) {
	enroll := func(m *Manifest) *Operation {
		return &m.Operations[len(m.Operations)-2]
	}
	beat := func(m *Manifest) *Operation {
		return &m.Operations[len(m.Operations)-1]
	}

	cases := []struct {
		name   string
		mutate func(*Manifest)
		want   string
	}{
		{"principal kind", func(m *Manifest) {
			m.Principals[0].Kind = "user"
		}, "kind must be"},
		{"principal table undeclared", func(m *Manifest) {
			m.Principals[0].Table = "nothing"
		}, "declared non-root table"},
		{"principal columns collide", func(m *Manifest) {
			m.Principals[0].KeyColumn = "id"
		}, "distinct"},
		{"principal tenant column reused", func(m *Manifest) {
			m.Principals[0].StateColumn = "org_id"
		}, "distinct"},
		{"principal same pending and active", func(m *Manifest) {
			m.Principals[0].PendingValue = "active"
		}, "distinct"},
		{"principal ttl too long", func(m *Manifest) {
			m.Principals[0].EnrollTTL = "1000h"
		}, "enroll_ttl"},
		{"principal replay window too long", func(m *Manifest) {
			m.Principals[0].ReplayWindow = "1h"
		}, "replay_window"},
		{"principal without permissions", func(m *Manifest) {
			m.Principals[0].Permissions = nil
		}, "permissions"},
		{"principal permission is a user permission", func(m *Manifest) {
			m.Principals[0].Permissions = []string{"audit.read"}
			beat(m).Permission = "audit.read"
		}, "declared separately from user roles"},
		{"principal without enroll operation", func(m *Manifest) {
			m.Operations = append(
				m.Operations[:len(m.Operations)-2],
				m.Operations[len(m.Operations)-1],
			)
			m.Endpoints = append(
				m.Endpoints[:len(m.Endpoints)-2],
				m.Endpoints[len(m.Endpoints)-1],
			)
		}, "no scope = \"enroll\" operation"},
		{"no local credentials", func(m *Manifest) {
			m.Capabilities.LocalCredentials = false
		}, "local credentials"},
		{"enroll with an undeclared principal", func(m *Manifest) {
			enroll(m).Principal = "gateways.Other"
		}, "principal"},
		{"enroll with a body", func(m *Manifest) {
			enroll(m).Body = BodyAuthored
		}, "body = \"system\""},
		{"enroll not public", func(m *Manifest) {
			enroll(m).Public = false
		}, "public write"},
		{"enroll without rate limit", func(m *Manifest) {
			enroll(m).RateLimit = ""
		}, "rate_limit"},
		{"enroll with a permission", func(m *Manifest) {
			enroll(m).Permission = "members.read"
		}, "takes no permission"},
		{"enroll input fields", func(m *Manifest) {
			m.Models[len(m.Models)-4].Fields = []string{
				"token:string", "public_key:string", "tenant:string",
			}
		}, "exactly the fields"},
		{"enroll output fields", func(m *Manifest) {
			m.Models[len(m.Models)-3].Fields = []string{
				"machine_id:string", "tenant_id:string",
			}
		}, "exactly the fields"},
		{"machine operation unknown principal", func(m *Manifest) {
			beat(m).Auth = "machine:gateways.Other"
		}, "declared principal"},
		{"machine operation bad auth", func(m *Manifest) {
			beat(m).Auth = "bearer:gateways.Gateway"
		}, "machine:<principal>"},
		{"machine operation public", func(m *Manifest) {
			beat(m).Public = true
			beat(m).Write = false
			beat(m).Input = ""
			beat(m).Idempotency = "none"
			m.Endpoints[len(m.Endpoints)-1].Method = "GET"
		}, "protected"},
		{"machine operation permission not granted", func(m *Manifest) {
			beat(m).Permission = "gateway.other"
		}, "not a machine permission"},
		{"machine operation without resource", func(m *Manifest) {
			beat(m).Resource = ""
		}, "resource"},
		{"machine operation page", func(m *Manifest) {
			beat(m).Collection = true
		}, "no collection"},
		{"machine write page", func(m *Manifest) {
			beat(m).Response = ResponsePage
			beat(m).Collection = true
			beat(m).Item = "gateways.Ack"
			beat(m).Output = ""
		}, "a read with collection"},
		{"machine write without input", func(m *Manifest) {
			beat(m).Input = ""
		}, "JSON input model"},
		{"machine operation with scope", func(m *Manifest) {
			beat(m).Scope = ScopeLookup
		}, ""},
		{"machine operation without endpoint", func(m *Manifest) {
			m.Endpoints = m.Endpoints[:len(m.Endpoints)-1]
		}, "exactly one endpoint"},
		{"machine operation wrong method", func(m *Manifest) {
			m.Endpoints[len(m.Endpoints)-1].Method = "PUT"
		}, "must use POST"},
		{"principal field on a plain operation", func(m *Manifest) {
			beat(m).Principal = "gateways.Gateway"
		}, "scope = \"enroll\""},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			m := machineFixture(t)
			if err := m.Validate(); err != nil {
				t.Fatal(err)
			}

			test.mutate(&m)

			err := m.Validate()
			if err == nil {
				t.Fatal("manifest was accepted")
			}

			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error %q does not mention %q", err, test.want)
			}
		})
	}
}

func TestMachineDeclarationsRequireDeclaredPrincipals(t *testing.T) {
	m := machineFixture(t)
	m.Principals = nil

	if err := m.Validate(); err == nil ||
		!strings.Contains(err.Error(), "[[principals]]") {
		t.Fatalf("machine operations without principals: %v", err)
	}

	published := machineFixture(t)
	published.Boundary.Mode = ""

	if err := published.Validate(); err == nil {
		t.Fatal("published profile accepted principals")
	}
}

// The machine lookup is derived: its view exposes the key, state and tenant
// columns only, and its policy compares the id as text.
func TestMachineLookupMigrationShape(t *testing.T) {
	m := machineFixture(t)
	table := m.Tables[len(m.Tables)-1]

	sql := lookupMigrationSQL(m, table)
	for _, want := range []string{
		"CREATE POLICY pluto_lookup_gateways_gatewaymachine ON gateways FOR SELECT",
		"USING (id::text = current_setting('app.lookup_gateways_gatewaymachine', true))",
		"SELECT id, org_id, public_key, state FROM gateways",
		"security_invoker = true",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration misses %q:\n%s", want, sql)
		}
	}

	if !strings.Contains(lookupDownSQL(m, table), "DROP VIEW IF EXISTS "+
		"pluto_lookup_gateways_gatewaymachine") {
		t.Fatal("down migration does not drop the machine view")
	}
}

// The machine surface renders deterministically, scopes machine files as
// managed and keeps machine permissions out of the user permission catalog.
func TestMachineManagedRender(t *testing.T) {
	m := machineFixture(t)

	first, err := candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	second, err := candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		machinePortsFile,
		machineStorageFile,
		machineTransportFile,
		machineMigrationFile,
		"internal/operations/gateways/enroll_gate_gen.go",
		"internal/operations/gateways/heartbeat_gate_gen.go",
		"internal/operations/gateways/heartbeat_workflow_gen.go",
		"internal/handlers/gateways/enroll.go",
		"internal/handlers/gateways/heartbeat.go",
	} {
		if len(first[path]) == 0 {
			t.Fatalf("missing managed file %s", path)
		}

		if string(first[path]) != string(second[path]) {
			t.Fatalf("%s is not deterministic", path)
		}
	}

	for path, source := range first {
		if strings.HasPrefix(path, "internal/access/") &&
			strings.Contains(string(source), "gateway.heartbeat") {
			t.Fatalf("%s lists a machine permission", path)
		}
	}

	storage := string(first[machineStorageFile])
	for _, want := range []string{
		"gateways.Gateway",
		"pluto_lookup_gateways_gatewaymachine",
		"machineports.NewVerified",
		"ed25519.Verify",
		"ON CONFLICT DO NOTHING",
	} {
		if !strings.Contains(storage, want) {
			t.Fatalf("storage runtime misses %q", want)
		}
	}

	migration := string(first[machineMigrationFile])
	for _, want := range []string{
		"FORCE ROW LEVEL SECURITY",
		"token_hash text PRIMARY KEY",
		"pluto_machine_enrollment_lookup",
	} {
		if !strings.Contains(migration, want) {
			t.Fatalf("machine migration misses %q", want)
		}
	}
}

// An application without principals renders no machine file and keeps the
// shared templates free of machine surface.
func TestMachineSurfaceAbsentWithoutPrincipals(t *testing.T) {
	m := generatableSeamFixture(t)

	files, err := candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	for path, source := range files {
		if strings.Contains(path, "machineauth") ||
			strings.Contains(path, "machine_identity") {
			t.Fatalf("unexpected machine file %s", path)
		}

		if strings.Contains(string(source), "scopeMachine") ||
			strings.Contains(string(source), "machineSignature") {
			t.Fatalf("%s carries machine surface", path)
		}
	}
}

// machinePageStream adds a signed paginated read and a signed stream.
func machinePageStream(t *testing.T) Manifest {
	t.Helper()

	m := machineFixture(t)
	m.Principals[0].Permissions = append(
		m.Principals[0].Permissions, "gateway.history", "gateway.channel",
	)
	m.Models = append(m.Models, Model{
		ID: "gateways.BeatRow", Fields: []string{"id:int64", "seq:int64"},
	})
	m.Ports[len(m.Ports)-1].Methods = append(
		m.Ports[len(m.Ports)-1].Methods,
		"History(scope.Execution, models.HistoryRequest) "+
			"(pagination.Page[models.BeatRow], error)",
	)
	m.Operations = append(
		m.Operations,
		Operation{
			Group:          "gateways",
			Action:         "History",
			Auth:           "machine:gateways.Gateway",
			Permission:     "gateway.history",
			Collection:     true,
			Idempotency:    "none",
			Body:           BodyAuthored,
			Resource:       "gateways.history",
			ResourcePolicy: "tenant",
			Ports:          []string{"gateways.Registry"},
			Response:       ResponsePage,
			Item:           "gateways.BeatRow",
		},
		Operation{
			Group:          "gateways",
			Action:         "Channel",
			Auth:           "machine:gateways.Gateway",
			Permission:     "gateway.channel",
			Idempotency:    "none",
			Body:           BodyAuthored,
			Resource:       "gateways.channel",
			ResourcePolicy: "tenant",
			Ports:          []string{"gateways.Registry"},
			Response:       ResponseStream,
			Item:           "gateways.Ack",
			Input:          "gateways.Beat",
		},
	)
	m.Endpoints = append(
		m.Endpoints,
		Endpoint{
			Group:       "gateways",
			Action:      "History",
			Operation:   "gateways.History",
			Method:      "GET",
			Path:        "/api/gateway/beats",
			Name:        "History",
			Description: "A gateway pages its heartbeats.",
		},
		Endpoint{
			Group:       "gateways",
			Action:      "Channel",
			Operation:   "gateways.Channel",
			Transport:   TransportWebSocket,
			Method:      "GET",
			Path:        "/api/gateway/channel",
			Name:        "Channel",
			Description: "A gateway opens its job channel.",
		},
	)

	return m
}

func TestMachinePageAndStreamAreGenerated(t *testing.T) {
	m := machinePageStream(t)
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}

	files, err := candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	for path, wants := range map[string][]string{
		"internal/operations/gateways/history_workflow_gen.go": {
			"verified.MachineID()", "pagination.Binding",
		},
		"internal/operations/gateways/channel_gate_gen.go": {
			"machineauth.Verifier", "Admit()", "Prepare(",
		},
		"internal/operations/gateways/channel_workflow_gen.go": {
			"scope.RunMachineStream", "Verifier.Recheck",
		},
		"internal/handlers/gateways/channel.go": {
			"MachineRequestFrom", "websocket.Accept", "rate_limited",
		},
		"internal/handlers/gateways/history.go": {
			"output.Page", "rate_limited",
		},
		"internal/scope/stream_gen.go": {"func RunMachineStream"},
	} {
		source, ok := files[path]
		if !ok {
			t.Fatalf("missing %s", path)
		}

		for _, want := range wants {
			if !strings.Contains(string(source), want) {
				t.Errorf("%s misses %q", path, want)
			}
		}
	}
}

func TestMachineProtectedColumnsReachSQLCheck(t *testing.T) {
	m := machineFixture(t)

	got := m.protectedColumns(m.Tables[len(m.Tables)-1])
	if strings.Join(got, ",") != "public_key,state" {
		t.Fatalf("protected columns %v", got)
	}

	if len(m.protectedColumns(m.Tables[0])) != 0 {
		t.Fatal("an unrelated table gained protected columns")
	}

	m.Principals[0].RevokedValue = "active"
	if err := m.Validate(); err == nil ||
		!strings.Contains(err.Error(), "must be distinct") {
		t.Fatalf("revoked value equal to active accepted: %v", err)
	}
}
