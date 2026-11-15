package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func commitAll(t *testing.T, root string) {
	t.Helper()

	for _, args := range [][]string{
		{"add", "-A"},
		{
			"-c", "user.name=Pluto Test",
			"-c", "user.email=test@example.invalid",
			"-c", "core.hooksPath=/dev/null",
			"commit", "-q", "-m", "fixture",
		},
	} {
		command := exec.Command("git", args...)
		command.Dir = root

		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
}

func fakeCandidateSources(t *testing.T) []string {
	t.Helper()

	base := temporary(t)
	args := []string{}

	for _, name := range []string{"auth", "rbac", "core", "jobs"} {
		modules := map[string][]string{
			"auth": {"", "stores/postgres", "methods/password"},
			"rbac": {"", "identity"},
			"core": {"", "stores/postgres"},
			"jobs": {"", "stores/postgres"},
		}[name]
		root := filepath.Join(base, "pluto-"+name)

		for _, suffix := range modules {
			directory := filepath.Join(root, filepath.FromSlash(suffix))

			module := "github.com/plutoframework/pluto-" + name
			if suffix != "" {
				module += "/" + suffix
			}

			if err := os.MkdirAll(directory, 0o755); err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(
				filepath.Join(directory, "go.mod"),
				[]byte("module "+module+"\n\ngo 1.25.5\n"),
				0o644,
			); err != nil {
				t.Fatal(err)
			}
		}

		args = append(args, "--candidate-"+name+"-source", root)
	}

	return args
}

func TestMigrateDryRunPreconditionsAndNoWrites(t *testing.T) {
	root := temporary(t)
	mustInvoke(
		t,
		root,
		"--no-input",
		"init",
		".",
		"--name",
		"shop",
		"--module",
		"example.com/shop",
		"--multi-tenant",
		"--no-mise",
		"--no-fnox",
		"--no-docker",
	)
	commitAll(t, root)

	manifest, err := os.ReadFile(filepath.Join(root, "pluto.toml"))
	if err != nil {
		t.Fatal(err)
	}

	sources := fakeCandidateSources(t)
	args := append(
		[]string{"--no-input", "migrate", "--to", "candidate", "--dry-run"},
		sources...,
	)

	// A pristine scaffold test that calls the removed published app.New is
	// deleted and reported, so no manual item remains for it.
	output, err := invoke(t, root, args...)
	if err != nil {
		t.Fatalf("pristine scaffold test must not block:\n%v\n%s", err, output)
	}

	for _, want := range []string{
		"internal/app/app_test.go:29: pristine scaffold test removed",
		"Migrating schema 10/published -> schema 10/candidate",
		"step 010-published-to-candidate",
		"  A db/migrations/099_memberships.sql",
		"+mode = 'candidate'",
		"Dry run: nothing written.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("dry run lacks %q:\n%s", want, output)
		}
	}

	status := exec.Command("git", "status", "--porcelain")
	status.Dir = root

	if dirty, err := status.Output(); err != nil || len(dirty) != 0 {
		t.Fatalf("dry run wrote files: %v %s", err, dirty)
	}

	if err := os.WriteFile(
		filepath.Join(root, "scratch.txt"),
		[]byte("x"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	output, err = invoke(
		t,
		root,
		append([]string{"--no-input", "migrate", "--to", "candidate"},
			sources...)...,
	)
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("dirty tree accepted: %v\n%s", err, output)
	}

	after, err := os.ReadFile(filepath.Join(root, "pluto.toml"))
	if err != nil || string(after) != string(manifest) {
		t.Fatal("refused migration changed pluto.toml")
	}

	output = mustInvoke(t, root, "--no-input", "migrate", "--dry-run")
	if !strings.Contains(output, "Already at schema 10/published") {
		t.Fatalf("default target must be a no-op:\n%s", output)
	}

	_, err = invoke(
		t,
		root,
		"--no-input",
		"migrate",
		"--to",
		"candidate",
		"--dry-run",
	)
	if err == nil || !strings.Contains(err.Error(), "manual migration items") {
		t.Fatalf("missing sources must leave manual items: %v", err)
	}
}

func TestMigrateRequiresGitWorkTree(t *testing.T) {
	root := temporary(t)
	if err := os.WriteFile(
		filepath.Join(root, "pluto.toml"),
		[]byte("schema_version = 10\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))

	_, err := invoke(t, root, "--no-input", "migrate", "--to", "candidate")
	if err == nil || !strings.Contains(err.Error(), "Git work tree") {
		t.Fatalf("non-Git directory accepted: %v", err)
	}
}

// Applying needs real candidate checkouts; strict candidate CI enables it.
func TestMigrateAppliesPublishedToCandidate(t *testing.T) {
	if os.Getenv("PLUTO_CLI_CANDIDATE_PROFILE") != "1" {
		t.Skip("explicit local candidate profile disabled")
	}

	args := []string{"--no-input", "migrate", "--to", "candidate"}

	for _, name := range []string{"AUTH", "RBAC", "CORE", "JOBS"} {
		source := os.Getenv("PLUTO_CLI_CANDIDATE_" + name + "_SOURCE")
		if source == "" {
			t.Fatal("missing explicit candidate source", name)
		}

		args = append(
			args,
			"--candidate-"+strings.ToLower(name)+"-source",
			source,
		)
	}

	root := temporary(t)
	mustInvoke(
		t,
		root,
		"--no-input",
		"init",
		".",
		"--name",
		"shop",
		"--module",
		"example.com/shop",
		"--multi-tenant",
		"--no-mise",
		"--no-fnox",
		"--no-docker",
	)
	mustInvoke(t, root, "fmt")

	// The scaffold test calls the removed published app.New; the migration
	// reports it as a manual item, so apply it as the user would.
	if err := os.Remove(
		filepath.Join(root, "internal", "app", "app_test.go"),
	); err != nil {
		t.Fatal(err)
	}

	commitAll(t, root)

	output := mustInvoke(t, root, args...)
	for _, want := range []string{
		"go mod tidy: ok",
		"pluto architecture check: ok",
		"pluto vet: ok",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("migration lacks %q:\n%s", want, output)
		}
	}

	commitAll(t, root)

	output = mustInvoke(t, root, args...)
	if !strings.Contains(output, "Already at schema 10/candidate") {
		t.Fatalf("second migration not a no-op:\n%s", output)
	}
}
