package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceInventoryCoversGoFixtures(t *testing.T) {
	for _, path := range []string{
		"testdata/fixture.go",
		"testdata/fixture.go.tmpl",
		"internal/cli/testdata/fixture.go.tmpl",
		"fixture.go.tmpl",
	} {
		t.Run(path, func(t *testing.T) {
			for _, readable := range []bool{false, true} {
				source := "package fixture\n" +
					"// {{ is literal comment content in valid Go. }}\n"
				if readable {
					source += "var callback = func() {\n" +
						"    first()\n    second()\n}\n"
				} else {
					source += "var callback = func(){first();second()}\n"
				}

				root := t.TempDir()

				absolute := filepath.Join(root, path)
				if err := os.MkdirAll(
					filepath.Dir(absolute),
					0o755,
				); err != nil {
					t.Fatal(err)
				}

				if err := os.WriteFile(
					absolute,
					[]byte(source),
					0o600,
				); err != nil {
					t.Fatal(err)
				}

				command := exec.Command("go", "run", ".", root)

				output, err := command.CombinedOutput()
				if readable {
					if err != nil {
						t.Fatalf(
							"wrapped fixture rejected: %v\n%s",
							err,
							output,
						)
					}

					continue
				}

				if err == nil || !strings.Contains(
					string(output),
					"expand callback body onto separate lines",
				) {
					t.Fatalf(
						"compact fixture escaped: error=%v\n%s",
						err,
						output,
					)
				}
			}
		})
	}
}

func TestInterpretedFixtureUsesActualDelimiters(t *testing.T) {
	for _, name := range []string{"behavior", "cache", "local_lifecycle"} {
		source, err := os.ReadFile(
			"../../internal/cli/testdata/" +
				name +
				".go.tmpl",
		)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := fixtureSources(name+".go.tmpl", source); err != nil {
			t.Fatalf("%s actual template delimiters: %v", name, err)
		}
	}

	source := []byte("package fixture\nvar module = \"{{.Module}}\"\nvar " +
		"rows = []Item{{Value: \"a\"}}\n")
	if _, err := fixtureSources("behavior.go.tmpl", source); err == nil {
		t.Fatal("ordinary Go brace collision accepted as template")
	}
}
