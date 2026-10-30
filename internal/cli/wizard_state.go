package cli

import (
	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func stateForm(model wizardModel, manifest project.Manifest,
	command *cobra.Command, args []string,
) wizardModel {
	model.title = "Add a business lifecycle"
	model.fields = []wizardField{
		textField("group", "Domain group", "Lowercase Go package.",
			argAt(args, 0, "")),
		boolField("dry-run", "Preview files only", "Validate without writing.",
			seed(command, "dry-run", "false")),
	}
	model.build = func(values map[string]string) ([]string, error) {
		if _, err := project.StateFiles(manifest, values["group"]); err != nil {
			return nil, err
		}

		return []string{
			"add", "state", values["group"],
			"--dry-run=" + values["dry-run"],
		}, nil
	}

	return model
}
