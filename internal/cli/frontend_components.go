package cli

import (
	"github.com/plutoframework/cli/internal/frontend"
	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func frontendComponentsCommand(options Options) *cobra.Command {
	command := &cobra.Command{
		Use:   "components",
		Short: "Discover built-in and configured local registry components",
	}
	command.AddCommand(&cobra.Command{
		Use: "list", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, manifest, err := project.Discover(options.Directory)
			if err != nil {
				return err
			}

			names, err := frontend.Components(root, manifest)
			if err != nil {
				return err
			}

			for _, name := range names {
				cmd.Println(name)
			}

			return nil
		},
	})

	return command
}
