package project

import "fmt"

// Candidate feature flags are owned by the operation so every entry point of
// one operation (HTTP today, other transports later) shares one declaration
// and one gate. An entry may restate the operation's flag, which happens when
// a loaded manifest is encoded again, but it can neither add nor change one.
func checkOperationOwnedFeature(entry Endpoint, owner Operation) error {
	if entry.Feature == owner.Feature &&
		entry.ClientVisible == owner.ClientVisible {
		return nil
	}

	if entry.Feature == "" && !entry.ClientVisible {
		return nil
	}

	return fmt.Errorf(
		"candidate feature flags are declared on the operation, not its "+
			"entry: %s",
		entry.Operation,
	)
}
