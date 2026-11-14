package migrate

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/plutoframework/cli/internal/project"

	"github.com/pelletier/go-toml/v2"
)

// publishedToCandidate adopts the candidate boundary profile at the current
// schema. Only the manifest, go.mod and pristine scaffold files change; edited
// files and applied SQL migrations are reported instead.
func publishedToCandidate() Step {
	return Step{
		ID: "010-published-to-candidate",
		Summary: "Adopt the candidate boundary profile: candidate sources, " +
			"managed foundation, typed operation contracts",
		From: State{Schema: project.SchemaVersion, Profile: ProfilePublished},
		To:   State{Schema: project.SchemaVersion, Profile: ProfileCandidate},
		Plan: planCandidate,
	}
}

// CandidateContracts is the --contracts input: typed declarations that cannot
// be derived from published endpoint metadata. The published profile rejects
// them, so they are supplied to the migration instead of to pluto.toml.
type CandidateContracts struct {
	Resources          []project.ResourceContract  `toml:"resources"`
	OperationContracts []project.OperationContract `toml:"operation_contracts"`
	Effects            []project.EffectContract    `toml:"effects"`
	Roles              []project.RoleContract      `toml:"roles"`
	Jobs               []candidateJobBudget        `toml:"jobs"`
}

// candidateJobBudget supplies the explicit retry budget candidate jobs need.
type candidateJobBudget struct {
	Kind        string `toml:"kind"`
	MaxAttempts int    `toml:"max_attempts"`
}

func planCandidate(w *Workspace) error {
	old := w.Manifest

	raw, _, err := w.Read("pluto.toml")
	if err != nil {
		return err
	}

	// Re-read the declarations as stored: Decode resolves endpoint fields
	// from operations, and the migrated manifest must keep the stored form.
	var stored project.Manifest
	if err := toml.Unmarshal(raw, &stored); err != nil {
		return fmt.Errorf("pluto.toml: %w", err)
	}

	next, blockers, err := candidateManifest(stored, raw, w.Options)
	if err != nil {
		return err
	}

	if len(blockers) == 0 {
		if err := next.Validate(); err != nil {
			blockers = append(blockers, Item{
				Path:   "pluto.toml",
				Line:   1,
				Reason: "candidate manifest is invalid: " + err.Error(),
				Action: "repair the declaration (or --contracts input) " +
					"and rerun pluto migrate",
			})
		}
	}

	for _, item := range blockers {
		w.Block(item)
	}

	previous, err := publishedScaffold(old)
	if err != nil {
		return err
	}

	// Rendering a target that failed validation is still useful for the
	// report; the engine discards every write of a blocked step.
	files, renderErr := project.InitialFiles(next)
	if renderErr != nil {
		if len(blockers) == 0 {
			return renderErr
		}

		return nil
	}

	if err := reconcile(w, previous, files, ProfileCandidate); err != nil {
		return err
	}

	if err := candidateModules(w, next); err != nil {
		return err
	}

	manifest, err := project.Encode(next)
	if err != nil {
		return err
	}

	if err := w.Write("pluto.toml", manifest); err != nil {
		return err
	}

	if err := danglingReferences(
		w,
		old.Project.Module,
		ProfileCandidate,
	); err != nil {
		return err
	}

	w.Manifest = next

	return nil
}

// candidateManifest is the deterministic manifest transform. It returns the
// target manifest and every precondition the published declarations cannot
// meet mechanically.
func candidateManifest(
	old project.Manifest,
	raw []byte,
	options Options,
) (project.Manifest, []Item, error) {
	next := old
	next.Boundary = options.Sources
	next.Boundary.Mode = ProfileCandidate

	next.Endpoints = append([]project.Endpoint(nil), old.Endpoints...)
	next.Jobs = append([]project.Job(nil), old.Jobs...)

	blockers := []Item{}
	boundaryLine := tomlLine(raw, "[boundary]", 0)

	if _, err := project.CandidateReplacements(next.Boundary); err != nil {
		blockers = append(blockers, Item{
			Path:   "pluto.toml",
			Line:   boundaryLine,
			Reason: err.Error(),
			Action: "pass --candidate-auth-source, --candidate-rbac-source, " +
				"--candidate-core-source and --candidate-jobs-source as " +
				"absolute checkout paths",
		})
	}

	if old.Capabilities.Tenancy != "multi" {
		blockers = append(blockers, Item{
			Path: "pluto.toml",
			Line: tomlLine(raw, "tenancy", 0),
			Reason: "candidate profile requires multi tenancy; switching " +
				"tenancy changes authorization semantics and is not " +
				"mechanical",
			Action: "move the application to tenancy = 'multi' (verified " +
				"tenant context on every protected route) first",
		})
	}

	blockers = append(blockers, identityIngress(next, raw)...)

	if len(options.Contracts) > 0 {
		var contracts CandidateContracts

		decoder := toml.NewDecoder(bytes.NewReader(options.Contracts)).
			DisallowUnknownFields()
		if err := decoder.Decode(&contracts); err != nil {
			var strict *toml.StrictMissingError
			if errors.As(err, &strict) {
				return next, nil, fmt.Errorf("--contracts: %s", strict.String())
			}

			return next, nil, fmt.Errorf("--contracts: %w", err)
		}

		next.Resources = contracts.Resources
		next.OperationContracts = contracts.OperationContracts
		next.Effects = contracts.Effects
		next.Roles = contracts.Roles

		for _, budget := range contracts.Jobs {
			for i := range next.Jobs {
				if next.Jobs[i].Kind == budget.Kind {
					next.Jobs[i].MaxAttempts = budget.MaxAttempts
				}
			}
		}
	}

	declared := map[string]bool{}
	for _, contract := range next.OperationContracts {
		declared[contract.ID] = true
	}

	for i, operation := range next.Operations {
		if declared[operation.ID()] {
			continue
		}

		blockers = append(blockers, Item{
			Path: "pluto.toml",
			Line: tomlLine(raw, "[[operations]]", i),
			Reason: "operation " + operation.ID() + " has no typed " +
				"behavior contract; endpoint metadata cannot define its " +
				"resource, states or effects",
			Action: "declare its [[resources]] and [[operation_contracts]] " +
				"(kind " + suggestedKind(operation) + ") in a --contracts " +
				"file",
		})
	}

	for i, endpoint := range next.Endpoints {
		if endpoint.Feature == "" && !endpoint.ClientVisible {
			continue
		}

		blockers = append(blockers, Item{
			Path: "pluto.toml",
			Line: tomlLine(raw, "[[endpoints]]", i),
			Reason: "route-only feature flag " + endpoint.Feature +
				" is unsupported by the candidate profile",
			Action: "remove the route flag or gate the operation itself",
		})
	}

	for i, job := range next.Jobs {
		if job.MaxAttempts >= 1 && job.MaxAttempts <= 100 {
			continue
		}

		blockers = append(blockers, Item{
			Path:   "pluto.toml",
			Line:   tomlLine(raw, "[[jobs]]", i),
			Reason: "candidate job " + job.Kind + " needs max_attempts",
			Action: "add [[jobs]] kind = '" + job.Kind + "' max_attempts " +
				"= N (1-100) to the --contracts file",
		})
	}

	return next, blockers, nil
}

