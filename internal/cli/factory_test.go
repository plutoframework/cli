package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func factoryTestEngine(t *testing.T, body string) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	path := filepath.Join(t.TempDir(), "factory engine")
	if err := os.WriteFile(
		path,
		[]byte("#!/bin/sh\n"+body),
		0o700,
	); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestFactoryForwardsLiteralArgumentsAndStreams(t *testing.T) {
	engine := factoryTestEngine(t, `
read input
printf 'input=<%s>\n' "$input"
for argument do
    printf 'arg=<%s>\n' "$argument"
done
`)
	t.Setenv("PLUTO_FACTORY_ENGINE", engine)

	var output bytes.Buffer

	command := New(Options{
		In: strings.NewReader("a submitted request\n"), Out: &output,
		Err: &output, Directory: t.TempDir(),
	})
	literal := "title; $(touch should-not-exist)"
	command.SetArgs([]string{
		"--no-input", "factory", "submit", "--title", literal,
	})

	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, expected := range []string{
		"input=<a submitted request>", "arg=<--no-input>",
		"arg=<submit>", "arg=<--title>", "arg=<" + literal + ">",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("missing %q in %s", expected, output.String())
		}
	}
}

func TestFactoryMissingEngineIsActionable(t *testing.T) {
	t.Setenv("PLUTO_FACTORY_ENGINE", filepath.Join(t.TempDir(), "missing"))

	_, err := invoke(t, t.TempDir(), "factory", "setup")
	if err == nil || !strings.Contains(err.Error(), "PLUTO_FACTORY_ENGINE") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFactoryPropagatesEngineFailure(t *testing.T) {
	t.Setenv("PLUTO_FACTORY_ENGINE", factoryTestEngine(t, "exit 7\n"))

	_, err := invoke(t, t.TempDir(), "factory", "doctor")
	if err == nil || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFactoryWithoutArgumentsShowsEngineHelp(t *testing.T) {
	t.Setenv(
		"PLUTO_FACTORY_ENGINE",
		factoryTestEngine(t, "printf '%s' \"$1\"\n"),
	)

	output := mustInvoke(t, t.TempDir(), "factory")
	if output != "--help" {
		t.Fatalf("expected engine help, got %q", output)
	}
}

func TestFactoryForwardsInteractiveRootFlag(t *testing.T) {
	t.Setenv(
		"PLUTO_FACTORY_ENGINE",
		factoryTestEngine(t, "printf '%s\\n' \"$@\"\n"),
	)

	output := mustInvoke(t, t.TempDir(), "--interactive", "factory", "setup")
	if !strings.Contains(output, "--interactive") ||
		!strings.Contains(output, "setup") {
		t.Fatalf("expected interactive setup arguments, got %q", output)
	}
}
