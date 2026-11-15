package project

import (
	"strings"
	"testing"
)

// writeEndpointFixture exposes an authored write over HTTP.
func writeEndpointFixture(t *testing.T, idempotency string) Manifest {
	t.Helper()

	m := generatableSeamFixture(t)
	m.Models = append(m.Models, Model{
		ID: "audit.Note", Fields: []string{"text:string"},
	})

	operation := Operation{
		Group:          "audit",
		Action:         "Annotate",
		Permission:     "audit.read",
		Write:          true,
		Idempotency:    idempotency,
		Body:           BodyAuthored,
		Resource:       "audit.events",
		ResourcePolicy: "tenant",
		Ports:          []string{"audit.Events"},
		Response:       ResponseObject,
		Input:          "audit.Note",
		Output:         "audit.Verification",
		Feature:        "annotations",
	}
	if idempotency == "exempt" {
		operation.ReplayReason = "Annotations are not replay-sensitive."
	}

	m.Operations = append(m.Operations, operation)
	m.Endpoints = append(m.Endpoints, Endpoint{
		Group:       "audit",
		Action:      "Annotate",
		Operation:   "audit.Annotate",
		Method:      "PUT",
		Path:        "/api/audit/annotate",
		Name:        "Annotate",
		Description: "Annotate the audit log.",
	})

	return m
}

func TestAuthoredWriteEndpointIdempotent(t *testing.T) {
	m := writeEndpointFixture(t, "required")
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}

	files, err := candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	handler := string(files["internal/handlers/audit/annotate.go"])
	for _, want := range []string{
		"transport.Decode(w, r, &input, bodyLimit)",
		"http.MaxBytesReader",
		"operation.Prepare(",
		"operation.AuthorizeIntent(",
		"reg.Idempotency.Serve(",
		"operation.ExecutePrepared(",
		"idempotency.Scope{",
	} {
		if !strings.Contains(handler, want) {
			t.Errorf("handler misses %q", want)
		}
	}

	// The claim is taken after the gate, never before it.
	if strings.Index(handler, "AuthorizeIntent(") >
		strings.Index(handler, "Idempotency.Serve(") {
		t.Error("the idempotency claim precedes authorization")
	}

	runtime := string(files["internal/app/candidate_operations_gen.go"])
	if !strings.Contains(runtime, "transport.NewIdempotency(") {
		t.Error("the runtime does not build the durable idempotency handler")
	}

	paths := string(files["internal/paths/audit_gen.go"])
	if !strings.Contains(paths, "audit.AnnotateRequest{}") ||
		!strings.Contains(paths, "Idempotent:    true") {
		t.Errorf("the contract lacks the request body or key: %s", paths)
	}
}

func TestAuthoredWriteEndpointExempt(t *testing.T) {
	m := writeEndpointFixture(t, "exempt")
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}

	files, err := candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	handler := string(files["internal/handlers/audit/annotate.go"])
	if !strings.Contains(handler, "transport.Decode(w, r, &input, bodyLimit)") {
		t.Error("an exempt write must still decode strictly")
	}

	if strings.Contains(handler, "Idempotency") ||
		!strings.Contains(handler, "operation.Execute(") {
		t.Errorf("an exempt write must not take a claim: %s", handler)
	}

	runtime := string(files["internal/app/candidate_operations_gen.go"])
	if strings.Contains(runtime, "NewIdempotency") {
		t.Error("the runtime built an idempotency handler nothing uses")
	}
}

func TestAuthoredWriteEndpointRejections(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(*Manifest)
		want string
	}{
		"no input model": {
			func(m *Manifest) {
				m.Operations[len(m.Operations)-1].Input = ""
			},
			"input model",
		},
		"page response": {
			func(m *Manifest) {
				operation := &m.Operations[len(m.Operations)-1]
				operation.Response = ResponsePage
				operation.Collection = true
			},
			"",
		},
		"read method for a write": {
			func(m *Manifest) {
				m.Endpoints[len(m.Endpoints)-1].Method = "GET"
			},
			"read-only HTTP method",
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := writeEndpointFixture(t, "required")
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

func TestInternalAuthoredWriteCannotRequireIdempotency(t *testing.T) {
	m := writeEndpointFixture(t, "required")
	m.Endpoints = m.Endpoints[:len(m.Endpoints)-1]

	if _, err := candidateManaged(m); err == nil ||
		!strings.Contains(err.Error(), "needs an HTTP endpoint") {
		t.Fatalf("an unexposed write with a claim was accepted: %v", err)
	}
}
