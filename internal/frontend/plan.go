// Package frontend generates application-owned React features for curated UI
// profiles. Planning is offline; installation is an explicit subsequent phase.
package frontend

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/plutoframework/cli/internal/project"
)

//go:embed all:templates all:profiles
var sources embed.FS

type Plan struct {
	Manifest project.Manifest
	Files    map[string][]byte
	Previous map[string][]byte
}

type templateData struct {
	Name, Prefix, UI, Directory string
	Component, Symbol, Kind     string
}

func render(name string, data templateData) ([]byte, error) {
	path := "templates/" + name
	switch name {
	case "component.tsx.tmpl":
		path = "profiles/" + data.UI + "/feature-" + data.Kind + ".tsx"
	case "src/features/session/forms/AuthForm.tsx.tmpl":
		path = "profiles/" + data.UI + "/auth-form.tsx"
	case "src/features/session/pages/AuthPage.tsx.tmpl":
		path = "profiles/" + data.UI + "/auth-page.tsx"
	case "src/app/styles.css.tmpl":
		path = "profiles/" + data.UI + "/styles.css"
	case "vite.config.ts.tmpl":
		path = "profiles/" + data.UI + "/vite.config.ts"
	}

	source, err := sources.ReadFile(path)
	if err != nil {
		return nil, err
	}

	t, err := template.New(name).Delims("<%", "%>").Parse(string(source))
	if err != nil {
		return nil, err
	}

	var output bytes.Buffer
	if err := t.Execute(&output, data); err != nil {
		return nil, err
	}

	return output.Bytes(), nil
}

func dataFor(m project.Manifest) templateData {
	return templateData{
		Name: m.Project.Name, Prefix: m.Project.EnvPrefix,
		UI: m.Frontend.UI, Directory: m.Frontend.Directory,
	}
}

func Init(
	root string,
	m project.Manifest,
	ui, directory string,
	auth bool,
) (Plan, error) {
	if m.Frontend != nil {
		return Plan{}, fmt.Errorf(
			"frontend already initialized; changing UI requires explicit migration",
		)
	}

	next := m

	next.Frontend = &project.Frontend{
		Framework: "react",
		Directory: directory,
		UI:        ui,
		Version:   1,
		Auth:      auth,
		Theme: &project.Theme{
			Mode:      "system",
			Overrides: directory + "/src/styles/overrides.css",
		},
	}
	if err := next.Validate(); err != nil {
		return Plan{}, err
	}

	p := Plan{
		Manifest: next,
		Files:    map[string][]byte{},
		Previous: map[string][]byte{},
	}
	for _, name := range initialTemplates {
		if err := p.addTemplate(name); err != nil {
			return Plan{}, err
		}
	}

	pkg, err := packageJSON(next)
	if err != nil {
		return Plan{}, err
	}

	p.Files[filepath.Join(directory, "package.json")] = pkg
	if err := p.profile(); err != nil {
		return Plan{}, err
	}

	if auth {
		if err := p.auth(); err != nil {
			return Plan{}, err
		}
	}

	if err := p.catalog(); err != nil {
		return Plan{}, err
	}

	if err := p.metadata(root); err != nil {
		return Plan{}, err
	}

	return p, nil
}

