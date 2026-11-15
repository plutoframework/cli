package project

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// authoredAdapter is one authored adapter type with every port it must
// satisfy. Ports that share an adapter merge their method sets.
type authoredAdapter struct {
	Group   string
	Name    string
	Ports   []Port
	Methods []portMethod
}

func (a authoredAdapter) constructor() string {
	return "New" + a.Name
}

// authoredAdapters groups declared ports by adapter, validating every method
// signature. The result is sorted so generation is deterministic.
func authoredAdapters(m Manifest) ([]authoredAdapter, error) {
	byID := map[string]*authoredAdapter{}

	for _, port := range m.Ports {
		group, _, _ := splitQualified(port.ID)
		_, name, _ := splitQualified(port.Adapter)

		adapter := byID[port.Adapter]
		if adapter == nil {
			adapter = &authoredAdapter{Group: group, Name: name}
			byID[port.Adapter] = adapter
		}

		adapter.Ports = append(adapter.Ports, port)

		for _, spec := range port.Methods {
			method, err := parsePortMethod(spec)
			if err != nil {
				return nil, fmt.Errorf("port %s: %w", port.ID, err)
			}

			if err := adapter.addMethod(method); err != nil {
				return nil, fmt.Errorf("port %s: %w", port.ID, err)
			}
		}
	}

	var adapters []authoredAdapter

	for _, adapter := range byID {
		adapters = append(adapters, *adapter)
	}

	sort.Slice(adapters, func(i, j int) bool {
		left, right := adapters[i], adapters[j]
		if left.Group != right.Group {
			return left.Group < right.Group
		}

		return left.Name < right.Name
	})

	return adapters, nil
}

func (a *authoredAdapter) addMethod(method portMethod) error {
	for _, existing := range a.Methods {
		if existing.Name != method.Name {
			continue
		}

		if existing.signature() != method.signature() {
			return fmt.Errorf(
				"adapter %s.%s has conflicting signatures for %s",
				a.Group,
				a.Name,
				method.Name,
			)
		}

		return nil
	}

	a.Methods = append(a.Methods, method)

	return nil
}

func (m portMethod) signature() string {
	return strings.Join(m.Params, ",") + "->" + strings.Join(m.Results, ",")
}

// candidateAdapterFiles renders the managed output for declared ports: one
// interface per port, one compile-time contract per adapter group, the app
// wiring and the managed sqlc configuration. Authored adapter bodies are
// never rendered here.
func candidateAdapterFiles(m Manifest) (map[string][]byte, error) {
	files := map[string][]byte{}

	if len(m.Ports) > 0 {
		adapters, err := authoredAdapters(m)
		if err != nil {
			return nil, err
		}

		for _, port := range m.Ports {
			source, err := renderPortInterface(m.Project.Module, port)
			if err != nil {
				return nil, err
			}

			group, name, _ := splitQualified(port.ID)
			files["internal/ports/"+group+"/"+strings.ToLower(name)+
				"_port_gen.go"] = source
		}

		for group, source := range renderAdapterContracts(
			m.Project.Module,
			adapters,
		) {
			files["internal/adapters/"+group+"/contract_gen.go"] = source
		}

		files[candidateAdaptersFile] = renderAdapterWiring(
			m.Project.Module,
			adapters,
		)
	}

	if len(m.Queries) > 0 {
		files["sqlc.yaml"] = candidateSQLCConfig(m)
	}

	return files, nil
}

