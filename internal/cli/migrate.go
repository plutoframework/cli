package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/plutoframework/cli/internal/migrate"
	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func migrateCommand(options Options) *cobra.Command {
	var (
		sources   project.BoundaryProfile
		target    string
		contracts string
		dry       bool
	)

	command := &cobra.Command{
		Use:   "migrate",
		Short: "Move an application to a newer schema or boundary profile",
		Long: "Plan ordered, versioned migrations from the current " +
			"pluto.toml state to --to.\nOnly the manifest, go.mod and " +
			"unmodified scaffold files change; edited files are\nreported " +
			"as manual items (file:line, reason, action) and the command " +
			"exits\nnonzero while any remain. Applying requires a clean Git " +
			"tree, then runs\ngo mod tidy, OpenAPI generation, pluto fmt, " +
			"pluto architecture check and\npluto vet.",
		Example: "  pluto migrate --to candidate --dry-run " +
			"--candidate-auth-source /src/pluto-auth ...\n  pluto migrate " +
			"--to candidate --contracts contracts.toml " +
			"--candidate-auth-revision <commit> ...",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, _, err := project.DiscoverAny(options.Directory)
			if root == "" {
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

			if project.CandidateRevisionsRequested(sources) {
				if target != migrate.ProfileCandidate {
					return fmt.Errorf(
						"candidate revisions only apply to --to candidate",
					)
				}

				sources.Mode = migrate.ProfileCandidate
			}

			pinned, sums, err := pinCandidateProfile(cmd.Context(), sources)
			if err != nil {
				return err
			}

			input := migrate.Options{Sources: pinned, Sums: sums}

			if contracts != "" {
				path := contracts
				if !filepath.IsAbs(path) {
					path = filepath.Join(options.Directory, path)
				}

				input.Contracts, err = os.ReadFile(path)
				if err != nil {
					return fmt.Errorf("--contracts: %w", err)
				}
			}

			plan, err := migrate.Build(root, target, input)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if len(plan.Steps) == 0 {
				fmt.Fprintf(
					out,
					"Already at %s; nothing to migrate.\n",
					plan.To,
				)

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
				return manualError(plan)
			}

			if err := migrate.Write(plan); err != nil {
				return err
			}

			fmt.Fprintf(out, "Wrote %d changes.\n", len(plan.Paths()))

			checks := verifyMigration(cmd, root, plan)

			if err := manualError(plan); err != nil {
				return err
			}

			return checks
		},
	}
	addCandidateSourceFlags(command, &sources)

	flags := command.Flags()
	flags.StringVar(
		&target,
		"to",
		"",
		"Target profile (published or candidate) or schema version "+
			"(default: latest schema, same profile)",
	)
	flags.StringVar(
		&contracts,
		"contracts",
		"",
		"TOML file of candidate [[resources]], [[operation_contracts]], "+
			"[[effects]], [[roles]] and job budgets",
	)
	flags.BoolVar(
		&dry,
		"dry-run",
		false,
		"Print the full diff and manual items without writing",
	)

	return command
}

func reportPlan(out io.Writer, plan *migrate.Plan) {
	if len(plan.Steps) == 0 {
		fmt.Fprintf(out, "Generating %s\n", plan.To)
	} else {
		fmt.Fprintf(out, "Migrating %s -> %s\n", plan.From, plan.To)
	}

	for _, step := range plan.Steps {
		fmt.Fprintf(out, "  step %s: %s\n", step.ID, step.Summary)
	}

	if plan.Blocked {
		fmt.Fprintln(out, "Blocked: nothing will be written until the "+
			"manual items below are resolved; changes are a preview.")
	}

	if paths := plan.Paths(); len(paths) > 0 {
		fmt.Fprintf(out, "Changes (%d):\n", len(paths))

		for _, name := range paths {
			mark := "M"

			switch {
			case plan.Deletes[name]:
				mark = "D"
			case plan.Original[name] == nil:
				mark = "A"
			}

			fmt.Fprintf(out, "  %s %s\n", mark, name)
		}
	}

	if len(plan.Manual) > 0 {
		fmt.Fprintf(out, "Manual items (%d):\n", len(plan.Manual))

		for _, item := range plan.Manual {
			fmt.Fprintln(out, "  "+item.String())
		}
	}

	if len(plan.Notes) > 0 {
		fmt.Fprintf(out, "Notes (%d):\n", len(plan.Notes))

		for _, item := range plan.Notes {
			fmt.Fprintln(out, "  "+item.String())
		}
	}
}

func manualError(plan *migrate.Plan) error {
	if len(plan.Manual) == 0 {
		return nil
	}

	return fmt.Errorf("%d manual migration items remain", len(plan.Manual))
}

// verifyMigration runs the post-migration toolchain and reports each result.
// Nothing is skipped or weakened; the first failure is returned.
func verifyMigration(
	cmd *cobra.Command,
	root string,
	plan *migrate.Plan,
) error {
	ctx, out, errOut := cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr()
	failures := []error{}
	report := func(name string, err error) bool {
		if err != nil {
			fmt.Fprintf(out, "  %s: FAILED: %v\n", name, err)

			failures = append(failures, fmt.Errorf("%s: %w", name, err))

			return false
		}

		fmt.Fprintf(out, "  %s: ok\n", name)

		return true
	}

	fmt.Fprintln(out, "Verifying:")

	if plan.Tidy && !report(
		"go mod tidy",
		project.Run(ctx, root, out, errOut, "go", "mod", "tidy"),
	) {
		return errors.Join(failures...)
	}

	if !report(
		"openapi",
		project.Run(ctx, root, out, errOut, "go", "run", "./cmd/openapi"),
	) {
		return errors.Join(failures...)
	}

	report("pluto fmt", project.Format(ctx, root, out, errOut))

	architecture := project.InspectArchitecture(root)

	var architectureErr error

	if !architecture.Passed {
		for _, d := range architecture.Diagnostics {
			fmt.Fprintf(
				out,
				"    %s:%d: %s: %s\n",
				d.File,
				d.Line,
				d.ID,
				d.Message,
			)
		}

		architectureErr = fmt.Errorf(
			"%d violations or blockers",
			len(architecture.Diagnostics),
		)
	}

	report("pluto architecture check", architectureErr)

	_, manifest, err := project.Discover(root)
	if err == nil {
		err = project.Vet(ctx, root, manifest, out, errOut)
	}

	report("pluto vet", err)

	return errors.Join(failures...)
}
