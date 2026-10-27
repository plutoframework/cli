package project

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const LintVersion = "2.15.0"

func Run(
	ctx context.Context,
	root string,
	out, errOut io.Writer,
	name string,
	args ...string,
) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = root
	command.Stdout = out

	command.Stderr = errOut
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}

	return nil
}

func linter(ctx context.Context) (string, error) {
	path, err := exec.LookPath("golangci-lint")
	if err == nil {
		data, versionErr := exec.CommandContext(ctx, path, "version", "--short").
			Output()
		if versionErr == nil && strings.TrimSpace(string(data)) == LintVersion {
			return path, nil
		}
	}

	return "", fmt.Errorf(
		"golangci-lint %s is required on PATH; install from https://github.com/golangci/golangci-lint/releases/tag/v%s or use mise exec golangci-lint@%s -- pluto fmt/vet",
		LintVersion,
		LintVersion,
		LintVersion,
	)
}

func Format(ctx context.Context, root string, out, errOut io.Writer) error {
	tool, err := linter(ctx)
	if err != nil {
		return err
	}

	for _, args := range [][]string{
		{"fmt", "./..."},
		{"run", "--enable-only", "wsl_v5", "--fix", "./..."},
		{"fmt", "./..."},
	} {
		if err := Run(ctx, root, out, errOut, tool, args...); err != nil {
			return err
		}
	}

	return nil
}

func Vet(
	ctx context.Context,
	root string,
	m Manifest,
	out, errOut io.Writer,
) error {
	if err := Contracts(root, m); err != nil {
		return err
	}

	tool, err := linter(ctx)
	if err != nil {
		return err
	}

	command := exec.CommandContext(ctx, tool, "fmt", "--diff", "./...")
	command.Dir = root
	command.Stderr = errOut

	diff, err := command.Output()
	if err != nil {
		return fmt.Errorf("formatter check: %w", err)
	}

	if len(diff) > 0 {
		_, _ = out.Write(diff)
		return fmt.Errorf("formatting differs; run pluto fmt")
	}

	for _, step := range []struct {
		name string
		args []string
	}{
		{tool, []string{"run", "./..."}},
		{"go", []string{"vet", "./..."}},
		{"go", []string{"test", "./..."}},
		{"go", []string{"run", "./cmd/openapi", "-check"}},
	} {
		if err := Run(
			ctx,
			root,
			out,
			errOut,
			step.name,
			step.args...); err != nil {
			return err
		}
	}

	return nil
}

// Contracts intentionally uses ordinary Go parsing/import reachability, not a
// custom semantic linter. Files are visited in lexical order for stable errors.
func Contracts(root string, m Manifest) error {
	if err := m.Validate(); err != nil {
		return err
	}

	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return err
	}

	if !strings.Contains(string(module), "module "+m.Project.Module+"\n") {
		return fmt.Errorf("go.mod module does not match pluto.toml")
	}

	managed, err := Managed(m)
	if err != nil {
		return err
	}

	names := []string{}
	for path := range managed {
		names = append(names, path)
	}

	sort.Strings(names)

	for _, path := range names {
		safe, err := SafePath(root, path)
		if err != nil {
			return err
		}

		current, err := os.ReadFile(safe)
		if err != nil {
			return err
		}

		if !Equivalent(current, managed[path]) {
			return fmt.Errorf(
				"generated contract differs: %s; restore generated content or intentionally migrate metadata",
				path,
			)
		}
	}

	imports := map[string][]string{}

	err = filepath.WalkDir(
		root,
		func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}

			if entry.IsDir() {
				if relative == ".git" {
					return filepath.SkipDir
				}

				return nil
			}

			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("project symlink refused: %s", relative)
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			if strings.ContainsRune(string(data), 0) {
				return nil
			}

			lines := strings.Count(string(data), "\n")
			if len(data) > 0 && data[len(data)-1] != '\n' {
				lines++
			}

			if lines > 1000 {
				return fmt.Errorf(
					"%s: %d lines exceeds hard 1000-line limit",
					relative,
					lines,
				)
			}

			if !strings.HasSuffix(path, ".go") {
				return nil
			}

			tree, err := parser.ParseFile(
				token.NewFileSet(),
				path,
				data,
				parser.AllErrors,
			)
			if err != nil {
				return err
			}

			dir := filepath.ToSlash(filepath.Dir(relative))
			key := m.Project.Module + "/" + dir

			for _, spec := range tree.Imports {
				imported, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return err
				}

				imports[key] = append(imports[key], imported)
			}

			return nil
		},
	)
	if err != nil {
		return err
	}

	packages := []string{}
	for name := range imports {
		packages = append(packages, name)
	}

	sort.Strings(packages)

	for _, start := range packages {
		relative := strings.TrimPrefix(start, m.Project.Module+"/")
		kind := ""

		switch {
		case relative == "internal/operations" || strings.HasPrefix(relative, "internal/operations/"):
			kind = "operations"
		case relative == "internal/handlers" || strings.HasPrefix(relative, "internal/handlers/"):
			kind = "handlers"
		case relative == "internal/paths":
			kind = "paths"
		}

		if kind == "" {
			continue
		}

		seen := map[string]bool{}

		var visit func(string) error

		visit = func(current string) error {
			if seen[current] {
				return nil
			}

			seen[current] = true
			dependencies := append([]string(nil), imports[current]...)
			sort.Strings(dependencies)

			for _, dependency := range dependencies {
				local := strings.TrimPrefix(dependency, m.Project.Module+"/")
				database := dependency == "database/sql" ||
					strings.HasPrefix(dependency, "github.com/jackc/") ||
					strings.HasPrefix(local, "internal/storage")

				http := dependency == "net/http" ||
					strings.HasPrefix(dependency, "net/http/") ||
					strings.HasPrefix(dependency, "github.com/go-chi/") ||
					strings.HasPrefix(local, "internal/transport") ||
					strings.HasPrefix(local, "internal/handlers") ||
					strings.HasPrefix(local, "internal/app")
				if kind == "operations" && (database || http) ||
					kind == "handlers" && database ||
					kind == "paths" &&
						(http || database || strings.HasPrefix(local, "internal/operations")) {
					return fmt.Errorf(
						"architecture: %s reaches forbidden import %s through %s",
						start,
						dependency,
						current,
					)
				}

				if strings.HasPrefix(dependency, m.Project.Module+"/") {
					if err := visit(dependency); err != nil {
						return err
					}
				}
			}

			return nil
		}
		if err := visit(start); err != nil {
			return err
		}
	}

	return nil
}
