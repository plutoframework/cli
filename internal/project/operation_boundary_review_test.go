package project

import (
	"context"
	"go/ast"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

func operationReviewWrite(t *testing.T, root, path, source string) {
	t.Helper()

	path = filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func operationReviewHas(all []ArchitectureDiagnostic, id, path string) bool {
	for _, item := range all {
		if item.ID == id && item.File == path && item.Line > 0 {
			return true
		}
	}

	return false
}

func TestOperationReviewImportedIgnoredPackage(t *testing.T) {
	for _, directory := range []string{"testdata", "dist"} {
		t.Run(directory, func(t *testing.T) {
			root, manifest := boundaryFixture(t)
			path := "internal/" + directory + "/adapter/bypass.go"
			operationReviewWrite(t, root, path, `package adapter
import ops "example.test/boundary/internal/operations/widgets"
type replacement struct { ops.CreateBoundary }
func (replacement) Execute() {}
`)
			operationReviewWrite(t, root, "internal/assembly/use.go",
				"package assembly\nimport _ \"example.test/boundary/"+
					"internal/"+directory+"/adapter\"\n")

			metadata, err := Encode(manifest)
			if err != nil {
				t.Fatal(err)
			}

			diagnostics := operationBoundaryDiagnostics(root, metadata)
			if !operationReviewHas(diagnostics, "ARCH204", path) {
				t.Fatalf(
					"imported production adapter escaped: %+v",
					diagnostics,
				)
			}
		})
	}
}

func TestOperationReviewRejectsPartiallyDecodedCandidate(t *testing.T) {
	metadata := []byte(`[boundary]
mode="candidate"
[[operations]]
group="widgets"
action="Create"
write="not a boolean"
`)

	var decoded Manifest
	if err := toml.Unmarshal(metadata, &decoded); err == nil ||
		decoded.Boundary.Mode != "candidate" {
		t.Fatal("fixture must populate candidate mode before decoding fails")
	}

	found := operationBoundaryDiagnostics(t.TempDir(), metadata)
	if !operationReviewHas(found, "ARCH201", "pluto.toml") {
		t.Fatalf("partial candidate metadata escaped: %+v", found)
	}

	for _, diagnostic := range found {
		if !strings.Contains(
			diagnostic.Message,
			"metadata cannot be assessed",
		) {
			t.Fatalf("partial metadata reached generation: %+v", found)
		}
	}
}

func TestOperationReviewLocalBoundaryAliasChain(t *testing.T) {
	root, manifest := boundaryFixture(t)
	operationReviewWrite(t, root, "internal/operations/widgets/first.go",
		"package widgets\ntype First = CreateBoundary\n")
	operationReviewWrite(t, root, "internal/operations/widgets/alias.go",
		"package widgets\ntype Alias = First\n")

	path := "internal/adapters/override.go"
	operationReviewWrite(t, root, path, `package adapters
import ops "example.test/boundary/internal/operations/widgets"
type replacement struct { ops.Alias }
func (replacement) Execute() {}
`)

	metadata, err := Encode(manifest)
	if err != nil {
		t.Fatal(err)
	}

	all := operationBoundaryDiagnostics(root, metadata)
	for _, expected := range []string{
		"internal/operations/widgets/first.go",
		"internal/operations/widgets/alias.go", path,
	} {
		if !operationReviewHas(all, "ARCH204", expected) {
			t.Fatalf("local alias chain escaped at %s: %+v", expected, all)
		}
	}
}

func TestOperationReviewAllowedLocalTypesAndIgnoredFixtures(t *testing.T) {
	root, manifest := boundaryFixture(t)
	operationReviewWrite(
		t,
		root,
		"internal/adapters/allowed.go",
		`package adapters
import ops "example.test/boundary/internal/operations/widgets"
type holder struct { Operation ops.CreateBoundary }
type Ordinary int
type Local = Ordinary
type Recursive struct { Next *Recursive }
type ordinaryEmbedding struct { Local }
`,
	)
	operationReviewWrite(t, root, "internal/testdata/negative/invalid.go",
		"package negative\nthis is intentionally not Go\n")
	operationReviewWrite(t, root, "internal/dist/negative/invalid.go",
		"package negative\nthis is intentionally not Go\n")

	metadata, err := Encode(manifest)
	if err != nil {
		t.Fatal(err)
	}

	if found := operationBoundaryDiagnostics(root, metadata); len(found) != 0 {
		t.Fatalf(
			"named injection, unrelated types or ignored fixtures "+
				"rejected: %+v",
			found,
		)
	}
}

func TestOperationReviewDerivedBoundaryShapes(t *testing.T) {
	for _, test := range []struct {
		name, declaration, embedding string
	}{
		{
			"defined interface",
			"type First CreateBoundary\ntype Alias = First",
			"ops.Alias",
		},

		{
			"embedded interface",
			"type Alias interface { CreateBoundary }",
			"ops.Alias",
		},

		{
			"embedded structure",
			"type Alias struct { CreateBoundary }",
			"ops.Alias",
		},

		{
			"generic alias",
			"type Alias[T any] = CreateBoundary",
			"ops.Alias[int]",
		},

		{"pointer alias", "type Alias = *Create", "ops.Alias"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, manifest := boundaryFixture(t)
			operationReviewWrite(
				t,
				root,
				"internal/operations/widgets/alias.go",

				"package widgets\n"+test.declaration+"\n",
			)

			path := "internal/adapters/override.go"
			operationReviewWrite(
				t,
				root,
				path,
				"package adapters\nimport ops \"example.test/boundary/"+
					"internal/operations/widgets\"\ntype replacement struct { "+

					test.embedding+
					" }\n",
			)

			metadata, err := Encode(manifest)
			if err != nil {
				t.Fatal(err)
			}

			found := operationBoundaryDiagnostics(root, metadata)
			if !operationReviewHas(found, "ARCH204", path) {
				t.Fatalf("derived boundary embedding escaped: %+v", found)
			}
		})
	}
}

func TestOperationReviewPackageAliasOverrideCompiles(t *testing.T) {
	_, manifest := boundaryFixture(t)
	root := t.TempDir()
	operationReviewWrite(t, root, "go.mod",
		"module example.test/boundary\n\ngo 1.25.0\n")
	operationReviewWrite(
		t,
		root,
		"internal/operations/widgets/boundary.go",
		`package widgets
type CreateBoundary interface {
 sealedCreate()
 Execute() string
}
type First = CreateBoundary
type Alias = First
`,
	)
	operationReviewWrite(
		t,
		root,
		"internal/adapters/bypass.go",
		`package adapters
import ops "example.test/boundary/internal/operations/widgets"
type replacement struct { ops.Alias }
func (replacement) Execute() string { return "bypassed" }
var _ ops.CreateBoundary = replacement{}
`,
	)
	operationReviewWrite(
		t,
		root,
		"internal/adapters/bypass_test.go",
		`package adapters
import "testing"
func TestReplacement(t *testing.T) {
 if (replacement{}).Execute() != "bypassed" { t.Fatal("override not invoked") }
}
`,
	)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, "go", "test", "./...")
	command.Dir = root

	command.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf(
			"valid sealed-interface replacement did not compile: %v\n%s",
			err,
			output,
		)
	}

	metadata, err := Encode(manifest)
	if err != nil {
		t.Fatal(err)
	}

	found := operationBoundaryDiagnostics(root, metadata)
	if !operationReviewHas(found, "ARCH204", "internal/adapters/bypass.go") {
		t.Fatalf("compiled replacement escaped inspection: %+v", found)
	}
}

