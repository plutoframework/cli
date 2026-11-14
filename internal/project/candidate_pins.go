package project

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	candidatePinVersion = regexp.MustCompile(
		`^v[01]\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
			`(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`,
	)
	candidatePseudoRevision = regexp.MustCompile(
		`[.-][0-9]{14}-([0-9a-f]{12})$`,
	)
	candidateCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// candidateLibraryPinned reports whether any module of the library is pinned.
func candidateLibraryPinned(
	profile BoundaryProfile,
	library candidateLibrary,
) bool {
	for _, suffix := range library.modules {
		if _, ok := profile.Pins[library.modulePath(suffix)]; ok {
			return true
		}
	}

	return false
}

// CandidatePins validates pinned candidate modules offline: exact canonical
// versions for every module of each pinned library, no library selected by
// both a source and pins, and no unrelated pin. It proves nothing about the
// fetched contents; VerifyCandidatePins does that before pins are adopted, and
// compiling the application against go.mod/go.sum does it afterwards.
func CandidatePins(profile BoundaryProfile) (map[string]string, error) {
	result := map[string]string{}
	if profile.Mode != "candidate" {
		return result, nil
	}

	known := map[string]bool{}

	for _, library := range candidateLibraries(profile) {
		for _, suffix := range library.modules {
			known[library.modulePath(suffix)] = true
		}

		if library.revision != "" {
			return nil, fmt.Errorf(
				"candidate %s revision %s must be resolved to verified "+
					"module versions before use",
				library.name,
				library.revision,
			)
		}

		if !candidateLibraryPinned(profile, library) {
			continue
		}

		if library.source != "" {
			return nil, fmt.Errorf(
				"candidate %s selects both a local source and pinned "+
					"module versions",
				library.name,
			)
		}

		revision := ""

		for _, suffix := range library.modules {
			module := library.modulePath(suffix)

			version, ok := profile.Pins[module]
			if !ok {
				return nil, fmt.Errorf(
					"candidate %s pins require an exact version for %s",
					library.name,
					module,
				)
			}

			if !candidatePinVersion.MatchString(version) {
				return nil, fmt.Errorf(
					"candidate pin %s %q must be an exact canonical module "+
						"version",
					module,
					version,
				)
			}

			// Submodules carry their own tags and pseudo-version bases, so
			// only the commit of each pseudo-version can be compared.
			match := candidatePseudoRevision.FindStringSubmatch(version)
			if match != nil {
				if revision != "" && revision != match[1] {
					return nil, fmt.Errorf(
						"candidate %s pins name different commits",
						library.name,
					)
				}

				revision = match[1]
			}

			result[module] = version
		}
	}

	unknown := []string{}

	for module := range profile.Pins {
		if !known[module] {
			unknown = append(unknown, module)
		}
	}

	if len(unknown) > 0 {
		sort.Strings(unknown)

		return nil, fmt.Errorf(
			"candidate pins name modules outside the candidate profile: %s",
			strings.Join(unknown, ", "),
		)
	}

	return result, nil
}

// CandidateRevisionsRequested reports whether any library is selected by a
// revision that still needs resolution.
func CandidateRevisionsRequested(profile BoundaryProfile) bool {
	for _, library := range candidateLibraries(profile) {
		if library.revision != "" {
			return true
		}
	}

	return false
}

// candidatePinnedModules lists the pinned module paths in sorted order.
func candidatePinnedModules(pins map[string]string) []string {
	modules := make([]string, 0, len(pins))
	for module := range pins {
		modules = append(modules, module)
	}

	sort.Strings(modules)

	return modules
}

// MergeSums appends go.sum lines that are not already present.
func MergeSums(current, additions []byte) []byte {
	present := map[string]bool{}
	for _, line := range strings.Split(string(current), "\n") {
		present[strings.TrimSpace(line)] = true
	}

	result := string(current)
	if result != "" && !strings.HasSuffix(result, "\n") {
		result += "\n"
	}

	for _, line := range strings.Split(string(additions), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || present[line] {
			continue
		}

		present[line] = true
		result += line + "\n"
	}

	return []byte(result)
}
