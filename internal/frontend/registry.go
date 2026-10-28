package frontend

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/plutoframework/cli/internal/project"

	"github.com/pelletier/go-toml/v2"
)

type RegistryItem struct {
	Primitives  []string          `toml:"primitives,omitempty"`
	Name        string            `toml:"name"`
	Description string            `toml:"description"`
	Profiles    []string          `toml:"profiles"`
	Requires    []string          `toml:"requires,omitempty"`
	Files       []RegistryFile    `toml:"files"`
	Packages    map[string]string `toml:"dependencies,omitempty"`
	DevPackages map[string]string `toml:"dev_dependencies,omitempty"`
}

type RegistryFile struct {
	Source string `toml:"source"`
	Target string `toml:"target"`
}

type localRegistry struct {
	Version int            `toml:"schema_version"`
	Items   []RegistryItem `toml:"items"`
}

type registryEntry struct {
	item   RegistryItem
	source map[string][]byte
	digest string
}

// Registries are data and templates only. No script, hook or remote execution.
func registryEntries(
	root string,
	m project.Manifest,
) (map[string]registryEntry, error) {
	entries := map[string]registryEntry{}
	if m.Frontend == nil {
		return entries, fmt.Errorf("run pluto frontend init first")
	}

	for _, namespace := range sortedRegistryNames(m.Frontend.Registries) {
		location := m.Frontend.Registries[namespace].Path

		name, err := project.SafePath(
			root,
			filepath.Join(location, "registry.toml"),
		)
		if err != nil {
			return nil, err
		}

		data, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}

		var registry localRegistry
		if err := toml.NewDecoder(bytes.NewReader(data)).
			DisallowUnknownFields().
			Decode(&registry); err != nil {
			return nil, fmt.Errorf("registry %s: %w", namespace, err)
		}

		if registry.Version != 1 || len(registry.Items) > 100 {
			return nil, fmt.Errorf(
				"registry %s requires schema_version=1 and at most 100 items",
				namespace,
			)
		}

		for _, item := range registry.Items {
			ref := namespace + "/" + item.Name
			if !project.FrontendName(item.Name) || len(item.Files) == 0 ||
				len(item.Files) > 100 ||
				len(item.Profiles) == 0 {
				return nil, fmt.Errorf("invalid registry item %s", ref)
			}

			if _, exists := entries[ref]; exists {
				return nil, fmt.Errorf("duplicate registry item %s", ref)
			}

			for _, profile := range item.Profiles {
				if profile != "native" && profile != "heroui-v3" &&
					profile != "shadcn" {
					return nil, fmt.Errorf(
						"unsupported registry profile %s",
						profile,
					)
				}
			}

			entry := registryEntry{item: item, source: map[string][]byte{}}
			hash := sha256.New()

			metadata, err := toml.Marshal(item)
			if err != nil {
				return nil, err
			}

			_, _ = hash.Write(metadata)

			for _, file := range item.Files {
				if !project.RegistryPath(file.Source) {
					return nil, fmt.Errorf(
						"unsafe registry source %s",
						file.Source,
					)
				}

				source, err := project.SafePath(
					root,
					filepath.Join(location, filepath.FromSlash(file.Source)),
				)
				if err != nil {
					return nil, err
				}

				contents, err := os.ReadFile(source)
				if err != nil {
					return nil, err
				}

				if len(contents) > 256*1024 ||
					bytes.Count(contents, []byte("\n")) > 1000 {
					return nil, fmt.Errorf(
						"registry template exceeds size/line limit: %s",
						file.Source,
					)
				}

				entry.source[file.Source] = contents
				// Delimited digest input includes metadata, paths and complete source bytes.
				_, _ = fmt.Fprintf(
					hash,
					"\n%d:%s:%d:",
					len(file.Source),
					file.Source,
					len(contents),
				)
				_, _ = hash.Write(contents)
			}

			entry.digest = hex.EncodeToString(hash.Sum(nil))
			entries[ref] = entry
		}
	}

	return entries, nil
}

func sortedRegistryNames(values map[string]project.ComponentRegistry) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

func Components(root string, m project.Manifest) ([]string, error) {
	entries, err := registryEntries(root, m)
	if err != nil {
		return nil, err
	}

	result := []string{
		"pluto/button",
		"pluto/input",
		"pluto/card",
		"pluto/dialog",
		"pluto/form",
	}
	for ref, entry := range entries {
		result = append(
			result,
			fmt.Sprintf(
				"%s [%s] %s",
				ref,
				strings.Join(entry.item.Profiles, ","),
				entry.item.Description,
			),
		)
	}

	sort.Strings(result)

	return result, nil
}

func registryRender(source []byte, data any) ([]byte, error) {
	t, err := template.New("component").
		Delims("<%", "%>").
		Option("missingkey=error").
		Parse(string(source))
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return nil, err
	}

	if out.Len() > 256*1024 || bytes.Count(out.Bytes(), []byte("\n")) > 1000 {
		return nil, fmt.Errorf("rendered registry file exceeds size/line limit")
	}

	return out.Bytes(), nil
}

