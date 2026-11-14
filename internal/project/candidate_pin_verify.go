package project

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// candidateAPIProbe references candidate-only APIs the managed candidate
// runtime and declared resource contracts depend on, one or more per pinned
// module. It is type-checked
// against the exact pinned versions; a pin that only carries the published API
// fails to compile instead of being trusted by its version string.
const candidateAPIProbe = `package probe

import (
	auth "github.com/plutoframework/pluto-auth"
	"github.com/plutoframework/pluto-auth/methods/password"
	authpostgres "github.com/plutoframework/pluto-auth/stores/postgres"
	"github.com/plutoframework/pluto-core/cache"
	"github.com/plutoframework/pluto-core/idempotency"
	"github.com/plutoframework/pluto-core/pagination"
	"github.com/plutoframework/pluto-core/state"
	corepostgres "github.com/plutoframework/pluto-core/stores/postgres"
	jobs "github.com/plutoframework/pluto-jobs"
	jobspostgres "github.com/plutoframework/pluto-jobs/stores/postgres"
	rbac "github.com/plutoframework/pluto-rbac"
	identity "github.com/plutoframework/pluto-rbac/identity"
)

var (
	_ = (*auth.Manager).UseIdentity
	_ = (*auth.Manager).WithIdentity
	_ *auth.Identity
	_ password.AttemptLimiter
	_ password.Sender
	_ = authpostgres.NewLocal
	_ = (*rbac.Authorizer).Require
	_ = (*rbac.Requirement).Execute
	_ = (*rbac.Requirement).WithResourcePolicy
	_ rbac.ResourcePolicyFunc
	_ = identity.New
	_ = (*identity.Gate).Within
	_ = (*identity.Gate).Use
	_ = idempotency.NewClaim
	_ = state.New[string, string, struct{}]
	_ = (*state.Machine[string, string, struct{}]).Evaluate
	_ state.Evaluated[string, string]
	_ state.Binding[string]
	_ = pagination.Finish[struct{}]
	_ = cache.NewLoader[struct{}]
	_ = corepostgres.New
	_ = jobs.NewDefinition[struct{}]
	_ = (*jobs.Definition[struct{}]).Bind
	_ jobs.Delivery
	_ = jobspostgres.New
)
`

// goModuleInfo is the subset of go list -m -json and go mod download -json
// output used here. Error is a string for downloads and an object for lists.
type goModuleInfo struct {
	Path    string
	Version string
	GoMod   string
	Error   json.RawMessage
}

// failure returns the reported module error, if any.
func (info goModuleInfo) failure() string {
	if len(info.Error) == 0 || string(info.Error) == "null" {
		return ""
	}

	var text string
	if json.Unmarshal(info.Error, &text) == nil {
		return text
	}

	var object struct {
		Err string
	}
	if json.Unmarshal(info.Error, &object) == nil && object.Err != "" {
		return object.Err
	}

	return string(info.Error)
}

// goModuleCommand runs the go command outside any workspace with module mode
// forced, so the user's GOFLAGS/go.work cannot redirect resolution. GOPROXY,
// GONOSUMDB, GOPRIVATE and checksum database settings are inherited.
func goModuleCommand(
	ctx context.Context,
	directory string,
	args ...string,
) ([]byte, error) {
	command := exec.CommandContext(ctx, "go", args...)
	command.Dir = directory

	command.Env = append(
		os.Environ(),
		"GOWORK=off",
		"GOFLAGS=-mod=mod",
		"GO111MODULE=on",
	)

	var stderr bytes.Buffer

	command.Stderr = &stderr

	output, err := command.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 2000 {
			detail = detail[len(detail)-2000:]
		}

		return output, fmt.Errorf(
			"go %s: %w: %s",
			strings.Join(args, " "),
			err,
			detail,
		)
	}

	return output, nil
}