func renderPortInterface(module string, port Port) ([]byte, error) {
	group, name, _ := splitQualified(port.ID)

	var methods []portMethod

	for _, spec := range port.Methods {
		method, err := parsePortMethod(spec)
		if err != nil {
			return nil, fmt.Errorf("port %s: %w", port.ID, err)
		}

		methods = append(methods, method)
	}

	var source bytes.Buffer

	fmt.Fprintf(&source, "package %s\n\nimport (\n", group)

	for _, spec := range portImports(module, group, methods) {
		fmt.Fprintf(&source, "\t%s\n", spec)
	}

	fmt.Fprintf(
		&source,
		"\n)\n\n// %s is implemented by the authored adapter %s. Every method "+
			"takes the\n// scoped Execution or Tx, never a pool or tenant "+
			"string.\ntype %s interface {\n",
		name,
		port.Adapter,
		name,
	)

	for _, method := range methods {
		fmt.Fprintf(
			&source,
			"\t%s(context.Context, %s)",
			method.Name,
			strings.Join(method.Params, ", "),
		)
		source.WriteString(" " + resultList(method.Results) + "\n")
	}

	source.WriteString("}\n")

	return source.Bytes(), nil
}

func resultList(results []string) string {
	if len(results) == 1 {
		return results[0]
	}

	return "(" + strings.Join(results, ", ") + ")"
}

func renderAdapterContracts(
	module string,
	adapters []authoredAdapter,
) map[string][]byte {
	byGroup := map[string][]authoredAdapter{}

	for _, adapter := range adapters {
		byGroup[adapter.Group] = append(byGroup[adapter.Group], adapter)
	}

	out := map[string][]byte{}

	for group, list := range byGroup {
		var source bytes.Buffer

		fmt.Fprintf(
			&source,
			"package %s\n\nimport (\n\tports %q\n\t%q\n)\n\n",
			group,
			module+"/internal/ports/"+group,
			module+"/internal/scope",
		)

		source.WriteString(
			"// Compile-time contract: each adapter satisfies its ports and " +
				"is built\n// only from a scoped DB by the application " +
				"factory.\n",
		)

		for _, adapter := range list {
			for _, port := range adapter.Ports {
				_, name, _ := splitQualified(port.ID)
				fmt.Fprintf(
					&source,
					"var _ ports.%s = (*%s)(nil)\n",
					name,
					adapter.Name,
				)
			}

			fmt.Fprintf(
				&source,
				"var _ func(scope.DB) *%s = %s\n",
				adapter.Name,
				adapter.constructor(),
			)
		}

		out[group] = source.Bytes()
	}

	return out
}

// renderAdapterWiring builds every authored adapter from one scoped DB. The
// pool is wrapped here and nowhere else, so authored code only ever holds a
// scope.DB.
func renderAdapterWiring(module string, adapters []authoredAdapter) []byte {
	var source bytes.Buffer

	source.WriteString(
		"package app\n\nimport (\n" +
			"\t\"github.com/jackc/pgx/v5/pgxpool\"\n" +
			"\t\"" + module + "/internal/scope\"\n",
	)

	groups := map[string]bool{}

	for _, adapter := range adapters {
		groups[adapter.Group] = true
	}

	names := make([]string, 0, len(groups))
	for group := range groups {
		names = append(names, group)
	}

	sort.Strings(names)

	for _, group := range names {
		fmt.Fprintf(
			&source,
			"\t%sadapters %q\n\t%sports %q\n",
			group,
			module+"/internal/adapters/"+group,
			group,
			module+"/internal/ports/"+group,
		)
	}

	source.WriteString(
		")\n\n// AuthoredAdapters holds every declared authored adapter as its " +
			"port.\ntype AuthoredAdapters struct {\n",
	)

	for _, adapter := range adapters {
		for _, port := range adapter.Ports {
			fmt.Fprintf(
				&source,
				"\t%s %sports.%s\n",
				adapterField(port),
				adapter.Group,
				portName(port),
			)
		}
	}

	source.WriteString(
		"}\n\n// NewAuthoredAdapters is the only place a scoped DB is created " +
			"from the pool.\nfunc NewAuthoredAdapters(pool *pgxpool.Pool) " +
			"AuthoredAdapters {\n\tdb := scope.NewDB(pool)\n\n" +
			"\treturn AuthoredAdapters{\n",
	)

	for _, adapter := range adapters {
		for _, port := range adapter.Ports {
			fmt.Fprintf(
				&source,
				"\t\t%s: %sadapters.%s(db),\n",
				adapterField(port),
				adapter.Group,
				adapter.constructor(),
			)
		}
	}

	source.WriteString("\t}\n}\n")

	return source.Bytes()
}

