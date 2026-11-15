package project

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// The adapter role is a declared framework extension: authored data access
// for one group. Like the credential seam, files are not trusted by content,
// only restricted by capability. They may reach the group's sqlc package and
// scope, never a pool, driver or another group.

const (
	adapterPgtype     = "github.com/jackc/pgx/v5/pgtype"
	adapterPagination = "github.com/plutoframework/pluto-core/pagination"
)

// candidateAdapterRole is the declaration subset the role check reads.
type candidateAdapterRole struct {
	Ports    []Port    `toml:"ports"`
	Authored *Authored `toml:"authored"`
}

// adapterDirectories maps each declared adapter directory to its group.
func adapterDirectories(metadata []byte) (map[string]string, []string) {
	var role candidateAdapterRole

	if err := toml.Unmarshal(metadata, &role); err != nil {
		return nil, nil
	}

	directories := map[string]string{}

	for _, port := range role.Ports {
		if group, _, ok := splitQualified(port.Adapter); ok {
			directories["internal/adapters/"+group] = group
		}
	}

	var modules []string

	if role.Authored != nil {
		for _, dependency := range role.Authored.Dependencies {
			modules = append(modules, dependency.Module)
		}
	}

	return directories, modules
}

func inspectCandidateAdapter(
	file *candidateSource,
	group, module string,
	declared []string,
	emit func(id string, pos token.Pos, message, repair string),
) {
	for _, spec := range file.tree.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || candidateAdapterImportAllowed(
			path,
			group,
			module,
			declared,
		) {
			continue
		}

		emit(
			"ARCH610",
			spec.Pos(),
			"authored adapter reaches a restricted capability: "+path,
			"Adapters may import scope, their own sqlc package under "+
				"internal/storage/queries/"+group+", the group's rules, "+
				"models and ports, pgx pgtype, pagination, the standard "+
				"library and declared authored dependencies. Never hold a "+
				"pool or driver; obtain queries through the generated queriesOf helper.",
		)
	}

	inspectAuthoredShape(file, "adapter", emit)
	inspectAdapterQueryAccess(file, emit)
}

// inspectAdapterQueryAccess rejects every route from authored adapter code to
// the transaction behind the sqlc package: its exported DBTX interface, its
// WithTx method, and a caller-chosen constructor for scope.Queries. Adapters
// get their handle from the generated queriesOf helper only.
func inspectAdapterQueryAccess(
	file *candidateSource,
	emit func(id string, pos token.Pos, message, repair string),
) {
	ast.Inspect(file.tree, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		switch selector.Sel.Name {
		case "DBTX", "WithTx":
			emit(
				"ARCH611",
				selector.Pos(),
				"authored adapter reaches the sqlc transaction handle: "+
					selector.Sel.Name,
				"Use queriesOf(tx) and the generated query methods only; "+
					"DBTX and WithTx expose a raw transaction.",
			)
		case "Queries":
			if ident, ok := selector.X.(*ast.Ident); ok &&
				ident.Name == "scope" {
				emit(
					"ARCH603",
					selector.Pos(),
					"authored adapter chooses its own scope.Queries "+
						"constructor",
					"Call queriesOf(tx); the generated helper is the "+
						"only caller of scope.Queries.",
				)
			}
		}

		return true
	})
}

// inspectAuthoredShape enforces the shape rules shared by authored roles: no
// init, no package-level variables and no goroutines.
func inspectAuthoredShape(
	file *candidateSource,
	role string,
	emit func(id string, pos token.Pos, message, repair string),
) {
	for _, decl := range file.tree.Decls {
		switch value := decl.(type) {
		case *ast.FuncDecl:
			if value.Recv == nil && value.Name.Name == "init" {
				emit(
					"ARCH611",
					value.Pos(),
					"authored "+role+" declares init",
					"Remove init; authored code is invoked only from "+
						"generated wiring.",
				)
			}
		case *ast.GenDecl:
			if value.Tok == token.VAR && !blankAssertion(value) {
				emit(
					"ARCH611",
					value.Pos(),
					"authored "+role+" declares a package-level variable",
					"Use constants or pass state through the adapter "+
						"struct.",
				)
			}
		}
	}

	ast.Inspect(file.tree, func(node ast.Node) bool {
		if statement, ok := node.(*ast.GoStmt); ok {
			emit(
				"ARCH611",
				statement.Pos(),
				"authored "+role+" starts a goroutine",
				"An Execution is valid only inside its gate callback; do "+
					"not outlive it.",
			)
		}

		return true
	})
}

// blankAssertion allows `var _ T = x` compile-time assertions.
func blankAssertion(declaration *ast.GenDecl) bool {
	for _, spec := range declaration.Specs {
		value, ok := spec.(*ast.ValueSpec)
		if !ok {
			return false
		}

		for _, name := range value.Names {
			if name.Name != "_" {
				return false
			}
		}
	}

	return true
}

func candidateAdapterImportAllowed(
	path, group, module string,
	declared []string,
) bool {
	switch {
	case path == "C" || path == "unsafe" || path == "reflect" ||
		path == "plugin":
		return false
	case path == module || strings.HasPrefix(path, module+"/"):
		local := strings.TrimPrefix(path, module+"/")

		return local == "internal/scope" ||
			local == CandidateQueryPackage(group) ||
			local == "internal/rules/"+group ||
			local == "internal/models/"+group ||
			local == "internal/ports/"+group
	case path == adapterPgtype || path == adapterPagination:
		return true
	case strings.HasPrefix(path, "github.com/plutoframework/"):
		return false
	case candidateDataPackage(path):
		return false
	}

	first, _, _ := strings.Cut(path, "/")
	if !strings.Contains(first, ".") {
		return candidateStdlibEffectFree(path)
	}

	for _, module := range declared {
		if path == module || strings.HasPrefix(path, module+"/") {
			return true
		}
	}

	return false
}

// candidateStdlibEffectFree rejects standard-library packages that reach the
// process, network or database. Everything else is pure enough for an adapter.
func candidateStdlibEffectFree(path string) bool {
	for _, banned := range []string{
		"os", "net", "syscall", "database/sql", "runtime/debug",
		"plugin", "log/syslog",
	} {
		if path == banned || strings.HasPrefix(path, banned+"/") {
			return false
		}
	}

	return true
}
