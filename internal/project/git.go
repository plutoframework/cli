package project

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func git(ctx context.Context, root string, args ...string) (string, error) {
	command := exec.CommandContext(
		ctx,
		"git",
		append([]string{"-C", root}, args...)...)

	data, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf(
			"git %s: %w: %s",
			strings.Join(args, " "),
			err,
			data,
		)
	}

	return strings.TrimSpace(string(data)), nil
}

func GitInit(ctx context.Context, root string) error {
	if _, err := git(ctx, root, "rev-parse", "--show-toplevel"); err == nil {
		return nil
	}

	// Disable templates explicitly so user template hooks cannot be installed/run.
	_, err := git(ctx, root, "-c", "init.templateDir=", "init", "--quiet")

	return err
}

func HookPath(ctx context.Context, root string) (string, error) {
	top, err := git(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}

	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}

	if top != resolved {
		return "", fmt.Errorf(
			"hook installation refused: project is nested in %s; install an explicit repository-wide dispatcher yourself",
			top,
		)
	}

	path, err := git(
		ctx,
		root,
		"rev-parse",
		"--path-format=absolute",
		"--git-path",
		"hooks/pre-commit",
	)
	if err != nil {
		return "", err
	}

	if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
		return "", fmt.Errorf(
			"existing pre-commit hook left untouched: %s",
			path,
		)
	}

	// Validate effective hooks directory, including configured core.hooksPath.
	return SafePath(filepath.Dir(path), "pre-commit")
}

func InstallHook(ctx context.Context, root string) error {
	path, err := HookPath(ctx, root)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	defer file.Close()
	// Git runs hooks at the worktree root. Resolve it each time rather than bake
	// in a path that breaks when the worktree moves.
	_, err = file.WriteString(
		"#!/bin/sh\nset -eu\ncd \"$(git rev-parse --show-toplevel)\"\nexec pluto vet --staged\n",
	)

	return err
}

// StagedPolicy guarantees the checked working tree matches index file contents
// by refusing all unstaged/untracked files, not by hiding changes with git stash.
func StagedPolicy(ctx context.Context, root string) error {
	command := exec.CommandContext(
		ctx,
		"git",
		"-C",
		root,
		"status",
		"--porcelain=v1",
		"--untracked-files=all",
		"--ignored=matching",
		"--",
		".",
	)

	data, err := command.Output()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}

	for _, line := range strings.Split(string(data), "\n") {
		if len(line) < 3 {
			continue
		}

		if line[0] == '?' || line[0] == '!' || line[1] != ' ' ||
			line[0] == 'U' {
			return fmt.Errorf(
				"staged policy requires all project files staged, including no ignored extras or conflicts: %s",
				line,
			)
		}
	}

	command = exec.CommandContext(
		ctx,
		"git",
		"-C",
		root,
		"ls-files",
		"--stage",
		"-z",
		"--",
		".",
	)

	data, err = command.Output()
	if err != nil {
		return err
	}

	for _, record := range strings.Split(string(data), "\x00") {
		if record == "" {
			continue
		}

		header, name, ok := strings.Cut(record, "\t")

		fields := strings.Fields(header)
		if !ok || len(fields) != 3 || fields[2] != "0" {
			return fmt.Errorf("unmerged index entry")
		}

		if fields[0] != "100644" && fields[0] != "100755" {
			return fmt.Errorf("staged symlinks/submodules are unsupported")
		}

		if _, err := SafePath(root, name); err != nil {
			return err
		}

		hash, err := git(ctx, root, "hash-object", "--no-filters", "--", name)
		if err != nil {
			return err
		}

		if hash != fields[1] {
			return fmt.Errorf(
				"working tree differs from staged blob: %s (also rejects clean/smudge transformations and hidden index flags)",
				name,
			)
		}
	}

	return nil
}

func InWorktree(ctx context.Context, root string) bool {
	_, err := git(ctx, root, "rev-parse", "--show-toplevel")
	return err == nil
}

// PreflightHook resolves an intended target even before its directory exists.
// Refuse nested/shared configuration rather than surprise an ancestor checkout.
func PreflightHook(ctx context.Context, target string) error {
	ancestor := target
	for {
		if _, err := os.Stat(ancestor); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return err
		}

		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return fmt.Errorf("cannot resolve target parent")
		}

		ancestor = parent
	}

	if top, err := git(
		ctx,
		ancestor,
		"rev-parse",
		"--show-toplevel",
	); err == nil {
		if top != target {
			return fmt.Errorf(
				"hook installation refused: target is nested in existing repository %s",
				top,
			)
		}

		_, err := HookPath(ctx, target)

		return err
	}

	configured, err := git(ctx, ancestor, "config", "--get", "core.hooksPath")
	if err != nil {
		return nil
	} // Unset; git init uses its own empty hooks directory.

	if filepath.IsAbs(configured) ||
		strings.HasPrefix(filepath.Clean(configured), "..") {
		return fmt.Errorf(
			"hook installation refused: inherited core.hooksPath %q is outside the new project; configure a project-local path explicitly",
			configured,
		)
	}

	path, err := SafePath(target, filepath.Join(configured, "pre-commit"))
	if err != nil {
		return err
	}

	if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
		return fmt.Errorf("existing pre-commit hook left untouched: %s", path)
	}

	return nil
}
