package project

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/scanner"
)

const (
	boundaryIdentityModule  = "github.com/plutoframework/pluto-rbac/identity"
	boundaryIdentityVersion = "v0.0.0-candidate"
)

type boundaryReplacement struct {
	module, version, target, targetVersion string
}

// ConfigureBoundaryModules preserves authored requirements and unrelated
// replacements. Candidate identities and local framework sources are explicit;
// a conflicting pin requires a deliberate upgrade, never an implicit rewrite.
func ConfigureBoundaryModules(
	profile BoundaryProfile,
	withAgents bool,
	current []byte,
) ([]byte, error) {
	replacements, err := CandidateReplacements(profile)
	if err != nil {
		return nil, err
	}

	requires, existing, err := boundaryModuleInventory(current)
	if err != nil {
		return nil, err
	}

	candidate := profile.Mode == "candidate"
	if !candidate && requires[boundaryIdentityModule] != "" {
		return nil, fmt.Errorf(
			"published boundary profile cannot retain candidate identity " +
				"requirements",
		)
	}

	if candidate && requires[boundaryIdentityModule] != "" &&
		requires[boundaryIdentityModule] != boundaryIdentityVersion {
		return nil, fmt.Errorf(
			"identity requirement %s requires explicit candidate adoption",
			requires[boundaryIdentityModule],
		)
	}

	const agentModule = "github.com/plutoframework/pluto-agent"
	if withAgents && requires[agentModule] != "" &&
		requires[agentModule] != AgentVersion {
		return nil, fmt.Errorf(
			"existing agent version %s requires explicit upgrade to %s",
			requires[agentModule],
			AgentVersion,
		)
	}

	matched := map[string]bool{}

	for _, replacement := range existing {
		local := replacement.targetVersion == ""

		framework := strings.HasPrefix(
			replacement.module,
			"github.com/plutoframework/",
		)
		if !candidate &&
			(replacement.module == boundaryIdentityModule ||
				(framework &&
					local)) {
			return nil, fmt.Errorf(
				"published boundary profile cannot retain local framework "+
					"replacement %s",
				replacement.module,
			)
		}

		path, required := replacements[replacement.module]
		if !required {
			if candidate && framework && local {
				return nil, fmt.Errorf(
					"candidate replacement %s has no declared source",
					replacement.module,
				)
			}

			continue
		}

		if matched[replacement.module] || replacement.version != "" || !local ||
			!filepath.IsAbs(replacement.target) {
			return nil, fmt.Errorf(
				"conflicting candidate replacement for %s",
				replacement.module,
			)
		}

		resolved, err := filepath.EvalSymlinks(replacement.target)
		if err != nil || resolved != path {
			return nil, fmt.Errorf(
				"conflicting candidate source for %s",
				replacement.module,
			)
		}

		matched[replacement.module] = true
	}

	var additions strings.Builder
	if withAgents && requires[agentModule] == "" {
		fmt.Fprintf(&additions, "\nrequire %s %s\n", agentModule, AgentVersion)
	}

	if candidate && requires[boundaryIdentityModule] == "" {
		fmt.Fprintf(
			&additions,
			"\nrequire %s %s\n",
			boundaryIdentityModule,
			boundaryIdentityVersion,
		)
	}

	modules := make([]string, 0, len(replacements))
	for module := range replacements {
		modules = append(modules, module)
	}

	sort.Strings(modules)

	for _, module := range modules {
		if !matched[module] {
			fmt.Fprintf(
				&additions,
				"\nreplace %s => %s\n",
				module,
				strconv.Quote(replacements[module]),
			)
		}
	}

	if additions.Len() == 0 {
		return append([]byte(nil), current...), nil
	}

	output := string(current)
	if !strings.HasSuffix(output, "\n") {
		output += "\n"
	}

	return []byte(output + additions.String()), nil
}

