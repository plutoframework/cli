package project

import (
	"strings"
	"testing"
)

// When another initial production file is added, the candidate ownership plan
// must adopt it explicitly. An existing file is not trusted just by its path.
func TestCandidateFoundationOwnsInitialProductionPaths(t *testing.T) {
	for _, profile := range []string{"sessions", "password", "oidc"} {
		t.Run(profile, func(t *testing.T) {
			manifest := Manifest{
				SchemaVersion: SchemaVersion,
				Project: Project{
					Name:      "foundation",
					Module:    "example.test/foundation",
					EnvPrefix: "FOUNDATION_",
				},
				Capabilities: Capabilities{
					Tenancy:          "multi",
					Cache:            "none",
					LocalCredentials: profile != "sessions",
					OIDC:             profile == "oidc",
				},
				Database: Database{Engine: "postgres", Queries: "sqlc"},
			}

			initial, err := InitialFiles(manifest)
			if err != nil {
				t.Fatal(err)
			}

			managed, err := Managed(manifest)
			if err != nil {
				t.Fatal(err)
			}

			manifest.Boundary.Mode = "candidate"

			foundation, err := candidateFoundation(manifest)
			if err != nil {
				t.Fatal(err)
			}

			production := func(path string) bool {
				return strings.HasSuffix(path, ".go") &&
					!strings.HasSuffix(path, "_test.go") &&
					(strings.HasPrefix(path, "cmd/") || strings.HasPrefix(path, "internal/")) ||
					strings.HasPrefix(path, "db/migrations/") &&
						strings.HasSuffix(path, ".sql")
			}
			for path := range initial {
				if _, alreadyManaged := managed[path]; alreadyManaged ||
					!production(path) {
					continue
				}

				if _, owned := foundation[path]; !owned {
					t.Errorf(
						"initial production file lacks candidate foundation ownership: %s",
						path,
					)
				}
			}

			for path := range foundation {
				if _, initialPath := initial[path]; !initialPath ||
					!production(path) {
					t.Errorf(
						"foundation includes non-production or unrelated output: %s",
						path,
					)
				}

				if strings.HasSuffix(path, ".sql") &&
					string(initial[path]) != string(foundation[path]) {
					t.Errorf(
						"foundation SQL drifted from initial migration: %s",
						path,
					)
				}
			}
		})
	}
}
