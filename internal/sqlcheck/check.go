package sqlcheck

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Check runs every ARCH612 check: authored query analysis, stray query
// files, sqlc diff and the migration scan. It fails closed: a missing sqlc
// binary is a diagnostic, never a silent pass. The returned error is only for
// infrastructure failures that are not findings.
func Check(ctx context.Context, cfg Config) ([]Diagnostic, error) {
	if cfg.Root == "" {
		return nil, errors.New("sqlcheck: Config.Root is required")
	}

	var diagnostics []Diagnostic

	diagnostics = append(diagnostics, checkQueryFiles(cfg)...)

	sqlc, err := findSQLC(cfg)
	if err != nil {
		diagnostics = append(diagnostics, newDiagnostic(
			"pluto.toml",
			1,
			"sqlc is required to verify authored SQL but was not found: "+
				err.Error(),
			"Install the pinned sqlc (mise install) or set the sqlc path; "+
				"authored SQL fails closed without it.",
		))
	} else {
		found, err := checkWithSQLC(ctx, cfg, sqlc)
		if err != nil {
			return nil, err
		}

		diagnostics = append(diagnostics, found...)
	}

	migrations, err := CheckMigrations(cfg)
	if err != nil {
		return nil, err
	}

	diagnostics = append(diagnostics, migrations...)

	sortDiagnostics(diagnostics)

	return diagnostics, nil
}

func findSQLC(cfg Config) (string, error) {
	if cfg.SQLCPath != "" {
		if _, err := os.Stat(cfg.SQLCPath); err != nil {
			return "", err
		}

		return cfg.SQLCPath, nil
	}

	return exec.LookPath("sqlc")
}

func tableMap(cfg Config) map[string]Table {
	tables := map[string]Table{}

	for _, table := range cfg.Tables {
		table.Name = tableKey(table.Name)
		tables[table.Name] = table
	}

	return tables
}

// expandSet resolves a set's globs to sorted project-relative files.
func expandSet(root string, set QuerySet) ([]string, error) {
	seen := map[string]bool{}

	var files []string

	for _, pattern := range set.Files {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return nil, fmt.Errorf("query glob %q: %w", pattern, err)
		}

		for _, match := range matches {
			rel, err := filepath.Rel(root, match)
			if err != nil {
				return nil, err
			}

			rel = filepath.ToSlash(rel)
			if seen[rel] {
				continue
			}

			seen[rel] = true
			files = append(files, rel)
		}
	}

	sort.Strings(files)

	return files, nil
}

// checkQueryFiles verifies query files live only in declared sets.
func checkQueryFiles(cfg Config) []Diagnostic {
	var diagnostics []Diagnostic

	owner := map[string]string{}

	for _, set := range cfg.Sets {
		files, err := expandSet(cfg.Root, set)
		if err != nil {
			diagnostics = append(diagnostics, newDiagnostic(
				"pluto.toml", 1, err.Error(),
				"Use a valid glob in [[queries]] files.",
			))

			continue
		}

		if len(files) == 0 {
			diagnostics = append(diagnostics, newDiagnostic(
				"pluto.toml",
				1,
				fmt.Sprintf("query set %q matches no files.", set.Group),
				"Add the query files or remove the [[queries]] entry.",
			))
		}

		for _, file := range files {
			if previous, taken := owner[file]; taken {
				diagnostics = append(diagnostics, newDiagnostic(
					file,
					1,
					fmt.Sprintf(
						"query file belongs to sets %q and %q.",
						previous,
						set.Group,
					),
					"Keep each query file in exactly one declared set.",
				))

				continue
			}

			owner[file] = set.Group
		}
	}

	for _, queryRoot := range cfg.QueryRoots {
		diagnostics = append(
			diagnostics,
			strayFiles(cfg.Root, queryRoot, owner)...,
		)
	}

	return diagnostics
}

func strayFiles(
	root string,
	queryRoot string,
	owner map[string]string,
) []Diagnostic {
	var diagnostics []Diagnostic

	base := filepath.Join(root, queryRoot)
	_ = filepath.WalkDir(
		base,
		func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() ||
				!strings.EqualFold(filepath.Ext(path), ".sql") {
				return nil
			}

			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return nil
			}

			rel = filepath.ToSlash(rel)
			if _, declared := owner[rel]; !declared {
				diagnostics = append(diagnostics, newDiagnostic(
					rel,
					1,
					"SQL query file is not part of any declared query set.",
					"Declare it in a [[queries]] set with its tables or "+
						"delete it.",
				))
			}

			return nil
		},
	)

	return diagnostics
}
