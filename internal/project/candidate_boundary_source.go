package project

import (
	"go/ast"
	"go/token"
	"strings"
)

func candidateExpressionName(
	expression ast.Expr,
	file *candidateSource,
) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return file.namespace + "." + value.Name
	case *ast.SelectorExpr:
		if prefix, ok := value.X.(*ast.Ident); ok {
			if path := file.imports[prefix.Name]; path != "" {
				return path + "." + value.Sel.Name
			}
		}
	case *ast.StarExpr:
		return candidateExpressionName(value.X, file)
	case *ast.ParenExpr:
		return candidateExpressionName(value.X, file)
	case *ast.IndexExpr:
		return candidateExpressionName(value.X, file)
	case *ast.IndexListExpr:
		return candidateExpressionName(value.X, file)
	}

	return ""
}

func candidateTypeName(
	value candidateType,
	types map[string]candidateType,
	seen map[string]bool,
) string {
	if value.file == nil || value.expression == nil {
		return ""
	}

	if _, ok := value.expression.(*ast.InterfaceType); ok {
		return "builtin.interface"
	}

	key := candidateExpressionName(value.expression, value.file)
	if key == "" || seen[key] {
		return key
	}

	seen[key] = true
	if definition, ok := types[key]; ok {
		switch definition.expression.(type) {
		case *ast.Ident,
			*ast.SelectorExpr,
			*ast.StarExpr,
			*ast.ParenExpr,
			*ast.IndexExpr,
			*ast.IndexListExpr:
			return candidateTypeName(definition, types, seen)
		}
	}

	return key
}

