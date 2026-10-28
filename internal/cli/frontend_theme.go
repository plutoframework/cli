package cli

import (
	"fmt"
	"path/filepath"

	"github.com/plutoframework/cli/internal/frontend"
	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func frontendThemeCommand(options Options) *cobra.Command {
	var check bool

	command := &cobra.Command{
		Use:   "theme",
		Short: "Compile pluto.toml theme settings without modifying authored CSS",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, m, err := project.Discover(options.Directory)
			if err != nil {
				return err
			}

			if m.Frontend == nil {
				return fmt.Errorf("run pluto frontend init first")
			}

			args := []string{"run", "theme:generate"}
			if check {
				args = []string{"run", "theme:check"}
			}

			return frontend.NPM(
				cmd.Context(),
				filepath.Join(root, m.Frontend.Directory),
				cmd.OutOrStdout(),
				cmd.ErrOrStderr(),
				args...)
		},
	}
	command.Flags().
		BoolVar(&check, "check", false, "Check generated theme freshness without writes")

	return command
}
