package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

type wizardRun func(context.Context, io.Reader, io.Writer, wizardModel) (wizardModel, error)

func terminalStreams(in io.Reader, out io.Writer) bool {
	input, ok := in.(*os.File)
	if !ok {
		return false
	}

	output, ok := out.(*os.File)

	return ok && term.IsTerminal(input.Fd()) && term.IsTerminal(output.Fd()) &&
		os.Getenv("TERM") != "dumb"
}

func runTea(
	ctx context.Context,
	in io.Reader,
	out io.Writer,
	model wizardModel,
) (wizardModel, error) {
	result, err := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(out)).
		Run()
	if err != nil {
		return wizardModel{}, err
	}

	return result.(wizardModel), nil
}

func interactiveRequested(
	cmd *cobra.Command,
	options Options,
	args []string,
	automatic bool,
) bool {
	forced, _ := cmd.Root().PersistentFlags().GetBool("interactive")

	disabled, _ := cmd.Root().PersistentFlags().GetBool("no-input")
	if disabled {
		return false
	}

	if forced {
		return true
	}

	ci := strings.ToLower(os.Getenv("CI"))

	return automatic && len(args) == 0 && cmd.Flags().NFlag() == 0 &&
		(ci == "" || ci == "false" || ci == "0") &&
		options.terminal(cmd.InOrStdin(), cmd.OutOrStdout())
}

func decorateWizard(
	cmd *cobra.Command,
	options Options,
	kind string,
	maxArgs int,
	automatic bool,
) {
	originalArgs, originalRun := cmd.Args, cmd.RunE
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		forced, _ := cmd.Root().PersistentFlags().GetBool("interactive")

		disabled, _ := cmd.Root().PersistentFlags().GetBool("no-input")
		if forced && disabled {
			return errors.New("--interactive and --no-input cannot be combined")
		}

		if interactiveRequested(cmd, options, args, automatic) {
			return cobra.MaximumNArgs(maxArgs)(cmd, args)
		}

		if originalArgs != nil {
			return originalArgs(cmd, args)
		}

		return cobra.NoArgs(cmd, args)
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if !interactiveRequested(cmd, options, args, automatic) {
			if originalRun != nil {
				return originalRun(cmd, args)
			}

			return cmd.Help()
		}

		if !options.terminal(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return errors.New(
				"wizard requires terminal input and output; use explicit arguments with --no-input",
			)
		}

		model, err := wizardForm(kind, options, cmd, args)
		if err != nil {
			return err
		}

		for {
			result, err := options.wizard(
				cmd.Context(),
				cmd.InOrStdin(),
				cmd.OutOrStdout(),
				model,
			)
			if err != nil {
				return err
			}

			if result.cancelled || !result.confirmed {
				return errors.New("wizard cancelled; no changes made")
			}

			if model.menu {
				model, err = wizardForm(result.args[0], options, cmd, nil)
				if err != nil {
					return err
				}

				continue
			}

			// Rebuild and validate after terminal cleanup. The wizard shares the
			// same Cobra validation, preflight and generator as scripted commands.
			plan, err := result.plan()
			if err != nil {
				return err
			}

			plan = append([]string{"--no-input"}, plan...)

			cmd.Println("Equivalent command:")
			cmd.Println(shellCommand(plan))

			next := New(options)
			next.SetArgs(plan)

			return next.ExecuteContext(cmd.Context())
		}
	}
}
