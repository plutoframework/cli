package project

import (
	"strings"
	"testing"
)

func streamValidManifest(t *testing.T) Manifest {
	t.Helper()

	m := seamFixture(t)
	m.Models = append(m.Models, Model{
		ID: "audit.Cursor", Fields: []string{"seq:int64"},
	})
	m.Operations = append(m.Operations, Operation{
		Group:          "audit",
		Action:         "Watch",
		Permission:     "audit.read",
		Idempotency:    "none",
		Body:           BodyAuthored,
		Resource:       "audit.events",
		ResourcePolicy: "tenant",
		Ports:          []string{"audit.Events"},
		Response:       ResponseStream,
		Item:           "audit.Entry",
		Input:          "audit.Cursor",
	})
	m.Endpoints = append(m.Endpoints, Endpoint{
		Group:       "audit",
		Action:      "Watch",
		Operation:   "audit.Watch",
		Transport:   TransportWebSocket,
		Method:      "GET",
		Path:        "/api/audit/watch",
		Name:        "Watch audit",
		Description: "Replay audit events.",
	})

	return m
}

func streamOp(m *Manifest) *Operation {
	for index := range m.Operations {
		if m.Operations[index].ID() == "audit.Watch" {
			return &m.Operations[index]
		}
	}

	panic("no stream operation")
}

func streamEndpoint(m *Manifest) *Endpoint {
	for index := range m.Endpoints {
		if m.Endpoints[index].Operation == "audit.Watch" {
			return &m.Endpoints[index]
		}
	}

	panic("no stream endpoint")
}

func TestStreamManifestValidates(t *testing.T) {
	m := streamValidManifest(t)
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}

	limits := streamOp(&m).StreamSettings()
	if limits.MaxMessageBytes != 65536 || limits.IdleSeconds != 60 ||
		limits.MaxConnections != 256 {
		t.Fatalf("defaults: %+v", limits)
	}

	// A writing stream is allowed with an explicit replay reason, and still
	// opens with GET.
	op := streamOp(&m)
	op.Write = true
	op.Idempotency = "exempt"
	op.ReplayReason = "Results are idempotent by job id."

	if err := m.Validate(); err != nil {
		t.Fatalf("writing stream: %v", err)
	}
}

func TestStreamManifestRejectsBadDeclarations(t *testing.T) {
	cases := map[string]struct {
		edit func(*Manifest)
		want string
	}{
		"websocket on a page operation": {
			func(m *Manifest) {
				op := streamOp(m)
				op.Response = ResponsePage
				op.Collection = true
				op.Input = ""
			},
			"requires an operation with response",
		},
		"stream without endpoint": {
			func(m *Manifest) {
				m.Endpoints = m.Endpoints[:len(m.Endpoints)-1]
			},
			"exactly one websocket endpoint",
		},
		"stream over plain http": {
			func(m *Manifest) {
				streamEndpoint(m).Transport = ""
			},
			"must be exposed with transport",
		},
		"two websocket endpoints": {
			func(m *Manifest) {
				second := *streamEndpoint(m)
				second.Path = "/api/audit/watch2"
				m.Endpoints = append(m.Endpoints, second)
			},
			"exactly one websocket endpoint",
		},
		"unknown transport": {
			func(m *Manifest) {
				streamEndpoint(m).Transport = "sse"
			},
			"transport must be",
		},
		"post websocket": {
			func(m *Manifest) {
				streamEndpoint(m).Method = "POST"
			},
			"requires method GET",
		},
		"origins on plain endpoint": {
			func(m *Manifest) {
				m.Endpoints[0].Origins = []string{"app.example.test"}
			},
			"origins require transport",
		},
		"wildcard origin": {
			func(m *Manifest) {
				streamEndpoint(m).Origins = []string{"*"}
			},
			"origins entries",
		},
		"origin with scheme": {
			func(m *Manifest) {
				streamEndpoint(m).Origins = []string{"https://app.example.test"}
			},
			"origins entries",
		},
		"duplicate origin": {
			func(m *Manifest) {
				streamEndpoint(m).Origins = []string{"a.test", "a.test"}
			},
			"origins entries",
		},
		"public stream": {
			func(m *Manifest) {
				op := streamOp(m)
				op.Public = true
				op.Permission = ""
			},
			"protected",
		},
		"collection stream": {
			func(m *Manifest) {
				streamOp(m).Collection = true
			},
			"non-collection",
		},
		"generated body": {
			func(m *Manifest) {
				streamOp(m).Body = BodyGenerated
			},
			"require body",
		},
		"writing stream without reason": {
			func(m *Manifest) {
				op := streamOp(m)
				op.Write = true
				op.Idempotency = "exempt"
			},
			"replay_reason",
		},
		"missing item": {
			func(m *Manifest) {
				streamOp(m).Item = ""
			},
			"requires item",
		},
		"output model": {
			func(m *Manifest) {
				streamOp(m).Output = "audit.Verification"
			},
			"requires item only",
		},
		"limits without stream": {
			func(m *Manifest) {
				m.Operations[0].Stream = &StreamLimits{MaxConnections: 1}
			},
			"requires response",
		},
		"oversized messages": {
			func(m *Manifest) {
				streamOp(m).Stream = &StreamLimits{MaxMessageBytes: 1 << 30}
			},
			"max_message_bytes",
		},
		"idle not above ping": {
			func(m *Manifest) {
				streamOp(m).Stream = &StreamLimits{
					PingSeconds: 30, IdleSeconds: 30,
				}
			},
			"idle_seconds must exceed",
		},
		"negative connections": {
			func(m *Manifest) {
				streamOp(m).Stream = &StreamLimits{MaxConnections: -1}
			},
			"max_connections",
		},
		"websocket library dependency": {
			func(m *Manifest) {
				m.Authored.Dependencies = append(
					m.Authored.Dependencies,
					AuthoredDependency{
						Module: "github.com/gorilla/websocket",
						Reason: "sockets",
					},
				)
			},
			"websocket library",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := streamValidManifest(t)
			tc.edit(&m)

			err := m.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestStreamRequiresCandidateProfile(t *testing.T) {
	m := streamValidManifest(t)
	m.Boundary = BoundaryProfile{}

	if err := m.Validate(); err == nil {
		t.Fatal("published profile accepted a stream")
	}
}

func TestEnsureStreamModulePinsOnce(t *testing.T) {
	m := streamValidManifest(t)

	mod, sum := EnsureStreamModule(m, []byte("module x\n"), []byte("a b c\n"))
	if !strings.Contains(string(mod), websocketModule+" "+websocketVersion) ||
		!strings.Contains(string(sum), "websocket v1.8.14 h1:") {
		t.Fatalf("not pinned:\n%s\n%s", mod, sum)
	}

	again, againSum := EnsureStreamModule(m, mod, sum)
	if string(again) != string(mod) || string(againSum) != string(sum) {
		t.Fatal("pinning is not idempotent")
	}

	// An existing requirement, whatever its version, is the application's.
	custom := []byte("module x\n\nrequire github.com/coder/websocket v1.8.15\n")
	kept, _ := EnsureStreamModule(m, custom, nil)

	if string(kept) != string(custom) {
		t.Fatal("existing requirement was rewritten")
	}

	plain := seamFixture(t)
	untouched, _ := EnsureStreamModule(plain, []byte("module x\n"), nil)

	if string(untouched) != "module x\n" {
		t.Fatal("go.mod changed without streams")
	}
}