func inspectCandidateFile(
	file *candidateSource,
	rules candidateRules,
	types, functions map[string]candidateType,
	emit func(string, token.Pos, string, string),
) {
	globals := map[string]candidateType{}

	var resolve func(ast.Expr, map[string]candidateType) candidateType

	resolve = func(expression ast.Expr, variables map[string]candidateType) candidateType {
		switch value := expression.(type) {
		case *ast.Ident:
			return variables[value.Name]
		case *ast.ParenExpr:
			return resolve(value.X, variables)
		case *ast.StarExpr:
			return resolve(value.X, variables)
		case *ast.UnaryExpr:
			return resolve(value.X, variables)
		case *ast.CompositeLit:
			return candidateType{value.Type, file}
		case *ast.TypeAssertExpr:
			return candidateType{value.Type, file}
		case *ast.CallExpr:
			if id, ok := value.Fun.(*ast.Ident); ok && id.Name == "new" &&
				len(value.Args) == 1 {
				return candidateType{value.Args[0], file}
			}

			key := candidateExpressionName(value.Fun, file)
			if result, ok := functions[key]; ok {
				return result
			}

			if _, ok := types[key]; ok {
				return candidateType{value.Fun, file}
			}

			if key == lifecycleStatePackage+".New" {
				base := value.Fun
				switch indexed := base.(type) {
				case *ast.IndexExpr:
					base = indexed.X
				case *ast.IndexListExpr:
					base = indexed.X
				}

				if selector, ok := base.(*ast.SelectorExpr); ok {
					return candidateType{
						&ast.SelectorExpr{
							X:   selector.X,
							Sel: ast.NewIdent("Machine"),
						},
						file,
					}
				}
			}
		case *ast.SelectorExpr:
			owner := resolve(value.X, variables)
			key := candidateTypeName(owner, types, map[string]bool{})

			definition, ok := types[key]
			if !ok {
				return candidateType{}
			}

			structure, ok := definition.expression.(*ast.StructType)
			if !ok {
				return candidateType{}
			}

			for _, field := range structure.Fields.List {
				for _, name := range field.Names {
					if name.Name == value.Sel.Name {
						return candidateType{field.Type, definition.file}
					}
				}
			}
		case *ast.IndexExpr:
			owner := resolve(value.X, variables)
			if array, ok := owner.expression.(*ast.ArrayType); ok {
				return candidateType{array.Elt, owner.file}
			}
		}

		return candidateType{}
	}
	recordValues := func(spec *ast.ValueSpec, variables map[string]candidateType) {
		for index, name := range spec.Names {
			if spec.Type != nil {
				variables[name.Name] = candidateType{spec.Type, file}
				continue
			}

			if index < len(spec.Values) {
				variables[name.Name] = resolve(spec.Values[index], variables)
			}
		}
	}

	for _, node := range file.tree.Decls {
		if declaration, ok := node.(*ast.GenDecl); ok &&
			declaration.Tok == token.VAR {
			for _, item := range declaration.Specs {
				if spec, ok := item.(*ast.ValueSpec); ok {
					recordValues(spec, globals)
				}
			}
		}
	}

	selectorNames := map[token.Pos]bool{}

	ast.Inspect(file.tree, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok {
			selectorNames[selector.Sel.Pos()] = true
		}

		return true
	})

	inspect := func(node ast.Node, variables map[string]candidateType) {
		switch value := node.(type) {
		case *ast.Ident:
			if owner := rules.privateNames[file.directory][value.Name]; owner != "" &&
				owner != file.path {
				emit(
					"ARCH604",
					value.Pos(),
					"reserved private candidate field referenced outside its owner: "+value.Name,
					"Use the declared domain constructor, getters and operation workflow; do not assign or capture generated private storage.",
				)
			}

			if selectorNames[value.Pos()] {
				return
			}

			if owners, restricted := rules.capabilities[file.namespace+"."+value.Name]; restricted &&
				!owners[file.path] {
				emit(
					"ARCH603",
					value.Pos(),
					"candidate capability referenced outside its exact owner: "+value.Name,
					"Invoke the managed operation or composition factory; do not capture, redeclare or alias restricted constructors/evaluators.",
				)
			}
		case *ast.SelectorExpr:
			key := candidateExpressionName(value, file)
			if key == "github.com/plutoframework/pluto-auth.Manager" ||
				key == "github.com/plutoframework/pluto-auth.SessionStore" ||
				key == "github.com/plutoframework/pluto-auth.AccountSessionStore" {
				emit(
					"ARCH606",
					value.Pos(),
					"trusted session issuer escapes canonical authentication composition",
					"Use CandidateRuntime.WithSession or the managed HTTP/job caller bridge; production extensions must not receive or replace the session issuer.",
				)
			}

			resolvedValue := candidateTypeName(
				resolve(value, variables),
				types,
				map[string]bool{},
			)
			if resolvedValue == "github.com/plutoframework/pluto-auth.Manager" {
				emit(
					"ARCH606",
					value.Pos(),
					"trusted session issuer value escapes its managed owner",
					"Keep the concrete Manager private; an interface or alias must not expose string-based session issuance.",
				)
			}

			if value.Sel.Name == "BoundaryManager" {
				receiver := candidateTypeName(
					resolve(value.X, variables),
					types,
					map[string]bool{},
				)
				if strings.HasPrefix(receiver, rules.transportNamespace) ||
					receiver == "" {
					emit(
						"ARCH606",
						value.Pos(),
						"session issuer getter referenced outside managed composition",
						"Use the managed runtime's opaque caller flow; do not capture the issuer getter.",
					)
				}
			}

			if value.Sel.Name == "Start" ||
				value.Sel.Name == "StartVersioned" ||
				value.Sel.Name == "StartAuthenticated" ||
				value.Sel.Name == "WithIdentity" ||
				value.Sel.Name == "UseIdentity" ||
				value.Sel.Name == "Authenticate" {
				receiver := candidateTypeName(
					resolve(value.X, variables),
					types,
					map[string]bool{},
				)
				if receiver == "github.com/plutoframework/pluto-auth.Manager" {
					emit(
						"ARCH606",
						value.Pos(),
						"session issuer method referenced outside its managed authentication owner",
						"Use the configured credential flow and runtime caller adapter; do not mint identity from application-supplied subject strings.",
					)
				}
			}

			if owners, restricted := rules.capabilities[key]; restricted &&
				!owners[file.path] {
				emit(
					"ARCH603",
					value.Pos(),
					"candidate capability referenced outside its exact owner: "+key,
					"Keep rehydration in managed data, lifecycle evaluation in the owning workflow and construction in managed composition.",
				)
			}

			if rules.protectedTypes[key] &&
				strings.Contains(key, "/internal/ports/") {
				emit(
					"ARCH604",
					value.Pos(),
					"raw candidate data port escapes managed construction",
					"Consume the sealed operation boundary; do not substitute, alias or implement a raw resource data interface in production extensions.",
				)
			}

			if value.Sel.Name == "Evaluate" || value.Sel.Name == "Next" {
				if prefix, ok := value.X.(*ast.Ident); ok &&
					file.imports[prefix.Name] != "" {
					return
				}

				receiver := candidateTypeName(
					resolve(value.X, variables),
					types,
					map[string]bool{},
				)
				if receiver == lifecycleStatePackage+".Machine" {
					emit(
						"ARCH603",
						value.Pos(),
						"raw lifecycle evaluation escapes the managed domain/workflow boundary",
						"Use the declared resource evaluator and its scoped evaluated change; do not capture raw Machine methods.",
					)
				} else if _, known := types[receiver]; receiver == "" || (!known && !strings.HasPrefix(receiver, "builtin.")) {
					emit(
						"ARCH608",
						value.Pos(),
						"candidate evaluator receiver cannot be resolved safely",
						"Use an explicit typed domain receiver or the declared resource evaluator; unresolved lifecycle factories are unsupported.",
					)
				}
			}

		case *ast.TypeSpec:
			key := candidateTypeName(
				candidateType{value.Type, file},
				types,
				map[string]bool{},
			)
			if key == lifecycleStatePackage+".Machine" {
				emit(
					"ARCH603",
					value.Pos(),
					"raw machine type is aliased outside its managed lifecycle owner",
					"Use the declared resource evaluator rather than hiding a raw Machine behind local or defined aliases.",
				)
			}

			if rules.protectedTypes[key] {
				emit(
					"ARCH604",
					value.Pos(),
					"candidate capability type is aliased or redefined outside its owner",
					"Use the concrete managed boundary directly; aliases and structurally equivalent wrappers are unsupported.",
				)
			}
		case *ast.StructType:
			for _, field := range value.Fields.List {
				if len(field.Names) == 0 {
					key := candidateTypeName(
						candidateType{field.Type, file},
						types,
						map[string]bool{},
					)
					if rules.protectedTypes[key] {
						emit(
							"ARCH604",
							field.Pos(),
							"embedded candidate capability can replace its managed entry",
							"Keep a named sealed-boundary dependency; do not promote private methods through embedded or aliased capability types.",
						)
					}
				}
			}
		case *ast.InterfaceType:
			for _, field := range value.Methods.List {
				if len(field.Names) == 0 {
					key := candidateTypeName(
						candidateType{field.Type, file},
						types,
						map[string]bool{},
					)
					if rules.protectedTypes[key] {
						emit(
							"ARCH604",
							field.Pos(),
							"candidate capability interface is embedded outside its owner",
							"Use the canonical sealed interface directly rather than creating a replacement authority interface.",
						)
					}
				}
			}
		case *ast.CompositeLit:
			key := candidateTypeName(
				candidateType{value.Type, file},
				types,
				map[string]bool{},
			)
			if rules.protectedTypes[key] {
				emit(
					"ARCH604",
					value.Pos(),
					"candidate state or capability constructed outside its owner",
					"Use the configured issuer, managed factory or declared domain constructor; zero/composite values do not establish authority.",
				)
			}
		case *ast.CallExpr:
			key := candidateTypeName(
				candidateType{value.Fun, file},
				types,
				map[string]bool{},
			)
			if id, ok := value.Fun.(*ast.Ident); ok && id.Name == "new" &&
				len(value.Args) == 1 {
				key = candidateTypeName(
					candidateType{value.Args[0], file},
					types,
					map[string]bool{},
				)
			}

			if rules.protectedTypes[key] {
				emit(
					"ARCH604",
					value.Pos(),
					"candidate state or capability converted/allocated outside its owner",
					"Use the managed constructor rather than conversions, new or a forged structurally equivalent value.",
				)
			}
		}
	}

	// A raw definition can mint evaluated transitions even when its result is
	// hidden behind a local function, alias or method expression.
	ast.Inspect(file.tree, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		prefix, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}

		if file.imports[prefix.Name] == lifecycleStatePackage &&
			(selector.Sel.Name == "New" || selector.Sel.Name == "Definition" || selector.Sel.Name == "Transition" || selector.Sel.Name == "Evaluated") {
			emit(
				"ARCH603",
				selector.Pos(),
				"raw lifecycle construction exists outside the declared managed domain",
				"Declare the lifecycle in resource metadata; do not mint another machine or transition catalog in application extensions.",
			)
		}

		return true
	})

	clone := func(source map[string]candidateType) map[string]candidateType {
		copy := map[string]candidateType{}
		for key, value := range source {
			copy[key] = value
		}

		return copy
	}

	var walk func(ast.Node, map[string]candidateType)

	walk = func(node ast.Node, variables map[string]candidateType) {
		if node == nil {
			return
		}

		if block, ok := node.(*ast.BlockStmt); ok {
			local := clone(variables)
			for _, statement := range block.List {
				walk(statement, local)
			}

			return
		}

		if literal, ok := node.(*ast.FuncLit); ok {
			local := clone(variables)

			if literal.Type.Params != nil {
				for _, field := range literal.Type.Params.List {
					for _, name := range field.Names {
						local[name.Name] = candidateType{field.Type, file}
					}
				}
			}

			walk(literal.Type, variables)
			walk(literal.Body, local)

			return
		}

		switch value := node.(type) {
		case *ast.IfStmt,
			*ast.ForStmt,
			*ast.SwitchStmt,
			*ast.TypeSwitchStmt,
			*ast.CaseClause:
			variables = clone(variables)
		case *ast.RangeStmt:
			variables = clone(variables)

			if value.Tok == token.DEFINE {
				if name, ok := value.Key.(*ast.Ident); ok {
					variables[name.Name] = candidateType{}
				}

				if name, ok := value.Value.(*ast.Ident); ok {
					variables[name.Name] = candidateType{}

					owner := resolve(value.X, variables)
					if array, ok := owner.expression.(*ast.ArrayType); ok {
						variables[name.Name] = candidateType{
							array.Elt,
							owner.file,
						}
					}
				}
			}
		}

		inspect(node, variables)
		ast.Inspect(node, func(child ast.Node) bool {
			if child == node {
				return true
			}

			if child != nil {
				walk(child, variables)
			}

			return false
		})

		switch value := node.(type) {
		case *ast.ValueSpec:
			recordValues(value, variables)
		case *ast.AssignStmt:
			for index, left := range value.Lhs {
				if name, ok := left.(*ast.Ident); ok && index < len(value.Rhs) {
					variables[name.Name] = resolve(value.Rhs[index], variables)
				}
			}
		}
	}

	for _, declaration := range file.tree.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok {
			variables := map[string]candidateType{}
			for name, value := range globals {
				variables[name] = value
			}

			for _, list := range []*ast.FieldList{function.Recv, function.Type.Params} {
				if list != nil {
					for _, field := range list.List {
						for _, name := range field.Names {
							variables[name.Name] = candidateType{
								field.Type,
								file,
							}
						}
					}
				}
			}

			// Inspect the signature too: method expressions/capability aliases are not
			// exempt merely because they occur outside the function body.
			inspect(function, variables)
			ast.Inspect(function.Type, func(node ast.Node) bool {
				if node != nil {
					inspect(node, variables)
				}

				return true
			})

			if function.Body != nil {
				walk(function.Body, variables)
			}
		} else {
			ast.Inspect(declaration, func(node ast.Node) bool {
				if node != nil {
					inspect(node, globals)
				}

				return true
			})
		}
	}
}
