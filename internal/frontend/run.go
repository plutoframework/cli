package frontend

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/plutoframework/cli/internal/project"
)

// NPM never invokes a shell or lifecycle scripts. Exact dependency pins are
// resolved into npm's standard lockfile, which the application should commit.
func NPM(
	ctx context.Context,
	directory string,
	out, errOut io.Writer,
	args ...string,
) error {
	version, err := exec.CommandContext(ctx, "node", "--version").Output()
	if err == nil &&
		strings.HasPrefix(strings.TrimSpace(string(version)), "v24.") {
		return project.Run(ctx, directory, out, errOut, "npm", args...)
	}

	if _, err := exec.LookPath("mise"); err == nil {
		return project.Run(
			ctx,
			directory,
			out,
			errOut,
			"mise",
			append(
				[]string{"exec", "node@" + NodeVersion, "--", "npm"},
				args...)...)
	}

	return project.Run(ctx, directory, out, errOut, "npm", args...)
}

func Install(
	ctx context.Context,
	root string,
	m project.Manifest,
	out, errOut io.Writer,
) error {
	if m.Frontend == nil {
		return fmt.Errorf("run pluto frontend init first")
	}

	if err := Contracts(root, m); err != nil {
		return err
	}

	if err := project.Run(
		ctx,
		root,
		out,
		errOut,
		"go",
		"run",
		"./cmd/openapi",
	); err != nil {
		return err
	}

	directory := filepath.Join(root, m.Frontend.Directory)
	for _, args := range [][]string{
		{"install", "--ignore-scripts", "--save-exact"},
		{"run", "api:generate"},
		{"run", "fmt"},
		{"run", "check"},
	} {
		if err := NPM(ctx, directory, out, errOut, args...); err != nil {
			return fmt.Errorf(
				"frontend files remain in place; repair the tool error and run pluto frontend install: %w",
				err,
			)
		}
	}

	return nil
}

func (p Plan) Apply(root string, dry bool) ([]string, error) {
	return project.Apply(root, p.Files, p.Previous, dry)
}
