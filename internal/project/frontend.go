package project

import "fmt"

// Frontend is optional, additive metadata in schema 7. Version identifies the
// template contract; changing UI libraries requires an explicit migration.
type Frontend struct {
	Theme      *Theme              `toml:"theme,omitempty"`
	Framework  string              `toml:"framework"`
	Directory  string              `toml:"directory"`
	UI         string              `toml:"ui"`
	Version    int                 `toml:"template_version"`
	Auth       bool                `toml:"auth"`
	Components []FrontendComponent `toml:"components,omitempty"`
}

type FrontendComponent struct {
	Name string `toml:"name"`
	Kind string `toml:"kind"`
}

func (m Manifest) ValidateFrontend() error {
	f := m.Frontend
	if f == nil {
		return nil
	}

	if f.Framework != "react" || f.Version != 1 {
		return fmt.Errorf(
			"frontend requires framework=react and template_version=1",
		)
	}

	if !appName.MatchString(f.Directory) {
		return fmt.Errorf(
			"frontend directory must be a single lowercase kebab-case directory",
		)
	}

	switch f.Directory {
	case "cmd",
		"internal",
		"vendor",
		"scripts",
		"db",
		"openapi",
		"bin",
		"node-modules":
		return fmt.Errorf("reserved frontend directory %q", f.Directory)
	}

	switch f.UI {
	case "native", "heroui-v3", "shadcn":
	default:
		return fmt.Errorf("frontend ui must be native, heroui-v3 or shadcn")
	}

	if f.Auth && !m.Capabilities.LocalCredentials {
		return fmt.Errorf(
			"frontend auth requires a backend initialized with --local-credentials; frontend generation never enables credential issuance",
		)
	}

	if err := f.ValidateTheme(); err != nil {
		return err
	}

	seen := map[string]bool{}
	for _, component := range f.Components {
		if !appName.MatchString(component.Name) || seen[component.Name] {
			return fmt.Errorf(
				"component name must be unique lowercase kebab-case",
			)
		}

		switch component.Kind {
		case "button", "input", "card", "dialog", "form":
		default:
			return fmt.Errorf(
				"component kind must be button, input, card, dialog or form",
			)
		}

		seen[component.Name] = true
	}

	return nil
}
