package project

import (
	"reflect"
	"strings"
	"testing"
)

func seamFixture(t *testing.T) Manifest {
	t.Helper()

	m := manifestContractFixture(t)
	m.SchemaVersion = SchemaVersion
	m.Project = Project{
		Name:      "gryt",
		Module:    "example.com/gryt",
		EnvPrefix: "GRYT_",
	}
	m.Capabilities.LocalCredentials = true
	m.Database = Database{Engine: "postgres", Queries: "sqlc"}

	m.Tables = []Table{
		{
			Group:        "audit",
			Name:         "audit_events",
			TenantColumn: "org_id",
			AppendOnly:   true,
			RowSecurity:  true,
		},
		{
			Group:        "access",
			Name:         "users",
			TenantColumn: "org_id",
			RowSecurity:  true,
		},
	}
	m.Queries = []QuerySet{{
		Group:  "audit",
		Files:  []string{"db/queries/audit/*.sql"},
		Tables: []string{"audit_events"},
	}}
	m.Ports = []Port{{
		ID:      "audit.Events",
		Adapter: "audit.EventStore",
		Methods: []string{
			"Append(scope.Execution, models.Event) error",
			"Query(scope.Execution, pagination.Request) " +
				"(pagination.Page[models.Entry], error)",
			"Scan(scope.Execution, int64, int64) ([]models.Entry, error)",
		},
	}}
	m.Models = []Model{
		{
			ID: "audit.Entry",
			Fields: []string{
				"seq:int64", "type:string", "payload:json",
				"hash:hex", "created_at:time", "ok:bool",
			},
		},
		{ID: "audit.Verification", Fields: []string{"valid:bool"}},
	}
	m.Authored = &Authored{Dependencies: []AuthoredDependency{{
		Module: "github.com/google/uuid",
		Reason: "event identifiers",
	}}}
	m.Memberships = &Memberships{
		Table:         "access.users",
		TenantColumn:  "org_id",
		SubjectColumn: "id",
		RoleColumn:    "role",
	}
	m.Roles[0].Permissions = append(
		m.Roles[0].Permissions,
		"audit.read",
	)
	m.Operations = append(
		m.Operations,
		Operation{
			Group:          "audit",
			Action:         "List",
			Permission:     "audit.read",
			Collection:     true,
			Idempotency:    "none",
			Body:           BodyAuthored,
			Resource:       "audit.events",
			ResourcePolicy: "tenant",
			Ports:          []string{"audit.Events"},
			Response:       ResponsePage,
			Item:           "audit.Entry",
			Query: []OperationQuery{
				{Name: "actor_id", Type: "string", Max: 256},
				{Name: "from", Type: "time"},
				{Name: "to", Type: "time", After: "from"},
				{Name: "limit", Type: "int64", Max: 100},
			},
		},
		Operation{
			Group:          "audit",
			Action:         "Verify",
			Permission:     "audit.read",
			Idempotency:    "none",
			Body:           BodyAuthored,
			Resource:       "audit.events",
			ResourcePolicy: "tenant",
			Ports:          []string{"audit.Events"},
			Response:       ResponseObject,
			Output:         "audit.Verification",
		},
	)
	m.Endpoints = append(
		m.Endpoints,
		Endpoint{
			Group:       "audit",
			Action:      "List",
			Operation:   "audit.List",
			Method:      "GET",
			Path:        "/api/audit",
			Name:        "List audit",
			Description: "List audit events.",
		},
	)

	return m
}

func TestSeamManifestValidatesAndRoundTrips(t *testing.T) {
	m := seamFixture(t)
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}

	data, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(data)
	if err != nil {
		t.Fatalf("%v\n%s", err, data)
	}

	again, err := Encode(decoded)
	if err != nil {
		t.Fatal(err)
	}

	stable, err := Decode(again)
	if err != nil || !reflect.DeepEqual(stable, decoded) {
		t.Fatalf("round trip unstable: %v", err)
	}

	if !reflect.DeepEqual(decoded.Tables, m.Tables) ||
		!reflect.DeepEqual(decoded.Memberships, m.Memberships) ||
		!reflect.DeepEqual(decoded.Models, m.Models) {
		t.Fatalf("declarations lost in round trip")
	}
}

