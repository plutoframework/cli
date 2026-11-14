package migrate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/plutoframework/cli/internal/project"
)

// RequireCleanTree refuses to migrate outside Git or with any uncommitted or
// untracked change, so the result is reviewable and reversible with Git.
// Only read-only Git plumbing runs; no hooks are invoked.
func RequireCleanTree(ctx context.Context, root string) error {
	command := exec.CommandContext(
		ctx,
		"git",
		"-C",
		root,
		"status",
		"--porcelain",
		"--untracked-files=normal",
		"--",
		".",
	)

	output, err := command.Output()
	if err != nil {
		return fmt.Errorf(
			"pluto migrate requires a Git work tree for review and " +
				"rollback (git status failed)",
		)
	}

	if dirty := strings.TrimSpace(string(output)); dirty != "" {
		lines := strings.Split(dirty, "\n")
		if len(lines) > 5 {
			lines = append(lines[:5], "...")
		}

		return fmt.Errorf(
			"working tree has uncommitted changes; commit or stash them "+
				"first:\n%s",
			strings.Join(lines, "\n"),
		)
	}

	return nil
}

// Write applies the plan. Every target is checked before the first write; on a
// write error, already written files are restored from the originals.
func Write(plan *Plan) error {
	if plan.Blocked {
		return fmt.Errorf("migration is blocked; nothing written")
	}

	names := plan.Paths()
	for _, name := range names {
		if _, err := project.SafePath(plan.Root, name); err != nil {
			return err
		}
	}

	done := []string{}
	restore := func() {
		for _, name := range done {
			path := filepath.Join(plan.Root, filepath.FromSlash(name))
			if original, ok := plan.Original[name]; ok {
				_ = os.WriteFile(path, original, 0o644)
			} else {
				_ = os.Remove(path)
			}
		}
	}

	for _, name := range names {
		path := filepath.Join(plan.Root, filepath.FromSlash(name))

		var err error

		if plan.Deletes[name] {
			err = os.Remove(path)
		} else {
			mode := os.FileMode(0o644)
			if strings.HasPrefix(name, "scripts/") {
				mode = 0o755
			}

			err = os.MkdirAll(filepath.Dir(path), 0o755)
			if err == nil {
				err = os.WriteFile(path, plan.Writes[name], mode)
			}
		}

		done = append(done, name)

		if err != nil {
			restore()

			return fmt.Errorf("%s: %w", name, err)
		}
	}

	return nil
}
