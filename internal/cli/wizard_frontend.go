package cli

import (
	"fmt"

	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func frontendMenu(kind string) wizardModel {
	choices := []wizardChoice{
		{"Initialize a React frontend", "frontend-init"},
		{"Add a frontend component", "frontend-component"},
		{"Add authentication screens", "frontend-auth"},
	}
	if kind == "frontend-add" {
		choices = choices[1:]
	}

	m := wizardModel{
		kind:    kind,
		title:   "Build your frontend",
		width:   80,
		height:  24,
		menu:    true,
		context: "React / saved UI library / application-owned components",
		fields: []wizardField{
			choiceField(
				"action",
				"Choose an action",
				"Review settings before any files are written.",
				choices[0].value,
				choices...),
		},
		build: func(v map[string]string) ([]string, error) { return []string{v["action"]}, nil },
	}
	m.focus(0)

	return m
}

func frontendForm(
	model wizardModel,
	m project.Manifest,
	cmd *cobra.Command,
	args []string,
) wizardModel {
	model.title = "Build your frontend"
	if m.Frontend != nil {
		model.context += " / UI: " + m.Frontend.UI
	}

	switch model.kind {
	case "frontend-init":
		model.fields = []wizardField{
			choiceField(
				"ui",
				"Component library",
				"Future generators reuse this choice. Migration is explicit.",
				seed(cmd, "ui", "native"),
				wizardChoice{
					"Native controls (smallest dependency set)",
					"native",
				},
				wizardChoice{
					"HeroUI v3",
					"heroui-v3",
				},
				wizardChoice{"shadcn (application-owned source)", "shadcn"},
			),
			textField(
				"directory",
				"Frontend directory",
				"Single project-relative directory.",
				seed(cmd, "directory", "web"),
			),
			boolField(
				"auth",
				"Include auth screens",
				"Requires an existing local-credential backend; never enables credential issuance.",
				seed(cmd, "auth", "false"),
			),
		}
	case "frontend-component":
		model.fields = []wizardField{
			textField(
				"name",
				"Component name",
				"Lowercase kebab-case, for example purchase-form.",
				argAt(args, 0, ""),
			),
			choiceField(
				"kind",
				"Component kind",
				"Automatically adds required controls for your saved UI library.",
				seed(cmd, "kind", "form"),
				wizardChoice{
					"Form",
					"form",
				},
				wizardChoice{"Button", "button"},
				wizardChoice{"Input", "input"},
				wizardChoice{"Card", "card"},
				wizardChoice{"Dialog", "dialog"},
			),
		}
	default:
		model.fields = []wizardField{
			choiceField(
				"feature",
				"Feature",
				"Sign-in, enrollment, verification and recovery. Go owns authentication.",
				"auth",
				wizardChoice{"Authentication", "auth"},
			),
		}
	}

	if model.kind == "frontend-component" && seed(cmd, "from", "") != "" {
		model.fields = model.fields[:1]
		model.context += " / registry: " + seed(cmd, "from", "")
	}

	model.fields = append(
		model.fields,
		boolField(
			"no-install",
			"Skip dependency installation",
			"Normally installs pins, generates API types, formats and checks.",
			seed(cmd, "no-install", "false"),
		),
		boolField(
			"dry-run",
			"Preview only",
			"Offline preflight; no writes or tool execution.",
			seed(cmd, "dry-run", "false"),
		),
	)
	model.build = func(v map[string]string) ([]string, error) {
		result := []string{"frontend"}

		switch model.kind {
		case "frontend-init":
			candidate := m

			candidate.Frontend = &project.Frontend{
				Framework: "react",
				Directory: v["directory"],
				UI:        v["ui"],
				Version:   1,
				Auth:      v["auth"] == "true",
			}
			if err := candidate.Validate(); err != nil {
				return nil, err
			}

			result = append(
				result,
				"init",
				"--ui="+v["ui"],
				"--directory="+v["directory"],
				"--auth="+v["auth"],
			)
		case "frontend-component":
			if m.Frontend == nil {
				return nil, fmt.Errorf("run pluto frontend init first")
			}

			if source := seed(cmd, "from", ""); source != "" {
				if !project.FrontendName(v["name"]) {
					return nil, fmt.Errorf(
						"component name must be lowercase kebab-case",
					)
				}

				result = append(
					result,
					"add",
					"component",
					v["name"],
					"--from="+source,
				)
			} else {
				candidate := m
				f := *m.Frontend
				f.Components = append(
					append([]project.FrontendComponent(nil), f.Components...),
					project.FrontendComponent{Name: v["name"], Kind: v["kind"]},
				)

				candidate.Frontend = &f
				if err := candidate.Validate(); err != nil {
					return nil, err
				}

				result = append(
					result,
					"add",
					"component",
					v["name"],
					"--kind="+v["kind"],
				)
			}
		default:
			result = append(result, "add", "auth")
		}

		return append(
			result,
			"--no-install="+v["no-install"],
			"--dry-run="+v["dry-run"],
		), nil
	}

	return model
}
