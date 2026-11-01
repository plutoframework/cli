package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func boundaryFixture(t *testing.T) (string, Manifest) {
	t.Helper()

	m := Manifest{
		SchemaVersion: SchemaVersion,
		Project: Project{
			Name:      "boundary",
			Module:    "example.test/boundary",
			EnvPrefix: "BOUNDARY_",
		},
		Capabilities: Capabilities{Tenancy: "single"},
		Database:     Database{Engine: "postgres", Queries: "sqlc"},
		Endpoints: []Endpoint{
			{
				Group:       "widgets",
				Action:      "Create",
				Name:        "Create widget",
				Description: "Create a widget.",
				Method:      "POST",
				Path:        "/api/widgets",
				Permission:  "widgets.write",
			},
		},
	}
	m.Endpoints[0].Operation = "widgets.Create"
	m.Operations = []Operation{
		{
			Group:       "widgets",
			Action:      "Create",
			Permission:  "widgets.write",
			Write:       true,
			Idempotency: "required",
		},
	}
	root := testRoot(t)

	files, err := InitialFiles(m)
	if err != nil {
		t.Fatal(err)
	}

	authored, err := EndpointFiles(m, m.Endpoints[0])
	if err != nil {
		t.Fatal(err)
	}

	for path, source := range authored {
		files[path] = source
	}

	operationFiles, err := OperationFiles(m, m.Operations[0])
	if err != nil {
		t.Fatal(err)
	}

	for path, source := range operationFiles {
		files[path] = source
	}

	if _, err := Apply(root, files, nil, false); err != nil {
		t.Fatal(err)
	}

	manifest, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "pluto.toml"),
		manifest,
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	return root, m
}

