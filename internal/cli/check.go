package cli

import (
	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func projectCommand(
	options Options,
	use, description string,
	vet bool,
) *cobra.Command {
	var staged bool

	command := &cobra.Command{
		Use:   use,
		Short: description,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, m, err := project.Discover(options.Directory)
			if err != nil {
				return err
			}

			if staged {
				if err := project.StagedPolicy(
					cmd.Context(),
					root,
				); err != nil {
					return err
				}
			}

			if vet {
				return project.Vet(
					cmd.Context(),
					root,
					m,
					cmd.OutOrStdout(),
					cmd.ErrOrStderr(),
				)
			}

			return project.Format(
				cmd.Context(),
				root,
				cmd.OutOrStdout(),
				cmd.ErrOrStderr(),
			)
		},
	}
	if vet {
		command.Flags().
			BoolVar(&staged, "staged", false, "Require all project contents staged, then vet the identical working tree")
	}

	return command
}

func openapiCommand(options Options) *cobra.Command {
	var check bool

	command := &cobra.Command{
		Use:   "openapi",
		Short: "Derive OpenAPI from the shared catalog and concrete models",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, m, err := project.Discover(options.Directory)
			if err != nil {
				return err
			}

			if err := project.Contracts(root, m); err != nil {
				return err
			}

			args := []string{"run", "./cmd/openapi"}
			if check {
				args = append(args, "-check")
			}

			return project.Run(
				cmd.Context(),
				root,
				cmd.OutOrStdout(),
				cmd.ErrOrStderr(),
				"go",
				args...)
		},
	}
	command.Flags().
		BoolVar(&check, "check", false, "Check freshness without writing")

	return command
}
