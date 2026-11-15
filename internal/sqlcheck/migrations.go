package sqlcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultTenantSetting = "app.tenant_id"

// CheckMigrations verifies every tenant-scoped declared table has FORCE ROW
// LEVEL SECURITY, a tenant policy on all commands, no wider permissive
// policy, and, when append-only, enabled no-update and no-delete triggers.
func CheckMigrations(cfg Config) ([]Diagnostic, error) {
	setting := cfg.TenantSetting
	if setting == "" {
		setting = defaultTenantSetting
	}

	state := newMigrationState(strings.ToLower(setting))
	for _, table := range cfg.Tables {
		state.columns[tableKey(table.Name)] = table.TenantColumn
	}

	for _, lookup := range cfg.Lookups {
		state.lookups[strings.ToLower(lookup.Policy)] = lookup
	}

	var diagnostics []Diagnostic

	files, err := migrationFiles(cfg)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(cfg.Root, file))
		if err != nil {
			return nil, err
		}

		tokens, ok := tokenize(upSection(string(data)))
		if !ok {
			diagnostics = append(diagnostics, newDiagnostic(
				file,
				1,
				"migration cannot be tokenized (unterminated string or "+
					"comment), so its RLS state cannot be verified.",
				"Fix the migration so it is valid SQL.",
			))

			continue
		}

		for _, statement := range splitStatements(tokens) {
			state.apply(file, statement)
		}
	}

	return append(diagnostics, state.evaluate(cfg)...), nil
}

func migrationFiles(cfg Config) ([]string, error) {
	var files []string

	for _, dir := range cfg.SchemaDirs {
		entries, err := os.ReadDir(filepath.Join(cfg.Root, dir))
		if err != nil {
			return nil, fmt.Errorf("read migrations %s: %w", dir, err)
		}

		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".sql") ||
				strings.HasSuffix(name, ".down.sql") {
				continue
			}

			files = append(files, filepath.ToSlash(filepath.Join(dir, name)))
		}
	}

	sort.Strings(files)

	return files, nil
}

// upSection drops everything from a goose or dbmate down marker onwards,
// keeping line numbers intact for the part that remains.
func upSection(source string) string {
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		marker := strings.ToLower(strings.Join(strings.Fields(line), " "))
		if marker == "-- +goose down" || marker == "-- migrate:down" {
			return strings.Join(lines[:i], "\n")
		}
	}

	return source
}

func splitStatements(tokens []token) [][]token {
	var statements [][]token

	start := 0

	for i, tok := range tokens {
		if tok.punct(";") {
			if i > start {
				statements = append(statements, tokens[start:i])
			}

			start = i + 1
		}
	}

	if start < len(tokens) {
		statements = append(statements, tokens[start:])
	}

	return statements
}

// evaluate turns the final migration state into diagnostics.
func (s *migrationState) evaluate(cfg Config) []Diagnostic {
	var diagnostics []Diagnostic

	for _, declared := range cfg.Tables {
		if declared.TenantColumn == "" {
			continue
		}

		state := s.table(declared.Name)
		if state == nil {
			diagnostics = append(diagnostics, newDiagnostic(
				"pluto.toml",
				1,
				fmt.Sprintf(
					"declared table %q is not created by any migration.",
					declared.Name,
				),
				"Add the CREATE TABLE migration or fix the declaration.",
			))

			continue
		}

		diagnostics = append(
			diagnostics,
			s.evaluateTable(declared, state)...,
		)
	}

	return diagnostics
}

func (s *migrationState) evaluateTable(
	declared Table,
	state *tableState,
) []Diagnostic {
	add := func(file string, line int, message, repair string) Diagnostic {
		return newDiagnostic(file, line, message, repair)
	}

	var diagnostics []Diagnostic
	if !state.Columns[tableKey(declared.TenantColumn)] {
		diagnostics = append(diagnostics, add(
			state.File,
			state.Line,
			fmt.Sprintf(
				"table %s has no tenant column %s.",
				declared.Name,
				declared.TenantColumn,
			),
			"Add the NOT NULL tenant column in a migration.",
		))
	}

	if !state.Enabled || !state.Forced {
		diagnostics = append(diagnostics, add(
			state.File,
			state.Line,
			fmt.Sprintf(
				"table %s must have ENABLE and FORCE ROW LEVEL SECURITY.",
				declared.Name,
			),
			fmt.Sprintf(
				"Add `ALTER TABLE %s ENABLE ROW LEVEL SECURITY` and "+
					"`ALTER TABLE %s FORCE ROW LEVEL SECURITY`.",
				declared.Name,
				declared.Name,
			),
		))
	}

	diagnostics = append(
		diagnostics,
		s.policyDiagnostics(declared, state)...,
	)
	diagnostics = append(
		diagnostics,
		s.lookupDiagnostics(declared, state)...,
	)

	if declared.AppendOnly {
		diagnostics = append(
			diagnostics,
			s.triggerDiagnostics(declared, state)...,
		)
	}

	return diagnostics
}

