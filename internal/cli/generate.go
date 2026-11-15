package cli

import (
	"fmt"

	"github.com/plutoframework/cli/internal/migrate"
	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func generateCommand(options Options) *cobra.Command {
	var dry bool

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
			"and is idempotent. Applying\nrequires a clean Git tree.",
		Example: "  pluto generate --dry-run\n  pluto generate",
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

			plan, err := migrate.Generate(root)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()

			if len(plan.Paths()) == 0 && len(plan.Manual) == 0 {
				fmt.Fprintln(out, "Already up to date; nothing to generate.")

				return nil
			}

			reportPlan(out, plan)

			if dry {
				if err := migrate.WriteDiff(out, plan); err != nil {
					return err
				}

				fmt.Fprintln(out, "Dry run: nothing written.")

				return manualError(plan)
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

	return command
}
