package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedOperationGateConsumer(t *testing.T) {
	root := temporary(t)
	mustInvoke(
		t,
		root,
		"init",
		".",
		"--name",
		"boundary-test",
		"--module",
		"example.test/boundary",
		"--no-mise",
		"--no-fnox",
		"--no-docker",
	)
	mustInvoke(
		t,
		root,
		"add",
		"endpoint",
		"widgets",
		"Create",
		"--method",
		"POST",
		"--path",
		"/api/widgets",
		"--permission",
		"widgets.write",
		"--name",
		"Create widget",
		"--description",
		"Create a widget.",
	)

	path := filepath.Join(root, "internal/operations/widgets/create.go")
	replaceSource(
		t,
		path,
		"type CreateDependencies struct{}",
		"type CreateDependencies struct{ Calls *int }",
	)
	replaceSource(
		t,
		path,
		"return widgets.CreateData{}, domain.ErrNotImplemented",
		"if operation.dependencies.Calls != nil { "+
			"*operation.dependencies.Calls++ }; return "+
			"widgets.CreateData{}, domain.ErrNotImplemented",
	)

	fixture, err := os.ReadFile("testdata/operation_boundary.go.tmpl")
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "internal/operations/widgets/boundary_test.go"),
		fixture,
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	external(
		t,
		root,
		"go",
		"test",
		"-race",
		"-count=1",
		"./internal/operations/widgets",
	)
	assertPublishedDraft(t, root)

	bypassPath := filepath.Join(
		root,
		"internal/handlers/widgets/private_bypass.go",
	)

	privateBypass := []byte(
		"package widgets\nimport ops \"example.test/boundary/internal/" +
			"operations/widgets\"\nfunc bypass(op *ops.Create) { _ = " +
			"op.execute }\n",
	)
	if err := os.WriteFile(bypassPath, privateBypass, 0o644); err != nil {
		t.Fatal(err)
	}

	compile := exec.Command("go", "test", "./internal/handlers/widgets")

	compile.Dir = root
	if output, err := compile.CombinedOutput(); err == nil ||
		!strings.Contains(string(output), "execute") {
		t.Fatalf(
			"private implementation was externally callable: %s %v",
			output,
			err,
		)
	}

	if err := os.Remove(bypassPath); err != nil {
		t.Fatal(err)
	}

	embeddingBypass := []byte(`package widgets

import (
	"context"
	"example.test/boundary/internal/domain"
	models "example.test/boundary/internal/models/widgets"
	ops "example.test/boundary/internal/operations/widgets"
)

type alternate struct {
	ops.CreateBoundary
}

func (alternate) Execute(
	context.Context,
	domain.Principal,
	models.CreateRequest,

) (
	models.CreateData,
	error,

) {
	return models.CreateData{}, nil
}

var _ CreateOperations = alternate{}
`)
	if err := os.WriteFile(bypassPath, embeddingBypass, 0o644); err != nil {
		t.Fatal(err)
	}

	external(t, root, "go", "test", "./internal/handlers/widgets")

	if report, err := invoke(
		t,
		root,
		"architecture",
		"check",
		"--directory",
		root,
		"--format",
		"json",
	); err == nil ||
		!strings.Contains(report, "ARCH204") {
		t.Fatalf(
			"compiled sealed-boundary embedding bypass accepted: %s %v",
			report,
			err,
		)
	}

	if err := os.Remove(bypassPath); err != nil {
		t.Fatal(err)
	}

	namedBypass := strings.Replace(
		string(embeddingBypass),
		"type alternate struct { ops.CreateBoundary }",
		"type inherited (ops.CreateBoundary)\ntype alternate struct { "+
			"inherited }",
		1,
	)
	if err := os.WriteFile(bypassPath, []byte(namedBypass), 0o644); err != nil {
		t.Fatal(err)
	}

	external(t, root, "go", "test", "./internal/handlers/widgets")

	if report, err := invoke(
		t,
		root,
		"architecture",
		"check",
		"--directory",
		root,
		"--format",
		"json",
	); err == nil ||
		!strings.Contains(report, "ARCH204") {
		t.Fatalf(
			"compiled named-interface embedding bypass accepted: %s %v",
			report,
			err,
		)
	}

	if err := os.Remove(bypassPath); err != nil {
		t.Fatal(err)
	}

	gate := filepath.Join(
		root,
		"internal/operations/widgets/create_gate_gen.go",
	)
	replaceSource(t, gate, "if !principal.Verified()", "if false")

	if _, err := invoke(
		t,
		root,
		"architecture",
		"check",
		"--directory",
		root,
		"--format",
		"json",
	); err == nil {
		t.Fatal("tampered managed gate was accepted")
	}

	report, err := invoke(
		t,
		root,
		"architecture",
		"check",
		"--directory",
		root,
		"--format",
		"json",
	)
	if err == nil || !strings.Contains(report, "ARCH202") {
		t.Fatalf(
			"missing precise managed integrity rejection: %s %v",
			report,
			err,
		)
	}
}
