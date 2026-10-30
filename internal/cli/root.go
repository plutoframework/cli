// Package cli exposes fresh Cobra trees with injected streams and context.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

type Options struct {
	In        io.Reader
	Out       io.Writer
	Err       io.Writer
	Directory string
	Version   string
	terminal  func(io.Reader, io.Writer) bool
	wizard    wizardRun
}

// Execute returns 0 on success and 1 on all command/validation/tool errors.
// Cobra usage errors and operational errors share a stable nonzero exit code.
func Execute(
	ctx context.Context,
	args []string,
	in io.Reader,
	out, errOut io.Writer,
	version string,
) int {
	root := New(Options{In: in, Out: out, Err: errOut, Version: version})
	root.SetArgs(args)

	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(errOut, "pluto:", err)

		return 1
	}

	return 0
}

func New(options Options) *cobra.Command {
	if options.terminal == nil {
		options.terminal = terminalStreams
	}

	if options.wizard == nil {
		options.wizard = runTea
	}

	if options.In == nil {
		options.In = os.Stdin
	}

	if options.Out == nil {
		options.Out = os.Stdout
	}

	if options.Err == nil {
		options.Err = os.Stderr
	}

	if options.Directory == "" {
		options.Directory = "."
	}

	root := &cobra.Command{
		Use:               "pluto",
		Short:             "Generate and check explicit Go APIs and React applications",
		Long:              "Pluto scaffolds Go/Chi APIs with explicit operations and shared Pluto libraries.\nRun pluto without arguments in a terminal for the guided wizard.\nUse explicit arguments and --no-input for agents and scripts.",
		Version:           options.Version,
		SilenceErrors:     true,
		SilenceUsage:      true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error { return cmd.Context().Err() },
	}
	root.PersistentFlags().
		Bool("interactive", false, "Open the terminal wizard (can prefill with arguments/flags)")
	root.PersistentFlags().
		Bool("no-input", false, "Never prompt; use explicit command arguments")
	root.SetIn(options.In)
	root.SetOut(options.Out)
	root.SetErr(options.Err)
	root.AddCommand(
		&cobra.Command{
			Use:   "version",
			Short: "Print CLI version",
			Args:  cobra.NoArgs,
			Run:   func(cmd *cobra.Command, _ []string) { cmd.Println("pluto " + options.Version) },
		},
	)
	root.AddCommand(initCommand(options))
	root.AddCommand(addCommand(options))
	root.AddCommand(
		projectCommand(
			options,
			"fmt",
			"Apply formatters and wsl_v5 whitespace fixes",
			false,
		),
	)
	root.AddCommand(
		projectCommand(
			options,
			"vet",
			"Check architecture, formatting, Go analysis, tests and OpenAPI",
			true,
		),
	)
	root.AddCommand(openapiCommand(options))
	root.AddCommand(frontendCommand(options))

	hook := &cobra.Command{
		Use:   "hook",
		Short: "Manage the optional conservative staged-check hook",
	}
	hook.AddCommand(
		&cobra.Command{
			Use:   "install",
			Short: "Install without replacing existing hooks or changing Git config",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				root, _, err := project.Discover(options.Directory)
				if err != nil {
					return err
				}

				return project.InstallHook(cmd.Context(), root)
			},
		},
	)
	root.AddCommand(hook)

	decorateWizard(root, options, "menu", 0, true)

	for _, command := range root.Commands() {
		switch command.Name() {
		case "init":
			decorateWizard(command, options, "init", 1, true)
		case "add":
			decorateWizard(command, options, "add", 0, true)

			for _, child := range command.Commands() {
				maxArgs := 2
				if child.Name() == "state" {
					maxArgs = 1
				}

				decorateWizard(child, options, child.Name(), maxArgs, true)
			}
		case "fmt", "vet", "openapi":
			decorateWizard(command, options, command.Name(), 0, false)
		case "hook":
			for _, child := range command.Commands() {
				decorateWizard(child, options, "hook", 0, false)
			}
		case "frontend":
			decorateWizard(command, options, "frontend", 0, true)

			for _, child := range command.Commands() {
				switch child.Name() {
				case "init":
					decorateWizard(child, options, "frontend-init", 0, true)
				case "add":
					decorateWizard(child, options, "frontend-add", 0, true)

					for _, feature := range child.Commands() {
						maxArgs := 0
						if feature.Name() == "component" {
							maxArgs = 1
						}

						decorateWizard(
							feature,
							options,
							"frontend-"+feature.Name(),
							maxArgs,
							true,
						)
					}
				}
			}
		}
	}

	return root
}
