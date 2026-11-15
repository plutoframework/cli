package project

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/plutoframework/cli/internal/sqlcheck"

	"github.com/pelletier/go-toml/v2"
)

// candidateLintConfig is the managed lint configuration. sqlc output is proved
// by sqlc diff, not by style linters; excluding both sqlc output directories
// keeps pluto fmt from rewriting files that sqlc would then report as stale.
func candidateLintConfig(m Manifest) []byte {
	config := strings.ReplaceAll(lintConfig, "MODULE", m.Project.Module)

	return []byte(strings.Replace(
		config,
		"  exclusions:\n    generated: strict\n    presets: []\n",
		"  exclusions:\n    generated: strict\n    presets: []\n"+
			"    paths:\n      - internal/storage/queries\n"+
			"      - internal/storage/postgres/db\n",
		1,
	))
}

// sqlcGeneratedFiles lists the files sqlc writes for the declared query sets:
// db.go, models.go and one <query file>.go per query file. Only these exact
// names are exempt from authored ownership; ARCH612 runs sqlc diff to prove
// their content is what sqlc generates.
func sqlcGeneratedFiles(root string, m Manifest) map[string]bool {
	paths := map[string]bool{}

	for _, set := range m.Queries {
		directory := CandidateQueryPackage(set.Group)
		paths[directory+"/db.go"] = true
		paths[directory+"/models.go"] = true

		for _, pattern := range set.Files {
			matches, _ := filepath.Glob(filepath.Join(
				root,
				filepath.FromSlash(pattern),
			))

			for _, match := range matches {
				name := strings.TrimSuffix(filepath.Base(match), ".sql")
				paths[directory+"/"+name+".sql.go"] = true
			}
		}
	}

	return paths
}

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

	// The managed framework output must not change when an application adds
	// its own tables, so its stanza reads only the foundation migration that
	// the checked-in managed models describe.
	out.WriteString(`version: "2"
sql:
  - engine: postgresql
    schema: db/migrations/001_initial.sql
    queries: db/queries
    gen:
      go:
        package: db
        out: internal/storage/postgres/db
        sql_package: pgx/v5
`)

	sets := append([]QuerySet(nil), m.Queries...)
	sort.Slice(
		sets,
		func(i, j int) bool {
			return sets[i].Group < sets[j].Group
		},
	)

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
			TenantRoot:   table.TenantRoot,
		})
	}

	provisioning := map[string]bool{}

	for _, operation := range m.Operations {
		if operation.Scope == ScopeProvision {
			provisioning[operation.Group] = true
		}
	}

	for _, set := range m.Queries {
		cfg.Sets = append(cfg.Sets, sqlcheck.QuerySet{
			Group:     set.Group,
			Files:     set.Files,
			Tables:    set.Tables,
			Provision: provisioning[set.Group],
		})
	}

	cfg.Lookups = sqlcheckLookups(m)

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

func sqlcheckLookups(m Manifest) []sqlcheck.Lookup {
	var lookups []sqlcheck.Lookup

	for _, lookup := range m.allLookups() {
		lookups = append(lookups, sqlcheck.Lookup{
			Table:   lookup.Table,
			Key:     lookup.Key,
			Policy:  lookup.PolicyName(),
			Setting: lookup.Setting(),
		})
	}

	if root, ok := m.TenantRoot(); ok && m.hasEachTenant() {
		lookups = append(lookups, sqlcheck.Lookup{
			Table:     root.Name,
			Key:       root.TenantColumn,
			Policy:    tenantIndexPolicy,
			Setting:   tenantIndexSetting,
			Enumerate: true,
		})
	}

	if index, ok := m.MemberIndexOf(); ok {
		lookups = append(lookups, sqlcheck.Lookup{
			Table:   index.Table,
			Key:     index.Subject,
			Policy:  index.PolicyName(),
			Setting: index.Setting(),
		})
	}

	return lookups
}
