package cli

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestMigrateGrytShapedFixture dry-runs the published-to-candidate step
// against a real pre-migration application (gryt's api at the commit before
// its manual migration). It is gated on PLUTO_CLI_GRYT_REPO, a checkout of
// that repository, and PLUTO_CLI_GRYT_REF (default 5b9af4e).
func TestMigrateGrytShapedFixture(t *testing.T) {
	repo := os.Getenv("PLUTO_CLI_GRYT_REPO")
	if repo == "" {
		t.Skip("set PLUTO_CLI_GRYT_REPO to a gryt checkout")
	}

	ref := os.Getenv("PLUTO_CLI_GRYT_REF")
	if ref == "" {
		ref = "5b9af4e"
	}

	root := temporary(t)

	archive := exec.Command("sh", "-c", "git -C \"$1\" archive \"$2\" api | "+
		"tar -x -C \"$3\" --strip-components=1", "sh", repo, ref, root)
	if output, err := archive.CombinedOutput(); err != nil {
		t.Fatalf("archive %s: %v\n%s", ref, err, output)
	}

	initRepo := exec.Command("git", "init", "-q")
	initRepo.Dir = root

	if output, err := initRepo.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}

	commitAll(t, root)

	args := append(
		[]string{"--no-input", "migrate", "--to", "candidate", "--dry-run"},
		fakeCandidateSources(t)...,
	)

	output, err := invoke(t, root, args...)
	if err == nil || !strings.Contains(err.Error(), "manual migration items") {
		t.Fatalf("expected remaining manual items: %v\n%s", err, output)
	}

	manual, _, _ := strings.Cut(
		output[strings.Index(output, "Manual items"):],
		"Notes (",
	)

	// Only the edits gryt made to generated paths and the uses of removed
	// scaffold names are application knowledge; older pristine scaffold
	// files, stale scaffold tests and sqlc output are not manual.
	var items []string

	for _, line := range strings.Split(manual, "\n") {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
			items = append(items, strings.TrimSpace(line))
		}
	}

	for _, stale := range []string{
		"cmd/api/main.go:83: modified since scaffold",
		"cmd/api/main.go:109: uses main.newLocalAuthentication",
		"internal/access/policy.go:20: modified since scaffold",
		"internal/api/openapi.go:5: modified since scaffold",
		"internal/app/app.go:8: modified since scaffold",
		"internal/app/app.go:131: uses app.RegisterGenerated",
	} {
		found := false

		for _, item := range items {
			found = found || strings.HasPrefix(item, stale)
		}

		if !found {
			t.Fatalf("manual items lack %q:\n%s", stale, manual)
		}
	}

	if len(items) != 6 {
		t.Fatalf("want exactly 6 manual items, got %d:\n%s", len(items), manual)
	}

	for _, want := range []string{
		"  D internal/app/app_test.go",
		"sqlc output kept unchanged",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("dry run lacks %q:\n%s", want, output)
		}
	}
}
