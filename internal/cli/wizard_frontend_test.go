package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

func TestFrontendWizardReusesRawCommands(t *testing.T) {
	t.Setenv("CI", "")

	root := frontendApp(t, true)

	var output bytes.Buffer

	options := Options{
		Directory: root,
		In:        strings.NewReader(""),
		Out:       &output,
		Err:       &output,
		terminal:  func(io.Reader, io.Writer) bool { return true },
		wizard: func(_ context.Context, _ io.Reader, _ io.Writer, m wizardModel) (wizardModel, error) {
			values := map[string]string{"no-install": "true"}

			switch m.kind {
			case "frontend":
				values["action"] = "frontend-init"
			case "frontend-init":
				values["ui"] = "shadcn"
				values["auth"] = "true"
			case "frontend-add":
				values["action"] = "frontend-component"
			case "frontend-component":
				values["name"] = "purchase-form"
				values["kind"] = "form"
			}

			return acceptWizard(t, m, values), nil
		},
	}
	for _, args := range [][]string{{"frontend"}, {"frontend", "add"}} {
		cmd := New(options)
		cmd.SetArgs(args)

		if err := cmd.ExecuteContext(context.Background()); err != nil {
			t.Fatal(err, output.String())
		}
	}

	_, m, err := project.Discover(root)
	if err != nil || m.Frontend == nil || m.Frontend.UI != "shadcn" ||
		!m.Frontend.Auth ||
		len(m.Frontend.Components) != 1 {
		t.Fatalf("wizard dispatch: %+v %v", m.Frontend, err)
	}

	if !strings.Contains(output.String(), "Equivalent command:") {
		t.Fatal(output.String())
	}
}

func TestFrontendWizardPreservesRegistry(t *testing.T) {
	t.Setenv("CI", "")

	root := registryApp(t)

	var output bytes.Buffer

	command := New(Options{
		Directory: root,
		In:        strings.NewReader(""),
		Out:       &output,
		Err:       &output,
		terminal:  func(io.Reader, io.Writer) bool { return true },
		wizard: func(_ context.Context, _ io.Reader, _ io.Writer, model wizardModel) (wizardModel, error) {
			return acceptWizard(
				t,
				model,
				map[string]string{"name": "review", "no-install": "true"},
			), nil
		},
	})
	command.SetArgs(
		[]string{
			"frontend",
			"add",
			"component",
			"--from",
			"company/panel",
			"--interactive",
		},
	)

	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err, output.String())
	}

	_, manifest, err := project.Discover(root)
	if err != nil || len(manifest.Frontend.Components) != 1 ||
		manifest.Frontend.Components[0].From != "company/panel" {
		t.Fatalf("lost registry: %v", err)
	}
}