func TestOperationReviewMalformedSourceFailsClosed(t *testing.T) {
	root, manifest := boundaryFixture(t)
	path := "internal/adapters/bad.go"
	operationReviewWrite(t, root, path, "package adapters\nfunc broken(\n")

	metadata, err := Encode(manifest)
	if err != nil {
		t.Fatal(err)
	}

	if found := operationBoundaryDiagnostics(
		root,
		metadata,
	); !operationReviewHas(
		found,
		"ARCH201",
		path,
	) {
		t.Fatalf("unparsed production source escaped: %+v", found)
	}
}

func TestOperationReviewAliasCycleTerminates(t *testing.T) {
	const namespace = "example.test/aliases"

	aliases := map[string][]operationBoundaryAlias{
		namespace +
			".First": {
			{
				namespace: namespace,
				expression: &ast.Ident{
					Name: "Second",
				},
			},
		},

		namespace +
			".Second": {
			{
				namespace: namespace,
				expression: &ast.Ident{
					Name: "First",
				},
			},
		},
	}
	if isOperationBoundaryType(&ast.Ident{Name: "First"}, namespace, nil,
		map[string]map[string]bool{}, aliases, map[string]bool{}) {
		t.Fatal("unrelated alias cycle became a protected boundary")
	}
}

func TestOperationReviewImportUsesDeclaredPackageName(t *testing.T) {
	root, manifest := boundaryFixture(t)
	operationReviewWrite(t, root, "internal/operations/widgets/alias.go",
		"package widgets\ntype Alias = CreateBoundary\n")
	operationReviewWrite(
		t,
		root,
		"internal/bridge/alias.go",
		`package forwarding
import ops "example.test/boundary/internal/operations/widgets"
type Alias = ops.Alias
`,
	)

	path := "internal/adapters/override.go"
	operationReviewWrite(t, root, path, `package adapters
import "example.test/boundary/internal/bridge"
type replacement struct { forwarding.Alias }
`)

	metadata, err := Encode(manifest)
	if err != nil {
		t.Fatal(err)
	}

	if found := operationBoundaryDiagnostics(
		root,
		metadata,
	); !operationReviewHas(
		found,
		"ARCH204",
		path,
	) {
		t.Fatalf("package-name-qualified alias escaped: %+v", found)
	}
}
