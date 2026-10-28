package cli

import (
	"fmt"
	"path/filepath"

	"github.com/plutoframework/cli/internal/frontend"
	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func frontendCommand(options Options) *cobra.Command {
	command := &cobra.Command{
		Use:   "frontend",
		Short: "Scaffold React features using your saved UI library",
		Args:  cobra.NoArgs,
	}

	var (
		ui, directory        string
		auth, dry, noInstall bool
	)

	init := &cobra.Command{
		Use:   "init",
		Short: "Initialize React with native, HeroUI v3 or shadcn controls",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, m, err := project.Discover(options.Directory)
			if err != nil {
				return err
			}

			plan, err := frontend.Init(root, m, ui, directory, auth)
			if err != nil {
				return err
			}

			return applyFrontend(cmd, root, plan, dry, noInstall)
		},
	}
	init.Flags().
		StringVar(&ui, "ui", "native", "UI profile: native, heroui-v3 or shadcn (saved in pluto.toml)")
	init.Flags().
		StringVar(&directory, "directory", "web", "Single project-relative frontend directory")
	init.Flags().
		BoolVar(&auth, "auth", false, "Include local credential UI; backend must already support it")
	frontendFlags(init, &dry, &noInstall)

	add := &cobra.Command{
		Use:   "add",
		Short: "Add a feature with the saved UI profile",
		Args:  cobra.NoArgs,
	}
	add.AddCommand(
		frontendAddCommand(options, false),
		frontendAddCommand(options, true),
	)

	install := &cobra.Command{
		Use:   "install",
		Short: "Install exact dependencies, generate API types, format and check",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, m, err := project.Discover(options.Directory)
			if err != nil {
				return err
			}

			if m.Frontend == nil {
				return fmt.Errorf("run pluto frontend init first")
			}

			return frontend.Install(
				cmd.Context(),
				root,
				m,
				cmd.OutOrStdout(),
				cmd.ErrOrStderr(),
			)
		},
	}
	command.AddCommand(init, add, install)

	return command
}

func frontendAddCommand(options Options, auth bool) *cobra.Command {
	var (
		kind           string
		dry, noInstall bool
	)

	command := &cobra.Command{
		Use:   "component name",
		Short: "Add a presentation component and its required primitives",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, m, err := project.Discover(options.Directory)
			if err != nil {
				return err
			}

			name := ""

			selectedKind := kind
			if auth {
				selectedKind = "auth"
			} else {
				name = args[0]
			}

			plan, err := frontend.Add(root, m, name, selectedKind)
			if err != nil {
				return err
			}

			return applyFrontend(cmd, root, plan, dry, noInstall)
		},
	}
	if auth {
		command.Use = "auth"
		command.Short = "Add sign-in, enrollment, verification and recovery UI"
		command.Args = cobra.NoArgs
	} else {
		command.Flags().
			StringVar(&kind, "kind", "form", "Component kind: button, input, card, dialog or form")
	}

	frontendFlags(command, &dry, &noInstall)

	return command
}

func frontendFlags(cmd *cobra.Command, dry, noInstall *bool) {
	cmd.Flags().
		BoolVar(dry, "dry-run", false, "Preflight and list files offline without writes or installs")
	cmd.Flags().
		BoolVar(noInstall, "no-install", false, "Write files without npm; later run pluto frontend install")
}

func applyFrontend(
	cmd *cobra.Command,
	root string,
	plan frontend.Plan,
	dry, noInstall bool,
) error {
	names, err := plan.Apply(root, dry)
	if err != nil {
		return err
	}

	for _, name := range names {
		cmd.Println(filepath.ToSlash(name))
	}

	if dry {
		return nil
	}

	cmd.Printf(
		"Frontend profile: %s (saved in pluto.toml).\n",
		plan.Manifest.Frontend.UI,
	)

	if noInstall {
		cmd.Println("Next: pluto frontend install")

		return nil
	}

	return frontend.Install(
		cmd.Context(),
		root,
		plan.Manifest,
		cmd.OutOrStdout(),
		cmd.ErrOrStderr(),
	)
}
