package cli

import (
	"os"
	"path/filepath"

	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func addJobCommand(options Options) *cobra.Command {
	var (
		maxAttempts int
		kind        string
		operationID string
		permission  string
		dry         bool
	)

	command := &cobra.Command{
		Use: "job GROUP ACTION",
		Short: "Add a typed job, handler, tests and explicit worker " +
			"registration",
		Example: "  pluto add job invoices Send --kind invoice.send",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, manifest, err := project.Discover(options.Directory)
			if err != nil {
				return err
			}

			if err := project.Contracts(root, manifest); err != nil {
				return err
			}

			previousManifest := manifest

			operation, created, err := ensureEntryOperation(
				&manifest,
				operationID,
				args[0],
				args[1],
				permission,
				true,
				false,
				false,
			)
			if err != nil {
				return err
			}

			job := project.Job{
				Group:       args[0],
				Action:      args[1],
				Kind:        kind,
				MaxAttempts: maxAttempts,
				Operation:   operation.ID(),
			}

			manifest.Jobs = append(
				append([]project.Job(nil), manifest.Jobs...),
				job,
			)
			if err := manifest.Validate(); err != nil {
				return err
			}

			files, err := project.Managed(manifest)
			if err != nil {
				return err
			}

			authored, err := project.JobFiles(manifest, job)
			if err != nil {
				return err
			}

			for path, source := range authored {
				files[path] = source
			}

			if created {
				operationFiles, err := project.OperationFiles(
					manifest,
					operation,
				)
				if err != nil {
					return err
				}

				for path, source := range operationFiles {
					files[path] = source
				}
			}

			previous, err := project.Managed(previousManifest)
			if err != nil {
				return err
			}

			previous["pluto.toml"], err = os.ReadFile(
				filepath.Join(root, "pluto.toml"),
			)
			if err != nil {
				return err
			}

			files["pluto.toml"], err = project.Encode(manifest)
			if err != nil {
				return err
			}

			paths, err := project.Apply(root, files, previous, dry)
			if err != nil {
				return err
			}

			if dry {
				for _, path := range paths {
					cmd.Println(path)
				}

				return nil
			}

			cmd.Println(
				"Job added; implement its handler before enqueueing. Run " +
					"pluto fmt and pluto vet.",
			)

			return nil
		},
	}
	command.Flags().
		IntVar(
			&maxAttempts,
			"max-attempts",
			0,
			"Explicit finite candidate delivery attempt budget (1..100)",
		)
	command.Flags().
		StringVar(
			&operationID,
			"operation",
			"",
			"Declared gated operation to invoke",
		)
	command.Flags().
		StringVar(
			&permission,
			"permission",
			"",
			"Permission for a newly declared write operation",
		)
	command.Flags().
		StringVar(
			&kind,
			"kind",
			"",
			"Stable lowercase delivery kind (required)",
		)
	command.Flags().
		BoolVar(
			&dry,
			"dry-run",
			false,
			"Preflight and list files without writing",
		)

	return command
}
