package project

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// KindFrontend marks a pluto.toml for a frontend in its own repository.
const KindFrontend = "frontend"

// Frontend is optional, additive metadata in schema 7. Version identifies the
// template contract; changing UI libraries requires an explicit migration.
type Frontend struct {
	Registries map[string]ComponentRegistry `toml:"registries,omitempty"`
	Theme      *Theme                       `toml:"theme,omitempty"`
	Framework  string                       `toml:"framework"`
	Directory  string                       `toml:"directory"`
	UI         string                       `toml:"ui"`
	Version    int                          `toml:"template_version"`
	Auth       bool                         `toml:"auth"`
	Components []FrontendComponent          `toml:"components,omitempty"`

	// API is set only for frontend-only repositories. It replaces the backend
	// manifest the embedded layout reads directly.
	API *FrontendAPI `toml:"api,omitempty"`
}

// FrontendAPI records the backend contract a separate frontend repository
// builds against. OpenAPI is a repository-relative path (for example a sibling
// backend checkout) or an HTTPS URL for the backend's openapi.json.
type FrontendAPI struct {
	OpenAPI          string `toml:"openapi"`
	EnvPrefix        string `toml:"env_prefix"`
	LocalCredentials bool   `toml:"local_credentials"`
	OIDC             bool   `toml:"oidc,omitempty"`

	// DevTarget is the local Vite proxy target, for example
	// http://127.0.0.1:8080. The WEB_API_TARGET environment variable wins.
	DevTarget string `toml:"dev_target,omitempty"`
}

// Path joins a frontend-relative slash path onto the frontend directory.
func (f Frontend) Path(relative string) string {
	return path.Join(f.Directory, relative)
}

// Standalone reports whether this manifest describes a frontend-only
// repository rather than a Go application.
func (m Manifest) Standalone() bool {
	return m.Kind == KindFrontend
}

// NewStandaloneFrontend returns the manifest base for a separate frontend
// repository; Frontend is filled in by frontend initialization.
func NewStandaloneFrontend(name string, api FrontendAPI) Manifest {
	return Manifest{
		SchemaVersion: SchemaVersion,
		Kind:          KindFrontend,
		Project:       Project{Name: name, EnvPrefix: api.EnvPrefix},
		Capabilities: Capabilities{
			LocalCredentials: api.LocalCredentials,
			OIDC:             api.OIDC,
		},
	}
}

type frontendDocument struct {
	Kind          string          `toml:"kind"`
	SchemaVersion int             `toml:"schema_version"`
	Project       frontendProject `toml:"project"`
	Frontend      *Frontend       `toml:"frontend"`
}

type frontendProject struct {
	Name string `toml:"name"`
}

func decodeFrontend(data []byte) (Manifest, error) {
	var document frontendDocument
	if err := strictDecode(data, &document); err != nil {
		return Manifest{}, err
	}

	if document.Frontend == nil || document.Frontend.API == nil {
		return Manifest{}, fmt.Errorf(
			"frontend-only pluto.toml requires [frontend] and [frontend.api]",
		)
	}

	m := NewStandaloneFrontend(document.Project.Name, *document.Frontend.API)
	m.SchemaVersion = document.SchemaVersion
	m.Frontend = document.Frontend

	return m, m.Validate()
}

func encodeFrontend(m Manifest) ([]byte, error) {
	return toml.Marshal(frontendDocument{
		SchemaVersion: m.SchemaVersion,
		Kind:          KindFrontend,
		Project:       frontendProject{Name: m.Project.Name},
		Frontend:      m.Frontend,
	})
}

func (m Manifest) validateStandaloneFrontend() error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf(
			"unsupported schema_version %d (need %d); no implicit migration",
			m.SchemaVersion,
			SchemaVersion,
		)
	}

	if !appName.MatchString(m.Project.Name) {
		return fmt.Errorf("name must be lowercase kebab-case")
	}

	if m.Frontend == nil || m.Frontend.API == nil {
		return fmt.Errorf(
			"frontend-only pluto.toml requires [frontend] and [frontend.api]",
		)
	}

	api := m.Frontend.API
	if !prefixName.MatchString(api.EnvPrefix) {
		return fmt.Errorf(
			"frontend.api.env_prefix must be uppercase with one trailing " +
				"underscore (MY_API_)",
		)
	}

	if api.OIDC && !api.LocalCredentials {
		return fmt.Errorf(
			"frontend.api.oidc requires frontend.api.local_credentials",
		)
	}

	if err := ValidateOpenAPISource(api.OpenAPI); err != nil {
		return err
	}

	if api.DevTarget != "" {
		u, err := url.Parse(api.DevTarget)
		if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" ||
			u.Port() == "" || (u.Path != "" && u.Path != "/") ||
			u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf(
				"frontend.api.dev_target must be plain HTTP on numeric " +
					"loopback, for example http://127.0.0.1:8080",
			)
		}
	}

	return m.ValidateFrontend()
}

