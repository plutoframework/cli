package project

import (
	"strconv"
	"strings"
)

// AdoptBoundaryModules is the explicit adoption path used by pluto init and
// pluto migrate. Requirements of pinned candidate modules are moved to their
// verified pins; every other line is preserved byte for byte and the result is
// then held to ConfigureBoundaryModules, which never rewrites a requirement.
func AdoptBoundaryModules(
	profile BoundaryProfile,
	withAgents bool,
	current []byte,
) ([]byte, error) {
	pins, err := CandidatePins(profile)
	if err != nil {
		return nil, err
	}

	if len(pins) > 0 {
		current = dropPinnedRequirements(current, pins)
	}

	return ConfigureBoundaryModules(profile, withAgents, current)
}

// dropPinnedRequirements removes single-line requirements of pinned modules
// that select another version. Unrecognized spellings are left in place, so
// the strict inventory in ConfigureBoundaryModules rejects them.
func dropPinnedRequirements(current []byte, pins map[string]string) []byte {
	lines := strings.SplitAfter(string(current), "\n")
	kept := make([]string, 0, len(lines))
	block := false

	for _, line := range lines {
		fields := goModLineFields(line)

		switch {
		case !block && len(fields) == 2 && fields[0] == "require" &&
			fields[1] == "(":
			block = true
		case block && len(fields) == 1 && fields[0] == ")":
			block = false
		case block && len(fields) == 2:
			if version, ok := pins[fields[0]]; ok && version != fields[1] {
				continue
			}
		case !block && len(fields) == 3 && fields[0] == "require":
			if version, ok := pins[fields[1]]; ok && version != fields[2] {
				continue
			}
		}

		kept = append(kept, line)
	}

	return []byte(strings.Join(kept, ""))
}

// goModLineFields splits one go.mod line without its trailing comment and
// unquotes quoted atoms. Malformed quoting yields no fields.
func goModLineFields(line string) []string {
	quoted := false

	for index := 0; index+1 < len(line); index++ {
		switch {
		case line[index] == '"' && (index == 0 || line[index-1] != '\\'):
			quoted = !quoted
		case !quoted && line[index] == '/' && line[index+1] == '/':
			line = line[:index]
		}
	}

	fields := strings.Fields(line)
	for index, field := range fields {
		if strings.HasPrefix(field, "\"") {
			value, err := strconv.Unquote(field)
			if err != nil {
				return nil
			}

			fields[index] = value
		}
	}

	return fields
}
