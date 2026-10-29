package cli

import (
	"testing"

	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func TestAgentWizardUsesValidatedCommand(t *testing.T) {
	model := agentForm(wizardModel{}, project.Manifest{}, &cobra.Command{}, nil)
	values := map[string]string{
		"group":      "purchases",
		"action":     "Review",
		"permission": "purchases:read",
		"dry-run":    "true",
		"provider":   "scripted",
	}

	args, err := model.build(values)
	if err != nil || len(args) != 7 || args[1] != "agent" {
		t.Fatalf("bad wizard command: %v %v", args, err)
	}

	values["permission"] = ""
	if _, err := model.build(values); err == nil {
		t.Fatal("missing access accepted")
	}
}
