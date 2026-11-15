package project

import (
	"strings"
	"testing"
)

const authoredDeclarations = `
[[tables]]
group="audit"
name="audit_events"
tenant_column="org_id"
append_only=true
row_security=true
[[ports]]
id="audit.Events"
adapter="audit.EventStore"
methods=["Query"]
[[models]]
id="audit.Entry"
fields=["seq:int64"]
[[operations]]
group="audit"
action="List"
permission="audit:read"
collection=true
idempotency="none"
body="authored"
resource="audit.events"
resource_policy="tenant"
ports=["audit.Events"]
response="page"
item="audit.Entry"
`

func enableAuthoredSeam(t *testing.T) {
	t.Helper()

	authoredSeamReady = true

	t.Cleanup(func() {
		authoredSeamReady = false
	})
}

func authoredManifest(t *testing.T) Manifest {
	t.Helper()

	m, err := Decode([]byte(
		candidateReviewMetadata(t) + authoredDeclarations,
	))
	if err != nil {
		t.Fatal(err)
	}

	return m
}

func authoredDiagnostics(
	t *testing.T,
	files map[string]string,
) []ArchitectureDiagnostic {
	t.Helper()
	enableAuthoredSeam(t)

	root := candidateReviewFixture(t, files)
	diagnostics, _ := inspectCandidateBoundaries(
		root,
		candidateReviewModule,
		[]byte(candidateReviewMetadata(t)+authoredDeclarations),
	)

	return diagnostics
}

const goodBody = `package audit

import (
	"context"

	"example.test/candidate/internal/scope"
)

func List(ctx context.Context, e scope.Execution) error {
	return nil
}
`

func TestAuthoredManagedFiles(t *testing.T) {
	enableAuthoredSeam(t)

	files, err := candidateAuthoredManaged(authoredManifest(t))
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		scopeFile,
		scopetestFile,
		"internal/logic/audit/contract_gen.go",
		"internal/operations/audit/list_gate_gen.go",
		"internal/operations/audit/list_workflow_gen.go",
	} {
		if _, ok := files[path]; !ok {
			t.Fatalf("missing managed file %s", path)
		}
	}

	workflow := string(files["internal/operations/audit/list_workflow_gen.go"])
	if !strings.Contains(workflow, "logic.List(") ||
		!strings.Contains(workflow, "scope.Bind(") ||
		!strings.Contains(workflow, `"audit.events"`) {
		t.Fatalf(
			"workflow does not call the body under its gate:\n%s",
			workflow,
		)
	}

	contract := string(files["internal/logic/audit/contract_gen.go"])
	if !strings.Contains(contract, "type ListPorts struct") ||
		!strings.Contains(contract, "pagination.Page[models.Entry]") {
		t.Fatalf("contract shape:\n%s", contract)
	}

	if _, err := candidateManaged(authoredManifest(t)); err != nil {
		t.Fatal(err)
	}
}

func TestAuthoredSeamFailsClosedUntilReady(t *testing.T) {
	if _, err := candidateManaged(authoredManifest(t)); err == nil {
		t.Fatal("authored operations generated while seam not ready")
	}
}