func (s *migrationState) policyDiagnostics(
	declared Table,
	state *tableState,
) []Diagnostic {
	var diagnostics []Diagnostic

	covered := map[string]bool{}

	names := make([]string, 0, len(state.Policies))
	for name := range state.Policies {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		policy := state.Policies[name]
		if policy.Restrictive {
			continue
		}

		// The declared lookup policy is the one permitted widening: it
		// admits a single SELECT keyed to a transaction-local setting only
		// generated code sets.
		if policy.LookupOK && policy.Command == "select" && !policy.HasCheck {
			continue
		}

		if (policy.HasUsing && !policy.UsingOK) ||
			(policy.HasCheck && !policy.CheckOK) {
			diagnostics = append(diagnostics, newDiagnostic(
				policy.File,
				policy.Line,
				fmt.Sprintf(
					"policy %s on %s is permissive but does not restrict "+
						"%s to current_setting('%s'); permissive "+
						"policies OR together, so it widens access.",
					name,
					declared.Name,
					declared.TenantColumn,
					s.setting,
				),
				fmt.Sprintf(
					"Make it `USING (%s = current_setting('%s', true)"+
						"::<type>)` or mark it AS RESTRICTIVE.",
					declared.TenantColumn,
					s.setting,
				),
			))

			continue
		}

		markCovered(covered, policy)
	}

	for _, command := range []string{"select", "insert", "update", "delete"} {
		if covered[command] {
			continue
		}

		diagnostics = append(diagnostics, newDiagnostic(
			state.File,
			state.Line,
			fmt.Sprintf(
				"table %s has no tenant policy covering %s.",
				declared.Name,
				strings.ToUpper(command),
			),
			fmt.Sprintf(
				"Add `CREATE POLICY ... ON %s USING (%s = "+
					"current_setting('%s', true)::<type>)` (WITH CHECK "+
					"for INSERT).",
				declared.Name,
				declared.TenantColumn,
				s.setting,
			),
		))
	}

	return diagnostics
}

func markCovered(covered map[string]bool, policy *policyState) {
	switch policy.Command {
	case "all":
		if policy.HasUsing {
			for _, command := range []string{
				"select", "insert", "update", "delete",
			} {
				covered[command] = true
			}
		}
	case "select", "delete":
		covered[policy.Command] = policy.HasUsing || covered[policy.Command]
	case "insert":
		covered["insert"] = policy.HasCheck || covered["insert"]
	case "update":
		covered["update"] = policy.HasUsing || covered["update"]
	}
}

func (s *migrationState) triggerDiagnostics(
	declared Table,
	state *tableState,
) []Diagnostic {
	covered := map[string]bool{}

	for _, trigger := range state.Triggers {
		if !trigger.Before || trigger.Disabled ||
			!s.functions[trigger.Function] {
			continue
		}

		for event := range trigger.Events {
			covered[event] = true
		}
	}

	var diagnostics []Diagnostic

	for _, event := range []string{"update", "delete"} {
		if covered[event] {
			continue
		}

		diagnostics = append(diagnostics, newDiagnostic(
			state.File,
			state.Line,
			fmt.Sprintf(
				"append-only table %s has no enabled BEFORE %s trigger "+
					"whose function always raises.",
				declared.Name,
				strings.ToUpper(event),
			),
			"Create a BEFORE UPDATE and BEFORE DELETE trigger that "+
				"executes a function containing RAISE EXCEPTION and "+
				"never returning NEW or OLD.",
		))
	}

	return diagnostics
}

// lookupDiagnostics requires every declared lookup of the table to have its
// exact policy and its key column.
func (s *migrationState) lookupDiagnostics(
	declared Table,
	state *tableState,
) []Diagnostic {
	var diagnostics []Diagnostic

	names := make([]string, 0, len(s.lookups))
	for name := range s.lookups {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		lookup := s.lookups[name]
		if tableKey(lookup.Table) != tableKey(declared.Name) {
			continue
		}

		if !state.Columns[tableKey(lookup.Key)] {
			diagnostics = append(diagnostics, newDiagnostic(
				state.File,
				state.Line,
				fmt.Sprintf(
					"lookup key %s is not a column of table %s.",
					lookup.Key,
					declared.Name,
				),
				"Fix the lookup key or add the column.",
			))
		}

		policy := state.Policies[name]
		if policy != nil && policy.LookupOK && policy.Command == "select" &&
			!policy.HasCheck && !policy.Restrictive {
			continue
		}

		diagnostics = append(diagnostics, newDiagnostic(
			state.File,
			state.Line,
			fmt.Sprintf(
				"table %s has no exact lookup policy %s.",
				declared.Name,
				lookup.Policy,
			),
			fmt.Sprintf(
				"Add `CREATE POLICY %s ON %s FOR SELECT USING (%s = "+
					"current_setting('%s', true))`.",
				lookup.Policy,
				declared.Name,
				lookup.Key,
				lookup.Setting,
			),
		))
	}

	return diagnostics
}
