package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCandidateIntegrityPreservesCompilerDirectives(t *testing.T) {
	const source = "package app\nfunc Gate() {}\n"

	for _, directive := range []string{
		"//go:build never_candidate_review\n\n",
		"// +build never_candidate_review\n\n",
		"//go:linkname Gate alternate.Gate\n",
		"//go:embed secret.txt\n",
		"//go:generate unexpected-command\n",
		"//go:cgo_import_dynamic leaked leaked\n",
		"//line generated.go:40\n",
		"/*line generated.go:40*/\n",
		"/*\n#cgo LDFLAGS: -lhidden\n*/\n",
	} {
		t.Run(strings.TrimSpace(directive), func(t *testing.T) {
			actual := []byte(directive + source)
			if !Equivalent(actual, []byte(source)) {
				t.Fatal("fixture must isolate ignored compiler comments")
			}

			if candidateSourceEquivalent(actual, []byte(source)) ||
				candidateSourceEquivalent([]byte(source), actual) {
				t.Fatal("added or deleted compiler directive accepted")
			}
		})
	}

	if !candidateSourceEquivalent(
		[]byte("// ordinary documentation\npackage app\n\nfunc Gate( ) {\n}\n"),
		[]byte(source),
	) {
		t.Fatal("ordinary formatting or comment rejected")
	}

	if !candidateSourceEquivalent(
		[]byte("package app\n//go:noinline\nfunc Gate( ) {\n}\n"),
		[]byte("package app\n//go:noinline\nfunc Gate() {}\n"),
	) {
		t.Fatal("matching directive lost formatting flexibility")
	}

	for _, actual := range []string{
		"package app\nfunc Gate() {}\n//go:noinline\nfunc Other() {}\n",
		"package app\n//go:noinline\n\nfunc Gate() {}\nfunc Other() {}\n",
	} {
		expected := []byte(
			"package app\n//go:noinline\nfunc Gate() {}\nfunc Other() {}\n",
		)
		if candidateSourceEquivalent([]byte(actual), expected) {
			t.Fatal(
				"compiler directive moved to another declaration or attachment",
			)
		}
	}
}

func TestCandidateIntegrityRejectsExcludedGateAndSQLChanges(t *testing.T) {
	m := manifestContractFixture(t)
	m.SchemaVersion = SchemaVersion
	m.Project = Project{
		Name:      "integrity",
		Module:    "example.test/integrity",
		EnvPrefix: "INTEGRITY_",
	}
	m.Database = Database{Engine: "postgres", Queries: "sqlc"}
	m.Capabilities.Cache = "none"

	files, err := candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	for path, source := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, source, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if diagnostics := candidateIntegrity(root, m); len(diagnostics) != 0 {
		t.Fatalf("canonical positive failed: %v", diagnostics)
	}

	for _, test := range []struct{ path, prefix string }{
		{"internal/operations/sales/approve_gate_gen.go", "//go:build never_candidate_review\n\n"},
		{"db/migrations/001_initial.sql", "-- changed managed migration\n"},
	} {
		original := files[test.path]
		if len(original) == 0 {
			t.Fatalf("missing required generated fixture: %s", test.path)
		}

		if err := os.WriteFile(
			filepath.Join(root, test.path),
			append([]byte(test.prefix), original...),
			0o644,
		); err != nil {
			t.Fatal(err)
		}

		diagnostics := candidateIntegrity(root, m)
		if len(diagnostics) != 1 || diagnostics[0].ID != "ARCH202" ||
			diagnostics[0].File != test.path {
			t.Fatalf(
				"expected precise semantic integrity rejection: %v",
				diagnostics,
			)
		}

		if err := os.WriteFile(
			filepath.Join(root, test.path),
			original,
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}
}