// decodeModuleInfo reads the concatenated JSON objects printed by go -json.
func decodeModuleInfo(output []byte) ([]goModuleInfo, error) {
	decoder := json.NewDecoder(bytes.NewReader(output))

	var infos []goModuleInfo

	for decoder.More() {
		var info goModuleInfo
		if err := decoder.Decode(&info); err != nil {
			return nil, fmt.Errorf("decode go module output: %w", err)
		}

		infos = append(infos, info)
	}

	return infos, nil
}

// ResolveCandidatePins resolves each library revision (a full commit hash)
// into the exact canonical version of every module the profile binds from
// that library. Resolution goes through the Go module system; the returned
// profile carries pins and no revisions. Pins are not yet verified.
func ResolveCandidatePins(
	ctx context.Context,
	profile BoundaryProfile,
) (BoundaryProfile, error) {
	if profile.Mode != "candidate" {
		if CandidateRevisionsRequested(profile) {
			return profile, fmt.Errorf(
				"candidate revisions require --boundary-profile candidate",
			)
		}

		return profile, nil
	}

	directory, err := os.MkdirTemp("", "pluto-candidate-resolve-")
	if err != nil {
		return profile, err
	}
	defer os.RemoveAll(directory)

	pins := map[string]string{}
	for module, version := range profile.Pins {
		pins[module] = version
	}

	for _, library := range candidateLibraries(profile) {
		if library.revision == "" {
			continue
		}

		if library.source != "" {
			return profile, fmt.Errorf(
				"candidate %s selects both a local source and a revision",
				library.name,
			)
		}

		if !candidateCommit.MatchString(library.revision) {
			return profile, fmt.Errorf(
				"candidate %s revision must be a full 40-character "+
					"lowercase commit hash",
				library.name,
			)
		}

		for _, suffix := range library.modules {
			module := library.modulePath(suffix)

			output, err := goModuleCommand(
				ctx,
				directory,
				"list",
				"-m",
				"-json",
				module+"@"+library.revision,
			)
			if err != nil {
				return profile, fmt.Errorf(
					"resolve candidate %s: %w",
					module,
					err,
				)
			}

			infos, err := decodeModuleInfo(output)
			if err != nil {
				return profile, err
			}

			if len(infos) != 1 || infos[0].failure() != "" ||
				infos[0].Path != module ||
				!candidatePinVersion.MatchString(infos[0].Version) {
				return profile, fmt.Errorf(
					"candidate %s@%s did not resolve to one exact module "+
						"version",
					module,
					library.revision,
				)
			}

			pins[module] = infos[0].Version
		}
	}

	profile.Pins = pins
	profile.AuthRevision = ""
	profile.RBACRevision = ""
	profile.CoreRevision = ""
	profile.JobsRevision = ""

	return profile, nil
}

