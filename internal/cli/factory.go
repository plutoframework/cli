package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

func factoryCommand(options Options) *cobra.Command {
	return &cobra.Command{
		Use:                "factory [command] [flags]",
		Short:              "Set up and run a durable local software factory",
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			engine := os.Getenv("PLUTO_FACTORY_ENGINE")
			if engine == "" {
				engine = "pluto-factory"
			}

			binary, err := exec.LookPath(engine)
			if err != nil {
				return fmt.Errorf(
					"factory engine is not installed; install the local "+
						"pluto-factory Python package or set "+
						"PLUTO_FACTORY_ENGINE to its executable: %w",
					err,
				)
			}

			arguments := append([]string(nil), args...)
			if len(arguments) == 0 {
				arguments = []string{"--help"}
			}

			noInput, _ := cmd.Root().PersistentFlags().GetBool("no-input")
			if noInput {
				arguments = append([]string{"--no-input"}, arguments...)
			}

			interactive, _ := cmd.Root().
				PersistentFlags().
				GetBool("interactive")
			if interactive {
				arguments = append([]string{"--interactive"}, arguments...)
			}

			process := exec.CommandContext(cmd.Context(), binary, arguments...)
			process.Dir = options.Directory
			process.Stdin = cmd.InOrStdin()
			process.Stdout = cmd.OutOrStdout()
			process.Stderr = cmd.ErrOrStderr()

			if err := process.Run(); err != nil {
				return fmt.Errorf("factory engine: %w", err)
			}

			return nil
		},
	}
}
