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
		Short:             "Generate and check small, explicit Go API applications",
		Long:              "Pluto scaffolds app-local Chi APIs with explicit operations, config and metadata.\nUse init, add endpoint, fmt, vet and openapi. No published framework is required.",
		Version:           options.Version,
		SilenceErrors:     true,
		SilenceUsage:      true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error { return cmd.Context().Err() },
	}
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

	return root
}
