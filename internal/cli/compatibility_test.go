package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

// Published fixtures verify compatibility and structural rules. Their protected
// operations intentionally fail the complete authority gate until migration.
func assertPublishedDraft(t *testing.T, root string) {
	t.Helper()

	output, err := invoke(
		t,
		root,
		"architecture",
		"check",
		"--directory",
		root,
		"--format",
		"json",
	)
	if err == nil {
		t.Fatal("published draft canonical check unexpectedly succeeded")
	}

	var report project.ArchitectureReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatal(err, output)
	}

	if report.Passed {
		t.Fatal(
			"published protected draft incorrectly claimed candidate " +
				"compliance",
		)
	}

	for _, diagnostic := range report.Diagnostics {
		if diagnostic.ID != "ARCH401" {
			t.Fatalf("unexpected compatibility diagnostic: %+v", diagnostic)
		}
	}
}

func mustVetPublishedCompatibility(t *testing.T, root string) {
	t.Helper()

	_, err := invoke(t, root, "vet")
	if err == nil {
		return
	}

	if !strings.Contains(err.Error(), "ARCH401") {
		t.Fatal(err)
	}

	assertPublishedDraft(t, root)

	// Continue independent compilation/lint checks without adding a production
	// flag that could waive the required candidate authority boundary.
	external(t, root, "golangci-lint", "run", "./cmd/...", "./internal/...")
	external(t, root, "go", "vet", "./cmd/...", "./internal/...")
	external(t, root, "go", "test", "./cmd/...", "./internal/...")
	external(t, root, "go", "run", "./cmd/openapi", "-check")
}
