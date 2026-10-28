package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

func TestFrontendThemePreflight(t *testing.T) {
	root := frontendApp(t, false)
	mustInvoke(t, root, "frontend", "init", "--ui", "heroui-v3", "--no-install")

	_, manifest, err := project.Discover(root)
	if err != nil {
		t.Fatal(err)
	}

	manifest.Frontend.Theme.Dark = map[string]string{"accent": "red; } body {}"}

	data, err := project.Encode(manifest)
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

	before := snapshot(t, root)
	if _, err := invoke(
		t,
		root,
		"frontend",
		"add",
		"component",
		"review",
		"--kind",
		"button",
		"--no-install",
	); err == nil {
		t.Fatal("invalid theme reached generation")
	}

	assertUnchanged(t, before, snapshot(t, root))
}
