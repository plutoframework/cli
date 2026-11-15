package project

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePortMethod(t *testing.T) {
	good := "Query(scope.Execution, rules.Filter, pagination.Request) " +
		"(pagination.Page[models.Entry], error)"

	method, err := parsePortMethod(good)
	if err != nil {
		t.Fatal(err)
	}

	if method.Name != "Query" || len(method.Params) != 3 ||
		len(method.Results) != 2 ||
		method.Results[0] != "pagination.Page[models.Entry]" {
		t.Fatalf("unexpected parse: %+v", method)
	}

	for name, spec := range map[string]string{
		"bare name":      "Query",
		"lowercase":      "query(scope.Execution) error",
		"no scope":       "Query(string) error",
		"no error":       "Query(scope.Execution) string",
		"named params":   "Query(e scope.Execution) error",
		"pool":           "Query(scope.Execution, pgxpool.Pool) error",
		"raw tenant":     "Query(scope.Execution, tenant) error",
		"unqualified":    "Query(scope.Execution, Filter) error",
		"os":             "Query(scope.Execution, os.File) error",
		"func":           "Query(scope.Execution, func()) error",
		"two methods":    "Query(scope.Execution) error; Other() error",
		"empty":          "",
		"not a function": "Query",
	} {
		if _, err := parsePortMethod(spec); err == nil {
			t.Errorf("%s: %q accepted", name, spec)
		}
	}
}

func TestAdapterFilesAreDeterministicAndParse(t *testing.T) {
	m := seamFixture(t)

	first, err := candidateAdapterFiles(m)
	if err != nil {
		t.Fatal(err)
	}

	second, err := candidateAdapterFiles(m)
	if err != nil {
		t.Fatal(err)
	}

	for path, source := range first {
		if string(source) != string(second[path]) {
			t.Fatalf("%s is not deterministic", path)
		}

		if strings.HasSuffix(path, ".go") {
			if _, err := parser.ParseFile(
				token.NewFileSet(), path, source, parser.AllErrors,
			); err != nil {
				t.Fatalf("%s does not parse: %v\n%s", path, err, source)
			}
		}
	}

	for path, want := range map[string][]string{
		"internal/ports/audit/events_port_gen.go": {
			"type Events interface",
			"Query(context.Context, scope.Execution, " +
				"pagination.Request) (pagination.Page[models.Entry], error)",
			"Append(context.Context, scope.Execution, models.Event) error",
		},
		"internal/adapters/audit/contract_gen.go": {
			"var _ ports.Events = (*EventStore)(nil)",
			"var _ func(scope.DB) *EventStore = NewEventStore",
		},
		"internal/app/candidate_adapters_gen.go": {
			"scope.NewDB(pool)",
			"AuditEvents: auditadapters.NewEventStore(db)",
		},
		"sqlc.yaml": {
			"queries: db/queries/audit",
			"out: internal/storage/queries/audit",
			`column: "audit_events.org_id"`,
			`import: "example.com/gryt/internal/scope"`,
			`type: "Tenant"`,
		},
	} {
		got := string(first[path])
		for _, fragment := range want {
			if !strings.Contains(got, fragment) {
				t.Errorf("%s lacks %q:\n%s", path, fragment, got)
			}
		}
	}

	if strings.Contains(string(first["sqlc.yaml"]), "users.org_id") {
		t.Error("tenant override leaked into a group without a query set")
	}
}

func TestAdapterConflictingSignaturesRejected(t *testing.T) {
	m := seamFixture(t)
	m.Ports = append(m.Ports, Port{
		ID:      "audit.Other",
		Adapter: "audit.EventStore",
		Methods: []string{"Append(scope.Execution, string) error"},
	})

	if _, err := candidateAdapterFiles(m); err == nil ||
		!strings.Contains(err.Error(), "conflicting signatures") {
		t.Fatalf("want conflict, got %v", err)
	}
}

func TestAdapterStubsCompileAgainstContract(t *testing.T) {
	m := seamFixture(t)
	m.Ports[0].Methods = []string{
		"Append(scope.Execution, models.Event) error",
		"Scan(scope.Execution, int64, int64) ([]models.Entry, error)",
		"Count(scope.Tx) (int64, error)",
	}

	files, err := candidateAdapterFiles(m)
	if err != nil {
		t.Fatal(err)
	}

	stubs, err := AuthoredAdapterStubs(m)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	write := func(path, content string) {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("go.mod", "module example.com/gryt\n\ngo 1.25\n")
	write("internal/scope/scope.go", `package scope

type Execution struct{}
type Tx struct{}
type DB struct{}

func Queries[Q, D any](Tx, func(D) Q) Q { var q Q; return q }
`)
	write("internal/storage/queries/audit/queries.go", `package queries

type Queries struct{}

func New(int) *Queries { return nil }
`)
	write("internal/models/audit/models.go", `package audit

type Event struct{}
type Entry struct{}
`)

	for path, source := range stubs {
		write(path, string(source))
	}

	for path, source := range files {
		if strings.HasPrefix(path, "internal/ports/") ||
			strings.HasPrefix(path, "internal/adapters/") {
			write(path, string(source))
		}
	}

	command := exec.Command("go", "build", "./...")
	command.Dir = root

	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated adapter does not compile: %v\n%s", err, output)
	}

	if _, ok := stubs["internal/adapters/audit/event_store.go"]; !ok {
		t.Fatalf("unexpected stub paths: %v", stubs)
	}
}

