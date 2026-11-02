package cli

import (
	"fmt"

	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func agentForm(model wizardModel, m project.Manifest,
	cmd *cobra.Command, args []string,
) wizardModel {
	model.title = "Add an opt-in read-only agent"
	model.fields = []wizardField{
		textField(
			"group",
			"Agent group",
			"Lowercase Go package.",
			argAt(args, 0, ""),
		),
		textField(
			"action",
			"Agent name",
			"Exported Go name.",
			argAt(args, 1, "Review"),
		),
		textField(
			"permission",
			"RBAC permission",
			"Explicit capability; resource access is checked by the injected "+
				"operation.",
			seed(cmd, "permission", ""),
		),
		choiceField(
			"provider",
			"Provider",
			"Scripted is deterministic and requires no credentials.",
			seed(cmd, "provider", "scripted"),
			wizardChoice{"Scripted demo", "scripted"},
			wizardChoice{"OpenAI", "openai"},
		),
		textField(
			"operation",
			"Existing read operation",
			"Optional declared protected read use case.",
			seed(cmd, "operation", ""),
		),

		boolField(
			"dry-run",
			"Preview files only",
			"Validate without writing.",
			seed(cmd, "dry-run", "false"),
		),
	}
	model.build = func(v map[string]string) ([]string, error) {
		candidate := m

		operation, _, err := ensureEntryOperation(
			&candidate,
			v["operation"],
			v["group"],
			v["action"],
			v["permission"],
			false,
			false,
			false,
		)
		if err != nil {
			return nil, err
		}

		if operation.Permission == "" {
			return nil, fmt.Errorf("agent requires explicit permission")
		}

		agents := append(
			append([]project.Agent(nil), m.Agents...),
			project.Agent{
				Operation:  operation.ID(),
				Group:      v["group"],
				Action:     v["action"],
				Permission: v["permission"],
				Provider:   v["provider"],
			},
		)
		if err := project.ValidateAgents(agents); err != nil {
			return nil, err
		}

		return []string{
			"add",
			"agent",
			v["group"],
			v["action"],
			"--permission=" + v["permission"],
			"--operation=" + v["operation"],
			"--provider=" + v["provider"],
			"--dry-run=" + v["dry-run"],
		}, nil
	}

	return model
}
