package project

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Operation owns use-case authority and portable contracts independently of any
// transport. Entry declarations refer to ID; none can weaken its authority.
type Operation struct {
	Group        string `toml:"group"`
	Action       string `toml:"action"`
	Permission   string `toml:"permission,omitempty"`
	Public       bool   `toml:"public,omitempty"`
	Write        bool   `toml:"write,omitempty"`
	Collection   bool   `toml:"collection,omitempty"`
	Idempotency  string `toml:"idempotency"`
	ReplayReason string `toml:"replay_reason,omitempty"`
}

func (o Operation) ID() string {
	return o.Group + "." + o.Action
}

func (o Operation) Endpoint() Endpoint {
	method := "GET"
	if o.Write {
		method = "POST"
	}

	return Endpoint{
		Group:       o.Group,
		Action:      o.Action,
		Permission:  o.Permission,
		Public:      o.Public,
		Collection:  o.Collection,
		Method:      method,
		Path:        "/operation",
		Name:        o.ID(),
		Description: "Declared application operation.",
	}
}

func (m Manifest) Operation(id string) (Operation, bool) {
	for _, operation := range m.Operations {
		if operation.ID() == id {
			return operation, true
		}
	}

	return Operation{}, false
}

func (m Manifest) OperationEndpoint(entry Endpoint) (Endpoint, error) {
	if entry.Operation == "" {
		return entry, fmt.Errorf(
			"endpoint %s.%s requires an operation reference",
			entry.Group,
			entry.Action,
		)
	}

	operation, ok := m.Operation(entry.Operation)
	if !ok {
		return entry, fmt.Errorf(
			"endpoint references undeclared operation %s",
			entry.Operation,
		)
	}

	if operation.Group != entry.Group || operation.Action != entry.Action {
		return entry, fmt.Errorf(
			"HTTP entry identity must match its operation reference",
		)
	}

	if operation.Write && !entry.isWrite() {
		return entry, fmt.Errorf(
			"write operation %s cannot be exposed through a read-only HTTP "+
				"method",
			operation.ID(),
		)
	}

	entry.Permission = operation.Permission
	entry.Public = operation.Public

	entry.Collection = operation.Collection
	if operation.Write {
		entry.Idempotency = operation.Idempotency
	} else {
		entry.Idempotency = "exempt"
	}

	return entry, nil
}

func OperationFiles(
	m Manifest,
	operation Operation,
) (map[string][]byte, error) {
	if m.Boundary.Mode == "candidate" {
		return map[string][]byte{}, nil
	}

	files := map[string][]byte{}

	for _, item := range []struct {
		directory, template, suffix string
	}{
		{
			"models",
			"models.go",
			".go",
		},
		{
			"operations",
			"operation.go",
			".go",
		},
		{
			"operations",
			"operation_test.go",
			"_test.go",
		},
	} {
		data, err := render(item.template, dataFor(m, operation.Endpoint()))
		if err != nil {
			return nil, err
		}

		files[filepath.ToSlash(
			filepath.Join(
				"internal",
				item.directory,
				operation.Group,
				strings.ToLower(
					operation.Action,
				)+item.suffix,
			),
		)] = data
	}

	return files, nil
}

func (operation Operation) Validate(multi bool) error {
	if operation.Public && operation.Write {
		return fmt.Errorf(
			"public write operations require a separately supported " +
				"authority contract",
		)
	}

	if operation.Write && operation.Idempotency != "required" &&
		(operation.Idempotency != "exempt" || operation.ReplayReason == "") {
		return fmt.Errorf(
			"write operation %s requires explicit idempotency=required or a "+
				"reasoned exemption",
			operation.ID(),
		)
	}

	if !operation.Write && operation.Idempotency != "none" {
		return fmt.Errorf(
			"read operation %s requires idempotency=none",
			operation.ID(),
		)
	}

	if err := operation.Endpoint().Validate(multi); err != nil {
		return fmt.Errorf("operation %s: %w", operation.ID(), err)
	}

	return nil
}

func validateOperations(m Manifest) error {
	seen := map[string]bool{}

	for _, operation := range m.Operations {
		if err := operation.Validate(
			m.Capabilities.Tenancy == "multi",
		); err != nil {
			return err
		}

		if seen[operation.ID()] {
			return fmt.Errorf("duplicate operation %s", operation.ID())
		}

		seen[operation.ID()] = true
	}

	for _, job := range m.Jobs {
		operation, ok := m.Operation(job.Operation)
		if !ok || operation.Public {
			return fmt.Errorf(
				"job %s.%s requires a declared protected operation",
				job.Group,
				job.Action,
			)
		}
	}

	for _, agent := range m.Agents {
		operation, ok := m.Operation(agent.Operation)
		if !ok || operation.Public ||
			(operation.Write && m.Boundary.Mode != "candidate") ||
			operation.Permission == "" ||
			operation.Collection {
			return fmt.Errorf(
				"agent %s.%s requires a declared protected read operation",
				agent.Group,
				agent.Action,
			)
		}
	}

	return nil
}
