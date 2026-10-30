package cli

import (
	"fmt"
	"io"

	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func addStateCommand(options Options) *cobra.Command {
	var dry bool

	command := &cobra.Command{
		Use:   "state GROUP",
		Short: "Add typed business lifecycle rules, unit tests and fuzz tests",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, manifest, err := project.Discover(options.Directory)
			if err != nil {
				return err
			}

			files, err := project.StateFiles(manifest, args[0])
			if err != nil {
				return err
			}

			if err := project.Contracts(root, manifest); err != nil {
				return err
			}

			// Read-only module resolution supports newer Core releases too. It
			// cannot silently update the application's module or checksum files.
			if err := project.Run(cmd.Context(), root, io.Discard,
				cmd.ErrOrStderr(), "go", "list", "-mod=readonly",
				"github.com/plutoframework/pluto-core/state",
			); err != nil {
				return fmt.Errorf(
					"state requires Pluto Core v0.4.0 or newer; upgrade with go get github.com/plutoframework/pluto-core@%s: %w",
					project.CoreVersion,
					err,
				)
			}

			paths, err := project.Apply(root, files, nil, dry)
			if err != nil {
				return err
			}

			for _, path := range paths {
				cmd.Println(path)
			}

			if !dry {
				cmd.Println(
					"Added authored lifecycle rules. Inject into operations; retain authorization, tenant scope and transactional version checks. Run pluto fmt and pluto vet.",
				)
			}

			return nil
		},
	}
	command.Flags().BoolVar(&dry, "dry-run", false,
		"Preflight and list files without writing")

	return command
}
