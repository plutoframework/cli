package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func seamModelsFixture(t *testing.T) Manifest {
	t.Helper()

	m := seamFixture(t)

	for index := range m.Operations {
		operation := &m.Operations[index]
		if operation.ID() != "audit.List" {
			continue
		}

		operation.Query = []OperationQuery{
			{Name: "actor_id", Type: "string", Max: 8},
			{Name: "count", Type: "int64", Max: 50},
			{Name: "ok", Type: "bool"},
			{Name: "from", Type: "time"},
			{Name: "to", Type: "time", After: "from"},
		}
	}

	m.Endpoints = append(m.Endpoints, Endpoint{
		Group:       "audit",
		Action:      "Verify",
		Operation:   "audit.Verify",
		Method:      "GET",
		Path:        "/api/audit/verify",
		Name:        "Verify audit",
		Description: "Verify the audit chain.",
	})

	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}

	return m
}

func TestSeamModelsRenderStructsAndEnvelopes(t *testing.T) {
	files, err := candidateSeamModels(seamModelsFixture(t))
	if err != nil {
		t.Fatal(err)
	}

	for path, needles := range map[string][]string{
		"internal/models/audit/models_gen.go": {
			"type Entry struct",
			"Seq int64 `json:\"seq\"`",
			"Payload json.RawMessage `json:\"payload\" openapi:\"json\"`",
			"Hash string `json:\"hash\" openapi:\"hex\"`",
			"CreatedAt time.Time",
			"type Verification struct",
		},
		"internal/models/audit/list.go": {
			"type ListRequest struct",
			"ActorID string",
			"Count *int64",
			"type ListData = pagination.Page[Entry]",
			"type ListResponse struct",
			"func ParseListQuery(",
		},
		"internal/models/audit/verify.go": {
			"type VerifyData = Verification",
			"Data Verification `json:\"data\"`",
		},
		"internal/models/audit/query_gen.go": {"type QueryError struct"},
	} {
		source, ok := files[path]
		if !ok {
			t.Fatalf("missing %s in %v", path, keysOf(files))
		}

		for _, needle := range needles {
			if !strings.Contains(string(source), needle) {
				t.Errorf("%s lacks %q", path, needle)
			}
		}
	}
}

func keysOf(files map[string][]byte) []string {
	keys := []string{}
	for key := range files {
		keys = append(keys, key)
	}

	return keys
}

func TestSeamModelsRejectBadDeclarations(t *testing.T) {
	for name, edit := range map[string]func(*Manifest){
		"reserved limit": func(m *Manifest) {
			for index := range m.Operations {
				if m.Operations[index].ID() == "audit.List" {
					m.Operations[index].Query = []OperationQuery{
						{Name: "limit", Type: "int64", Max: 10},
					}
				}
			}
		},
		"stream": func(m *Manifest) {
			for index := range m.Operations {
				if m.Operations[index].ID() == "audit.Verify" {
					m.Operations[index].Response = ResponseStream
					m.Operations[index].Item = "audit.Entry"
					m.Operations[index].Output = ""
				}
			}
		},
		"other group model": func(m *Manifest) {
			m.Models = append(m.Models, Model{
				ID: "other.Thing", Fields: []string{"a:string"},
			})
			for index := range m.Operations {
				if m.Operations[index].ID() == "audit.Verify" {
					m.Operations[index].Output = "other.Thing"
				}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := seamModelsFixture(t)
			edit(&m)

			if _, err := candidateSeamModels(m); err == nil {
				t.Fatal("expected generation error")
			}
		})
	}
}

const seamPaginationStub = `package pagination

const MaxCursor = 2048

type Key struct{ Sort, ID string }
type Request struct {
	Limit int
	After Key
}
type Metadata struct {
	HasMore    bool   ` + "`json:\"has_more\"`" + `
	NextCursor string ` + "`json:\"next_cursor\"`" + `
}
type Page[T any] struct {
	Data []T      ` + "`json:\"data\"`" + `
	Page Metadata ` + "`json:\"page\"`" + `
}
`