func TestManagedOperationBoundary(t *testing.T) {
	for _, tc := range []struct{ name, path, source, id string }{
		{"positive", "", "", ""},
		{"gate removed", "internal/operations/widgets/create_gate_gen.go", "", "ARCH201"},
		{"gate changed", "internal/operations/widgets/create_gate_gen.go", "package widgets\n", "ARCH202"},
		{"handler replaced", "internal/handlers/widgets/create.go", "package widgets\n", "ARCH202"},
		{"entry registration changed", "internal/app/routes_gen.go", "package app\n", "ARCH202"},
		{"group registration changed", "internal/handlers/widgets/register_gen.go", "package widgets\n", "ARCH202"},

		{"resolver changed", "internal/access/authorization_gen.go", "package access\n", "ARCH202"},
		{"private helper bypass", "internal/operations/widgets/helper.go", "package widgets\nfunc helper(op *Create) { _ = op.execute }\n", "ARCH203"},
		{"method expression bypass", "internal/operations/widgets/helper.go", "package widgets\nvar bypass = (*Create).execute\n", "ARCH203"},
		{"exported alternate entry", "internal/operations/widgets/helper.go", "package widgets\nfunc Unsafe() {}\n", "ARCH204"},
		{"sealed interface embedding", "internal/handlers/widgets/helper.go", "package widgets\nimport ops \"example.test/boundary/internal/operations/widgets\"\ntype bypass struct { ops.CreateBoundary }\n", "ARCH204"},
		{"interface embedding route", "internal/handlers/widgets/helper.go", "package widgets\nimport ops \"example.test/boundary/internal/operations/widgets\"\ntype bypass interface { ops.CreateBoundary }\n", "ARCH204"},
		{"dot import embedding route", "internal/handlers/widgets/helper.go", "package widgets\nimport . \"example.test/boundary/internal/operations/widgets\"\ntype bypass struct { CreateBoundary }\n", "ARCH204"},

		{"defined interface wrapper", "internal/handlers/widgets/helper.go", "package widgets\nimport ops \"example.test/boundary/internal/operations/widgets\"\ntype inherited ops.CreateBoundary\ntype bypass struct{ inherited }\n", "ARCH204"},
		{"parenthesized defined interface", "internal/handlers/widgets/helper.go", "package widgets\nimport ops \"example.test/boundary/internal/operations/widgets\"\ntype inherited (ops.CreateBoundary)\ntype bypass struct{ inherited }\n", "ARCH204"},

		{"alias embedding route", "internal/handlers/widgets/helper.go", "package widgets\nimport ops \"example.test/boundary/internal/operations/widgets\"\ntype bypass = ops.CreateBoundary\n", "ARCH204"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := boundaryFixture(t)
			if tc.path != "" {
				path := filepath.Join(root, tc.path)
				if tc.source == "" {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(path, []byte(tc.source), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			report := InspectArchitecture(root)
			if tc.id == "" {
				if report.Passed {
					t.Fatal(
						"published protected fixture incorrectly passed complete gate",
					)
				}

				for _, diagnostic := range report.Diagnostics {
					if diagnostic.ID != "ARCH401" {
						t.Fatalf(
							"fresh compatibility consumer rejected: %+v",
							diagnostic,
						)
					}
				}

				return
			}

			found := false

			for _, d := range report.Diagnostics {
				if d.ID == tc.id &&
					(d.File == tc.path || tc.name == "gate removed" && d.File == "pluto.toml" && strings.Contains(d.Message, tc.path)) {
					found = true
				}
			}

			if !found {
				t.Fatalf(
					"missing %s for %s: %+v",
					tc.id,
					tc.path,
					report.Diagnostics,
				)
			}
		})
	}
}

func TestLegacyOperationHasBoundaryDiagnostic(t *testing.T) {
	root := testRoot(t)
	if err := os.MkdirAll(
		filepath.Join(root, "internal/operations"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	source := "package operations\nfunc LegacyMutation() {}\n"
	if err := os.WriteFile(
		filepath.Join(root, "internal/operations/legacy.go"),
		[]byte(source),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "pluto.toml"),
		[]byte("schema_version=1\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	report := InspectArchitecture(root)
	found := false

	for _, d := range report.Diagnostics {
		if d.ID == "ARCH201" && d.File == "internal/operations/legacy.go" &&
			strings.Contains(
				d.Message,
				"no declared managed operation boundary",
			) {
			found = true
		}
	}

	if !found {
		t.Fatalf(
			"schema blocker hid missing operation boundary: %+v",
			report.Diagnostics,
		)
	}
}

func TestProtectedWriteBoundaryNeedsPermission(t *testing.T) {
	root, m := boundaryFixture(t)
	m.Operations[0].Permission = ""

	metadata, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "pluto.toml"),
		metadata,
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	found := false

	for _, d := range InspectArchitecture(root).Diagnostics {
		if d.ID == "ARCH201" &&
			strings.Contains(d.Message, "explicit permission") {
			found = true
		}
	}

	if !found {
		t.Fatal("unsupported protected write declaration was accepted")
	}
}

func TestWriteOperationCannotBindReadMethod(t *testing.T) {
	operation := Operation{
		Group:        "records",
		Action:       "Update",
		Permission:   "records.write",
		Write:        true,
		Idempotency:  "exempt",
		ReplayReason: "Explicit non-replayable workflow.",
	}

	m := Manifest{Operations: []Operation{operation}}
	for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
		if _, err := m.OperationEndpoint(
			Endpoint{
				Group:     operation.Group,
				Action:    operation.Action,
				Operation: operation.ID(),
				Method:    method,
			},
		); err == nil {
			t.Fatalf("write authority exposed through %s", method)
		}
	}

	if endpoint, err := m.OperationEndpoint(
		Endpoint{
			Group:     operation.Group,
			Action:    operation.Action,
			Operation: operation.ID(),
			Method:    "POST",
		},
	); err != nil ||
		endpoint.Idempotency != "exempt" {
		t.Fatal(endpoint, err)
	}
}
