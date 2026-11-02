package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeArchitectureInventory(
	t *testing.T,
	root string,
	files map[string]string,
) {
	t.Helper()

	for name, source := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func architectureInventoryFixture(t *testing.T, root string) {
	t.Helper()
	writeArchitectureInventory(t, root, map[string]string{
		"go.mod":           "module example.test/inventory\n",
		"pluto.toml":       fmt.Sprintf("schema_version=%d\n", SchemaVersion),
		"scripts/check.sh": "pluto vet\n",
	})
}

func TestArchitectureInspectionNeverSkipsRoot(t *testing.T) {
	for _, name := range []string{"ordinary", "dist", "testdata", "vendor"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), name)
			architectureInventoryFixture(t, root)
			writeArchitectureInventory(t, root, map[string]string{
				"internal/domain/rule.go": "package domain\nimport _ \"os\"\n",
			})

			if !candidateReviewHas(
				InspectArchitecture(root).Diagnostics,
				"ARCH101",
				"internal/domain/rule.go",
			) {
				t.Fatal("root name disabled domain effect inspection")
			}
		})
	}
}

func TestArchitectureModuleRootAndDirectiveSyntax(t *testing.T) {
	for _, directive := range []string{
		"module example.test/inventory\n",
		"module example.test/inventory // application module\n",
		"module \"example.test/inventory\" // application module\n",
	} {
		t.Run(directive, func(t *testing.T) {
			root := testRoot(t)
			architectureInventoryFixture(t, root)
			writeArchitectureInventory(t, root, map[string]string{
				"go.mod": directive,
				"contract.go": "package contract\nimport \"os\"\n" +
					"type File = os.File\n",
				"internal/domain/rule.go": "package domain\n" +
					"import root \"example.test/inventory\"\n" +
					"type File = root.File\n",
			})

			report := InspectArchitecture(root)
			for _, diagnostic := range report.Diagnostics {
				if diagnostic.ID == "ARCH003" || diagnostic.ID == "ARCH006" {
					t.Fatalf(
						"valid local module was unresolved: %+v",
						diagnostic,
					)
				}
			}

			if !candidateReviewHas(
				report.Diagnostics,
				"ARCH101",
				"contract.go",
			) {
				t.Fatal(
					"domain dependency through module root was not traversed",
				)
			}
		})
	}
}

func TestArchitectureImportedIgnoredPackages(t *testing.T) {
	for _, name := range []string{"dist", "testdata", "vendor"} {
		t.Run(name, func(t *testing.T) {
			root := testRoot(t)
			architectureInventoryFixture(t, root)

			prefix := "internal/" + name
			writeArchitectureInventory(t, root, map[string]string{
				"internal/domain/rule.go": "package domain\nimport _ " +
					fmt.Sprintf(
						"%q\n",
						"example.test/inventory/"+prefix+"/first",
					),
				prefix + "/first/helper.go": "package first\nimport _ " +
					fmt.Sprintf(
						"%q\n",
						"example.test/inventory/"+prefix+"/second",
					),
				prefix + "/second/helper.go": "package second\n" +
					"import _ \"os\"\n",
				prefix + "/unimported/broken.go": "invalid Go fixture",
			})

			report := InspectArchitecture(root)
			for _, diagnostic := range report.Diagnostics {
				if diagnostic.ID == "ARCH004" || diagnostic.ID == "ARCH006" {
					t.Fatalf(
						"inventory confused fixture and imported code: %+v",
						diagnostic,
					)
				}
			}

			if !candidateReviewHas(
				report.Diagnostics,
				"ARCH101",
				prefix+"/second/helper.go",
			) {
				t.Fatal(
					"transitive effect in imported ignored package was missed",
				)
			}
		})
	}
}

func TestArchitectureDeclaredIgnoredCandidatePackage(t *testing.T) {
	for _, name := range []string{"dist", "testdata", "vendor"} {
		t.Run(name, func(t *testing.T) {
			m := manifestContractFixture(t)
			m.SchemaVersion = SchemaVersion
			m.Project = Project{
				Name: "inventory", Module: "example.test/inventory",
				EnvPrefix: "INVENTORY_",
			}
			m.Database = Database{Engine: "postgres", Queries: "sqlc"}

			m.Resources[0].Group = name
			for index := range m.OperationContracts {
				m.OperationContracts[index].Resource = name + ".Invoice"
			}

			files, err := InitialFiles(m)
			if err != nil {
				t.Fatal(err)
			}

			root := testRoot(t)
			if _, err := Apply(root, files, nil, false); err != nil {
				t.Fatal(err)
			}

			for _, diagnostic := range InspectArchitecture(root).Diagnostics {
				if diagnostic.ID == "ARCH006" &&
					strings.Contains(diagnostic.Message, "/"+name) {
					t.Fatalf(
						"declared generated package missing: %+v",
						diagnostic,
					)
				}
			}
		})
	}
}

func TestCandidateInventoryNeverSkipsRoot(t *testing.T) {
	for _, name := range []string{"ordinary", "dist", "testdata", "vendor"} {
		t.Run(name, func(t *testing.T) {
			root := candidateReviewFixture(t, map[string]string{
				"internal/access/extra.go": "package access\n" +
					"var forged = Caller{callerTenant: \"forged\"}\n",
			})

			named := filepath.Join(t.TempDir(), name)
			if err := os.Rename(root, named); err != nil {
				t.Fatal(err)
			}

			diagnostics, _ := inspectCandidateBoundaries(
				named,
				candidateReviewModule,
				[]byte(candidateReviewMetadata(t)),
			)
			for _, id := range []string{"ARCH602", "ARCH604"} {
				if !candidateReviewHas(
					diagnostics,
					id,
					"internal/access/extra.go",
				) {
					t.Fatalf("%s root hid %s: %+v", name, id, diagnostics)
				}
			}
		})
	}
}
