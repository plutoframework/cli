package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	return root
}

func testGit(t *testing.T, root string, args ...string) string {
	t.Helper()

	out, err := git(context.Background(), root, args...)
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func TestHooksAndStagedPolicy(t *testing.T) {
	ctx := context.Background()

	root := testRoot(t)
	if err := GitInit(ctx, root); err != nil {
		t.Fatal(err)
	}

	testGit(t, root, "config", "core.hooksPath", ".hooks")

	if err := InstallHook(ctx, root); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, ".hooks/pre-commit")

	hook, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(hook), "pluto vet --staged") {
		t.Fatalf("hook: %s %v", hook, err)
	}

	if err := InstallHook(ctx, root); err == nil {
		t.Fatal("existing hook overwritten")
	}

	if err := StagedPolicy(ctx, root); err == nil {
		t.Fatal("untracked accepted")
	}

	testGit(t, root, "add", ".")

	if err := StagedPolicy(ctx, root); err != nil {
		t.Fatal(err)
	}

	file := filepath.Join(root, "sample.go")
	if err := os.WriteFile(
		file,
		[]byte("package sample\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	testGit(t, root, "add", ".")

	if err := StagedPolicy(ctx, root); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		file,
		[]byte("package changed\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if err := StagedPolicy(ctx, root); err == nil {
		t.Fatal("unstaged accepted")
	}

	testGit(t, root, "update-index", "--assume-unchanged", "sample.go")

	if err := StagedPolicy(ctx, root); err == nil {
		t.Fatal("hidden unstaged accepted")
	}

	testGit(t, root, "update-index", "--no-assume-unchanged", "sample.go")
	testGit(t, root, "add", ".")

	if err := os.WriteFile(
		filepath.Join(root, ".gitignore"),
		[]byte("ignored.go\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "ignored.go"),
		[]byte("package ignored\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	testGit(t, root, "add", ".gitignore")

	if err := StagedPolicy(ctx, root); err == nil {
		t.Fatal("ignored source accepted")
	}
}

func TestHookExecutionAndWorktree(t *testing.T) {
	ctx := context.Background()

	root := testRoot(t)
	if err := GitInit(ctx, root); err != nil {
		t.Fatal(err)
	}

	if err := InstallHook(ctx, root); err != nil {
		t.Fatal(err)
	}

	hook := testGit(
		t,
		root,
		"rev-parse",
		"--path-format=absolute",
		"--git-path",
		"hooks/pre-commit",
	)
	tools := testRoot(t)

	script := "#!/bin/sh\n[ \"$1 $2\" = 'vet --staged' ] || exit 94\n[ \"$PWD\" = \"$(git rev-parse --show-toplevel)\" ] || exit 93\nexit 17\n"
	if err := os.WriteFile(
		filepath.Join(tools, "pluto"),
		[]byte(script),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(hook)
	command.Dir = root

	command.Env = append(
		os.Environ(),
		"PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	if err := command.Run(); err == nil {
		t.Fatal("hook did not propagate failure")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 17 {
		t.Fatal(err)
	}

	// Exercise linked worktrees using only local temporary commits, bypassing
	// hooks explicitly; the user's real repositories are never committed.
	testGit(
		t,
		root,
		"-c",
		"core.hooksPath=/dev/null",
		"-c",
		"user.name=Test",
		"-c",
		"user.email=test@example.invalid",
		"commit",
		"--allow-empty",
		"-m",
		"fixture",
	)

	worktree := filepath.Join(testRoot(t), "linked")
	testGit(t, root, "worktree", "add", "-b", "test-linked", worktree)

	if err := GitInit(ctx, worktree); err != nil {
		t.Fatal(err)
	}

	// Existing shared hook must remain intact and never be invoked by GitInit.
	if err := InstallHook(ctx, worktree); err == nil {
		t.Fatal("shared existing hook overwritten")
	}
}

func TestApplyAndContracts(t *testing.T) {
	root := testRoot(t)
	m := Manifest{
		SchemaVersion: SchemaVersion,
		Project: Project{
			Name:      "demo",
			Module:    "example.com/demo",
			EnvPrefix: "DEMO_",
		},
		Capabilities: Capabilities{Tenancy: "single"},
		Database:     Database{Engine: "postgres", Queries: "sqlc"},
	}

	files, err := InitialFiles(m)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Apply(root, files, nil, false); err != nil {
		t.Fatal(err)
	}

	if err := Contracts(root, m); err != nil {
		t.Fatal(err)
	}

	if _, err := Apply(
		root,
		map[string][]byte{"../escape": []byte("bad")},
		nil,
		false,
	); err == nil {
		t.Fatal("traversal accepted")
	}

	// Prove transitive reachability through a helper, not only direct imports.
	helper := filepath.Join(root, "internal/helper")
	operation := filepath.Join(root, "internal/operations/test")

	if err := os.MkdirAll(helper, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(operation, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(helper, "helper.go"),
		[]byte("package helper\nimport _ \"net/http\"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(operation, "operation.go")
	if err := os.WriteFile(
		path,
		[]byte("package test\nimport _ \"example.com/demo/internal/helper\"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if err := Contracts(
		root,
		m,
	); err == nil ||
		!strings.Contains(err.Error(), "architecture") {
		t.Fatalf("transitive check: %v", err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	long := filepath.Join(root, "long.txt")
	if err := os.WriteFile(
		long,
		[]byte(strings.Repeat("readable\n", 1001)),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if err := Contracts(
		root,
		m,
	); err == nil ||
		!strings.Contains(err.Error(), "1000") {
		t.Fatalf("line limit: %v", err)
	}
}

func TestMissingTool(t *testing.T) {
	t.Setenv("PATH", testRoot(t))

	if _, err := linter(
		context.Background(),
	); err == nil ||
		!strings.Contains(err.Error(), "2.15.0") {
		t.Fatal("missing tool silently passed")
	}
}

func FuzzEndpointValidation(f *testing.F) {
	f.Add("widgets", "Create", "/api/widgets/{id}")
	f.Add("../escape", "bad;evil", "/api/{id}/{id}")
	f.Fuzz(func(t *testing.T, group, action, path string) {
		if len(group)+len(action)+len(path) > 4096 {
			t.Skip()
		}

		endpoint := Endpoint{
			Group:       group,
			Action:      action,
			Method:      "POST",
			Path:        path,
			Name:        "Name",
			Description: "Description",
		}
		if endpoint.Validate(false) != nil {
			return
		}

		m := Manifest{Project: Project{Module: "example.com/demo"}}

		files, err := EndpointFiles(m, endpoint)
		if err != nil {
			t.Fatal(err)
		}

		for name := range files {
			if filepath.IsAbs(name) || strings.Contains(name, "..") {
				t.Fatalf("unsafe accepted path %s", name)
			}
		}
	})
}

func TestRepositoryFileCeiling(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	err = filepath.WalkDir(
		root,
		func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if entry.IsDir() {
				if entry.Name() == ".git" || entry.Name() == "bin" {
					return filepath.SkipDir
				}

				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			if strings.ContainsRune(string(data), 0) {
				return nil
			}

			if strings.Count(string(data), "\n") > 1000 {
				t.Errorf("%s exceeds 1000 lines", path)
			}

			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}
