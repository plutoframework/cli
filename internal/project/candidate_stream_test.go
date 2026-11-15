package project

import (
	"strings"
	"testing"
)

const streamDeclarations = `
[[models]]
id="audit.Cursor"
fields=["seq:int64"]
[[operations]]
group="audit"
action="Watch"
permission="audit:read"
idempotency="none"
body="authored"
resource="audit.events"
resource_policy="tenant"
ports=["audit.Events"]
response="stream"
item="audit.Entry"
input="audit.Cursor"
[[endpoints]]
group="audit"
action="Watch"
operation="audit.Watch"
transport="websocket"
origins=["app.example.test"]
method="GET"
path="/api/audit/watch"
name="Watch audit"
description="Replay audit events over a WebSocket."
`

func streamManifest(t *testing.T) Manifest {
	t.Helper()

	m, err := Decode([]byte(
		candidateReviewMetadata(t) + authoredDeclarations + streamDeclarations,
	))
	if err != nil {
		t.Fatal(err)
	}

	return m
}

func streamDiagnostics(
	t *testing.T,
	files map[string]string,
) []ArchitectureDiagnostic {
	t.Helper()

	root := candidateReviewFixture(t, files)
	diagnostics, _ := inspectCandidateBoundaries(
		root,
		candidateReviewModule,
		[]byte(
			candidateReviewMetadata(t)+authoredDeclarations+
				streamDeclarations,
		),
	)

	return diagnostics
}

func TestStreamManagedFiles(t *testing.T) {
	m := streamManifest(t)

	files, err := candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		path string
		want []string
		deny []string
	}{
		{
			scopeStreamFile,
			[]string{
				"SetReadLimit(bytes)", "DisallowUnknownFields()",
				"e.scopeGate.Use(", "context.WithTimeout(",
				"func RunStream[", "ErrStreamClosed",
				"state.scopeStreamTransport.ping(",
			},
			[]string{"websocket.Accept"},
		},
		{
			"internal/operations/audit/watch_workflow_gen.go",
			[]string{
				"scope.RunStream(", "scope.Bind(", "MaxMessageBytes: 65536",
				"scope.Stream[models.Cursor, models.Entry]",
				"logic.Watch(",
			},
			nil,
		},
		{
			"internal/operations/audit/watch_gate_gen.go",
			[]string{
				"Admit() (func(), bool)", "make(chan struct{}, 256)",
				"Gate.Within(",
			},
			nil,
		},
		{
			"internal/handlers/audit/watch.go",
			[]string{
				"websocket.Accept(", `"app.example.test"`,
				"operation.Prepare(", "scope.NewWire(", "503",
			},
			[]string{"InsecureSkipVerify"},
		},
		{
			"internal/logic/audit/contract_gen.go",
			[]string{"scope.Stream[models.Cursor, models.Entry]"},
			nil,
		},
	}

	for _, check := range checks {
		source, ok := files[check.path]
		if !ok {
			t.Fatalf("missing managed file %s", check.path)
		}

		for _, want := range check.want {
			if !strings.Contains(string(source), want) {
				t.Errorf("%s lacks %q", check.path, want)
			}
		}

		for _, deny := range check.deny {
			if strings.Contains(string(source), deny) {
				t.Errorf("%s contains %q", check.path, deny)
			}
		}
	}

	paths, err := CandidateManagedPaths(m)
	if err != nil || !paths[scopeStreamFile] {
		t.Fatalf("stream runtime is not managed: %v", err)
	}
}

func TestStreamManagedFilesAbsentWithoutStreams(t *testing.T) {
	files, err := candidateManaged(authoredManifest(t))
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := files[scopeStreamFile]; ok {
		t.Fatal("stream runtime rendered for an application without streams")
	}

	if strings.Contains(string(files[scopeFile]), "websocket") {
		t.Fatal("scope package imports the websocket library without streams")
	}
}

func TestStreamScaffoldReceivesTypedStream(t *testing.T) {
	m := streamManifest(t)

	files, err := candidateAuthoredScaffold(
		m,
		m.Operations[len(m.Operations)-1],
	)
	if err != nil {
		t.Fatal(err)
	}

	body := string(files["internal/logic/audit/watch.go"])
	if !strings.Contains(
		body, "stream scope.Stream[models.Cursor, models.Entry]",
	) {
		t.Fatalf("scaffold lacks the typed stream:\n%s", body)
	}
}

func TestStreamPushOnlyUsesNoMessage(t *testing.T) {
	m := streamManifest(t)
	m.Operations[len(m.Operations)-1].Input = ""

	files, err := candidateAuthoredManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	workflow := string(files["internal/operations/audit/watch_workflow_gen.go"])
	if !strings.Contains(
		workflow,
		"scope.Stream[scope.NoMessage, models.Entry]",
	) ||
		!strings.Contains(workflow, "false,") {
		t.Fatalf("push-only workflow:\n%s", workflow)
	}
}

func TestStreamCapabilitiesStayWithGeneratedCode(t *testing.T) {
	cases := map[string]struct {
		path, source, id string
	}{
		"websocket library": {
			"internal/logic/audit/x.go",
			"package audit\n\nimport _ \"github.com/coder/websocket\"\n",
			"ARCH610",
		},
		"net/http": {
			"internal/logic/audit/x.go",
			"package audit\n\nimport _ \"net/http\"\n", "ARCH610",
		},
		"run stream": {
			"internal/logic/audit/x.go",
			"package audit\n\nimport \"example.test/candidate/internal/" +
				"scope\"\n\nvar _ = scope.RunStream[int, int]\n",
			"ARCH603",
		},
		"new wire": {
			"internal/logic/audit/x.go",
			"package audit\n\nimport \"example.test/candidate/internal/" +
				"scope\"\n\nvar _ = scope.NewWire\n",
			"ARCH603",
		},
		"memory wire": {
			"internal/adapters/audit/x.go",
			"package audit\n\nimport \"example.test/candidate/internal/" +
				"scope\"\n\nvar _ = scope.NewMemoryWire\n",
			"ARCH603",
		},
		"wire storage": {
			"internal/logic/audit/x.go",
			"package audit\n\ntype t struct{ scopeWireTransport int }\n",
			"ARCH604",
		},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			diagnostics := streamDiagnostics(t, map[string]string{
				"internal/logic/audit/list.go":  goodBody,
				"internal/logic/audit/watch.go": goodBody,
				test.path:                       test.source,
			})

			if !candidateReviewHas(diagnostics, test.id, test.path) {
				t.Fatalf("%s not reported: %+v", test.id, diagnostics)
			}
		})
	}
}
