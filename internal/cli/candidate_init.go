package cli

import (
	"context"
	"strings"

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
		StringVar(
			&profile.Mode,
			"boundary-profile",
			mode,
			"Boundary dependencies: published compatibility or explicit "+
				"local candidate",
		)
	addCandidateSourceFlags(command, profile)
}

// addCandidateSourceFlags declares explicit candidate inputs: a local checkout
// (go.mod replacement) or a pushed commit resolved to exact module versions
// (plain requirements that build anywhere the Go module proxy reaches).
func addCandidateSourceFlags(
	command *cobra.Command,
	profile *project.BoundaryProfile,
) {
	command.Flags().
		StringVar(
			&profile.AuthSource,
			"candidate-auth-source",
			profile.AuthSource,
			"Absolute candidate Pluto Auth checkout",
		)
	command.Flags().
		StringVar(
			&profile.RBACSource,
			"candidate-rbac-source",
			profile.RBACSource,
			"Absolute candidate Pluto RBAC checkout",
		)
	command.Flags().
		StringVar(
			&profile.CoreSource,
			"candidate-core-source",
			profile.CoreSource,
			"Absolute candidate Pluto Core checkout",
		)
	command.Flags().
		StringVar(
			&profile.JobsSource,
			"candidate-jobs-source",
			profile.JobsSource,
			"Absolute candidate Pluto Jobs checkout",
		)

	for _, revision := range []struct {
		target *string
		name   string
	}{
		{&profile.AuthRevision, "Auth"},
		{&profile.RBACRevision, "RBAC"},
		{&profile.CoreRevision, "Core"},
		{&profile.JobsRevision, "Jobs"},
	} {
		command.Flags().
			StringVar(
				revision.target,
				"candidate-"+strings.ToLower(revision.name)+"-revision",
				*revision.target,
				"Pinned candidate Pluto "+revision.name+" commit (full "+
					"hash); verified exact module versions, no replace",
			)
	}
}

// pinCandidateProfile resolves requested revisions to exact module versions
// and verifies every pin before anything is written. It returns the profile
// to persist and the verified go.sum lines of the pinned modules.
func pinCandidateProfile(
	ctx context.Context,
	profile project.BoundaryProfile,
) (project.BoundaryProfile, []byte, error) {
	if !project.CandidateRevisionsRequested(profile) {
		return profile, nil, nil
	}

	resolved, err := project.ResolveCandidatePins(ctx, profile)
	if err != nil {
		return profile, nil, err
	}

	if _, err := project.CandidateReplacements(resolved); err != nil {
		return profile, nil, err
	}

	sums, err := project.VerifyCandidatePins(ctx, resolved)
	if err != nil {
		return profile, nil, err
	}

	return resolved, sums, nil
}
