package project

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// Metadata cannot claim one dependency profile while go.mod selects another.
// This checks explicit bindings, not the contents of external dependencies.
func inspectCandidateModules(
	root string,
	metadata []byte,
) []ArchitectureDiagnostic {
	var m Manifest
	if toml.Unmarshal(metadata, &m) != nil || m.Boundary.Mode != "candidate" {
		return nil
	}

	current, err := os.ReadFile(filepath.Join(root, "go.mod"))
	path := "go.mod"
	message := "candidate module bindings cannot be assessed"

	if err != nil {
		path = "pluto.toml"
		message += "; go.mod is missing"
	} else {
		expected, configureErr := ConfigureBoundaryModules(
			m.Boundary,
			len(m.Agents) > 0,
			current,
		)
		if configureErr == nil && bytes.Equal(expected, current) {
			return nil
		}

		if configureErr != nil {
			message += ": " + configureErr.Error()
		} else {
			message = "candidate go.mod does not contain the required " +
				"declared dependency bindings"
		}
	}

	return []ArchitectureDiagnostic{
		{
			ID:       "ARCH607",
			File:     path,
			Line:     1,
			Severity: "error",
			Message:  message,
			Repair: "Preserve authored module requirements and explicitly " +
				"restore the validated candidate sources/identity " +
				"dependency; declaration alone does not select the " +
				"required runtime.",
		},
	}
}
