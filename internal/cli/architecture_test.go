package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

func TestArchitectureCommandJSONFailure(t *testing.T) {
	var out, stderr bytes.Buffer

	code := Execute(
		context.Background(),
		[]string{
			"architecture",
			"check",
			"--directory",
			t.TempDir(),
			"--format",
			"json",
		},
		nil,
		&out,
		&stderr,
		"test",
	)

	var report project.ArchitectureReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("stdout is not one JSON report: %v: %s", err, out.String())
	}

	if code != 1 || report.Passed || len(report.Diagnostics) == 0 ||
		len(report.Unassessed) == 0 ||
		report.CheckedScope == "" {
		t.Fatalf("unexpected command result code=%d report=%+v", code, report)
	}
}
