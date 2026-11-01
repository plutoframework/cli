package project

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type primitiveSource struct {
	path, directory, role string
	imports               map[string]string
	tree                  *ast.File
}

type primitiveType struct {
	expression ast.Expr
	file       *primitiveSource
}

// inspectPrimitiveBoundaries checks concrete capability placement and declaration
// prerequisites. It never infers that arbitrary SQL implements visibility or
// atomic replay merely because a corresponding primitive was imported.
func inspectPrimitiveBoundaries(
	root, module string,
	manifest []byte,
) ([]ArchitectureDiagnostic, []string) {
	diagnostics := []ArchitectureDiagnostic{}
	unassessed := []string{
		"primitive inspection checks known source capabilities and declaration prerequisites; arbitrary normalization, SQL visibility, cache authority and transaction semantics require typed adapters and runtime proofs",
	}

	var declaration struct {
		Operations []struct {
			Group       string `toml:"group"`
			Action      string `toml:"action"`
			Write       bool   `toml:"write"`
			Idempotency string `toml:"idempotency"`
		} `toml:"operations"`
		Jobs []struct {
			Operation string `toml:"operation"`
		} `toml:"jobs"`
	}

	_ = toml.Unmarshal(manifest, &declaration)
	set := token.NewFileSet()
	files := []*primitiveSource{}
	byDirectory := map[string][]*primitiveSource{}
	types := map[string]primitiveType{}
	_ = filepath.WalkDir(
		root,
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			if entry.IsDir() {
				switch entry.Name() {
				case ".git", "node_modules", "vendor", "testdata", "dist":
					return filepath.SkipDir
				}

				if path != root {
					if _, err := os.Stat(
						filepath.Join(path, "go.mod"),
					); err == nil {
						return filepath.SkipDir
					}
				}

				return nil
			}

			if entry.Type()&fs.ModeSymlink != 0 ||
				!strings.HasSuffix(path, ".go") ||
				strings.HasSuffix(path, "_test.go") {
				return nil
			}

			tree, err := parser.ParseFile(set, path, nil, 0)
			if err != nil {
				return nil
			}

			rel, _ := filepath.Rel(root, path)
			directory := filepath.ToSlash(filepath.Dir(rel))

			file := &primitiveSource{
				path:      filepath.ToSlash(rel),
				directory: directory,
				role:      architectureRole(directory),
				imports:   map[string]string{},
				tree:      tree,
			}
			for _, spec := range tree.Imports {
				value, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					continue
				}

				name := filepath.Base(value)
				if spec.Name != nil {
					name = spec.Name.Name
				}

				file.imports[name] = value
			}

			files = append(files, file)
			byDirectory[directory] = append(byDirectory[directory], file)

			for _, declaration := range tree.Decls {
				group, ok := declaration.(*ast.GenDecl)
				if !ok {
					continue
				}

				for _, item := range group.Specs {
					if spec, ok := item.(*ast.TypeSpec); ok {
						types[directory+"."+spec.Name.Name] = primitiveType{
							spec.Type,
							file,
						}
					}
				}
			}

			return nil
		},
	)

	var unboundedSlice func(ast.Expr, *primitiveSource, map[string]bool) bool

	unboundedSlice = func(expression ast.Expr, file *primitiveSource, seen map[string]bool) bool {
		key := ""

		switch value := expression.(type) {
		case *ast.ArrayType:
			return value.Len == nil
		case *ast.StarExpr:
			return unboundedSlice(value.X, file, seen)
		case *ast.ParenExpr:
			return unboundedSlice(value.X, file, seen)
		case *ast.IndexExpr:
			return unboundedSlice(value.X, file, seen)
		case *ast.IndexListExpr:
			return unboundedSlice(value.X, file, seen)
		case *ast.Ident:
			key = file.directory + "." + value.Name
		case *ast.SelectorExpr:
			if prefix, ok := value.X.(*ast.Ident); ok && module != "" &&
				strings.HasPrefix(file.imports[prefix.Name], module+"/") {
				key = strings.TrimPrefix(
					file.imports[prefix.Name],
					module+"/",
				) + "." + value.Sel.Name
			}
		}

		if key == "" || seen[key] {
			return false
		}

		seen[key] = true
		if definition, ok := types[key]; ok {
			return unboundedSlice(definition.expression, definition.file, seen)
		}

		return false
	}

	// Unclassified helpers retain the restrictions of their operation consumer.
	// A recognized domain/data boundary keeps its own role; normal dependency
	// checks independently reject an operation reaching a concrete data package.
	reached := map[string]bool{}

	var visit func(string)

	visit = func(directory string) {
		if reached[directory] {
			return
		}

		reached[directory] = true
		for _, file := range byDirectory[directory] {
			for _, dependency := range file.imports {
				if module != "" && strings.HasPrefix(dependency, module+"/") {
					local := strings.TrimPrefix(dependency, module+"/")
					if architectureRole(local) == "unknown" ||
						architectureRole(local) == "operations" {
						visit(local)
					}
				}
			}
		}
	}
	hasOperations := false

	for _, file := range files {
		if file.role == "operations" {
			hasOperations = true

			visit(file.directory)
		}
	}

	declared := map[string]bool{}

	replayDeclared := len(declaration.Operations) > 0
	for _, operation := range declaration.Operations {
		declared[operation.Group+"."+operation.Action] = true
		if operation.Write && operation.Idempotency != "required" &&
			operation.Idempotency != "exempt" {
			replayDeclared = false
		}
	}

	effectsDeclared := len(declaration.Jobs) > 0
	for _, job := range declaration.Jobs {
		if !declared[job.Operation] {
			effectsDeclared = false
		}
	}

	add := func(id string, file *primitiveSource, pos token.Pos, message, repair string) {
		diagnostics = append(
			diagnostics,
			ArchitectureDiagnostic{
				ID:         id,
				File:       file.path,
				Line:       set.Position(pos).Line,
				Severity:   "error",
				Message:    message,
				Repair:     repair,
				SourceRole: file.role,
			},
		)
	}
	for _, file := range files {
		operationCode := file.role == "operations" ||
			file.role == "unknown" && reached[file.directory]
		for alias, dependency := range file.imports {
			if alias == "." &&
				(dependency == "strings" || strings.HasPrefix(dependency, "github.com/plutoframework/pluto-core/") || dependency == "github.com/plutoframework/pluto-jobs") {
				add(
					"ARCH509",
					file,
					file.tree.Pos(),
					"dot import hides primitive capability ownership",
					"Use an explicit import name so the canonical capability checks can resolve the owning package.",
				)
			}
		}

		ast.Inspect(file.tree, func(node ast.Node) bool {
			if function, ok := node.(*ast.FuncDecl); ok &&
				file.role == "operations" &&
				function.Name.IsExported() &&
				function.Type.Results != nil {
				for _, field := range function.Type.Results.List {
					if unboundedSlice(field.Type, file, map[string]bool{}) {
						add(
							"ARCH505",
							file,
							field.Pos(),
							"operation entry exposes an unbounded slice result",
							"Expose a declared bounded collection or capped export result through the operation contract; apply visibility in the scoped query before limiting rows.",
						)
					}
				}
			}

			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			prefix, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}

			dependency := file.imports[prefix.Name]

			capability := selector.Sel.Name
			if operationCode && dependency == "strings" {
				switch capability {
				case "TrimSpace",
					"Trim",
					"TrimPrefix",
					"TrimSuffix",
					"ToLower",
					"ToUpper":
					add(
						"ARCH501",
						file,
						selector.Pos(),
						"normalization capability is authored outside the domain owner",
						"Normalize and validate through a domain constructor producing a controlled value; operations consume that value instead of repeating normalization.",
					)
				}
			}

			if operationCode &&
				dependency == "github.com/plutoframework/pluto-core/cache" {
				switch capability {
				case "NewLoader",
					"DefaultLoaderOptions",
					"Loader",
					"Key",
					"Scope",
					"Store":
					add(
						"ARCH502",
						file,
						selector.Pos(),
						"cache mechanics escape the data boundary",
						"Inject a restricted scoped data port; the data adapter owns keys, TTL and loading, with live authorization before every access including hits.",
					)
				}
			}

			if dependency == "github.com/plutoframework/pluto-core/httpidem" &&
				capability == "New" &&
				hasOperations &&
				!replayDeclared {
				add(
					"ARCH503",
					file,
					selector.Pos(),
					"HTTP replay lacks declared operation idempotency semantics",
					"Declare each write operation's idempotency mode and checked exemption where applicable; transactional completion requires the constrained data commit, not HTTP response capture.",
				)
			}

			if operationCode &&
				dependency == "github.com/plutoframework/pluto-core/pagination" &&
				capability == "Finish" {
				add(
					"ARCH504",
					file,
					selector.Pos(),
					"collection finishing escapes the scoped data contract",
					"Use a bounded data query port that applies resource visibility before pagination and returns the declared page; use the same visibility contract for capped exports.",
				)
			}

			if dependency == "github.com/plutoframework/pluto-jobs" &&
				(capability == "NewRequest" || capability == "Decode") &&
				!effectsDeclared {
				add(
					"ARCH506",
					file,
					selector.Pos(),
					"job payload handling lacks a declared operation-bound effect",
					"Declare the typed effect and its target operation; use the managed entry binding, strict payload validation, transactional enqueue and bounded retry contract.",
				)
			}

			return true
		})
	}

	return diagnostics, unassessed
}
