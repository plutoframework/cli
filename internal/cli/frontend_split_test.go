package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

// splitFrontend creates a backend and an empty sibling frontend directory.
func splitFrontend(t *testing.T, local bool) (string, string) {
	t.Helper()

	backend := frontendApp(t, local)
	web := filepath.Join(filepath.Dir(backend), "orbital-web")

	if err := os.Mkdir(web, 0o755); err != nil {
		t.Fatal(err)
	}

	return backend, web
}

func TestFrontendSplitInitFromBackendCheckout(t *testing.T) {
	backend, web := splitFrontend(t, true)
	before, _ := os.ReadFile(filepath.Join(backend, "pluto.toml"))

	api := "../" + filepath.Base(backend)
	mustInvoke(
		t, web, "frontend", "init", "--api", api, "--auth", "--no-install",
	)

	after, _ := os.ReadFile(filepath.Join(backend, "pluto.toml"))
	if string(before) != string(after) {
		t.Fatal("backend manifest changed")
	}

	if _, err := os.Stat(filepath.Join(backend, "web")); err == nil {
		t.Fatal("frontend written into backend")
	}

	root, m, err := project.DiscoverAny(web)
	if err != nil {
		t.Fatal(err)
	}

	if root != web || !m.Standalone() || m.Frontend.Directory != "." ||
		m.Project.Name != "orbital" || !m.Frontend.Auth {
		t.Fatalf("unexpected manifest: %+v", m)
	}

	if got := m.Frontend.API.OpenAPI; got != api+"/openapi/openapi.json" {
		t.Fatalf("openapi = %q", got)
	}

	if !m.Frontend.API.LocalCredentials || m.Frontend.API.EnvPrefix == "" {
		t.Fatalf("backend settings not copied: %+v", m.Frontend.API)
	}

	for _, name := range []string{
		"package.json", "scripts/manifest.mjs", "src/main.tsx",
		"src/styles/overrides.css",
	} {
		if _, err := os.Stat(filepath.Join(web, name)); err != nil {
			t.Fatal(err)
		}
	}

	readme, _ := os.ReadFile(filepath.Join(web, "README.md"))
	if !strings.Contains(string(readme), "frontend-only repository") {
		t.Fatal("README lacks split guidance")
	}

	if err := project.CheckBackendDrift(web, m); err != nil {
		t.Fatal(err)
	}

	// Later frontend commands work from the frontend repository.
	mustInvoke(
		t, web, "frontend", "add", "component", "purchase-form",
		"--no-install",
	)

	// Application generators refuse the frontend repository.
	if _, err := invoke(
		t, web, "--no-input", "add", "endpoint", "orbit", "List",
		"--method", "GET", "--description", "List orbits.",
	); err == nil ||
		!strings.Contains(err.Error(), "frontend-only") {
		t.Fatalf("expected frontend-only refusal, got %v", err)
	}
}

func TestFrontendSplitDetectsBackendDrift(t *testing.T) {
	backend, web := splitFrontend(t, true)
	mustInvoke(
		t, web, "frontend", "init", "--api", "../"+filepath.Base(backend),
		"--no-install",
	)

	path := filepath.Join(web, "pluto.toml")
	data, _ := os.ReadFile(path)
	edited := strings.Replace(
		string(
			data,
		),
		"local_credentials = true",
		"local_credentials = false",
		1,
	)

	if edited == string(data) {
		t.Fatal("fixture did not change")
	}

	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	_, m, err := project.DiscoverAny(web)
	if err != nil {
		t.Fatal(err)
	}

	err = project.CheckBackendDrift(web, m)
	if err == nil || !strings.Contains(err.Error(), "drifted") {
		t.Fatalf("expected drift, got %v", err)
	}
}

func TestFrontendSplitWithURLRequiresExplicitSettings(t *testing.T) {
	web := temporary(t)

	_, err := invoke(
		t, web, "frontend", "init", "--openapi",
		"https://api.example.com/openapi.json", "--no-install",
	)
	if err == nil || !strings.Contains(err.Error(), "--name") {
		t.Fatalf("expected --name error, got %v", err)
	}

	mustInvoke(
		t, web, "frontend", "init", "--openapi",
		"https://api.example.com/openapi.json", "--name", "orbital",
		"--env-prefix", "ORBITAL_", "--dev-target", "http://127.0.0.1:9090",
		"--no-install",
	)

	_, m, err := project.DiscoverAny(web)
	if err != nil {
		t.Fatal(err)
	}

	if m.Frontend.API.DevTarget != "http://127.0.0.1:9090" ||
		m.Frontend.API.LocalCredentials {
		t.Fatalf("unexpected api: %+v", m.Frontend.API)
	}
}

func TestFrontendSplitRefusesDirectoryAndExistingManifest(t *testing.T) {
	backend, web := splitFrontend(t, false)
	api := "../" + filepath.Base(backend)

	if _, err := invoke(
		t, web, "frontend", "init", "--api", api, "--directory", "ui",
	); err == nil || !strings.Contains(err.Error(), "--directory") {
		t.Fatalf("expected --directory refusal, got %v", err)
	}

	if _, err := invoke(
		t, backend, "frontend", "init", "--api", ".",
	); err == nil || !strings.Contains(err.Error(), "already has") {
		t.Fatalf("expected existing manifest refusal, got %v", err)
	}
}

func TestFrontendEmbeddedRemainsDefault(t *testing.T) {
	backend := frontendApp(t, false)
	mustInvoke(t, backend, "frontend", "init", "--no-install")

	_, m, err := project.Discover(backend)
	if err != nil {
		t.Fatal(err)
	}

	if m.Standalone() || m.Frontend.Directory != "web" ||
		m.Frontend.API != nil {
		t.Fatalf("embedded layout changed: %+v", m.Frontend)
	}
}