// VerifyCandidatePins proves the pins before adoption: each module downloads
// at exactly its pinned version (checksum-verified by the go command), its
// go.mod declares the expected module path, the pinned set is
// self-consistent (minimal version selection keeps every pin) and the
// candidate API probe compiles against it. Libraries selected by a local
// source take part through their validated replacement. It returns the
// verified go.sum lines of the pinned modules.
func VerifyCandidatePins(
	ctx context.Context,
	profile BoundaryProfile,
) ([]byte, error) {
	replacements, err := CandidateReplacements(profile)
	if err != nil {
		return nil, err
	}

	pins, err := CandidatePins(profile)
	if err != nil || len(pins) == 0 {
		return nil, err
	}

	directory, err := os.MkdirTemp("", "pluto-candidate-verify-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)

	modules := candidatePinnedModules(pins)

	var probe strings.Builder

	probe.WriteString("module pluto.invalid/candidateprobe\n\ngo 1.25.5\n")

	for _, module := range modules {
		fmt.Fprintf(&probe, "\nrequire %s %s\n", module, pins[module])
	}

	for _, module := range candidatePinnedModules(replacements) {
		fmt.Fprintf(
			&probe,
			"\nrequire %s %s\n\nreplace %s => %s\n",
			module,
			boundaryIdentityVersion,
			module,
			strconv.Quote(replacements[module]),
		)
	}

	if err := os.WriteFile(
		filepath.Join(directory, "go.mod"),
		[]byte(probe.String()),
		0o600,
	); err != nil {
		return nil, err
	}

	for _, module := range modules {
		if err := verifyCandidateDownload(
			ctx,
			directory,
			module,
			pins[module],
		); err != nil {
			return nil, err
		}
	}

	if err := os.WriteFile(
		filepath.Join(directory, "probe.go"),
		[]byte(candidateAPIProbe),
		0o600,
	); err != nil {
		return nil, err
	}

	if _, err := goModuleCommand(ctx, directory, "build", "./..."); err != nil {
		return nil, fmt.Errorf(
			"pinned candidate modules do not provide the required "+
				"candidate APIs: %w",
			err,
		)
	}

	output, err := goModuleCommand(
		ctx,
		directory,
		append([]string{"list", "-m", "-json"}, modules...)...,
	)
	if err != nil {
		return nil, err
	}

	selected, err := decodeModuleInfo(output)
	if err != nil {
		return nil, err
	}

	if len(selected) != len(modules) {
		return nil, fmt.Errorf("pinned candidate selection is incomplete")
	}

	for _, info := range selected {
		if pins[info.Path] != info.Version {
			return nil, fmt.Errorf(
				"candidate pins are inconsistent: %s %s is raised to %s by "+
					"another pinned module",
				info.Path,
				pins[info.Path],
				info.Version,
			)
		}
	}

	sums, err := os.ReadFile(filepath.Join(directory, "go.sum"))
	if err != nil {
		return nil, fmt.Errorf("pinned candidate checksums: %w", err)
	}

	return candidatePinSums(sums, pins)
}

// verifyCandidateDownload downloads one exact module version and checks the
// module path declared by its own go.mod.
func verifyCandidateDownload(
	ctx context.Context,
	directory, module, version string,
) error {
	output, err := goModuleCommand(
		ctx,
		directory,
		"mod",
		"download",
		"-json",
		module+"@"+version,
	)

	infos, decodeErr := decodeModuleInfo(output)
	if decodeErr != nil {
		return decodeErr
	}

	if len(infos) == 1 && infos[0].failure() != "" {
		return fmt.Errorf(
			"download candidate %s@%s: %s",
			module,
			version,
			infos[0].failure(),
		)
	}

	if err != nil {
		return fmt.Errorf("download candidate %s@%s: %w", module, version, err)
	}

	if len(infos) != 1 || infos[0].Path != module ||
		infos[0].Version != version || infos[0].GoMod == "" {
		return fmt.Errorf(
			"candidate %s@%s is not an exact downloadable module version",
			module,
			version,
		)
	}

	data, err := os.ReadFile(infos[0].GoMod)
	if err != nil {
		return fmt.Errorf("candidate %s@%s go.mod: %w", module, version, err)
	}

	declared, err := boundaryModulePath(data)
	if err != nil || declared != module {
		return fmt.Errorf(
			"candidate %s@%s declares module %q, not the exact module",
			module,
			version,
			declared,
		)
	}

	return nil
}

// candidatePinSums keeps only the go.sum lines of the pinned module versions.
// Both the module and go.mod hashes must be present.
func candidatePinSums(sums []byte, pins map[string]string) ([]byte, error) {
	var result strings.Builder

	found := map[string]int{}

	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}

		version := strings.TrimSuffix(fields[1], "/go.mod")
		if pins[fields[0]] != version {
			continue
		}

		found[fields[0]]++

		result.WriteString(line + "\n")
	}

	for _, module := range candidatePinnedModules(pins) {
		if found[module] != 2 {
			return nil, fmt.Errorf(
				"pinned candidate %s has no verified checksums",
				module,
			)
		}
	}

	return []byte(result.String()), nil
}