func Add(root string, m project.Manifest, name, kind string) (Plan, error) {
	if m.Frontend == nil {
		return Plan{}, fmt.Errorf("run pluto frontend init first")
	}

	if err := Contracts(root, m); err != nil {
		return Plan{}, err
	}

	f := *m.Frontend

	f.Components = append([]project.FrontendComponent(nil), f.Components...)
	if kind == "auth" {
		if f.Auth {
			return Plan{}, fmt.Errorf(
				"frontend auth already installed; authored files are never overwritten",
			)
		}

		f.Auth = true
	} else {
		f.Components = append(
			f.Components,
			project.FrontendComponent{Name: name, Kind: kind},
		)
	}

	next := m

	next.Frontend = &f
	if err := next.Validate(); err != nil {
		return Plan{}, err
	}

	p := Plan{
		Manifest: next,
		Files:    map[string][]byte{},
		Previous: map[string][]byte{},
	}
	if kind == "auth" {
		if err := p.auth(); err != nil {
			return Plan{}, err
		}
	} else {
		data := dataFor(next)
		data.Component, data.Kind, data.Symbol = name, kind, symbol(name)

		source, err := render("component.tsx.tmpl", data)
		if err != nil {
			return Plan{}, err
		}

		p.Files[filepath.Join(f.Directory, "src/features", name, "components", data.Symbol+".tsx")] = source
		if kind == "form" {
			test, err := render("component.test.tsx.tmpl", data)
			if err != nil {
				return Plan{}, err
			}

			p.Files[filepath.Join(f.Directory, "src/features", name, "components", data.Symbol+".test.tsx")] = test
		}

		p.Files[filepath.Join(f.Directory, "src/features", name, "public.ts")] = []byte(
			fmt.Sprintf(
				"export { %s } from \"./components/%s\";\n",
				data.Symbol,
				data.Symbol,
			),
		)
	}

	if err := p.primitives(root, m); err != nil {
		return Plan{}, err
	}

	if err := p.catalog(); err != nil {
		return Plan{}, err
	}

	catalogPath := filepath.Join(f.Directory, "src/app/catalog.gen.ts")

	old, err := catalog(m)
	if err != nil {
		return Plan{}, err
	}

	p.Previous[catalogPath] = old
	if err := p.mergePackage(root, m); err != nil {
		return Plan{}, err
	}

	if err := p.metadata(root); err != nil {
		return Plan{}, err
	}

	return p, nil
}

func symbol(name string) string {
	var out strings.Builder
	for _, part := range strings.Split(name, "-") {
		out.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}

	return out.String()
}

func (p *Plan) addTemplate(name string) error {
	data, err := render(name, dataFor(p.Manifest))
	if err != nil {
		return err
	}

	p.Files[filepath.Join(p.Manifest.Frontend.Directory, strings.TrimSuffix(name, ".tmpl"))] = data

	return nil
}

func (p *Plan) metadata(root string) error {
	path, err := project.SafePath(root, "pluto.toml")
	if err != nil {
		return err
	}

	previous, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	data, err := project.Encode(p.Manifest)
	if err != nil {
		return err
	}

	p.Previous["pluto.toml"] = previous
	p.Files["pluto.toml"] = data

	return nil
}

func sortedKeys(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

func jsonBytes(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	return append(data, '\n'), err
}

var initialTemplates = []string{
	"mise.toml.tmpl",
	".gitignore.tmpl",
	".npmrc.tmpl",
	".prettierignore.tmpl",
	".prettierrc.json.tmpl",
	"AGENTS.md.tmpl",
	"README.md.tmpl",
	"index.html.tmpl",
	"tsconfig.json.tmpl",
	"vite.config.ts.tmpl",
	"eslint.config.js.tmpl",
	"src/main.tsx.tmpl",
	"src/app/Application.tsx.tmpl",
	"src/app/styles.css.tmpl",
	"src/app/query-client.ts.tmpl",
	"src/app/router.tsx.tmpl",
	"src/api/client.ts.tmpl",
	"src/api/errors.ts.tmpl",
	"scripts/certificate.mjs.tmpl",
	"scripts/generate-api.mjs.tmpl",
	"scripts/check.mjs.tmpl",
	"scripts/format.mjs.tmpl",
	"scripts/format.test.mjs.tmpl",
	"scripts/readability.mjs.tmpl",
	"scripts/theme.mjs.tmpl",
	"scripts/theme-vite.mjs.tmpl",
	"scripts/theme-vite.d.mts.tmpl",
	"scripts/theme.test.mjs.tmpl",
	"src/styles/overrides.css.tmpl",
	"scripts/architecture.mjs.tmpl",
	"scripts/architecture.test.mjs.tmpl",
	"src/test/setup.ts.tmpl",
	"src/shared/lib/search.ts.tmpl",
	"src/shared/lib/search.test.ts.tmpl",
}
