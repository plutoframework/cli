package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

func TestInitPreflight(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{"bad-name", []string{"--name", "../bad", "--module", "example.com/demo", "--env-prefix", "DEMO_"}},
		{"bad-module", []string{"--module", "x;evil"}},
		{"bad-prefix", []string{"--env-prefix", "mixed_"}},
		{"double-prefix", []string{"--env-prefix", "BAD__"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := temporary(t)
			if _, err := invoke(
				t,
				root,
				append(
					[]string{"init", ".", "--name", "demo"},
					test.args...)...); err == nil {
				t.Fatal("invalid init accepted")
			}

			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatal("invalid init wrote files")
			}
		})
	}

	t.Run("conflict", func(t *testing.T) {
		root := temporary(t)
		if err := os.WriteFile(
			filepath.Join(root, "README.md"),
			[]byte("user content"),
			0o644,
		); err != nil {
			t.Fatal(err)
		}

		before := snapshot(t, root)
		if _, err := invoke(
			t,
			root,
			"init",
			".",
			"--name",
			"demo",
		); err == nil {
			t.Fatal("overwrote user file")
		}

		assertUnchanged(t, before, snapshot(t, root))
	})
	t.Run("symlink", func(t *testing.T) {
		root := temporary(t)

		outside := temporary(t)
		if err := os.Symlink(
			outside,
			filepath.Join(root, "internal"),
		); err != nil {
			t.Fatal(err)
		}

		if _, err := invoke(
			t,
			root,
			"init",
			".",
			"--name",
			"demo",
		); err == nil {
			t.Fatal("followed symlink")
		}

		entries, err := os.ReadDir(outside)
		if err != nil || len(entries) != 0 {
			t.Fatal("symlink destination changed")
		}
	})
	t.Run("dry-run", func(t *testing.T) {
		root := temporary(t)
		target := filepath.Join(root, "demo")

		output := mustInvoke(t, root, "init", target, "--dry-run")
		if !strings.Contains(output, "pluto.toml") {
			t.Fatal(output)
		}

		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatal("dry run wrote target")
		}
	})
	t.Run("nested-git", func(t *testing.T) {
		root := temporary(t)
		external(t, root, "git", "-c", "init.templateDir=", "init", "--quiet")

		nested := filepath.Join(root, "nested")
		mustInvoke(t, root, "init", nested, "--name", "nested")

		if _, err := os.Stat(
			filepath.Join(nested, ".git"),
		); !os.IsNotExist(
			err,
		) {
			t.Fatal("nested Git repo created")
		}

		if _, err := invoke(t, nested, "hook", "install"); err == nil {
			t.Fatal("changed ancestor hooks")
		}
	})
	t.Run("existing-hook-preflight", func(t *testing.T) {
		root := temporary(t)
		external(t, root, "git", "-c", "init.templateDir=", "init", "--quiet")

		hook := filepath.Join(root, ".git/hooks/pre-commit")
		if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(
			hook,
			[]byte("#!/bin/sh\nexit 99\n"),
			0o755,
		); err != nil {
			t.Fatal(err)
		}

		if _, err := invoke(
			t,
			root,
			"init",
			".",
			"--name",
			"demo",
			"--hook",
		); err == nil {
			t.Fatal("replaced hook")
		}

		if _, err := os.Stat(
			filepath.Join(root, "go.mod"),
		); !os.IsNotExist(
			err,
		) {
			t.Fatal("hook conflict left partial app")
		}

		if !project.InWorktree(context.Background(), root) {
			t.Fatal("repo removed")
		}
	})
}

func TestUnknownManifest(t *testing.T) {
	root := temporary(t)
	mustInvoke(t, root, "init", ".", "--name", "demo")

	file := filepath.Join(root, "pluto.toml")

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		file,
		append(data, []byte("\nunknown_setting = true\n")...),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := invoke(
		t,
		root,
		"add",
		"endpoint",
		"widgets",
		"Create",
	); err == nil ||
		!strings.Contains(err.Error(), "unknown_setting") {
		t.Fatalf("unknown setting: %v", err)
	}
}
