package project

import (
	"fmt"
	"slices"
)

// Cross-group transactional writes. A port may expose methods that take a
// scope.Tx so another group's operation can write inside its own transaction,
// for example an audit append that commits with an approval. Both sides must
// declare it: the port lists tx_methods and the operations allowed to call
// them in tx_callers, and the caller lists the port in its ports. The caller
// then sees only a generated narrow interface holding the tx methods, never
// the callee's Execution methods, so the callee's own gate cannot be skipped
// or re-entered and the transaction keeps the caller's verified tenant.

// validateTxMethods checks the tx_methods and tx_callers of one port.
func validateTxMethods(port Port) error {
	if len(port.TxMethods) == 0 {
		if len(port.TxCallers) != 0 {
			return fmt.Errorf("port %s tx_callers require tx_methods", port.ID)
		}

		return nil
	}

	if len(port.TxCallers) == 0 {
		return fmt.Errorf(
			"port %s tx_methods require tx_callers naming the operations "+
				"allowed to write in their own transaction",
			port.ID,
		)
	}

	shapes := map[string]portMethod{}

	for _, spec := range port.Methods {
		method, err := parsePortMethod(spec)
		if err != nil {
			return fmt.Errorf("port %s: %w", port.ID, err)
		}

		shapes[method.Name] = method
	}

	seen := map[string]bool{}

	for _, name := range port.TxMethods {
		method, ok := shapes[name]
		if !ok || seen[name] || method.Params[0] != "scope.Tx" {
			return fmt.Errorf(
				"port %s tx_methods %q must name a distinct declared method "+
					"that takes scope.Tx first",
				port.ID,
				name,
			)
		}

		seen[name] = true
	}

	callers := map[string]bool{}

	for _, id := range port.TxCallers {
		if _, _, ok := splitQualified(id); !ok || callers[id] {
			return fmt.Errorf(
				"port %s tx_callers %q must be distinct <group>.<Action> ids",
				port.ID,
				id,
			)
		}

		callers[id] = true
	}

	return nil
}

// validateTxCallers requires every tx caller to be a declared authored write
// operation in some group.
func validateTxCallers(ports []Port, operations []Operation) error {
	byID := map[string]Operation{}
	for _, operation := range operations {
		byID[operation.ID()] = operation
	}

	for _, port := range ports {
		for _, id := range port.TxCallers {
			operation, ok := byID[id]
			if !ok || operation.Body != BodyAuthored || !operation.Write {
				return fmt.Errorf(
					"port %s tx_callers %s must be a declared authored "+
						"operation with write = true",
					port.ID,
					id,
				)
			}
		}
	}

	return nil
}

// checkPortGroup admits a port of the operation's group, or a port of another
// group only through its declared tx methods.
func (o Operation) checkPortGroup(port Port) error {
	group, _, _ := splitQualified(port.ID)
	if group == o.Group {
		return nil
	}

	if len(port.TxMethods) == 0 ||
		!slices.Contains(port.TxCallers, o.ID()) {
		return fmt.Errorf(
			"port %s belongs to another group and does not list %s in "+
				"tx_callers",
			port.ID,
			o.ID(),
		)
	}

	if !o.Write {
		return fmt.Errorf(
			"port %s is a cross-group transactional write; %s needs "+
				"write = true",
			port.ID,
			o.ID(),
		)
	}

	return nil
}

// foreignPort reports whether an operation consumes a port of another group.
func foreignPort(group, id string) bool {
	owner, _, _ := splitQualified(id)

	return owner != group
}
