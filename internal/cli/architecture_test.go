package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

func TestLifecycleArchitectureCheck(t *testing.T) {
	root := temporary(t)

	files := map[string]string{
		"go.mod":     "module example.test/lifecycle\n",
		"pluto.toml": "schema_version=9\n[project]\nmodule='example.test/lifecycle'\n",
		"internal/domain/model.go": `package domain
import "github.com/plutoframework/pluto-core/state"
type Stage string
type Entity struct { Status Stage }
func Machine() (*state.Machine[Stage,string,bool],error) {return state.New(state.Definition[Stage,string,bool]{})}
`,
		"internal/storage/store.go": `package storage
import "example.test/lifecycle/internal/domain"
func Save(value domain.Entity) error {return nil}
`,
	}
	for path, source := range files {
		target := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(target, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}

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
		t.Fatal("mutable/raw lifecycle boundary accepted")
	}

	var report project.ArchitectureReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatal(err)
	}

	ids := map[string]bool{}
	for _, d := range report.Diagnostics {
		ids[d.ID] = true
	}

	if !ids["ARCH301"] || !ids["ARCH303"] {
		t.Fatalf(
			"missing lifecycle boundary diagnostics: %+v",
			report.Diagnostics,
		)
	}
}
