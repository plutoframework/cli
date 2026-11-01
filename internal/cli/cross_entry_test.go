package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
)

func TestCrossEntryOperationConsumer(t *testing.T) {
	root := temporary(t)
	mustInvoke(
		t,
		root,
		"init",
		".",
		"--name",
		"cross-entry",
		"--module",
		"example.test/cross",
		"--multi-tenant",
		"--no-mise",
		"--no-fnox",
		"--no-docker",
	)
	mustInvoke(
		t,
		root,
		"add",
		"operation",
		"reports",
		"Read",
		"--permission",
		"reports.read",
	)

	path := filepath.Join(root, "internal/operations/reports/read.go")
	replaceSource(
		t,
		path,
		"type ReadDependencies struct{}",
		"type ReadDependencies struct{Calls *int}",
	)
	replaceSource(t, path, "_ reports.ReadRequest", "input reports.ReadRequest")

	private := replaceSource(
		t,
		path,
		"return reports.ReadData{}, domain.ErrNotImplemented",
		"if operation.dependencies.Calls==nil{return reports.ReadData{},domain.ErrNotImplemented}; *operation.dependencies.Calls++; return reports.ReadData{Value:input.ResourceID},nil",
	)
	mustInvoke(
		t,
		root,
		"add",
		"endpoint",
		"reports",
		"Read",
		"--operation",
		"reports.Read",
		"--method",
		"POST",
		"--path",
		"/api/reports",
		"--name",
		"Read report",
		"--description",
		"Read the selected report.",
	)
	mustInvoke(
		t,
		root,
		"add",
		"job",
		"deliveries",
		"Read",
		"--operation",
		"reports.Read",
		"--kind",
		"reports.read",
	)
	mustInvoke(
		t,
		root,
		"add",
		"agent",
		"reviews",
		"Inspect",
		"--operation",
		"reports.Read",
	)

	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, private) {
		t.Fatal("entry addition overwrote private business implementation", err)
	}

	fixture, err := os.ReadFile("testdata/cross_entry.go.tmpl")
	if err != nil {
		t.Fatal(err)
	}

	tmpl, err := template.New("cross").
		Delims("[[[", "]]]").
		Parse(string(fixture))
	if err != nil {
		t.Fatal(err)
	}

	var source bytes.Buffer
	if err := tmpl.Execute(&source, nil); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "internal/app/cross_entry_test.go"),
		source.Bytes(),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	external(t, root, "go", "mod", "tidy")
	external(t, root, "go", "test", "-race", "./...")
	mustInvoke(
		t,
		root,
		"architecture",
		"check",
		"--directory",
		root,
		"--format",
		"json",
	)

	// A separately constructed worker can compile but cannot enter the supported
	// app through an authored registry replacement, even without Handler/Job types.
	rogue := filepath.Join(root, "internal/app/rogue_worker.go")

	rawWorker := []byte(`package app
import (
 "example.test/cross/internal/background"
 jobs "github.com/plutoframework/pluto-jobs"
)
func rogueWorker(store jobs.Store) (*jobs.Worker,error) {
 handlers:=background.Handlers(background.Dependencies{})
 handlers["alternate"]=handlers["reports.read"]
 return jobs.NewWorker(store,handlers,jobs.DefaultConfig())
}
`)
	if err := os.WriteFile(rogue, rawWorker, 0o644); err != nil {
		t.Fatal(err)
	}

	external(t, root, "go", "test", "./internal/app")

	if output, err := invoke(
		t,
		root,
		"architecture",
		"check",
		"--directory",
		root,
		"--format",
		"json",
	); err == nil ||
		!strings.Contains(output, "ARCH211") {
		t.Fatalf(
			"compiled raw worker registry bypass accepted: %s %v",
			output,
			err,
		)
	}

	if err := os.Remove(rogue); err != nil {
		t.Fatal(err)
	}

	// Every entry adapter is managed; deleting its gate reference is rejected.
	for _, relative := range []string{"internal/jobs/deliveries/read.go", "internal/agents/reviews/inspect.go"} {
		target := filepath.Join(root, relative)

		before, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(
			target,
			[]byte("package altered\n"),
			0o644,
		); err != nil {
			t.Fatal(err)
		}

		if _, err := invoke(
			t,
			root,
			"architecture",
			"check",
			"--directory",
			root,
			"--format",
			"json",
		); err == nil {
			t.Fatal("managed cross-entry adapter replacement passed", relative)
		}

		if err := os.WriteFile(target, before, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