func portName(port Port) string {
	_, name, _ := splitQualified(port.ID)

	return name
}

// adapterField is the AuthoredAdapters field for a port: Group + Port name.
func adapterField(port Port) string {
	group, name, _ := splitQualified(port.ID)

	return strings.ToUpper(group[:1]) + group[1:] + name
}

// AuthoredAdapterStubs returns create-once starter adapters for declared
// ports. They are authored files: callers must write only the missing ones
// and never overwrite an existing file.
func AuthoredAdapterStubs(m Manifest) (map[string][]byte, error) {
	files := map[string][]byte{}

	if m.Boundary.Mode != "candidate" || len(m.Ports) == 0 {
		return files, nil
	}

	adapters, err := authoredAdapters(m)
	if err != nil {
		return nil, err
	}

	for _, adapter := range adapters {
		path := AuthoredAdapterPath(adapter.Group, adapter.Name)
		files[path] = renderAdapterStub(m.Project.Module, adapter)
	}

	return files, nil
}

// AuthoredAdapterPath is the conventional file of an authored adapter.
func AuthoredAdapterPath(group, name string) string {
	return "internal/adapters/" + group + "/" + snakeCase(name) + ".go"
}

func snakeCase(name string) string {
	var out strings.Builder

	for index, char := range name {
		if char >= 'A' && char <= 'Z' {
			if index > 0 {
				out.WriteByte('_')
			}

			char += 'a' - 'A'
		}

		out.WriteRune(char)
	}

	return out.String()
}

func renderAdapterStub(module string, adapter authoredAdapter) []byte {
	scopeImport := fmt.Sprintf("scope %q", module+"/internal/scope")
	imports := []string{`"errors"`, scopeImport}

	for _, spec := range portImports(module, adapter.Group, adapter.Methods) {
		if spec != scopeImport {
			imports = append(imports, spec)
		}
	}

	sort.Strings(imports)

	var source bytes.Buffer

	fmt.Fprintf(&source, "package %s\n\nimport (\n", adapter.Group)

	for _, spec := range imports {
		fmt.Fprintf(&source, "\t%s\n", spec)
	}

	fmt.Fprintf(
		&source,
		")\n\n// %s is the authored adapter. Replace the starter "+
			"bodies; the managed\n// contract in contract_gen.go fixes its "+
			"shape.\ntype %s struct{ db scope.DB }\n\n"+
			"func %s(db scope.DB) *%s { return &%s{db: db} }\n",
		adapter.Name,
		adapter.Name,
		adapter.constructor(),
		adapter.Name,
		adapter.Name,
	)

	for _, method := range adapter.Methods {
		source.WriteString(stubMethod(adapter, method))
	}

	return source.Bytes()
}

func stubMethod(adapter authoredAdapter, method portMethod) string {
	params := []string{"_ context.Context"}
	for _, param := range method.Params {
		params = append(params, "_ "+param)
	}

	var results []string

	for index, result := range method.Results {
		if index == len(method.Results)-1 {
			results = append(results, "err "+result)

			continue
		}

		results = append(results, fmt.Sprintf("result%d %s", index, result))
	}

	return fmt.Sprintf(
		"\nfunc (s *%s) %s(%s) (%s) {\n\terr = errors.New(%q)\n\n"+
			"\treturn\n}\n",
		adapter.Name,
		method.Name,
		strings.Join(params, ", "),
		strings.Join(results, ", "),
		adapter.Group+"."+adapter.Name+"."+method.Name+" is not implemented",
	)
}