// identityIngress is the hook for local credentials and OIDC. The candidate
// validator accepts both as managed ingress; their foundation files are
// reconciled like every other scaffold file. Any combination the validator
// rejects is reported here, and credential-specific transforms belong here.
func identityIngress(next project.Manifest, raw []byte) []Item {
	if !next.Capabilities.LocalCredentials && !next.Capabilities.OIDC {
		return nil
	}

	// Probe identity alone: sources, tenancy and operations are reported by
	// their own checks, and identity is rechecked once sources resolve.
	if _, err := project.CandidateReplacements(next.Boundary); err != nil {
		return nil
	}

	probe := project.Manifest{
		Boundary:      next.Boundary,
		SchemaVersion: next.SchemaVersion,
		Capabilities:  next.Capabilities,
		Database:      next.Database,
	}
	probe.Capabilities.Tenancy = "multi"

	err := project.ValidateManifestContracts(probe)
	if err == nil {
		return nil
	}

	return []Item{
		{
			Path: "pluto.toml",
			Line: tomlLine(raw, "local_credentials", 0),
			Reason: "candidate profile rejects this identity ingress: " +
				err.Error(),
			Action: "wait for candidate credential/OIDC support or disable " +
				"local_credentials and oidc before migrating",
		},
	}
}

func suggestedKind(operation project.Operation) string {
	switch {
	case operation.Collection:
		return "list or export"
	case operation.Write:
		return "create, transition or effect"
	default:
		return "load"
	}
}

// publishedScaffold renders everything the published generators would have
// produced for this manifest: initial files plus per-declaration scaffolds.
func publishedScaffold(m project.Manifest) (map[string][]byte, error) {
	files, err := project.InitialFiles(m)
	if err != nil {
		return nil, err
	}

	merge := func(extra map[string][]byte, err error) error {
		if err != nil {
			return err
		}

		for name, source := range extra {
			if _, ok := files[name]; !ok {
				files[name] = source
			}
		}

		return nil
	}

	for _, operation := range m.Operations {
		if err := merge(project.OperationFiles(m, operation)); err != nil {
			return nil, err
		}
	}

	for _, endpoint := range m.Endpoints {
		if err := merge(project.EndpointFiles(m, endpoint)); err != nil {
			return nil, err
		}
	}

	for _, job := range m.Jobs {
		if err := merge(project.JobFiles(m, job)); err != nil {
			return nil, err
		}
	}

	for _, agent := range m.Agents {
		if err := merge(project.AgentFiles(m, agent)); err != nil {
			return nil, err
		}
	}

	return files, nil
}

// candidateModules adds candidate requirements and local replacements to the
// existing go.mod, keeping every application requirement.
func candidateModules(w *Workspace, next project.Manifest) error {
	current, exists, err := w.Read("go.mod")
	if err != nil {
		return err
	}

	if !exists {
		return fmt.Errorf("go.mod not found")
	}

	updated, err := project.ConfigureBoundaryModules(
		next.Boundary,
		len(next.Agents) > 0,
		current,
	)
	if err != nil {
		return err
	}

	if err := w.Write("go.mod", updated); err != nil {
		return err
	}

	w.RequireTidy()

	return nil
}

// tomlLine finds the 1-based line of the index-th line starting with key.
func tomlLine(raw []byte, key string, index int) int {
	seen := 0

	for i, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != key && !strings.HasPrefix(trimmed, key+" ") &&
			!strings.HasPrefix(trimmed, key+"=") {
			continue
		}

		if seen == index {
			return i + 1
		}

		seen++
	}

	return 1
}
