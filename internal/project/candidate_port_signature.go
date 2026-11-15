package project

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

// portMethod is one declared method of an authored port. A declaration reads
// like a Go interface method without the leading context parameter, for
// example:
//
//	Query(scope.Execution, rules.Filter) (models.Page, error)
//
// The generated interface adds context.Context first. Parameters are types
// only, the first is scope.Execution or scope.Tx, and the last result is
// error, so authored adapters can never receive a raw pool or tenant string
// through a port.
type portMethod struct {
	Name    string
	Params  []string
	Results []string
	Spec    string
}

// portQualifiers maps a type qualifier allowed in a port signature to the
// import path it resolves to. The path is empty for packages whose location
// depends on the port group; resolvePortImport fills those in.
var portQualifiers = map[string]string{
	"scope":      "internal/scope",
	"models":     "internal/models/",
	"rules":      "internal/rules/",
	"pagination": "github.com/plutoframework/pluto-core/pagination",
	"time":       "time",
	"context":    "context",
}

var portBuiltinTypes = map[string]bool{
	"string": true, "int": true, "int32": true, "int64": true,
	"bool": true, "byte": true, "error": true, "float64": true,
}

func parsePortMethod(spec string) (portMethod, error) {
	method := portMethod{Spec: spec}

	trimmed := strings.TrimSpace(spec)
	if trimmed == "" || strings.ContainsAny(trimmed, "\n;{}") {
		return method, fmt.Errorf("method %q is not a signature", spec)
	}

	source := "package p\ntype I interface{ " + trimmed + " }\n"

	tree, err := parser.ParseFile(
		token.NewFileSet(),
		"port.go",
		source,
		parser.SkipObjectResolution,
	)
	if err != nil {
		return method, fmt.Errorf("method %q: %w", spec, err)
	}

	iface := interfaceOf(tree)
	if iface == nil || len(iface.Methods.List) != 1 ||
		len(iface.Methods.List[0].Names) != 1 {
		return method, fmt.Errorf("method %q must declare one method", spec)
	}

	field := iface.Methods.List[0]

	signature, ok := field.Type.(*ast.FuncType)
	if !ok {
		return method, fmt.Errorf("method %q needs a parameter list", spec)
	}

	method.Name = field.Names[0].Name
	if !exportName.MatchString(method.Name) {
		return method, fmt.Errorf(
			"method %q needs an exported name",
			method.Name,
		)
	}

	for _, list := range []*ast.FieldList{signature.Params, signature.Results} {
		if list == nil {
			continue
		}

		for _, entry := range list.List {
			if len(entry.Names) != 0 {
				return method, fmt.Errorf(
					"method %s: declare types only, without parameter names",
					method.Name,
				)
			}

			if err := checkPortType(method.Name, entry.Type); err != nil {
				return method, err
			}

			text := types.ExprString(entry.Type)
			if list == signature.Params {
				method.Params = append(method.Params, text)
			} else {
				method.Results = append(method.Results, text)
			}
		}
	}

	return method, method.checkShape()
}

func (m portMethod) checkShape() error {
	if len(m.Params) == 0 ||
		m.Params[0] != "scope.Execution" && m.Params[0] != "scope.Tx" &&
			m.Params[0] != "scope.Provisioning" {
		return fmt.Errorf(
			"method %s must take scope.Execution, scope.Tx or "+
				"scope.Provisioning first",
			m.Name,
		)
	}

	if len(m.Results) == 0 || m.Results[len(m.Results)-1] != "error" {
		return fmt.Errorf("method %s must return error last", m.Name)
	}

	return nil
}

func interfaceOf(tree *ast.File) *ast.InterfaceType {
	for _, decl := range tree.Decls {
		generic, ok := decl.(*ast.GenDecl)
		if !ok || len(generic.Specs) != 1 {
			continue
		}

		spec, ok := generic.Specs[0].(*ast.TypeSpec)
		if !ok {
			continue
		}

		if iface, ok := spec.Type.(*ast.InterfaceType); ok {
			return iface
		}
	}

	return nil
}

// checkPortType admits builtin types and qualified names from the allowed
// packages. Anything else, including unqualified names, could smuggle a
// capability the port contract does not know about.
func checkPortType(method string, expression ast.Expr) error {
	var failure error

	ast.Inspect(expression, func(node ast.Node) bool {
		if failure != nil {
			return false
		}

		switch value := node.(type) {
		case *ast.SelectorExpr:
			qualifier, ok := value.X.(*ast.Ident)
			if !ok {
				failure = fmt.Errorf("method %s: unsupported type", method)

				return false
			}

			if _, allowed := portQualifiers[qualifier.Name]; !allowed {
				failure = fmt.Errorf(
					"method %s: package %s is not allowed in a port "+
						"signature (use scope, models, rules, pagination, "+
						"time or context)",
					method,
					qualifier.Name,
				)
			}

			return false
		case *ast.Ident:
			if !portBuiltinTypes[value.Name] {
				failure = fmt.Errorf(
					"method %s: type %s must be a builtin or package "+
						"qualified",
					method,
					value.Name,
				)
			}
		case *ast.ChanType, *ast.FuncType, *ast.InterfaceType,
			*ast.StructType:
			failure = fmt.Errorf(
				"method %s: channel, func, interface and struct types are "+
					"not allowed",
				method,
			)

			return false
		}

		return true
	})

	return failure
}

// portImports returns the sorted import specs a set of methods needs, in the
// group's namespace. Every method also uses context.
func portImports(module, group string, methods []portMethod) []string {
	used := map[string]bool{"context": true}

	for _, method := range methods {
		for _, text := range append(method.Params, method.Results...) {
			for qualifier := range portQualifiers {
				if strings.Contains(text, qualifier+".") {
					used[qualifier] = true
				}
			}
		}
	}

	var imports []string

	for qualifier := range used {
		path := portQualifiers[qualifier]

		switch {
		case strings.HasSuffix(path, "/"):
			path = module + "/" + path + group
		case strings.HasPrefix(path, "internal/"):
			path = module + "/" + path
		}

		imports = append(imports, fmt.Sprintf("%s %q", qualifier, path))
	}

	sort.Strings(imports)

	return imports
}

// validPortMethod requires a full signature so the generated interface is
// derived entirely from metadata.
func validPortMethod(spec string) bool {
	_, err := parsePortMethod(spec)

	return err == nil
}

func portMethodName(spec string) string {
	name, _, _ := strings.Cut(strings.TrimSpace(spec), "(")

	return strings.TrimSpace(name)
}