func adapterRoleMetadata(t *testing.T) string {
	return candidateReviewMetadata(t) + `
[[ports]]
id="sales.Events"
adapter="sales.EventStore"
methods=["Append(scope.Execution, int64) error"]
[authored]
[[authored.dependencies]]
module="github.com/google/uuid"
reason="identifiers"
`
}

func adapterSource(imports []string, body string) string {
	var source strings.Builder

	source.WriteString("package sales\n\nimport (\n")

	for index, path := range imports {
		source.WriteString("\tx" + strings.Repeat("x", index) + " \"" +
			path + "\"\n")
	}

	source.WriteString(")\n\n" + body)

	return source.String()
}

func inspectAdapter(t *testing.T, source string) []ArchitectureDiagnostic {
	t.Helper()

	path := "internal/adapters/sales/events.go"
	root := candidateReviewFixture(t, map[string]string{path: source})
	diagnostics, _ := inspectCandidateBoundaries(
		root,
		candidateReviewModule,
		[]byte(adapterRoleMetadata(t)),
	)

	return diagnostics
}

func TestAdapterRoleAllowsDeclaredCapabilities(t *testing.T) {
	m := candidateReviewModule
	diagnostics := inspectAdapter(t, adapterSource([]string{
		"context",
		"errors",
		"github.com/jackc/pgx/v5/pgtype",
		"github.com/google/uuid",
		"github.com/plutoframework/pluto-core/pagination",
		m + "/internal/scope",
		m + "/internal/storage/queries/sales",
		m + "/internal/rules/sales",
		m + "/internal/models/sales",
		m + "/internal/ports/sales",
	}, "type EventStore struct{}\n"))

	for _, id := range []string{"ARCH602", "ARCH608", "ARCH610", "ARCH611"} {
		if candidateReviewHas(
			diagnostics, id, "internal/adapters/sales/events.go",
		) {
			t.Fatalf("allowed adapter rejected by %s: %+v", id, diagnostics)
		}
	}
}

func TestAdapterRoleRejectsEscapes(t *testing.T) {
	m := candidateReviewModule

	for _, path := range []string{
		"github.com/jackc/pgx/v5",
		"github.com/jackc/pgx/v5/pgxpool",
		"database/sql",
		"github.com/redis/go-redis/v9",
		"github.com/plutoframework/pluto-rbac/identity",
		"github.com/plutoframework/pluto-core/stores/postgres",
		"github.com/undeclared/module",
		"net/http",
		"os",
		"unsafe",
		"reflect",
		"C",
		m + "/internal/storage/queries/billing",
		m + "/internal/storage/postgres",
		m + "/internal/operations/sales",
		m + "/internal/rules/billing",
		m + "/internal/access",
	} {
		t.Run(path, func(t *testing.T) {
			diagnostics := inspectAdapter(t, adapterSource(
				[]string{"context", path},
				"type EventStore struct{}\n",
			))

			if !candidateReviewHas(
				diagnostics, "ARCH610", "internal/adapters/sales/events.go",
			) && !candidateReviewHas(
				diagnostics, "ARCH608", "internal/adapters/sales/events.go",
			) && !candidateReviewHas(
				diagnostics, "ARCH605", "internal/adapters/sales/events.go",
			) {
				t.Fatalf("%s accepted: %+v", path, diagnostics)
			}
		})
	}
}

func TestAdapterRoleShapeRules(t *testing.T) {
	for name, body := range map[string]string{
		"init":      "func init() {}\n",
		"variable":  "var shared int\n",
		"goroutine": "func run() { go func() {}() }\n",
	} {
		t.Run(name, func(t *testing.T) {
			diagnostics := inspectAdapter(t, adapterSource(nil, body))
			if !candidateReviewHas(
				diagnostics, "ARCH611", "internal/adapters/sales/events.go",
			) {
				t.Fatalf("%s accepted: %+v", name, diagnostics)
			}
		})
	}
}

func TestAdapterOutsideDeclaredGroupStaysUnowned(t *testing.T) {
	path := "internal/adapters/billing/events.go"
	root := candidateReviewFixture(t, map[string]string{
		path: "package billing\n",
	})
	diagnostics, _ := inspectCandidateBoundaries(
		root,
		candidateReviewModule,
		[]byte(adapterRoleMetadata(t)),
	)

	if !candidateReviewHas(diagnostics, "ARCH602", path) {
		t.Fatalf("undeclared adapter group accepted: %+v", diagnostics)
	}
}
