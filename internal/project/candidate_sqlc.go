package project

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/pelletier/go-toml/v2"

	"github.com/plutoframework/cli/internal/sqlcheck"
)

// CandidateQueryPackage is the sqlc output directory of an authored query set.
func CandidateQueryPackage(group string) string {
	return "internal/storage/queries/" + group
}

// candidateSQLCConfig renders the managed sqlc.yaml: the framework stanza
// plus one stanza per authored query set. Every declared tenant_column maps
// to scope.Tenant, so a generated query argument for it cannot be a plain
// string supplied by authored code.
func candidateSQLCConfig(m Manifest) []byte {
	var out bytes.Buffer

	out.WriteString(`version: "2"
sql:
  - engine: postgresql
    schema: db/migrations
    queries: db/queries
    gen:
      go:
        package: db
        out: internal/storage/postgres/db
        sql_package: pgx/v5
`)

	sets := append([]QuerySet(nil), m.Queries...)
	sort.Slice(sets, func(i, j int) bool { return sets[i].Group < sets[j].Group })

	for _, set := range sets {
		fmt.Fprintf(
			&out,
			`  - engine: postgresql
    schema: db/migrations
    queries: db/queries/%s
    gen:
      go:
        package: queries
        out: %s
        sql_package: pgx/v5
`,
			set.Group,
			CandidateQueryPackage(set.Group),
		)

		var overrides []string

		for _, table := range m.Tables {
			if table.Group != set.Group {
				continue
			}

			overrides = append(overrides, fmt.Sprintf(
				`          - column: %q
            go_type:
              import: %q
              type: "Tenant"
`,
				table.Name+"."+table.TenantColumn,
				m.Project.Module+"/internal/scope",
			))
		}

		if len(overrides) == 0 {
			continue
		}

		sort.Strings(overrides)
		out.WriteString("        overrides:\n")

		for _, override := range overrides {
			out.WriteString(override)
		}
	}

	return out.Bytes()
}

// sqlcheckConfig converts the manifest declarations to ARCH612 inputs.
func sqlcheckConfig(root string, m Manifest) sqlcheck.Config {
	cfg := sqlcheck.Config{
		Root:       root,
		SchemaDirs: []string{"db/migrations"},
		SQLCConfig: "sqlc.yaml",
	}

	for _, table := range m.Tables {
		cfg.Tables = append(cfg.Tables, sqlcheck.Table{
			Name:         table.Name,
			Group:        table.Group,
			TenantColumn: table.TenantColumn,
			AppendOnly:   table.AppendOnly,
		})
	}

	for _, set := range m.Queries {
		cfg.Sets = append(cfg.Sets, sqlcheck.QuerySet{
			Group:  set.Group,
			Files:  set.Files,
			Tables: set.Tables,
		})
	}

	// Top-level files under db/queries belong to the framework; only group
	// subdirectories hold authored SQL.
	entries, err := os.ReadDir(filepath.Join(root, "db/queries"))
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				cfg.QueryRoots = append(
					cfg.QueryRoots,
					"db/queries/"+entry.Name(),
				)
			}
		}
	}

	return cfg
}

// inspectAuthoredSQL reports ARCH612. It runs only for candidate projects
// that declare tables or query sets, and fails closed when sqlc is missing.
func inspectAuthoredSQL(
	root string,
	metadata []byte,
) []ArchitectureDiagnostic {
	var m Manifest

	if err := toml.Unmarshal(metadata, &m); err != nil ||
		m.Boundary.Mode != "candidate" ||
		len(m.Tables) == 0 && len(m.Queries) == 0 {
		return nil
	}

	found, err := sqlcheck.Check(context.Background(), sqlcheckConfig(root, m))
	if err != nil {
		return []ArchitectureDiagnostic{{
			ID:       sqlcheck.RuleID,
			File:     "pluto.toml",
			Line:     1,
			Severity: "error",
			Message:  "authored SQL could not be verified: " + err.Error(),
			Repair:   "Fix the reported infrastructure problem and rerun.",
		}}
	}

	var diagnostics []ArchitectureDiagnostic

	for _, diagnostic := range found {
		diagnostics = append(diagnostics, ArchitectureDiagnostic{
			ID:       diagnostic.ID,
			File:     diagnostic.File,
			Line:     diagnostic.Line,
			Severity: diagnostic.Severity,
			Message:  diagnostic.Message,
			Repair:   diagnostic.Repair,
		})
	}

	return diagnostics
}
