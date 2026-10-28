package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

func registryApp(t *testing.T) string {
	t.Helper()

	root := frontendApp(t, false)
	mustInvoke(t, root, "frontend", "init", "--ui", "heroui-v3", "--no-install")

	_, m, err := project.Discover(root)
	if err != nil {
		t.Fatal(err)
	}

	m.Frontend.Registries = map[string]project.ComponentRegistry{
		"company": {Path: ".pluto/components"},
	}

	data, err := project.Encode(m)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "pluto.toml"),
		data,
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	putRegistry(t, root, "registry.toml", `schema_version=1
[[items]]
name="panel"
profiles=["heroui-v3"]
files=[{source="panel.tsx.tmpl",target="components/<%.Symbol%>.tsx"}]
`)
	putRegistry(
		t,
		root,
		"panel.tsx.tmpl",
		"export function <%.Symbol%>() { return <p>Registry panel</p>; }\n",
	)

	return root
}

func putRegistry(t *testing.T, root, name, value string) {
	t.Helper()

	destination := filepath.Join(root, ".pluto/components", name)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(destination, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFrontendRegistry(t *testing.T) {
	root := registryApp(t)
	if list := mustInvoke(
		t,
		root,
		"frontend",
		"components",
		"list",
	); !strings.Contains(
		list,
		"company/panel",
	) {
		t.Fatal(list)
	}

	before := snapshot(t, root)
	mustInvoke(
		t,
		root,
		"frontend",
		"add",
		"component",
		"review",
		"--from",
		"company/panel",
		"--dry-run",
	)
	assertUnchanged(t, before, snapshot(t, root))
	mustInvoke(
		t,
		root,
		"frontend",
		"add",
		"component",
		"review",
		"--from",
		"company/panel",
		"--no-install",
	)

	_, m, err := project.Discover(root)
	if err != nil || m.Frontend.Components[0].From != "company/panel" ||
		len(m.Frontend.Components[0].Digest) != 64 {
		t.Fatalf("provenance: %v %+v", err, m.Frontend)
	}

	data, err := os.ReadFile(
		filepath.Join(root, "web/src/features/review/components/Review.tsx"),
	)
	if err != nil || !strings.Contains(string(data), "function Review()") {
		t.Fatalf("render: %s %v", data, err)
	}

	before = snapshot(t, root)
	if _, err := invoke(
		t,
		root,
		"frontend",
		"add",
		"component",
		"review",
		"--from",
		"company/panel",
		"--no-install",
	); err == nil {
		t.Fatal("overwrote authored component")
	}

	assertUnchanged(t, before, snapshot(t, root))
}

func TestFrontendRegistryPreflight(t *testing.T) {
	for _, scenario := range []string{"source-traversal", "target-traversal", "symlink", "profile", "cycle", "package-pin", "framework-pin", "hook", "late-conflict"} {
		t.Run(scenario, func(t *testing.T) {
			root := registryApp(t)
			file := filepath.Join(root, ".pluto/components/registry.toml")
			data, _ := os.ReadFile(file)
			text := string(data)

			switch scenario {
			case "source-traversal":
				if err := os.WriteFile(
					filepath.Join(root, ".pluto/outside"),
					[]byte("export function <%.Symbol%>() { return null; }"),
					0o644,
				); err != nil {
					t.Fatal(err)
				}

				text = strings.ReplaceAll(text, "panel.tsx.tmpl", "../outside")
			case "target-traversal":
				text = strings.ReplaceAll(
					text,
					"components/<%.Symbol%>.tsx",
					"../app/owned.tsx",
				)
			case "symlink":
				if err := os.Remove(
					filepath.Join(root, ".pluto/components/panel.tsx.tmpl"),
				); err != nil {
					t.Fatal(err)
				}

				if err := os.Symlink(
					filepath.Join(root, "pluto.toml"),
					filepath.Join(root, ".pluto/components/panel.tsx.tmpl"),
				); err != nil {
					t.Fatal(err)
				}
			case "profile":
				text = strings.ReplaceAll(text, "heroui-v3", "native")
			case "cycle":
				text += "requires=[\"company/panel\"]\n"
			case "package-pin":
				text += "[items.dependencies]\nclsx=\"latest\"\n"
			case "framework-pin":
				text += "[items.dependencies]\nreact=\"1.0.0\"\n"
			case "hook":
				text += "post_install=\"echo unsafe\"\n"
			case "late-conflict":
				destination := filepath.Join(
					root,
					"web/src/features/review/public.ts",
				)
				if err := os.MkdirAll(
					filepath.Dir(destination),
					0o755,
				); err != nil {
					t.Fatal(err)
				}

				if err := os.WriteFile(
					destination,
					[]byte("owned"),
					0o644,
				); err != nil {
					t.Fatal(err)
				}
			}

			if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}

			before := snapshot(t, root)
			if _, err := invoke(
				t,
				root,
				"frontend",
				"add",
				"component",
				"review",
				"--from",
				"company/panel",
				"--no-install",
			); err == nil {
				t.Fatal("invalid registry accepted")
			}

			assertUnchanged(t, before, snapshot(t, root))
		})
	}
}

func TestFrontendRegistryDependencies(t *testing.T) {
	root := registryApp(t)
	file := filepath.Join(root, ".pluto/components/registry.toml")
	source, _ := os.ReadFile(file)

	source = append(source, []byte(`requires=["company/badge"]
[[items]]
name="badge"
profiles=["heroui-v3"]
files=[{source="badge.tsx.tmpl",target="components/<%.Symbol%>.tsx"}]
[items.dependencies]
clsx="2.1.1"
`)...)
	if err := os.WriteFile(file, source, 0o644); err != nil {
		t.Fatal(err)
	}

	putRegistry(
		t,
		root,
		"badge.tsx.tmpl",
		"export function <%.Symbol%>() { return <span>Ready</span>; }\n",
	)
	mustInvoke(
		t,
		root,
		"frontend",
		"add",
		"component",
		"review",
		"--from",
		"company/panel",
		"--no-install",
	)

	_, manifest, err := project.Discover(root)
	if err != nil || len(manifest.Frontend.Components) != 2 {
		t.Fatalf("graph: %v", err)
	}

	mustInvoke(
		t,
		root,
		"frontend",
		"add",
		"component",
		"second-review",
		"--from",
		"company/panel",
		"--no-install",
	)

	_, manifest, err = project.Discover(root)
	if err != nil || len(manifest.Frontend.Components) != 3 {
		t.Fatalf("reuse: %v", err)
	}

	putRegistry(
		t,
		root,
		"badge.tsx.tmpl",
		"export function <%.Symbol%>() { return <span>Changed</span>; }\n",
	)

	before := snapshot(t, root)
	if _, err := invoke(
		t,
		root,
		"frontend",
		"add",
		"component",
		"third-review",
		"--from",
		"company/panel",
		"--no-install",
	); err == nil {
		t.Fatal("reused drifted dependency")
	}

	assertUnchanged(t, before, snapshot(t, root))
}
