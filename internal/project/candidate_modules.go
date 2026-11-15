package project

import (
	"bytes"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
		prunable, scanErr := prunableCandidatePins(root, m.Boundary)

		expected, configureErr := configureBoundaryModules(
			m.Boundary,
			len(m.Agents) > 0,
			current,
			prunable,
		)
		if configureErr == nil && scanErr != nil {
			configureErr = scanErr
		}

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

// prunableCandidatePins lists pinned modules that no application Go file
// imports; go mod tidy removes their requirements. Each import belongs to the
// longest candidate module path that prefixes it. Unparseable sources fail
// closed by making nothing prunable.
func prunableCandidatePins(
	root string,
	profile BoundaryProfile,
) (map[string]bool, error) {
	pins, err := CandidatePins(profile)
	if err != nil || len(pins) == 0 {
		return nil, err
	}

	imported := map[string]bool{}
	files := token.NewFileSet()

	err = filepath.WalkDir(
		root,
		func(current string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if entry.IsDir() {
				base := entry.Name()
				if current != root && (strings.HasPrefix(base, ".") ||
					strings.HasPrefix(base, "_") || base == "vendor" ||
					base == "testdata" || base == "node_modules") {
					return filepath.SkipDir
				}

				return nil
			}

			if !entry.Type().IsRegular() || filepath.Ext(current) != ".go" {
				return nil
			}

			parsed, err := parser.ParseFile(
				files,
				current,
				nil,
				parser.ImportsOnly,
			)
			if err != nil {
				return err
			}

			for _, spec := range parsed.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return err
				}

				owner := ""

				for module := range pins {
					if (path == module ||
						strings.HasPrefix(path, module+"/")) &&
						len(module) > len(owner) {
						owner = module
					}
				}

				imported[owner] = true
			}

			return nil
		},
	)
	if err != nil {
		return nil, err
	}

	prunable := map[string]bool{}

	for module := range pins {
		if !imported[module] && module != boundaryIdentityModule {
			prunable[module] = true
		}
	}

	return prunable, nil
}
