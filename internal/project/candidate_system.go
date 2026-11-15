package project

import (
	"fmt"
	"strings"
)

// Generated output of the tenant-less system operations: provision bodies
// reuse the authored-body seam with their own gate, workflow and handler;
// lookups are fully generated, with no authored body at all.

// lookupData builds the template view of one lookup operation.
func lookupData(m Manifest, operation Operation) (templateData, error) {
	lookup, ok := m.LookupByID(operation.Lookup)
	if !ok {
		return templateData{}, fmt.Errorf(
			"operation %s references undeclared lookup %s",
			operation.ID(),
			operation.Lookup,
		)
	}

	var output Model

	for _, model := range m.Models {
		if model.ID == operation.Output {
			output = model
		}
	}

	var columns, fields []string

	for _, declared := range output.Fields {
		field, err := seamModelField(output.ID, declared)
		if err != nil {
			return templateData{}, err
		}

		fields = append(fields, field.GoName)

		if field.GoType == "string" {
			columns = append(columns, quoteIdent(field.Name)+"::text")
		} else {
			columns = append(columns, quoteIdent(field.Name))
		}
	}

	query := "SELECT " + strings.Join(columns, ", ") + " FROM " +
		quoteIdent(lookup.ViewName()) + " WHERE " + quoteIdent(lookup.Key) +
		" = $1 LIMIT 2"

	data := dataFor(m, operation.Endpoint())
	data.Authored = authoredOp{ID: operation.ID(), Action: operation.Action}
	data.Lookup = lookup
	data.KeyGoName = seamGoName(lookup.Key)
	data.LookupQuery = query
	data.LookupBind = "SELECT set_config('" + lookup.Setting() +
		"', $1, true)"
	data.LookupFields = fields

	return data, nil
}

// candidateSystemManaged renders the generated half of every lookup.
// Provision gates and workflows are rendered with the authored seam.
func candidateSystemManaged(m Manifest) (map[string][]byte, error) {
	files := map[string][]byte{}

	for _, operation := range m.Operations {
		if operation.Scope != ScopeLookup {
			continue
		}

		data, err := lookupData(m, operation)
		if err != nil {
			return nil, err
		}

		name := lowerAction(operation.Action)

		for path, template := range map[string]string{
			"internal/operations/" + operation.Group + "/" + name +
				"_gate_gen.go": "candidate_lookup_gate.go",
			"internal/storage/" + operation.Group + "/" + name +
				"_lookup_gen.go": "candidate_lookup_reader.go",
		} {
			source, err := render(template, data)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}

			files[path] = source
		}
	}

	return files, nil
}

// systemHandler renders the HTTP handler of a provision or lookup operation.
func systemHandler(m Manifest, operation Operation) ([]byte, error) {
	switch operation.Scope {
	case ScopeProvision:
		data := dataFor(m, operation.Endpoint())
		data.Symbol = symbol(operation.Endpoint())

		return render("candidate_provision_handler.go", data)
	case ScopeEnroll:
		return enrollHandler(m, operation)
	case ScopeLookup:
		data, err := lookupData(m, operation)
		if err != nil {
			return nil, err
		}

		data.Symbol = symbol(operation.Endpoint())

		return render("candidate_lookup_handler.go", data)
	}

	return nil, fmt.Errorf("operation %s has no system scope", operation.ID())
}

// validateSystemEndpoints requires every system operation to be exposed by
// exactly one endpoint of the matching method: an unreachable provisioning or
// lookup body would only be dead, ungated surface.
func validateSystemEndpoints(m Manifest) error {
	for _, operation := range m.Operations {
		if operation.Scope == "" || operation.Scope == ScopeEachTenant {
			continue
		}

		method := "POST"
		if operation.Scope == ScopeLookup {
			method = "GET"
		}

		count := 0

		for _, endpoint := range m.Endpoints {
			if endpoint.Operation != operation.ID() {
				continue
			}

			count++

			if endpoint.Method != method {
				return fmt.Errorf(
					"operation %s endpoint must use %s",
					operation.ID(),
					method,
				)
			}
		}

		if count != 1 {
			return fmt.Errorf(
				"operation %s requires exactly one endpoint",
				operation.ID(),
			)
		}
	}

	return nil
}
