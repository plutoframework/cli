package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

func TestAgentScaffoldConsumers(t *testing.T) {
	for _, multi := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "single", true: "multi"}[multi],
			func(t *testing.T) {
				root := temporary(t)

				args := []string{
					"init",
					".",
					"--name",
					"agent-test",
					"--module",
					"example.com/agent-test",
					"--no-mise",
					"--no-fnox",
				}
				if multi {
					args = append(args, "--multi-tenant")
				}

				mustInvoke(t, root, args...)

				module, err := os.ReadFile(filepath.Join(root, "go.mod"))
				if err != nil ||
					strings.Contains(string(module), "pluto-agent") {
					t.Fatal(
						"ordinary app acquired agent dependencies",
						err,
					)
				}

				before := snapshot(t, root)
				command := []string{
					"add",
					"agent",
					"purchases",
					"Review",
					"--permission",
					"purchases:read",
				}
				mustInvoke(t, root, append(command, "--dry-run")...)
				assertUnchanged(t, before, snapshot(t, root))

				if _, err := invoke(
					t,
					root,
					"add",
					"agent",
					"purchases",
					"Review",
				); err == nil {
					t.Fatal("missing tool access accepted")
				}

				assertUnchanged(t, before, snapshot(t, root))
				mustInvoke(t, root, command...)

				_, manifest, err := project.Discover(root)
				if err != nil || len(manifest.Agents) != 1 {
					t.Fatal("agent choices not persisted", err)
				}

				before = snapshot(t, root)
				if _, err := invoke(t, root, command...); err == nil {
					t.Fatal("duplicate agent accepted")
				}

				assertUnchanged(t, before, snapshot(t, root))
				mustInvoke(t, root, "add", "agent", "purchases", "Summarize",
					"--permission", "purchases:read", "--provider", "openai")
				external(t, root, "go", "mod", "tidy")
				external(t, root, "go", "test", "-race", "./...")
				external(t, root, "go", "vet", "./...")
				mustInvoke(t, root, "fmt")
				mustInvoke(t, root, "vet")
			},
		)
	}
}
