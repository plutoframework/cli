package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnmanagedEntryFactories(t *testing.T) {
	for _, tc := range []struct{ name, source, id string }{
		{"raw worker constructor", `package app
import jobs "github.com/plutoframework/pluto-jobs"
func bypass(store jobs.Store) { _,_ = jobs.NewWorker(store,nil,jobs.DefaultConfig()) }
`, "ARCH211"},
		{"constructor capture", `package app
import jobs "github.com/plutoframework/pluto-jobs"
var factory = jobs.NewWorker
`, "ARCH211"},
		{"typed bind", `package app
import jobs "github.com/plutoframework/pluto-jobs"
func bypass(){definition,_:=jobs.NewDefinition[string]("kind",func(string)error{return nil});_ = definition.Bind}
`, "ARCH211"},
		{"agent tool factory", `package app
import agent "github.com/plutoframework/pluto-agent"
var factory = agent.NewTool[int,int,int]
`, "ARCH212"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := testRoot(t)
			if err := os.MkdirAll(
				filepath.Join(root, "internal/app"),
				0o755,
			); err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(
				filepath.Join(root, "internal/app/bypass.go"),
				[]byte(tc.source),
				0o644,
			); err != nil {
				t.Fatal(err)
			}

			found := false

			for _, d := range inspectEntryBoundaries(root, []byte("schema_version=1\n")) {
				if d.ID == tc.id && d.File == "internal/app/bypass.go" {
					found = true
				}
			}

			if !found {
				t.Fatalf("unmanaged %s was accepted", tc.name)
			}
		})
	}
}

func TestDefinitionPrepareIsDataNotWorkerBinding(t *testing.T) {
	root := testRoot(t)

	dir := filepath.Join(root, "internal/storage")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	source := `package storage
import jobs "github.com/plutoframework/pluto-jobs"
func prepare(){definition,_:=jobs.NewDefinition[string]("kind",func(string)error{return nil});_ = definition.Prepare}
`
	if err := os.WriteFile(
		filepath.Join(dir, "effect.go"),
		[]byte(source),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	for _, d := range inspectEntryBoundaries(root, []byte("schema_version=1\n")) {
		if strings.HasPrefix(d.ID, "ARCH21") {
			t.Fatalf("effect preparation misclassified as worker: %+v", d)
		}
	}
}