func AddRegistry(
	root string,
	m project.Manifest,
	name, reference string,
) (Plan, error) {
	if !project.FrontendName(name) {
		return Plan{}, fmt.Errorf("component name must be lowercase kebab-case")
	}

	if err := Contracts(root, m); err != nil {
		return Plan{}, err
	}

	entries, err := registryEntries(root, m)
	if err != nil {
		return Plan{}, err
	}

	f := *m.Frontend
	f.Components = append([]project.FrontendComponent(nil), f.Components...)
	next := m
	next.Frontend = &f
	p := Plan{
		Manifest: next,
		Files:    map[string][]byte{},
		Previous: map[string][]byte{},
	}
	active, resolved := map[string]bool{}, map[string]string{}

	var visit func(string, string, bool) error

	visit = func(ref, target string, dependency bool) error {
		if active[ref] {
			return fmt.Errorf("registry dependency cycle at %s", ref)
		}

		if _, exists := resolved[ref]; exists {
			return nil
		}

		entry, exists := entries[ref]
		if !exists {
			return fmt.Errorf("registry item not found: %s", ref)
		}

		supported := false
		for _, ui := range entry.item.Profiles {
			supported = supported || ui == f.UI
		}

		if !supported {
			return fmt.Errorf("registry item %s does not support %s", ref, f.UI)
		}

		for _, existing := range f.Components {
			if existing.Name != target {
				continue
			}

			if dependency && existing.From == ref &&
				existing.Digest == entry.digest {
				resolved[ref] = target
				return nil
			}

			return fmt.Errorf("component name/provenance conflict: %s", target)
		}

		active[ref] = true
		deps := map[string]string{}

		for _, required := range entry.item.Requires {
			dependencyName := strings.ReplaceAll(required, "/", "-")
			if !project.FrontendName(dependencyName) {
				return fmt.Errorf("invalid registry dependency %s", required)
			}

			if err := visit(required, dependencyName, true); err != nil {
				return err
			}

			deps[required] = "@/features/" + resolved[required] + "/public"
		}

		data := struct {
			Name, Symbol, UI string
			Dependencies     map[string]string
		}{target, symbol(target), f.UI, deps}
		base := filepath.Join(f.Directory, "src/features", target)
		main := filepath.Join(base, "components", symbol(target)+".tsx")

		for _, file := range entry.item.Files {
			rendered, err := registryRender([]byte(file.Target), data)
			if err != nil {
				return err
			}

			relative := string(rendered)

			ext := filepath.Ext(relative)
			if !project.RegistryPath(relative) || relative == "public.ts" ||
				(ext != ".tsx" && ext != ".ts" && ext != ".css") {
				return fmt.Errorf("unsafe registry target %s", relative)
			}

			destination := filepath.Join(base, filepath.FromSlash(relative))
			if _, exists := p.Files[destination]; exists {
				return fmt.Errorf("duplicate registry destination %s", relative)
			}

			contents, err := registryRender(entry.source[file.Source], data)
			if err != nil {
				return err
			}

			p.Files[destination] = contents
		}

		if _, exists := p.Files[main]; !exists {
			return fmt.Errorf(
				"registry item must create components/%s.tsx exporting %s",
				symbol(target),
				symbol(target),
			)
		}

		p.Files[filepath.Join(base, "public.ts")] = []byte(
			fmt.Sprintf(
				"export { %s } from \"./components/%s\";\n",
				symbol(target),
				symbol(target),
			),
		)
		f.Components = append(
			f.Components,
			project.FrontendComponent{
				Name:        target,
				Kind:        "registry",
				From:        ref,
				Digest:      entry.digest,
				Primitives:  entry.item.Primitives,
				Packages:    entry.item.Packages,
				DevPackages: entry.item.DevPackages,
			},
		)
		active[ref] = false
		resolved[ref] = target

		return nil
	}
	if err := visit(reference, name, false); err != nil {
		return Plan{}, err
	}

	if err := p.Manifest.Validate(); err != nil {
		return Plan{}, err
	}

	// Reject conflicts with both framework pins and opposite dependency sections.
	baseline := p.Manifest
	baselineFrontend := *baseline.Frontend

	baselineFrontend.Components = append(
		[]project.FrontendComponent(nil),
		f.Components...)
	for index := range baselineFrontend.Components {
		baselineFrontend.Components[index].Packages = nil
		baselineFrontend.Components[index].DevPackages = nil
	}

	baseline.Frontend = &baselineFrontend
	defaults := dependencies(baseline)

	for _, c := range f.Components {
		for section, values := range map[string]map[string]string{"dependencies": c.Packages, "devDependencies": c.DevPackages} {
			for pkg, pin := range values {
				for other, pins := range defaults {
					if existing, exists := pins[pkg]; exists &&
						(existing != pin || section != other) {
						return Plan{}, fmt.Errorf(
							"registry dependency conflicts with existing pin: %s",
							pkg,
						)
					}
				}
			}
		}
	}

	if err := p.primitives(root, m); err != nil {
		return Plan{}, err
	}

	if err := p.catalog(); err != nil {
		return Plan{}, err
	}

	old, err := catalog(m)
	if err != nil {
		return Plan{}, err
	}

	p.Previous[filepath.Join(f.Directory, "src/app/catalog.gen.ts")] = old
	if err := p.mergePackage(root, m); err != nil {
		return Plan{}, err
	}

	if err := p.metadata(root); err != nil {
		return Plan{}, err
	}

	return p, nil
}
