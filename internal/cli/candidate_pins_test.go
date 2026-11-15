package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

// Revisions are resolved and verified before any file is written; mutable or
// misplaced revisions fail closed.
func TestCandidateRevisionFlagsFailBeforeWriting(t *testing.T) {
	commit := strings.Repeat("a", 40)

	for name, args := range map[string][]string{
		"branch revision": {
			"--boundary-profile", "candidate", "--multi-tenant",
			"--candidate-auth-revision", "main",
		},
		"published profile": {"--candidate-core-revision", commit},
		"source and revision": {
			"--boundary-profile", "candidate", "--multi-tenant",
			"--candidate-jobs-source", "/nonexistent/pluto-jobs",
			"--candidate-jobs-revision", commit,
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := temporary(t)

			output, err := invoke(t, root, append(
				[]string{"--no-input", "init", ".", "--name", "pins"},
				args...,
			)...)
			if err == nil {
				t.Fatalf("invalid candidate revision accepted:\n%s", output)
			}

			entries, readErr := os.ReadDir(root)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("files written before verification: %v", entries)
			}
		})
	}

	root := temporary(t)
	mustInvoke(
		t,
		root,
		"--no-input", "init", ".", "--name", "pins", "--multi-tenant",
		"--no-mise", "--no-fnox", "--no-docker",
	)

	output, err := invoke(
		t,
		root,
		"--no-input", "migrate", "--to", "published", "--dry-run",
		"--candidate-rbac-revision", commit,
	)
	if err == nil || !strings.Contains(err.Error(), "--to candidate") {
		t.Fatalf("revision accepted outside candidate adoption: %v\n%s", err,
			output)
	}
}

// candidateRevisionArgs returns the explicit pushed commits of the four
// candidate libraries, e.g. their origin/main.
func candidateRevisionArgs(t *testing.T) []string {
	t.Helper()

	if os.Getenv("PLUTO_CLI_CANDIDATE_PINS") != "1" {
		t.Skip("pinned candidate profile (network) disabled")
	}

	args := []string{}

	for _, name := range []string{"AUTH", "RBAC", "CORE", "JOBS"} {
		revision := os.Getenv("PLUTO_CLI_CANDIDATE_" + name + "_REVISION")
		if revision == "" {
			t.Fatal("missing explicit candidate revision", name)
		}

		args = append(
			args,
			"--candidate-"+strings.ToLower(name)+"-revision",
			revision,
		)
	}

	return args
}

// assertPinnedModule requires plain pinned requirements with no replacement.
func assertPinnedModule(t *testing.T, root string) {
	t.Helper()

	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(module), "replace") ||
		strings.Contains(string(module), "v0.0.0-candidate") {
		t.Fatalf("pinned candidate go.mod is machine-local:\n%s", module)
	}

	_, manifest, err := project.Discover(root)
	if err != nil || len(manifest.Boundary.Pins) != 9 ||
		manifest.Boundary.AuthSource != "" {
		t.Fatalf("pins not persisted: %v %+v", err, manifest.Boundary)
	}
}

// A pinned candidate app needs no sibling checkout: it builds, passes the
// architecture check and pluto vet through the module proxy alone.
func TestCandidatePinnedConsumer(t *testing.T) {
	revisions := candidateRevisionArgs(t)
	t.Setenv("GOFLAGS", "-mod=mod")

	root := temporary(t)
	mustInvoke(t, root, append([]string{
		"--no-input", "init", ".",
		"--name", "pinned",
		"--module", "example.test/pinned",
		"--env-prefix", "PINNED_",
		"--multi-tenant", "--local-credentials", "--oidc",
		"--no-mise", "--no-fnox", "--no-docker",
		"--boundary-profile", "candidate",
	}, revisions...)...)
	assertPinnedModule(t, root)

	external(t, root, "go", "build", "./...")

	if report := project.InspectArchitecture(root); !report.Passed {
		t.Fatalf("pinned candidate architecture: %+v", report.Diagnostics)
	}

	mustInvoke(t, root, "fmt")
	mustInvoke(t, root, "vet")
	assertPinnedModule(t, root)

	// Tidy prunes pins nothing imports yet; a blueprint whose storage
	// imports them restores the exact pins before tidy runs again.
	external(t, root, "go", "mod", "tidy")
	mustInvoke(t, root, "vet")

	path := filepath.Join(root, "ticket-blueprint.toml")
	if err := os.WriteFile(
		path,
		[]byte(candidateLocalBlueprint),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	mustInvoke(t, root, "--no-input", "add", "blueprint", "--file", path)
	external(t, root, "go", "mod", "tidy")
	mustInvoke(t, root, "fmt")
	mustInvoke(t, root, "openapi")
	mustInvoke(t, root, "vet")
	assertPinnedModule(t, root)

	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil || !strings.Contains(
		string(module),
		"github.com/plutoframework/pluto-core/stores/postgres v0.1.1-0.",
	) {
		t.Fatalf("blueprint storage does not use the pinned store: %v\n%s",
			err, module)
	}
}

// pluto migrate adopts the same pins for an existing published app and its
// own verification (tidy, OpenAPI, fmt, architecture, vet) must pass.
func TestCandidatePinnedMigration(t *testing.T) {
	revisions := candidateRevisionArgs(t)

	root := temporary(t)
	mustInvoke(
		t,
		root,
		"--no-input", "init", ".",
		"--name", "adopted",
		"--module", "example.test/adopted",
		"--env-prefix", "ADOPTED_",
		"--multi-tenant", "--local-credentials", "--oidc",
		"--no-mise", "--no-fnox", "--no-docker",
	)
	commitAll(t, root)

	output := mustInvoke(t, root, append(
		[]string{"--no-input", "migrate", "--to", "candidate"},
		revisions...,
	)...)
	for _, want := range []string{
		"pluto architecture check: ok",
		"pluto vet: ok",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("migration lacks %q:\n%s", want, output)
		}
	}

	assertPinnedModule(t, root)
}
