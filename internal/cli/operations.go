package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func addOperationCommand(options Options) *cobra.Command {
	var (
		operation project.Operation
		dry       bool
	)

	command := &cobra.Command{
		Use:   "operation GROUP ACTION",
		Short: "Declare a transport-independent gated use case",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, m, err := project.Discover(options.Directory)
			if err != nil {
				return err
			}

			if err := project.Contracts(root, m); err != nil {
				return err
			}

			old := m

			operation.Group, operation.Action = args[0], args[1]
			if operation.Idempotency == "" {
				if operation.Write {
					operation.Idempotency = "required"
				} else {
					operation.Idempotency = "none"
				}
			}

			m.Operations = append(
				append([]project.Operation(nil), m.Operations...),
				operation,
			)
			if err := m.Validate(); err != nil {
				return err
			}

			files, err := project.Managed(m)
			if err != nil {
				return err
			}

			authored, err := project.OperationFiles(m, operation)
			if err != nil {
				return err
			}

			for path, data := range authored {
				files[path] = data
			}

			return applyOperationChange(cmd, root, m, old, files, dry)
		},
	}
	command.Flags().
		StringVar(&operation.Permission, "permission", "", "Required use-case permission")
	command.Flags().
		BoolVar(&operation.Public, "public", false, "Explicit single-tenant unauthenticated use case")
	command.Flags().
		BoolVar(&operation.Write, "write", false, "Declare mutation authority (permission required)")
	command.Flags().
		StringVar(&operation.Idempotency, "idempotency", "", "Operation replay contract: required, exempt or none")
	command.Flags().
		StringVar(&operation.ReplayReason, "replay-reason", "", "Required justification for write replay exemption")
	command.Flags().
		BoolVar(&operation.Collection, "collection", false, "Declare paginated result contracts")
	command.Flags().BoolVar(&dry, "dry-run", false, "Preflight without writes")

	return command
}

func applyOperationChange(
	cmd *cobra.Command,
	root string,
	m, old project.Manifest,
	files map[string][]byte,
	dry bool,
) error {
	previous, err := project.Managed(old)
	if err != nil {
		return err
	}

	previous["pluto.toml"], err = os.ReadFile(filepath.Join(root, "pluto.toml"))
	if err != nil {
		return err
	}

	files["pluto.toml"], err = project.Encode(m)
	if err != nil {
		return err
	}

	paths, err := project.Apply(root, files, previous, dry)
	if err != nil {
		return err
	}

	for _, path := range paths {
		cmd.Println(path)
	}

	if !dry {
		cmd.Println(
			"Operation contracts created; author only the private implementation and domain logic.",
		)
	}

	return nil
}

func ensureEntryOperation(
	m *project.Manifest,
	id, group, action, permission string,
	write, public, collection bool,
	replay ...string,
) (project.Operation, bool, error) {
	if id != "" {
		operation, ok := m.Operation(id)
		if !ok {
			return project.Operation{}, false, fmt.Errorf(
				"operation %s is not declared; use pluto add operation",
				id,
			)
		}

		if permission != "" && permission != operation.Permission {
			return operation, false, fmt.Errorf(
				"entry permission cannot override operation authority",
			)
		}

		return operation, false, nil
	}

	mode := "none"
	if write {
		mode = "required"
	}

	if len(replay) > 0 && replay[0] == "exempt" {
		mode = "exempt"
	}

	operation := project.Operation{
		Group:       group,
		Action:      action,
		Permission:  permission,
		Write:       write,
		Public:      public,
		Collection:  collection,
		Idempotency: mode,
	}
	if _, ok := m.Operation(operation.ID()); ok {
		return operation, false, fmt.Errorf(
			"operation %s already exists; supply --operation explicitly",
			operation.ID(),
		)
	}

	m.Operations = append(
		append([]project.Operation(nil), m.Operations...),
		operation,
	)

	return operation, true, nil
}
