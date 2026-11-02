package project

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedRegenerationPreservesAuthoredHandler(t *testing.T) {
	root, manifest := boundaryFixture(t)
	name := "internal/handlers/widgets/create.go"
	path := filepath.Join(root, filepath.FromSlash(name))

	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Authored behavior remains valid Go alongside the emitted adapter.
	authored := append(bytes.Clone(original), []byte(`
func AuthoredBusinessRule() string {
	return "preserve this authored behavior"
}
`)...)
	if err := os.WriteFile(path, authored, 0o600); err != nil {
		t.Fatal(err)
	}

	previous, err := Managed(manifest)
	if err != nil {
		t.Fatal(err)
	}

	files, err := Managed(manifest)
	if err != nil {
		t.Fatal(err)
	}

	files["internal/handlers/widgets/new_file.go"] = []byte(
		"package widgets\n",
	)
	if _, err := Apply(root, files, previous, false); err == nil ||
		!strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("authored handler was not protected by preflight: %v", err)
	}

	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, authored) {
		t.Fatalf("regeneration changed authored handler: %v", err)
	}

	if _, err := os.Stat(filepath.Join(
		root,
		"internal/handlers/widgets/new_file.go",
	)); !os.IsNotExist(err) {
		t.Fatalf("failed preflight wrote a partial regeneration: %v", err)
	}
}