// ValidateOpenAPISource accepts an HTTPS URL, a loopback HTTP URL, or a clean
// relative path to a JSON document.
func ValidateOpenAPISource(source string) error {
	if strings.Contains(source, "://") {
		u, err := url.Parse(source)
		if err != nil || u.Host == "" || u.User != nil {
			return fmt.Errorf("frontend.api.openapi URL is invalid")
		}

		loopback := u.Hostname() == "127.0.0.1" ||
			u.Hostname() == "localhost" || u.Hostname() == "::1"
		if u.Scheme != "https" && (u.Scheme != "http" || !loopback) {
			return fmt.Errorf(
				"frontend.api.openapi URL must use https (http only for " +
					"loopback)",
			)
		}

		return nil
	}

	if source == "" || strings.HasPrefix(source, "/") ||
		strings.Contains(source, "\\") || path.Clean(source) != source ||
		!strings.HasSuffix(source, ".json") {
		return fmt.Errorf(
			"frontend.api.openapi must be an https URL or a clean relative " +
				"path to a .json file, for example " +
				"../my-api/openapi/openapi.json",
		)
	}

	return nil
}

type FrontendComponent struct {
	Primitives  []string          `toml:"primitives,omitempty"`
	From        string            `toml:"from,omitempty"`
	Digest      string            `toml:"digest,omitempty"`
	Packages    map[string]string `toml:"dependencies,omitempty"`
	DevPackages map[string]string `toml:"dev_dependencies,omitempty"`
	Name        string            `toml:"name"`
	Kind        string            `toml:"kind"`
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

	if m.Standalone() {
		if f.Directory != "." {
			return fmt.Errorf(
				"frontend-only repositories use frontend.directory = \".\"",
			)
		}
	} else if f.API != nil {
		return fmt.Errorf(
			"[frontend.api] is only for frontend-only repositories; this " +
				"frontend reads the application's own OpenAPI",
		)
	} else if !appName.MatchString(f.Directory) {
		return fmt.Errorf(
			"frontend directory must be a single lowercase kebab-case " +
				"directory",
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
			"frontend auth requires a backend initialized with " +
				"--local-credentials; frontend generation never enables " +
				"credential issuance",
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
		case "button", "input", "card", "dialog", "form", "registry":
		default:
			return fmt.Errorf(
				"component kind must be button, input, card, dialog or form",
			)
		}

		seen[component.Name] = true
	}

	return f.ValidateRegistries()
}

// BackendSettings returns the API settings a separate frontend copies from a
// Go application manifest.
func BackendSettings(m Manifest) FrontendAPI {
	return FrontendAPI{
		EnvPrefix:        m.Project.EnvPrefix,
		LocalCredentials: m.Capabilities.LocalCredentials,
		OIDC:             m.Capabilities.OIDC,
	}
}

// LinkedBackend finds the Go application whose openapi.json a frontend-only
// repository reads through a local path. It reports false for URLs or when the
// backend checkout is not present.
func LinkedBackend(root string, m Manifest) (string, Manifest, bool) {
	if !m.Standalone() || strings.Contains(m.Frontend.API.OpenAPI, "://") {
		return "", Manifest{}, false
	}

	contract := filepath.Join(root, filepath.FromSlash(m.Frontend.API.OpenAPI))
	if _, err := os.Stat(contract); err != nil {
		return "", Manifest{}, false
	}

	backend, manifest, err := Discover(filepath.Dir(contract))
	if err != nil {
		return "", Manifest{}, false
	}

	return backend, manifest, true
}

// CheckBackendDrift fails when settings copied into a frontend-only
// repository no longer match its locally checked-out backend.
func CheckBackendDrift(root string, m Manifest) error {
	backend, manifest, ok := LinkedBackend(root, m)
	if !ok {
		return nil
	}

	want := BackendSettings(manifest)

	got := *m.Frontend.API
	if got.EnvPrefix != want.EnvPrefix ||
		got.LocalCredentials != want.LocalCredentials ||
		got.OIDC != want.OIDC {
		return fmt.Errorf(
			"[frontend.api] drifted from %s: want env_prefix=%q "+
				"local_credentials=%t oidc=%t; update this pluto.toml",
			filepath.Join(backend, "pluto.toml"),
			want.EnvPrefix,
			want.LocalCredentials,
			want.OIDC,
		)
	}

	return nil
}
