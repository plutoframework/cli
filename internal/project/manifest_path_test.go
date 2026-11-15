package project

import (
	"strings"
	"testing"
)

// pathFixture exposes the authored write as PUT /api/audit/annotate/{id}.
func pathFixture(t *testing.T, idempotency string) Manifest {
	t.Helper()

	m := writeEndpointFixture(t, idempotency)

	operation := &m.Operations[len(m.Operations)-1]
	operation.Path = []OperationPath{{Name: "id", Type: "uuid"}}

	entry := &m.Endpoints[len(m.Endpoints)-1]
	entry.Path = "/api/audit/annotate/{id}"
	entry.ResourceSource = "path:id"

	return m
}

func TestPathParamWriteGeneration(t *testing.T) {
	for _, idempotency := range []string{"required", "exempt"} {
		m := pathFixture(t, idempotency)
		if err := m.Validate(); err != nil {
			t.Fatal(err)
		}

		files, err := candidateManaged(m)
		if err != nil {
			t.Fatal(err)
		}

		handler := string(files["internal/handlers/audit/annotate.go"])
		for _, want := range []string{
			"input.BindPath(",
			"chi.URLParam(r, name)",
			"transport.Decode(w, r, &input.Body, bodyLimit)",
			"models.PathError",
		} {
			if !strings.Contains(handler, want) {
				t.Errorf("%s handler misses %q", idempotency, want)
			}
		}

		// A malformed id is refused before the body is read.
		if strings.Index(handler, "BindPath(") >
			strings.Index(handler, "io.ReadAll(") {
			t.Error("the path is bound after the body is read")
		}

		request := string(files["internal/models/audit/annotate.go"])
		for _, want := range []string{
			"type AnnotateBody = Note",
			"Body AnnotateBody",
			"ID   string",
			"func (r AnnotateRequest) ResourceID() string",
			"pathUUID(\"id\", get(\"id\"))",
		} {
			if !strings.Contains(request, want) {
				t.Errorf("request misses %q:\n%s", want, request)
			}
		}

		gate := string(files["internal/operations/audit/annotate_gate_gen.go"])
		if !strings.Contains(
			gate,
			`"audit.events"+":"+`,
		) || !strings.Contains(gate, "IntentRequest.ResourceID()") {
			t.Errorf("the gate is not bound to the path resource:\n%s", gate)
		}

		runtime := string(files["internal/app/candidate_operations_gen.go"])
		if !strings.Contains(
			runtime,
			`candidateTenantResourceID("audit.events")`,
		) {
			t.Error("the runtime lacks the path resource policy")
		}

		paths := string(files["internal/paths/audit_gen.go"])
		if !strings.Contains(paths, "audit.AnnotateBody{}") ||
			!strings.Contains(paths, `{Name: "id", Type: "uuid", Max: 0}`) {
			t.Errorf("the contract lacks the typed parameter: %s", paths)
		}
	}
}

func TestPathParamWithoutBody(t *testing.T) {
	m := pathFixture(t, "exempt")
	operation := &m.Operations[len(m.Operations)-1]
	operation.Input = ""

	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}

	files, err := candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	handler := string(files["internal/handlers/audit/annotate.go"])
	if !strings.Contains(handler, "len(raw) != 0") ||
		strings.Contains(handler, "transport.Decode") {
		t.Errorf("a bodyless write must refuse a body: %s", handler)
	}

	if !strings.Contains(
		string(files["internal/paths/audit_gen.go"]),
		"Request:nil",
	) && strings.Contains(
		string(files["internal/paths/audit_gen.go"]),
		"AnnotateRequest{}",
	) {
		t.Error("a bodyless write must not advertise a request body")
	}
}

func TestPathParamKinds(t *testing.T) {
	m := pathFixture(t, "exempt")
	operation := &m.Operations[len(m.Operations)-1]
	operation.Path = []OperationPath{
		{Name: "id", Type: "string", Max: 40},
	}

	files, err := candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(
		string(files["internal/models/audit/annotate.go"]),
		`pathText("id", get("id"), 40)`,
	) {
		t.Error("string parameter is not bounded")
	}

	operation.Path = []OperationPath{{Name: "id", Type: "int64", Max: 9}}

	files, err = candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	request := string(files["internal/models/audit/annotate.go"])
	if !strings.Contains(request, "ID   int64") ||
		!strings.Contains(request, "pathKeyInt64(r.ID)") {
		t.Errorf("int64 parameter is not typed: %s", request)
	}
}

func TestPathParamRejections(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(*Manifest)
		want string
	}{
		"undeclared segment": {
			func(m *Manifest) {
				m.Operations[len(m.Operations)-1].Path = nil
			},
			"needs a [[operations.path]]",
		},
		"declared but not routed": {
			func(m *Manifest) {
				m.Endpoints[len(m.Endpoints)-1].Path = "/api/audit/annotate"
			},
			"is not a {segment}",
		},
		"no resource source": {
			func(m *Manifest) {
				m.Endpoints[len(m.Endpoints)-1].ResourceSource = ""
			},
			"resource_source must be path:<name>",
		},
		"resource source names another": {
			func(m *Manifest) {
				m.Endpoints[len(m.Endpoints)-1].ResourceSource = "path:other"
			},
			"resource_source must be path:<name>",
		},
		"unknown type": {
			func(m *Manifest) {
				m.Operations[len(m.Operations)-1].Path[0].Type = "bool"
			},
			"type must be uuid, string or int64",
		},
		"string without max": {
			func(m *Manifest) {
				m.Operations[len(m.Operations)-1].Path[0].Type = "string"
			},
			"requires max",
		},
		"uuid with max": {
			func(m *Manifest) {
				m.Operations[len(m.Operations)-1].Path[0].Max = 3
			},
			"cannot declare max",
		},
		"duplicate": {
			func(m *Manifest) {
				operation := &m.Operations[len(m.Operations)-1]
				operation.Path = append(operation.Path, operation.Path[0])
			},
			"duplicate path parameter",
		},
		"bad name": {
			func(m *Manifest) {
				m.Operations[len(m.Operations)-1].Path[0].Name = "Bad_Name"
			},
			"must start lowercase",
		},
		"collides with body": {
			func(m *Manifest) {
				m.Operations[len(m.Operations)-1].Path[0].Name = "body"
				m.Endpoints[len(m.Endpoints)-1].Path = "/api/audit/{body}"
				m.Endpoints[len(m.Endpoints)-1].ResourceSource = "path:body"
			},
			"collides on Go name",
		},
		"public": {
			func(m *Manifest) {
				m.Operations[len(m.Operations)-1].Public = true
			},
			"",
		},
		"no endpoint": {
			func(m *Manifest) {
				m.Endpoints = m.Endpoints[:len(m.Endpoints)-1]
			},
			"has no HTTP endpoint",
		},
		"not authored": {
			func(m *Manifest) {
				m.Operations[0].Path = []OperationPath{
					{Name: "id", Type: "uuid"},
				}
			},
			"path parameters require an authored operation",
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := pathFixture(t, "exempt")
			tc.edit(&m)

			_, err := candidateManaged(m)
			if err == nil {
				err = m.Validate()
			}

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestPathParamPublishedProfileRefused(t *testing.T) {
	m := pathFixture(t, "exempt")
	m.Boundary.Mode = ""

	if err := m.Validate(); err == nil {
		t.Fatal("published projects may not declare path parameters")
	}
}
