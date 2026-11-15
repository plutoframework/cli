// Package sqlcheck implements the ARCH612 authored-SQL checks.
//
// The package is self-contained: it takes plain inputs (declared tables,
// authored query sets and migration directories) and returns diagnostics.
// Query analysis runs on the catalog that sqlc itself parsed, captured through
// a sqlc process plugin, so no SQL is matched with regular expressions and no
// cgo parser is linked into the CLI.
package sqlcheck

import (
	"fmt"
	"sort"
)

// RuleID is the architecture rule reported by every diagnostic.
const RuleID = "ARCH612"

// Table is a declared authored-data table.
type Table struct {
	Name string

	// Group owns the table; informational only.
	Group string

	// TenantColumn is the column every query and RLS policy must bind.
	// An empty value declares a table that is not tenant scoped. Tenant
	// scoped tables always require FORCE ROW LEVEL SECURITY and a policy.
	TenantColumn string

	// AppendOnly requires no-update and no-delete triggers.
	AppendOnly bool

	// TenantRoot marks the table whose rows are the tenants: its
	// TenantColumn is its own identifier. Only provisioning query sets may
	// INSERT into it.
	TenantRoot bool
}

// Lookup is one declared tenant-root lookup by unique key. Its migration
// state is exactly one SELECT policy keyed to a transaction-local setting.
type Lookup struct {
	Table   string
	Key     string
	Policy  string
	Setting string
}

// QuerySet is a declared set of authored sqlc query files.
type QuerySet struct {
	Group string

	// Files are glob patterns relative to the project root.
	Files []string

	// Tables lists the declared tables this set may touch.
	Tables []string

	// Provision allows INSERT into the tenant root table: the set belongs
	// to a group whose provisioning operation mints the new tenant.
	Provision bool
}

// Config is the complete input of a check run.
type Config struct {

	// Root is the project directory; all other paths are relative to it.
	Root    string
	Tables  []Table
	Sets    []QuerySet
	Lookups []Lookup

	// SchemaDirs hold the migrations sqlc and the migration scanner read.
	SchemaDirs []string

	// QueryRoots are directories scanned for stray *.sql query files.
	QueryRoots []string

	// SQLCConfig is the managed sqlc configuration verified by sqlc diff.
	// Empty skips the diff step.
	SQLCConfig string

	// SQLCPath overrides the sqlc binary; empty searches PATH.
	SQLCPath string

	// TenantSetting is the per-transaction setting read by RLS policies.
	// Empty means app.tenant_id.
	TenantSetting string
}

// Diagnostic is one ARCH612 finding.
type Diagnostic struct {
	ID       string
	File     string
	Line     int
	Severity string
	Message  string
	Repair   string
}

// String renders the diagnostic in the CLI's file:line style.
func (d Diagnostic) String() string {
	return fmt.Sprintf(
		"%s:%d: %s %s Repair: %s",
		d.File,
		d.Line,
		d.ID,
		d.Message,
		d.Repair,
	)
}

func newDiagnostic(
	file string,
	line int,
	message string,
	repair string,
) Diagnostic {
	if line < 1 {
		line = 1
	}

	return Diagnostic{
		ID:       RuleID,
		File:     file,
		Line:     line,
		Severity: "error",
		Message:  message,
		Repair:   repair,
	}
}

func sortDiagnostics(diagnostics []Diagnostic) {
	sort.SliceStable(diagnostics, func(i, j int) bool {
		left, right := diagnostics[i], diagnostics[j]
		if left.File != right.File {
			return left.File < right.File
		}

		return left.Line < right.Line
	})
}
