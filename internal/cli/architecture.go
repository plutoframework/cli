package cli

import (
	"encoding/json"
	"fmt"

	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func architectureCommand(options Options) *cobra.Command {
	directory, format := options.Directory, "text"
	command := &cobra.Command{
		Use:   "architecture",
		Short: "Inspect production source dependency boundaries without migration",
	}
	check := &cobra.Command{
		Use:  "check",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if format != "json" && format != "text" {
				return fmt.Errorf("format must be text or json")
			}

			report := project.InspectArchitecture(directory)
			if format == "json" {
				if err := json.NewEncoder(cmd.OutOrStdout()).
					Encode(report); err != nil {
					return err
				}
			} else {
				for _, d := range report.Diagnostics {
					cmd.Printf(
						"%s:%d: %s: %s\n  Repair: %s\n",
						d.File,
						d.Line,
						d.ID,
						d.Message,
						d.Repair,
					)
				}

				for _, u := range report.Unassessed {
					cmd.Println("Unassessed: " + u)
				}
			}

			if !report.Passed {
				return fmt.Errorf(
					"architecture inspection found %d violations or blockers",
					len(report.Diagnostics),
				)
			}

			return nil
		},
	}
	check.Flags().
		StringVar(&directory, "directory", directory, "Project directory to inspect (read-only)")
	check.Flags().
		StringVar(&format, "format", format, "Output format: text or json")
	command.AddCommand(check)

	return command
}
