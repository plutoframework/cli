package migrate

import (
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

func policyWorkspace(root string) (*Workspace, *Plan) {
	plan := &Plan{
		Root:     root,
		Writes:   map[string][]byte{},
		Deletes:  map[string]bool{},
		Original: map[string][]byte{},
	}

	return &Workspace{
		Root: root,
		Manifest: project.Manifest{
			Project: project.Project{Module: "ex.com/a"},
		},
		plan: plan,
	}, plan
}

func TestScaffoldTestPolicy(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{

		// Subject removed, replacement generated: delete and note.
		"internal/gone/old_test.go": "package gone\n",
		"internal/gone/old.go":      "package gone\n",

		// Subject stays and imports resolve: keep.
		"internal/kept/kept_test.go": "package kept\n\nimport _ \"ex.com/a/internal/dep\"\n",
		"internal/kept/kept.go":      "package kept\n",
		"internal/dep/dep.go":        "package dep\n",

		// Import target removed, no replacement: manual.
		"internal/broken/broken_test.go": "package broken\n\nimport _ \"ex.com/a/internal/vanish\"\n",
		"internal/broken/broken.go":      "package broken\n",
		"internal/vanish/vanish.go":      "package vanish\n",

		// Edited: manual, as before.
		"internal/edited/edited_test.go": "package edited\n\n// mine\n",
		"internal/edited/edited.go":      "package edited\n",
	})

	previous := map[string][]byte{
		"internal/gone/old_test.go": []byte("package gone\n"),
		"internal/gone/old.go":      []byte("package gone\n"),
		"internal/kept/kept_test.go": []byte(
			"package kept\n\nimport _ \"ex.com/a/internal/dep\"\n",
		),
		"internal/kept/kept.go": []byte("package kept\n"),
		"internal/broken/broken_test.go": []byte(
			"package broken\n\nimport _ \"ex.com/a/internal/vanish\"\n",
		),
		"internal/broken/broken.go":      []byte("package broken\n"),
		"internal/vanish/vanish.go":      []byte("package vanish\n"),
		"internal/edited/edited_test.go": []byte("package edited\n"),
		"internal/edited/edited.go":      []byte("package edited\n"),
	}
	next := map[string][]byte{
		"internal/gone/replacement_test.go": []byte("package gone\n"),
		"internal/kept/kept.go":             []byte("package kept\n"),
		"internal/dep/dep.go":               []byte("package dep\n"),
		"internal/broken/broken.go":         []byte("package broken\n"),
		"internal/edited/edited.go":         []byte("package edited\n"),
	}

	workspace, plan := policyWorkspace(root)

	if err := reconcile(workspace, previous, next, "candidate"); err != nil {
		t.Fatal(err)
	}

	if !plan.Deletes["internal/gone/old_test.go"] {
		t.Fatalf("pristine test with replacement kept: %v", plan.Paths())
	}

	for name := range plan.Deletes {
		if strings.HasSuffix(name, "_test.go") &&
			name != "internal/gone/old_test.go" {
			t.Fatalf("test %s deleted without a replacement", name)
		}
	}

	notes := []string{}
	for _, item := range plan.Notes {
		notes = append(notes, item.String())
	}

	if joined := strings.Join(notes, "\n"); !strings.Contains(
		joined,
		"internal/gone/replacement_test.go",
	) {
		t.Fatalf("deletion did not name the replacement:\n%s", joined)
	}

	manual := []string{}
	for _, item := range plan.Manual {
		manual = append(manual, item.Path)
	}

	joined := strings.Join(manual, " ")
	for _, want := range []string{
		"internal/broken/broken_test.go",
		"internal/edited/edited_test.go",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing manual item %s in %v", want, manual)
		}
	}

	if strings.Contains(joined, "kept_test.go") {
		t.Fatalf("resolvable pristine test reported: %v", manual)
	}
}
