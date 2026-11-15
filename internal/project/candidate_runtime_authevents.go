package project

import (
	"bytes"
	"fmt"
)

// writeAuthEvents composes the generated authentication event hook around the
// declared operation. The recorder is the only code that calls it.
func writeAuthEvents(source *bytes.Buffer, m Manifest) error {
	if !m.AuthEventsOn() {
		return nil
	}

	operation, ok := m.Operation(m.Auth.Events.Operation)
	if !ok {
		return fmt.Errorf(
			"auth events operation %s is not declared",
			m.Auth.Events.Operation,
		)
	}

	fmt.Fprintf(
		source,
		`
	events, err := newAuthEvents(
		authentication,
		memberships,
		integrations.Ingress,
		func(
			ctx context.Context,
			caller access.Caller,
			event string,
		) error {
			return runtime.Operations.%[1]s.Execute(
				ctx,
				caller,
				authEventModels.%[2]sRequest{Event: event},
			)
		},
		logger,
	)
	if err != nil {
		return nil, err
	}
`,
		symbol(operation.Endpoint()),
		operation.Action,
	)

	return nil
}