func TestAuthoredScaffoldCreatedOnce(t *testing.T) {
	m := authoredManifest(t)

	files, err := candidateAuthoredScaffold(
		m,
		m.Operations[len(m.Operations)-1],
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 2 || files["internal/logic/audit/list.go"] == nil ||
		files["internal/rules/audit/doc.go"] == nil {
		t.Fatalf("scaffold set: %v", len(files))
	}

	if strings.Contains(string(files["internal/logic/audit/list.go"]),
		"DO NOT EDIT") {
		t.Fatal("authored scaffold marked managed")
	}
}

func TestAuthoredRolesAccepted(t *testing.T) {
	diagnostics := authoredDiagnostics(t, map[string]string{
		"internal/logic/audit/list.go": goodBody,
		"internal/rules/audit/chain.go": `package audit

import (
	"crypto/sha256"
	"encoding/json"
)

var _ = sha256.New
var _ = json.Marshal
`,
	})

	for _, id := range []string{"ARCH602", "ARCH610", "ARCH603", "ARCH604"} {
		if candidateReviewHas(diagnostics, id, "internal/logic/audit/list.go") {
			t.Fatalf("good body rejected by %s: %+v", id, diagnostics)
		}
	}

	if candidateReviewHas(
		diagnostics,
		"ARCH602",
		"internal/rules/audit/chain.go",
	) ||
		candidateReviewHas(
			diagnostics,
			"ARCH610",
			"internal/rules/audit/chain.go",
		) {
		t.Fatalf("rules role rejected: %+v", diagnostics)
	}
}

func TestAuthoredRoleViolations(t *testing.T) {
	cases := map[string]struct {
		path, source, id string
	}{
		"rules os": {
			"internal/rules/audit/x.go",
			"package audit\n\nimport _ \"os\"\n", "ARCH610",
		},
		"rules net": {
			"internal/rules/audit/x.go",
			"package audit\n\nimport _ \"net/http\"\n", "ARCH610",
		},
		"rules logic": {
			"internal/rules/audit/x.go",
			"package audit\n\nimport _ \"example.test/candidate/internal/" +
				"logic/audit\"\n", "ARCH610",
		},
		"rules reflect": {
			"internal/rules/audit/x.go",
			"package audit\n\nimport _ \"reflect\"\n", "ARCH610",
		},
		"logic pgx": {
			"internal/logic/audit/x.go",
			"package audit\n\nimport _ \"github.com/jackc/pgx/v5\"\n",
			"ARCH610",
		},
		"logic rbac": {
			"internal/logic/audit/x.go",
			"package audit\n\nimport _ \"github.com/plutoframework/pluto-rbac\"\n",
			"ARCH610",
		},
		"logic access": {
			"internal/logic/audit/x.go",
			"package audit\n\nimport _ \"example.test/candidate/internal/" +
				"access\"\n", "ARCH610",
		},
		"logic other group": {
			"internal/logic/audit/x.go",
			"package audit\n\nimport _ \"example.test/candidate/internal/" +
				"models/other\"\n", "ARCH610",
		},
		"logic undeclared third party": {
			"internal/logic/audit/x.go",
			"package audit\n\nimport _ \"example.org/lib\"\n", "ARCH610",
		},
		"logic scopetest": {
			"internal/logic/audit/x.go",
			"package audit\n\nimport _ \"example.test/candidate/internal/" +
				"scope/scopetest\"\n", "ARCH610",
		},
		"adapter pgxpool": {
			"internal/adapters/audit/x.go",
			"package audit\n\nimport _ \"github.com/jackc/pgx/v5/pgxpool\"\n",
			"ARCH610",
		},
		"adapter logic": {
			"internal/adapters/audit/x.go",
			"package audit\n\nimport _ \"example.test/candidate/internal/" +
				"logic/audit\"\n", "ARCH611",
		},
		"init": {
			"internal/logic/audit/x.go",
			"package audit\n\nfunc init() {}\n", "ARCH611",
		},
		"package var": {
			"internal/rules/audit/x.go",
			"package audit\n\nvar cache = map[string]int{}\n", "ARCH611",
		},
		"goroutine": {
			"internal/logic/audit/x.go",
			"package audit\n\nfunc f() { go func() {}() }\n", "ARCH611",
		},
		"forged tenant": {
			"internal/logic/audit/x.go",
			"package audit\n\nimport \"example.test/candidate/internal/" +
				"scope\"\n\nfunc f() any { return scope.Tenant{} }\n",
			"ARCH604",
		},
		"forged execution": {
			"internal/logic/audit/x.go",
			"package audit\n\nimport \"example.test/candidate/internal/" +
				"scope\"\n\nfunc f() any { return new(scope.Execution) }\n",
			"ARCH604",
		},
		"scope bind": {
			"internal/logic/audit/x.go",
			"package audit\n\nimport \"example.test/candidate/internal/" +
				"scope\"\n\nvar _ = 1\n\nfunc f() { _ = scope.Bind }\n",
			"ARCH603",
		},
		"scope newdb": {
			"internal/adapters/audit/x.go",
			"package audit\n\nimport \"example.test/candidate/internal/" +
				"scope\"\n\nfunc f() { _ = scope.NewDB(nil) }\n",
			"ARCH603",
		},
		"body called from elsewhere": {
			"internal/transport/x.go",
			"package transport\n\nimport \"example.test/candidate/internal/" +
				"logic/audit\"\n\nfunc f() { _ = audit.List }\n",
			"ARCH611",
		},
		"private field": {
			"internal/logic/audit/x.go",
			"package audit\n\ntype t struct{ scopeTenant string }\n",
			"ARCH604",
		},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			diagnostics := authoredDiagnostics(t, map[string]string{
				"internal/logic/audit/list.go": goodBody,
				test.path:                      test.source,
			})

			if !candidateReviewHas(diagnostics, test.id, test.path) {
				t.Fatalf("%s not reported: %+v", test.id, diagnostics)
			}
		})
	}
}

func TestAuthoredBodyMustBeDeclared(t *testing.T) {
	diagnostics := authoredDiagnostics(t, map[string]string{
		"internal/logic/audit/other.go": "package audit\n",
	})

	if !candidateReviewHas(
		diagnostics,
		"ARCH611",
		"internal/logic/audit/list.go",
	) {
		t.Fatalf("missing body not reported: %+v", diagnostics)
	}
}

func TestAuthoredDeclaredDependencyAllowed(t *testing.T) {
	enableAuthoredSeam(t)

	root := candidateReviewFixture(t, map[string]string{
		"internal/logic/audit/list.go": goodBody,
		"internal/rules/audit/x.go": "package audit\n\nimport _ " +
			"\"example.org/lib/sub\"\n",
	})
	metadata := candidateReviewMetadata(t) + authoredDeclarations +
		"[[authored.dependencies]]\nmodule=\"example.org/lib\"\n" +
		"reason=\"parsing\"\n"
	diagnostics, _ := inspectCandidateBoundaries(
		root,
		candidateReviewModule,
		[]byte(metadata),
	)

	if candidateReviewHas(diagnostics, "ARCH610", "") {
		t.Fatalf("declared dependency rejected: %+v", diagnostics)
	}
}

func TestAuthoredOutsideRoleStillARCH602(t *testing.T) {
	diagnostics := authoredDiagnostics(t, map[string]string{
		"internal/logic/audit/list.go":  goodBody,
		"internal/logic/other/stuff.go": "package other\n",
		"internal/rules/audit/sub/x.go": "package sub\n",
	})

	for _, path := range []string{
		"internal/logic/other/stuff.go",
		"internal/rules/audit/sub/x.go",
	} {
		if !candidateReviewHas(diagnostics, "ARCH602", path) {
			t.Fatalf("%s escaped ARCH602: %+v", path, diagnostics)
		}
	}
}
