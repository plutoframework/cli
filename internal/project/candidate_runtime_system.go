package project

import (
	"bytes"
	"fmt"
)

// writeSystemOperation wires one provision or lookup operation: the shared
// attempt limiter plus either the provisioning gate over the declared ports
// or the generated lookup reader. Neither has a membership gate: no tenant
// or membership exists at that point.
func writeSystemOperation(
	source *bytes.Buffer,
	operation Operation,
	index int,
) error {
	attempts, window, err := ParseRateLimit(operation.RateLimit)
	if err != nil {
		return fmt.Errorf("operation %s: %w", operation.ID(), err)
	}

	suffix := fmt.Sprint(index)

	fmt.Fprintf(
		source,
		`
	limiter%[1]s, err := identity.NewAttemptLimiter(
		pool,
		%[2]d,
		time.Duration(%[3]d),
	)
	if err != nil {
		return nil, err
	}
`,
		suffix,
		attempts,
		window.Nanoseconds(),
	)

	if operation.Scope == ScopeEnroll {
		fmt.Fprintf(
			source,
			`
	runtime.Operations.%[1]s = %[2]soperations.New%[3]s(
		machineEnroller,
		limiter%[4]s,
	)
`,
			symbol(operation.Endpoint()),
			operation.Group,
			operation.Action,
			suffix,
		)

		return nil
	}

	if operation.Scope == ScopeLookup {
		fmt.Fprintf(
			source,
			`
	runtime.Operations.%[1]s = %[2]soperations.New%[3]s(
		%[2]slookup.New%[3]sReader(pool),
		limiter%[4]s,
	)
`,
			symbol(operation.Endpoint()),
			operation.Group,
			operation.Action,
			suffix,
		)

		return nil
	}

	var ports bytes.Buffer

	transactional := false

	for _, id := range operation.Ports {
		if foreignPort(operation.Group, id) {
			transactional = true

			fmt.Fprintf(
				&ports,
				"%s: adapters.%sTx, ",
				foreignPortField(id),
				adapterField(Port{ID: id}),
			)

			continue
		}

		fmt.Fprintf(
			&ports,
			"%s: adapters.%s, ",
			modelName(id),
			adapterField(Port{ID: id}),
		)
	}

	if transactional {
		ports.WriteString("TxDB: adapters.DB, ")
	}

	fmt.Fprintf(
		source,
		`
	runtime.Operations.%[1]s = %[2]soperations.New%[3]s(
		authentication.Manager,
		limiter%[4]s,
		%[2]slogic.%[3]sPorts{%[5]s},
	)
`,
		symbol(operation.Endpoint()),
		operation.Group,
		operation.Action,
		suffix,
		ports.String(),
	)

	return nil
}