const seamParseTest = `package audit

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

func code(t *testing.T, raw string) string {
	t.Helper()

	query, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ParseListQuery(query)
	if err == nil {
		return ""
	}

	var rejected *QueryError
	if !errors.As(err, &rejected) {
		t.Fatalf("untyped error %v", err)
	}

	return rejected.Code
}

func TestParseListQuery(t *testing.T) {
	for raw, want := range map[string]string{
		"":                               "",
		"actor_id=a&count=5&ok=true":     "",
		"limit=10&cursor=abc":            "",
		"from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z": "",
		"evil=1":                         "unknown_query_parameter",
		"actor_id=a&actor_id=b":          "repeated_query_parameter",
		"actor_id=":                      "empty_query_parameter",
		"actor_id=" + strings.Repeat("x", 9): "query_parameter_too_long",
		"actor_id=%00":                   "invalid_query_parameter",
		"count=51":                       "invalid_query_parameter",
		"count=-1":                       "invalid_query_parameter",
		"count=x":                        "invalid_query_parameter",
		"ok=yes":                         "invalid_query_parameter",
		"from=yesterday":                 "invalid_query_parameter",
		"from=2026-01-02T00:00:00Z&to=2026-01-01T00:00:00Z": "invalid_time_range",
		"from=2026-01-01T00:00:00Z&to=2026-01-01T00:00:00Z": "invalid_time_range",
		"limit=0":                        "invalid_limit",
		"limit=101":                      "invalid_limit",
		"limit=+5":                       "invalid_limit",
		"limit=":                         "empty_query_parameter",
		"cursor=" + strings.Repeat("c", 2049): "invalid_cursor",
	} {
		if got := code(t, raw); got != want {
			t.Errorf("%q: got %q want %q", raw, got, want)
		}
	}
}

func TestListRequestValuesCanonical(t *testing.T) {
	query, _ := url.ParseQuery("actor_id=a&count=5&from=2026-01-01T02:00:00%2B02:00")
	request, err := ParseListQuery(query)
	if err != nil {
		t.Fatal(err)
	}

	values := request.Values()
	if values.Get("from") != "2026-01-01T00:00:00Z" ||
		values.Get("count") != "5" {
		t.Fatal(values)
	}

	if !request.From.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal(request.From)
	}
}
`

// TestSeamModelsCompileAndParseStrictly compiles the generated models package
// against a stub pagination module and runs the generated parser.
func TestSeamModelsCompileAndParseStrictly(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain unavailable")
	}

	files, err := candidateSeamModels(seamModelsFixture(t))
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

	write("go.mod", "module example.test/app\n\ngo 1.24\n\n"+
		"require github.com/plutoframework/pluto-core v0.0.0\n"+
		"replace github.com/plutoframework/pluto-core => ./stub\n")
	write("stub/go.mod", "module github.com/plutoframework/pluto-core\n\n"+
		"go 1.24\n")
	write("stub/pagination/pagination.go", seamPaginationStub)

	for path, source := range files {
		formatted, err := formatGenerated(source, "example.test/app")
		if err != nil {
			t.Fatalf("%s: %v\n%s", path, err, source)
		}

		write(path, string(formatted))
	}

	write("internal/models/audit/parse_test.go", seamParseTest)

	command := exec.Command("go", "test", "./internal/models/audit/")
	command.Dir = root

	command.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")

	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
}

// Authentication must precede query parsing so unauthenticated callers never
// see query-schema diagnostics.
func TestSeamHandlerAuthenticatesBeforeParsingQuery(t *testing.T) {
	m := seamModelsFixture(t)

	for _, operation := range m.Operations {
		if !operation.isAuthored() || operation.Action == "Append" {
			continue
		}

		source, err := seamHandler(m, operation)
		if err != nil {
			t.Fatal(err)
		}

		text := string(source)
		caller := strings.Index(text, "access.CallerFromHTTP")
		parse := strings.Index(text, "models.Parse"+operation.Action+"Query")

		if caller < 0 || parse < 0 || caller > parse {
			t.Fatalf("%s must resolve the caller before parsing", operation.ID())
		}
	}
}
