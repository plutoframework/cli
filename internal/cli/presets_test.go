package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

func TestInitPresetDefaultsAndOptOuts(t *testing.T) {
	for omitted := 0; omitted < 8; omitted++ {
		t.Run(fmt.Sprintf("omit-%03b", omitted), func(t *testing.T) {
			root := temporary(t)
			args := []string{"init", ".", "--name", "presets"}

			expected := project.Development{
				Docker: omitted&1 == 0,
				Mise:   omitted&2 == 0,
				Fnox:   omitted&4 == 0,
			}
			if !expected.Docker {
				args = append(args, "--no-docker")
			}

			if !expected.Mise {
				args = append(args, "--no-mise")
			}

			if !expected.Fnox {
				args = append(args, "--no-fnox")
			}

			mustInvoke(t, root, args...)

			_, manifest, err := project.Discover(root)
			if err != nil {
				t.Fatal(err)
			}

			if manifest.Development != expected {
				t.Fatalf(
					"saved presets=%+v; want %+v",
					manifest.Development,
					expected,
				)
			}

			files := map[string]bool{
				"Dockerfile":    expected.Docker,
				"compose.yaml":  expected.Docker,
				".dockerignore": expected.Docker,
				"mise.toml":     expected.Mise,
				"fnox.toml":     expected.Fnox,
				".golangci.yml": true,
				".env.example":  true,
				"sqlc.yaml":     true,
			}
			checkFiles := func() {
				t.Helper()

				for path, present := range files {
					_, err := os.Stat(filepath.Join(root, path))
					if present && err != nil ||
						!present && !os.IsNotExist(err) {
						t.Fatalf(
							"%s: expected present=%t, stat error=%v",
							path,
							present,
							err,
						)
					}
				}
			}
			checkFiles()

			if expected.Mise {
				data, err := os.ReadFile(filepath.Join(root, "mise.toml"))
				if err != nil {
					t.Fatal(err)
				}

				if strings.Contains(
					string(data),
					"fnox exec --",
				) != expected.Fnox ||
					strings.Contains(
						string(data),
						`fnox = "1.43.0"`,
					) != expected.Fnox {
					t.Fatal("mise run task/tool pins do not match fnox choice")
				}
			}

			if expected.Fnox && !expected.Mise {
				data, err := os.ReadFile(filepath.Join(root, "README.md"))
				if err != nil ||
					!strings.Contains(
						string(data),
						"Install\nfnox 1.43.0 on PATH",
					) ||
					strings.Contains(string(data), "`mise run run`") {
					t.Fatalf("standalone fnox instructions missing: %v", err)
				}
			}

			mustInvoke(
				t,
				root,
				"add",
				"endpoint",
				"widgets",
				"Get",
				"--method",
				"GET",
				"--path",
				"/api/widgets/{id}",
				"--name",
				"Read widget",
				"--description",
				"Read authorized widget",
			)
			checkFiles()

			_, after, err := project.Discover(root)
			if err != nil || after.Development != expected {
				t.Fatalf(
					"endpoint addition changed saved presets: %+v %v",
					after.Development,
					err,
				)
			}
		})
	}
}

func TestInitOptOutHelp(t *testing.T) {
	root := temporary(t)

	help := mustInvoke(t, root, "init", "--help")
	for _, flag := range []string{"--no-docker", "--no-mise", "--no-fnox"} {
		if !strings.Contains(help, flag) {
			t.Fatalf("missing %s help", flag)
		}
	}

	output := mustInvoke(
		t,
		root,
		"init",
		"demo",
		"--dry-run",
		"--no-docker=false",
		"--no-mise=false",
		"--no-fnox=false",
	)
	for _, path := range []string{"Dockerfile", "mise.toml", "fnox.toml"} {
		if !strings.Contains(output, path) {
			t.Fatalf("false opt-out omitted %s", path)
		}
	}
}