// Only require/replace structure is interpreted. Other directives remain byte
// for byte intact. Unrecognized relevant syntax fails closed instead of hiding
// an authority dependency behind an alternate spelling or comment.
func boundaryModuleInventory(
	current []byte,
) (map[string]string, []boundaryReplacement, error) {
	if _, err := boundaryModulePath(current); err != nil {
		return nil, nil, err
	}

	lines, err := boundaryModuleFields(current)
	if err != nil {
		return nil, nil, err
	}

	requires := map[string]string{}

	var replacements []boundaryReplacement

	block := ""

	for _, fields := range lines {
		if len(fields) == 0 {
			continue
		}

		kind := block
		if kind != "" {
			if len(fields) == 1 && fields[0] == ")" {
				block = ""
				continue
			}
		} else {
			kind = fields[0]

			fields = fields[1:]
			if len(fields) == 1 && fields[0] == "(" {
				block = kind
				continue
			}
		}

		switch kind {
		case "require":
			if len(fields) != 2 || fields[0] == "(" || fields[1] == ")" {
				return nil, nil, fmt.Errorf(
					"unsupported require syntax; format go.mod before " +
						"configuring boundaries",
				)
			}

			if _, exists := requires[fields[0]]; exists {
				return nil, nil, fmt.Errorf(
					"duplicate requirement %s",
					fields[0],
				)
			}

			requires[fields[0]] = fields[1]
		case "replace":
			for _, field := range fields {
				if field == "(" || field == ")" {
					return nil, nil, fmt.Errorf(
						"unsupported inline replacement block; format go.mod " +
							"before configuring boundaries",
					)
				}
			}

			arrow := -1

			for index, field := range fields {
				if field == "=>" {
					if arrow != -1 {
						return nil, nil, fmt.Errorf(
							"invalid replacement directive",
						)
					}

					arrow = index
				}
			}

			if (arrow != 1 && arrow != 2) ||
				(len(fields)-arrow != 2 && len(fields)-arrow != 3) {
				return nil, nil, fmt.Errorf(
					"unsupported replacement syntax; format go.mod before " +
						"configuring boundaries",
				)
			}

			replacement := boundaryReplacement{
				module: fields[0],
				target: fields[arrow+1],
			}
			if arrow == 2 {
				replacement.version = fields[1]
			}

			if len(fields)-arrow == 3 {
				replacement.targetVersion = fields[arrow+2]
			}

			if replacement.targetVersion == "" &&
				!filepath.IsAbs(replacement.target) &&
				!strings.HasPrefix(replacement.target, "./") &&
				!strings.HasPrefix(replacement.target, "../") {
				return nil, nil, fmt.Errorf(
					"replacement target without a version must be a " +
						"filesystem path",
				)
			}

			replacements = append(replacements, replacement)
		}
	}

	if block != "" {
		return nil, nil, fmt.Errorf("unterminated go.mod directive block")
	}

	return requires, replacements, nil
}

// Preserve quoted path atoms while treating comments as whitespace. Adjacent
// punctuation in ordinary module paths is joined; directive operators remain
// separate even when the input omits spaces around them.
func boundaryModuleFields(data []byte) ([][]string, error) {
	var scan scanner.Scanner
	scan.Init(strings.NewReader(string(data)))

	scan.Mode = scanner.ScanIdents |
		scanner.ScanStrings |
		scanner.ScanComments |
		scanner.SkipComments
	scan.Error = func(_ *scanner.Scanner, _ string) {}

	var (
		lines [][]string
		raw   []string
	)

	line, lastEnd := 0, -1

	var parseErr error

	finish := func() {
		if len(raw) == 0 {
			return
		}

		var fields []string

		for _, value := range raw {
			if strings.HasPrefix(value, "\"") {
				decoded, err := strconv.Unquote(value)
				if err != nil {
					parseErr = err
					return
				}

				fields = append(fields, decoded)
			} else {
				value = strings.NewReplacer(
					"=>",
					" => ",
					"(",
					" ( ",
					")",
					" ) ",
				).
					Replace(value)
				fields = append(fields, strings.Fields(value)...)
			}
		}

		lines = append(lines, fields)
	}

	for kind := scan.Scan(); kind != scanner.EOF; kind = scan.Scan() {
		if scan.Line != line {
			finish()

			raw = nil
			line = scan.Line
			lastEnd = -1
		}

		value := scan.TokenText()
		if len(raw) > 0 && scan.Offset == lastEnd && kind != scanner.String {
			raw[len(raw)-1] += value
		} else {
			raw = append(raw, value)
		}

		lastEnd = scan.Offset + len(value)
		if kind == scanner.String {
			lastEnd = -1
		}
	}

	finish()

	if parseErr != nil || scan.ErrorCount != 0 {
		return nil, fmt.Errorf("invalid go.mod token syntax")
	}

	return lines, nil
}
