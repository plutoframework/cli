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
)

const lifecycleStatePackage = "github.com/plutoframework/pluto-core/state"

type lifecycleFile struct {
	path, pkg string
	imports   map[string]string
	tree      *ast.File
}

type lifecycleType struct {
	expr  ast.Expr
	file  *lifecycleFile
	alias bool
}

// This source-only inventory intentionally does not load dependencies or execute
// consumer tooling. It recognizes direct local type declarations and aliases;
// full type checking and external/re-exported lifecycle factories remain separate
// coverage obligations, never implied by an empty diagnostic list.
func inspectLifecycleBoundaries(
	root, module string,
) ([]ArchitectureDiagnostic, []string) {
	diagnostics := []ArchitectureDiagnostic{}

	unassessed := []string{
		"lifecycle inspection resolves direct local types and aliases only; external factories, reflection, unsafe, generic wrappers and full assignment flow are not assessed",
	}
	if module == "" {
		return diagnostics, unassessed
	}

	set := token.NewFileSet()
	files := []*lifecycleFile{}
	types := map[string]lifecycleType{}

	// General inventory diagnostics belong to InspectArchitecture. Match its
	// exclusions here so this pass cannot silently execute or include test code.
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
			dir := filepath.ToSlash(filepath.Dir(rel))

			pkg := module
			if dir != "." {
				pkg += "/" + dir
			}

			file := &lifecycleFile{
				path:    filepath.ToSlash(rel),
				pkg:     pkg,
				tree:    tree,
				imports: map[string]string{},
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

			for _, declaration := range tree.Decls {
				group, ok := declaration.(*ast.GenDecl)
				if !ok {
					continue
				}

				for _, item := range group.Specs {
					if spec, ok := item.(*ast.TypeSpec); ok {
						types[pkg+"."+spec.Name.Name] = lifecycleType{
							spec.Type,
							file,
							spec.Assign.IsValid(),
						}
					}
				}
			}

			return nil
		},
	)

	var name func(ast.Expr, *lifecycleFile) string

	name = func(expr ast.Expr, file *lifecycleFile) string {
		switch value := expr.(type) {
		case *ast.Ident:
			return file.pkg + "." + value.Name
		case *ast.SelectorExpr:
			if prefix, ok := value.X.(*ast.Ident); ok {
				if path := file.imports[prefix.Name]; path != "" {
					return path + "." + value.Sel.Name
				}
			}
		case *ast.StarExpr:
			return name(value.X, file)
		case *ast.ParenExpr:
			return name(value.X, file)
		}

		return ""
	}
	canonical := func(key string) string {
		seen := map[string]bool{}
		for !seen[key] {
			seen[key] = true

			definition, ok := types[key]
			if !ok || !definition.alias {
				break
			}

			key = name(definition.expr, definition.file)
		}

		return key
	}
	states := map[string]bool{}

	for _, file := range files {
		ast.Inspect(file.tree, func(node ast.Node) bool {
			generic, ok := node.(*ast.IndexListExpr)
			if !ok || len(generic.Indices) != 3 {
				return true
			}

			base := name(generic.X, file)
			if base == lifecycleStatePackage+".Definition" ||
				base == lifecycleStatePackage+".Machine" {
				key := canonical(name(generic.Indices[0], file))
				if key != "" {
					states[key] = true
				}
			}

			return true
		})
	}

	add := func(id string, file *lifecycleFile, pos token.Pos, message, repair string) {
		diagnostics = append(diagnostics, ArchitectureDiagnostic{
			ID:       id,
			File:     file.path,
			Line:     set.Position(pos).Line,
			Severity: "error",
			Message:  message,
			Repair:   repair,
		})
	}
	entities := map[string]bool{}

	for key, definition := range types {
		structure, ok := definition.expr.(*ast.StructType)
		if !ok ||
			architectureRole(
				strings.TrimPrefix(definition.file.pkg, module+"/"),
			) != "domain" {
			continue
		}

		for _, field := range structure.Fields.List {
			if !states[canonical(name(field.Type, definition.file))] {
				continue
			}

			entities[key] = true

			for _, fieldName := range field.Names {
				if fieldName.IsExported() {
					add(
						"ARCH301",
						definition.file,
						fieldName.Pos(),
						"publicly mutable lifecycle field: "+key+"."+fieldName.Name,
						"Keep lifecycle state private to its domain owner; expose read access and controlled construction, evaluation and rehydration paths.",
					)
				}
			}

			if len(field.Names) == 0 {
				add(
					"ARCH301",
					definition.file,
					field.Pos(),
					"embedded lifecycle state exposes uncontrolled mutation: "+key,
					"Use a private named lifecycle field with controlled domain construction and mutation.",
				)
			}
		}
	}

	// Resolve declared receiver/parameter types and straightforward local aliases.
	// An unresolved expression is not evidence of a safe write or decision path.
	var machine func(lifecycleType, map[string]bool) bool

	machine = func(value lifecycleType, seen map[string]bool) bool {
		switch expr := value.expr.(type) {
		case *ast.StarExpr:
			return machine(lifecycleType{expr.X, value.file, false}, seen)
		case *ast.IndexListExpr:
			return name(expr.X, value.file) == lifecycleStatePackage+".Machine"
		}

		key := canonical(name(value.expr, value.file))
		if raw := name(value.expr, value.file); types[raw].alias {
			key = raw
		}

		if seen[key] {
			return false
		}

		seen[key] = true
		if next, ok := types[key]; ok {
			return machine(next, seen)
		}

		return false
	}

	var containsEntity func(lifecycleType, map[string]bool) bool

	containsEntity = func(value lifecycleType, seen map[string]bool) bool {
		switch expr := value.expr.(type) {
		case *ast.StarExpr:
			return containsEntity(
				lifecycleType{expr.X, value.file, false},
				seen,
			)
		case *ast.ArrayType:
			return containsEntity(
				lifecycleType{expr.Elt, value.file, false},
				seen,
			)
		case *ast.Ellipsis:
			return containsEntity(
				lifecycleType{expr.Elt, value.file, false},
				seen,
			)
		case *ast.MapType:
			return containsEntity(
				lifecycleType{expr.Key, value.file, false},
				seen,
			) ||
				containsEntity(
					lifecycleType{expr.Value, value.file, false},
					seen,
				)
		case *ast.StructType:
			for _, field := range expr.Fields.List {
				if containsEntity(
					lifecycleType{field.Type, value.file, false},
					seen,
				) {
					return true
				}
			}
		}

		key := name(value.expr, value.file)
		if entities[key] {
			return true
		}

		if seen[key] {
			return false
		}

		seen[key] = true
		if definition, ok := types[key]; ok {
			return containsEntity(definition, seen)
		}

		return false
	}

	globals := map[string]map[string]lifecycleType{}
	for _, file := range files {
		if globals[file.pkg] == nil {
			globals[file.pkg] = map[string]lifecycleType{}
		}

		for _, declaration := range file.tree.Decls {
			group, ok := declaration.(*ast.GenDecl)
			if !ok || group.Tok != token.VAR {
				continue
			}

			for _, item := range group.Specs {
				spec := item.(*ast.ValueSpec)
				if spec.Type == nil {
					continue
				}

				for _, id := range spec.Names {
					globals[file.pkg][id.Name] = lifecycleType{
						spec.Type,
						file,
						false,
					}
				}
			}
		}
	}

	for _, file := range files {
		role := architectureRole(strings.TrimPrefix(file.pkg, module+"/"))
		for _, declaration := range file.tree.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				group, isGroup := declaration.(*ast.GenDecl)
				if !isGroup || group.Tok != token.VAR {
					continue
				}

				// Inspect package-level method captures with the same typed resolver.
				function = &ast.FuncDecl{
					Name: ast.NewIdent(""),
					Type: &ast.FuncType{},
					Body: &ast.BlockStmt{
						List: []ast.Stmt{&ast.DeclStmt{Decl: group}},
					},
				}
			}

			variables := map[string]lifecycleType{}
			for id, value := range globals[file.pkg] {
				variables[id] = value
			}

			for _, list := range []*ast.FieldList{function.Recv, function.Type.Params} {
				if list == nil {
					continue
				}

				for _, field := range list.List {
					for _, id := range field.Names {
						variables[id.Name] = lifecycleType{
							field.Type,
							file,
							false,
						}
					}
				}
			}

			if role == "data" && function.Name.IsExported() &&
				function.Type.Params != nil {
				for _, parameter := range function.Type.Params.List {
					if containsEntity(
						lifecycleType{parameter.Type, file, false},
						map[string]bool{},
					) {
						add(
							"ARCH303",
							file,
							parameter.Pos(),
							"data entry accepts a raw lifecycle entity: "+function.Name.Name,
							"Require an opaque evaluated transition at the lifecycle write boundary; atomically match scope, entity, state and version in persistence. Model initial creation separately.",
						)
					}
				}
			}

			if role == "domain" || function.Body == nil {
				continue
			}

			var resolve func(ast.Expr) lifecycleType

			resolve = func(expr ast.Expr) lifecycleType {
				switch value := expr.(type) {
				case *ast.Ident:
					if variable, ok := variables[value.Name]; ok {
						return variable
					}

					if definition, ok := types[name(value, file)]; ok {
						return definition
					}
				case *ast.ParenExpr:
					return resolve(value.X)
				case *ast.StarExpr:
					return resolve(value.X)
				case *ast.IndexListExpr:
					return lifecycleType{value, file, false}
				case *ast.SelectorExpr:
					owner := resolve(value.X)
					if owner.file == nil {
						return lifecycleType{}
					}

					definition := types[canonical(name(owner.expr, owner.file))]

					structure, ok := definition.expr.(*ast.StructType)
					if !ok {
						return lifecycleType{}
					}

					for _, field := range structure.Fields.List {
						for _, id := range field.Names {
							if id.Name == value.Sel.Name {
								return lifecycleType{
									field.Type,
									definition.file,
									false,
								}
							}
						}
					}
				}

				return lifecycleType{}
			}

			ast.Inspect(function.Body, func(node ast.Node) bool {
				if spec, ok := node.(*ast.ValueSpec); ok {
					for index, id := range spec.Names {
						if spec.Type != nil {
							variables[id.Name] = lifecycleType{
								spec.Type,
								file,
								false,
							}
						} else if index < len(spec.Values) {
							if resolved := resolve(
								spec.Values[index],
							); resolved.file != nil {
								variables[id.Name] = resolved
							}
						}
					}
				}

				if assignment, ok := node.(*ast.AssignStmt); ok &&
					len(assignment.Lhs) == len(assignment.Rhs) {
					for index, left := range assignment.Lhs {
						if id, ok := left.(*ast.Ident); ok {
							if resolved := resolve(
								assignment.Rhs[index],
							); resolved.file != nil {
								variables[id.Name] = resolved
							}
						}
					}
				}

				selector, ok := node.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Next" {
					return true
				}

				value := resolve(selector.X)
				if value.file != nil && machine(value, map[string]bool{}) {
					add(
						"ARCH302",
						file,
						selector.Pos(),
						"raw lifecycle decision escapes domain ownership",
						"Expose a domain-owned evaluator returning an opaque evaluated transition and require it at the write boundary; do not persist a raw Next result.",
					)
				}

				return true
			})
		}
	}

	return diagnostics, unassessed
}