func TestSeamManifestRejections(t *testing.T) {
	list := func(m *Manifest) *Operation {
		return &m.Operations[len(m.Operations)-2]
	}

	for name, tc := range map[string]struct {
		edit func(*Manifest)
		want string
	}{
		"published profile": {
			func(m *Manifest) {
				m.Boundary = BoundaryProfile{}
				m.Capabilities.Tenancy = "single"
				m.Resources, m.OperationContracts = nil, nil
				m.Effects, m.Roles, m.Jobs = nil, nil, nil
				m.Operations, m.Endpoints = nil, nil
				m.Agents = nil
			},
			"require the candidate profile",
		},
		"duplicate table": {
			func(m *Manifest) {
				m.Tables = append(m.Tables, m.Tables[0])
			},
			"duplicate table",
		},
		"bad table group": {
			func(m *Manifest) {
				m.Tables[0].Group = "Audit"
			},
			"lowercase group",
		},
		"missing tenant column": {
			func(m *Manifest) {
				m.Tables[0].TenantColumn = ""
			},
			"tenant_column",
		},
		"row security off": {
			func(m *Manifest) {
				m.Tables[0].RowSecurity = false
			},
			"row_security = true",
		},
		"query undeclared table": {
			func(m *Manifest) {
				m.Queries[0].Tables = []string{"nope"}
			},
			"undeclared table",
		},
		"query glob escape": {
			func(m *Manifest) {
				m.Queries[0].Files = []string{"db/queries/audit/../x.sql"}
			},
			"clean .sql paths",
		},
		"query other group dir": {
			func(m *Manifest) {
				m.Queries[0].Files = []string{"db/queries/other/*.sql"}
			},
			"under db/queries/audit/",
		},
		"duplicate query set": {
			func(m *Manifest) {
				m.Queries = append(m.Queries, m.Queries[0])
			},
			"duplicate query set",
		},
		"duplicate port": {
			func(m *Manifest) {
				m.Ports = append(m.Ports, m.Ports[0])
			},
			"duplicate port",
		},
		"port adapter group": {
			func(m *Manifest) {
				m.Ports[0].Adapter = "other.Store"
			},
			"adapter must be audit.",
		},
		"port method lowercase": {
			func(m *Manifest) {
				m.Ports[0].Methods = []string{"query"}
			},
			"invalid or duplicate method",
		},
		"port id syntax": {
			func(m *Manifest) {
				m.Ports[0].ID = "events"
			},
			"port id",
		},
		"duplicate model": {
			func(m *Manifest) {
				m.Models = append(m.Models, m.Models[0])
			},
			"duplicate model",
		},
		"model bad type": {
			func(m *Manifest) {
				m.Models[0].Fields[0] = "seq:float"
			},
			"must be name:type",
		},
		"model duplicate field": {
			func(m *Manifest) {
				m.Models[0].Fields[1] = "seq:string"
			},
			"repeats field",
		},
		"model no fields": {
			func(m *Manifest) {
				m.Models[0].Fields = nil
			},
			"requires fields",
		},
		"dependency stdlib": {
			func(m *Manifest) {
				m.Authored.Dependencies[0].Module = "fmt"
			},
			"third-party",
		},
		"dependency framework": {
			func(m *Manifest) {
				m.Authored.Dependencies[0].Module = "github.com/plutoframework/pluto-core"
			},
			"third-party",
		},
		"dependency no reason": {
			func(m *Manifest) {
				m.Authored.Dependencies[0].Reason = " "
			},
			"requires a reason",
		},
		"membership table undeclared": {
			func(m *Manifest) {
				m.Memberships.Table = "access.nope"
			},
			"declared table",
		},
		"membership tenant mismatch": {
			func(m *Manifest) {
				m.Memberships.TenantColumn = "tenant"
			},
			"must equal table",
		},
		"membership columns collide": {
			func(m *Manifest) {
				m.Memberships.RoleColumn = "id"
			},
			"distinct",
		},
		"bad body": {
			func(m *Manifest) {
				list(m).Body = "custom"
			},
			"authored or generated",
		},
		"authored without ports or resource": {
			func(m *Manifest) {
				list(m).Ports, list(m).Resource = nil, ""
				list(m).ResourcePolicy = ""
			},
			"requires ports or a resource",
		},
		"undeclared port": {
			func(m *Manifest) {
				list(m).Ports = []string{"audit.Nope"}
			},
			"undeclared port",
		},
		"ports on generated body": {
			func(m *Manifest) {
				list(m).Body = BodyGenerated
			},
			"require body",
		},
		"resource wrong group": {
			func(m *Manifest) {
				list(m).Resource = "other.events"
			},
			"operation group",
		},
		"resource without policy": {
			func(m *Manifest) {
				list(m).ResourcePolicy = ""
			},
			"resource_policy",
		},
		"undeclared item model": {
			func(m *Manifest) {
				list(m).Item = "audit.Nope"
			},
			"undeclared model",
		},
		"page without item": {
			func(m *Manifest) {
				list(m).Item = ""
			},
			"requires item",
		},
		"object without output": {
			func(m *Manifest) {
				m.Operations[len(m.Operations)-1].Output = ""
			},
			"requires output",
		},
		"bad response": {
			func(m *Manifest) {
				list(m).Response = "sse"
			},
			"page, object or stream",
		},
		"query bad type": {
			func(m *Manifest) {
				list(m).Query[0].Type = "float"
			},
			"type must be",
		},
		"query string without max": {
			func(m *Manifest) {
				list(m).Query[0].Max = 0
			},
			"requires max",
		},
		"query string oversized": {
			func(m *Manifest) {
				list(m).Query[0].Max = 1 << 20
			},
			"requires max",
		},
		"query time with max": {
			func(m *Manifest) {
				list(m).Query[1].Max = 5
			},
			"cannot declare max",
		},
		"query after unknown": {
			func(m *Manifest) {
				list(m).Query[2].After = "nope"
			},
			"after must name",
		},
		"query after self": {
			func(m *Manifest) {
				list(m).Query[2].After = "to"
			},
			"after must name",
		},
		"query after non-time": {
			func(m *Manifest) {
				list(m).Query[2].After = "actor_id"
			},
			"after must name",
		},
		"query duplicate": {
			func(m *Manifest) {
				list(m).Query = append(list(m).Query, list(m).Query[0])
			},
			"duplicate query parameter",
		},
		"query name syntax": {
			func(m *Manifest) {
				list(m).Query[0].Name = "ActorID"
			},
			"snake_case",
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := seamFixture(t)
			tc.edit(&m)

			err := m.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestSeamQueryRequiresProtectedRead(t *testing.T) {
	m := seamFixture(t)
	op := &m.Operations[len(m.Operations)-1]
	op.Write = true
	op.Idempotency = "required"
	op.Response = ""
	op.Output = ""
	op.Query = []OperationQuery{{Name: "x", Type: "bool"}}

	if err := m.Validate(); err == nil ||
		!strings.Contains(err.Error(), "protected read") {
		t.Fatalf("got %v", err)
	}
}
