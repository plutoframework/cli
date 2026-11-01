package cli

import (
	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

// Candidate sources are opt-in inputs, never inferred from sibling checkouts.
// Project validation rejects unsupported profiles before generation writes.
func addBoundaryProfileFlags(
	command *cobra.Command,
	profile *project.BoundaryProfile,
) {
	mode := profile.Mode
	if mode == "" {
		mode = "published"
	}

	command.Flags().
		StringVar(&profile.Mode, "boundary-profile", mode, "Boundary dependencies: published compatibility or explicit local candidate")
	command.Flags().
		StringVar(&profile.AuthSource, "candidate-auth-source", profile.AuthSource, "Absolute candidate Pluto Auth checkout")
	command.Flags().
		StringVar(&profile.RBACSource, "candidate-rbac-source", profile.RBACSource, "Absolute candidate Pluto RBAC checkout")
	command.Flags().
		StringVar(&profile.CoreSource, "candidate-core-source", profile.CoreSource, "Absolute candidate Pluto Core checkout")
	command.Flags().
		StringVar(&profile.JobsSource, "candidate-jobs-source", profile.JobsSource, "Absolute candidate Pluto Jobs checkout")
}
