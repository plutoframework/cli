package migrate

import (
	"strings"
	"testing"
)

func TestSameSourceIgnoresFormattingButNotMeaningOrComments(t *testing.T) {
	base := "package a\n\n// Run runs.\nfunc Run(a,\n\tb string) {}\n"

	for _, formatted := range []string{
		"package a\n// Run runs.\nfunc Run(\n\ta,\n\tb string,\n) {\n}\n",
		"package a\n\n// Run runs.   \nfunc Run(a, b string) {}\n",
	} {
		if !sameSource("a.go", []byte(base), []byte(formatted)) {
			t.Fatalf("formatter-only change counted as authored:\n%s", formatted)
		}
	}

	for _, authored := range []string{
		"package a\n\n// Run executes.\nfunc Run(a, b string) {}\n",
		"package a\n\n// Run runs.\nfunc Run(a, c string) {}\n",
		"package a\n\n// Run runs.\nfunc Run(a, b string) {}\n// extra\n",
		"package a\n\n// Run runs.\nfunc Run(a, b string) { \"\n",
	} {
		if sameSource("a.go", []byte(base), []byte(authored)) {
			t.Fatalf("authored change counted as pristine:\n%s", authored)
		}
	}

	if sameSource("a.yaml", []byte("a: 1\n"), []byte("a:  1\n")) {
		t.Fatal("non-Go files must compare exactly")
	}
}

func TestReconcileReplacesOnlyPristineScaffold(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"pluto.toml":                  "schema_version = 1\n",
		"internal/a/pristine.go":      "package a\n\nfunc Old() {}\n",
		"internal/a/edited.go":        "package a\n\nfunc Mine() {}\n",
		"internal/a/old_test.go":      "package a\n",
		"internal/a/edited_test.go":   "package a\n\n// mine\n",
		"internal/a/unexpected.go":    "package a\n",
		"db/migrations/001_init.sql":  "CREATE TABLE a ();\n",
		"README.md":                   "# Mine\n",
		"docs/OLD.md":                 "edited\n",
		"internal/a/user_deleted.txt": "kept by user\n",
	})

	previous := map[string][]byte{
		"pluto.toml":                 []byte("never touched\n"),
		"internal/a/pristine.go":     []byte("package a\n\nfunc Old() {\n}\n"),
		"internal/a/edited.go":       []byte("package a\n\nfunc Old() {}\n"),
		"internal/a/old_test.go":     []byte("package a\n"),
		"internal/a/edited_test.go":  []byte("package a\n"),
		"internal/a/missing.go":      []byte("package a\n"),
		"db/migrations/001_init.sql": []byte("CREATE TABLE a ();\n"),
		"README.md":                  []byte("# Scaffold\n"),
		"docs/OLD.md":                []byte("scaffold\n"),
	}
	next := map[string][]byte{
		"pluto.toml":                 []byte("never touched\n"),
		"internal/a/pristine.go":     []byte("package a\n\nfunc New() {}\n"),
		"internal/a/edited.go":       []byte("package a\n\nfunc New() {}\n"),
		"internal/a/unexpected.go":   []byte("package a\n\nfunc New2() {}\n"),
		"internal/a/created.go":      []byte("package a\n"),
		"db/migrations/001_init.sql": []byte("CREATE TABLE b ();\n"),
		"db/migrations/002_more.sql": []byte("CREATE TABLE c ();\n"),
		"README.md":                  []byte("# Candidate\n"),
	}

	plan := &Plan{
		Root:     root,
		Writes:   map[string][]byte{},
		Deletes:  map[string]bool{},
		Original: map[string][]byte{},
	}
	workspace := &Workspace{Root: root, plan: plan}

	if err := reconcile(workspace, previous, next, "candidate"); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"internal/a/pristine.go",
		"internal/a/created.go",
		"db/migrations/002_more.sql",
	} {
		if string(plan.Writes[name]) != string(next[name]) {
			t.Fatalf("%s not written", name)
		}
	}

	if !plan.Deletes["internal/a/old_test.go"] || len(plan.Deletes) != 1 {
		t.Fatalf("deletes %v", plan.Deletes)
	}

	if len(plan.Writes) != 3 {
		t.Fatalf("unexpected writes %v", plan.Paths())
	}

	manual := []string{}
	for _, item := range plan.Manual {
		manual = append(manual, item.String())
	}

	joined := strings.Join(manual, "\n")
	for _, want := range []string{
		"db/migrations/001_init.sql:1: existing SQL migration",
		"internal/a/edited.go:3: modified since scaffold",
		"internal/a/edited_test.go:3: modified published scaffold",
		"internal/a/unexpected.go:2: modified since scaffold",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}

	if len(plan.Manual) != 4 || len(plan.Notes) != 2 {
		t.Fatalf("manual %d notes %d:\n%s", len(plan.Manual), len(plan.Notes),
			joined)
	}
}
