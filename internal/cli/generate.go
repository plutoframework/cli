package cli

import (
	"fmt"

	"github.com/plutoframework/cli/internal/migrate"
	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func generateCommand(options Options) *cobra.Command {
	var dry, adopt bool

	command := &cobra.Command{
		Use:   "generate",
		Short: "Rewrite CLI-owned files from pluto.toml",
		Long: "Render every managed file from pluto.toml with the " +
			"installed CLI. Edit pluto.toml,\nthen run pluto generate; " +
			"never edit managed files by hand.\n\nA managed file is " +
			"overwritten only while it is pristine relative to a render\n" +
			"this CLI knows (drift from a CLI upgrade). A hand-edited " +
			"managed file is\nreported with file:line and its differing " +
			"line, nothing is written, and the\ncommand exits nonzero. " +
			"Required migrations (table RLS, runtime role) are\nproposed " +
			"with framework numbering. Authored files are never " +
			"overwritten.\nWorks for the published and candidate profiles " +
			"and is idempotent. Applying\nrequires a clean Git tree.\n\n" +
			"\"Known\" means the file matches what the CLI last wrote, " +
			"recorded in\n" + project.LockPath + " (commit it), or a recorded " +
			"older scaffold. An app with no\nlock whose files came from an " +
			"older CLI: pluto generate --adopt --dry-run,\nreview the diff, " +
			"then pluto generate --adopt. --adopt overwrites hand edits\n" +
			"to managed files too; Git is the safety net.",
		Example: "  pluto generate --dry-run\n  pluto generate\n  pluto generate --adopt --dry-run",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, _, err := project.Discover(options.Directory)
			if err != nil {
				return err
			}

			if !dry {
				if err := migrate.RequireCleanTree(
					cmd.Context(),
					root,
				); err != nil {
					return err
				}
			}

			generate := migrate.Generate
			if adopt {
				generate = migrate.Adopt
			}

			plan, err := generate(root)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()

			if len(plan.Paths()) == 0 && len(plan.Manual) == 0 {
				fmt.Fprintln(out, "Already up to date; nothing to generate.")

				return nil
			}

			reportPlan(out, plan)

			if dry || adopt {
				if err := migrate.WriteDiff(out, plan); err != nil {
					return err
				}

				if dry {
					fmt.Fprintln(out, "Dry run: nothing written.")

					return manualError(plan)
				}
			}

			if plan.Blocked {
				return fmt.Errorf(
					"%d managed files were edited by hand; nothing written",
					len(plan.Manual),
				)
			}

			if err := migrate.Write(plan); err != nil {
				return err
			}

			fmt.Fprintf(out, "Wrote %d files.\n", len(plan.Paths()))

			ctx, errOut := cmd.Context(), cmd.ErrOrStderr()

			if err := project.Run(
				ctx, root, out, errOut, "go", "run", "./cmd/openapi",
			); err != nil {
				return fmt.Errorf("openapi: %w", err)
			}

			if err := project.Format(ctx, root, out, errOut); err != nil {
				return fmt.Errorf("pluto fmt: %w", err)
			}

			fmt.Fprintln(
				out,
				"Review new migrations, apply them explicitly, then run "+
					"pluto vet.",
			)

			return nil
		},
	}
	command.Flags().BoolVar(
		&dry,
		"dry-run",
		false,
		"Print the full diff and refusals without writing",
	)

	command.Flags().BoolVar(
		&adopt,
		"adopt",
		false,
		"Bring an application without "+project.LockPath+" onto the lock: "+
			"print the diff, overwrite every managed file with the current "+
			"render and record it (review with --dry-run first)",
	)

	return command
}
