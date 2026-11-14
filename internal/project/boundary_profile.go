package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/scanner"
)

// BoundaryProfile makes unpublished dependency adoption explicit. Empty mode
// retains published compatibility; it never silently enables candidate APIs.
//
// Each candidate library is selected either by a local checkout (Source,
// written as a go.mod replacement) or by exact fetchable module versions
// (Pins, written as plain requirements). Revisions are transient CLI inputs:
// they are resolved and verified into Pins before anything is written.
type BoundaryProfile struct {
	Mode       string            `toml:"mode"`
	AuthSource string            `toml:"auth_source,omitempty"`
	RBACSource string            `toml:"rbac_source,omitempty"`
	CoreSource string            `toml:"core_source,omitempty"`
	JobsSource string            `toml:"jobs_source,omitempty"`
	Pins       map[string]string `toml:"pins,omitempty"`

	AuthRevision string `toml:"-"`
	RBACRevision string `toml:"-"`
	CoreRevision string `toml:"-"`
	JobsRevision string `toml:"-"`
}

// candidateLibrary is one framework repository and the modules the candidate
// profile binds from it.
type candidateLibrary struct {
	name             string
	source, revision string
	modules          []string
}

func candidateLibraries(profile BoundaryProfile) []candidateLibrary {
	return []candidateLibrary{
		{
			"pluto-auth",
			profile.AuthSource,
			profile.AuthRevision,
			[]string{"", "stores/postgres", "methods/password"},
		},
		{
			"pluto-rbac",
			profile.RBACSource,
			profile.RBACRevision,
			[]string{"", "identity"},
		},
		{
			"pluto-core",
			profile.CoreSource,
			profile.CoreRevision,
			[]string{"", "stores/postgres"},
		},
		{
			"pluto-jobs",
			profile.JobsSource,
			profile.JobsRevision,
			[]string{"", "stores/postgres"},
		},
	}
}

func (library candidateLibrary) modulePath(suffix string) string {
	path := "github.com/plutoframework/" + library.name
	if suffix != "" {
		path += "/" + suffix
	}

	return path
}

func ValidateBoundaryProfile(profile BoundaryProfile, tenancy string) error {
	if profile.Mode == "candidate" && tenancy != "multi" {
		return fmt.Errorf(
			"candidate boundary profile requires explicit multi tenancy",
		)
	}

	if _, err := CandidateReplacements(profile); err != nil {
		return err
	}

	_, err := CandidatePins(profile)

	return err
}

// CandidateReplacements inspects local metadata only: no network access,
// application scripts, module commands or dependency downloads are performed.
// Libraries selected by pins have no replacement; pins are checked by
// CandidatePins.
func CandidateReplacements(profile BoundaryProfile) (map[string]string, error) {
	result := map[string]string{}

	if profile.Mode == "" || profile.Mode == "published" {
		if profile.AuthSource != "" || profile.RBACSource != "" ||
			profile.CoreSource != "" ||
			profile.JobsSource != "" {
			return nil, fmt.Errorf(
				"published boundary profile forbids candidate source paths",
			)
		}

		if len(profile.Pins) > 0 || profile.AuthRevision != "" ||
			profile.RBACRevision != "" || profile.CoreRevision != "" ||
			profile.JobsRevision != "" {
			return nil, fmt.Errorf(
				"published boundary profile forbids candidate module pins",
			)
		}

		return result, nil
	}

	if profile.Mode != "candidate" {
		return nil, fmt.Errorf(
			"boundary profile mode must be published or candidate",
		)
	}

	for _, repository := range candidateLibraries(profile) {
		if repository.source == "" &&
			(repository.revision != "" ||
				candidateLibraryPinned(profile, repository)) {
			continue
		}

		if !filepath.IsAbs(repository.source) {
			return nil, fmt.Errorf(
				"candidate %s source must be an explicit absolute "+
					"directory, or pin exact module versions",
				repository.name,
			)
		}

		root, err := filepath.EvalSymlinks(repository.source)
		if err != nil {
			return nil, fmt.Errorf(
				"candidate %s source: %w",
				repository.name,
				err,
			)
		}

		for _, suffix := range repository.modules {
			directory, err := filepath.EvalSymlinks(
				filepath.Join(root, filepath.FromSlash(suffix)),
			)
			if err != nil {
				return nil, fmt.Errorf(
					"candidate %s/%s module: %w",
					repository.name,
					suffix,
					err,
				)
			}

			relative, err := filepath.Rel(root, directory)
			if err != nil || relative == ".." ||
				strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf(
					"candidate %s/%s module escapes its declared source",
					repository.name,
					suffix,
				)
			}

			info, err := os.Stat(directory)
			if err != nil || !info.IsDir() {
				return nil, fmt.Errorf(
					"candidate %s/%s module is not an accessible directory",
					repository.name,
					suffix,
				)
			}

			data, err := os.ReadFile(filepath.Join(directory, "go.mod"))
			if err != nil {
				return nil, fmt.Errorf(
					"candidate %s/%s go.mod: %w",
					repository.name,
					suffix,
					err,
				)
			}

			module, err := boundaryModulePath(data)

			expected := repository.modulePath(suffix)

			if err != nil || module != expected {
				return nil, fmt.Errorf(
					"candidate %s/%s requires exact module %s",
					repository.name,
					suffix,
					expected,
				)
			}

			result[expected] = directory
		}
	}

	return result, nil
}

// Read the module directive without executing tooling. Comments and quoted
// paths are recognized; separated unquoted tokens and duplicate directives are
// rejected. This validates module identity, not the complete dependency graph.
func boundaryModulePath(data []byte) (string, error) {
	var scan scanner.Scanner
	scan.Init(strings.NewReader(string(data)))

	scan.Mode = scanner.ScanIdents |
		scanner.ScanStrings |
		scanner.ScanComments |
		scanner.SkipComments
	scan.Error = func(_ *scanner.Scanner, _ string) {}

	type token struct {
		text   string
		offset int
	}

	var line []token

	lineNumber, count := 0, 0
	module := ""
	valid := true
	finish := func() {
		if len(line) == 0 || line[0].text != "module" {
			return
		}

		count++

		if len(line) < 2 {
			valid = false
			return
		}

		if strings.HasPrefix(line[1].text, "\"") {
			value, err := strconv.Unquote(line[1].text)
			if err != nil || len(line) != 2 {
				valid = false
			}

			module = value

			return
		}

		var value strings.Builder

		for i, item := range line[1:] {
			if i > 0 && item.offset != line[i].offset+len(line[i].text) {
				valid = false
			}

			value.WriteString(item.text)
		}

		module = value.String()
	}

	for kind := scan.Scan(); kind != scanner.EOF; kind = scan.Scan() {
		if scan.Line != lineNumber {
			finish()

			line = nil
			lineNumber = scan.Line
		}

		line = append(line, token{scan.TokenText(), scan.Offset})
	}

	finish()

	if !valid || count != 1 || scan.ErrorCount != 0 || module == "" {
		return "", fmt.Errorf("invalid module directive")
	}

	return module, nil
}
