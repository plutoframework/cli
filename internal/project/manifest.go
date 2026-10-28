package project

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
	rbac "github.com/plutoframework/pluto-rbac"
)

const SchemaVersion = 7

type Manifest struct {
	SchemaVersion int          `toml:"schema_version"`
	Project       Project      `toml:"project"`
	Capabilities  Capabilities `toml:"capabilities"`
	Database      Database     `toml:"database"`
	Development   Development  `toml:"development"`
	Endpoints     []Endpoint   `toml:"endpoints,omitempty"`
	Jobs          []Job        `toml:"jobs,omitempty"`
}

type Project struct {
	Name      string `toml:"name"`
	Module    string `toml:"module"`
	EnvPrefix string `toml:"env_prefix"`
}

type Capabilities struct {
	Cache            string `toml:"cache"`
	LocalCredentials bool   `toml:"local_credentials"`
	Tenancy          string `toml:"tenancy"`
}
type Database struct {
	Engine  string `toml:"engine"`
	Queries string `toml:"queries"`
}
type Development struct {
	Mise   bool `toml:"mise"`
	Fnox   bool `toml:"fnox"`
	Docker bool `toml:"docker"`
}
type Endpoint struct {
	Collection    bool   `toml:"collection"`
	Idempotency   string `toml:"idempotency"`
	Group         string `toml:"group"`
	Action        string `toml:"action"`
	Name          string `toml:"name"`
	Description   string `toml:"description"`
	Method        string `toml:"method"`
	Path          string `toml:"path"`
	Public        bool   `toml:"public"`
	Permission    string `toml:"permission,omitempty"`
	Feature       string `toml:"feature,omitempty"`
	ClientVisible bool   `toml:"feature_client_visible"`
}

var (
	appName    = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	moduleName = regexp.MustCompile(
		`^[a-z0-9]+(?:[.-][a-z0-9]+)+/(?:[a-zA-Z0-9_-]+/)*[a-zA-Z0-9_-]+$`,
	)
	prefixName  = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)*_$`)
	packageName = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	actionName  = regexp.MustCompile(`^[A-Z][a-zA-Z0-9]*$`)
	metadataKey = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)
	routePart   = regexp.MustCompile(
		`^(?:[a-zA-Z0-9._~-]+|\{[a-z][a-zA-Z0-9]*\})$`,
	)
)

func (m Manifest) Validate() error {
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

	if !moduleName.MatchString(m.Project.Module) ||
		strings.Contains(m.Project.Module, "..") {
		return fmt.Errorf(
			"module must be a domain/path Go module (example.com/my-api)",
		)
	}

	if !prefixName.MatchString(m.Project.EnvPrefix) {
		return fmt.Errorf(
			"env_prefix must be uppercase with one trailing underscore (MY_API_)",
		)
	}

	if m.Capabilities.Tenancy != "single" && m.Capabilities.Tenancy != "multi" {
		return fmt.Errorf("tenancy must be single or multi")
	}

	switch m.Capabilities.Cache {
	case "", "none", "memory", "valkey":
	default:
		return fmt.Errorf("cache must be none, memory or valkey")
	}

	if m.Database.Engine != "postgres" || m.Database.Queries != "sqlc" {
		return fmt.Errorf("database requires engine=postgres and queries=sqlc")
	}

	if err := validateJobs(m.Jobs); err != nil {
		return err
	}

	seenID, seenRoute := map[string]bool{}, map[string]bool{}
	flags := map[string]bool{}
	routeNames := map[string]string{}
	envKeys := map[string]string{}

	for _, e := range m.Endpoints {
		if err := e.Validate(m.Capabilities.Tenancy == "multi"); err != nil {
			return err
		}

		id := e.Group + "." + e.Action

		route := e.Method + " " + canonicalRoute(e.Path)
		if seenID[id] || seenRoute[route] || e.Path == "/api/features" ||
			e.Path == "/healthz" || e.Path == "/auth" || strings.HasPrefix(e.Path, "/auth/") {
			return fmt.Errorf("duplicate/reserved endpoint %s (%s)", id, route)
		}

		shape := canonicalRoute(e.Path)
		if prior, ok := routeNames[shape]; ok && prior != e.Path {
			return fmt.Errorf(
				"route parameter names must match existing template %s",
				prior,
			)
		}

		routeNames[shape] = e.Path

		seenID[id], seenRoute[route] = true, true

		if visible, ok := flags[e.Feature]; e.Feature != "" && ok &&
			visible != e.ClientVisible {
			return fmt.Errorf(
				"feature %s has conflicting client visibility",
				e.Feature,
			)
		}

		if e.Feature != "" {
			envKey := strings.NewReplacer(".", "_", "-", "_").Replace(e.Feature)
			if prior, ok := envKeys[envKey]; ok && prior != e.Feature {
				return fmt.Errorf(
					"feature environment collision: %s and %s",
					prior,
					e.Feature,
				)
			}

			envKeys[envKey] = e.Feature
		}

		flags[e.Feature] = e.ClientVisible
	}

	return nil
}

func (e Endpoint) isWrite() bool {
	switch e.Method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}

func (e Endpoint) RequiresIdempotency() bool {
	return !e.Public && e.Idempotency != "exempt" &&
		e.isWrite()
}

func (e Endpoint) Validate(multi bool) error {
	if e.Collection && e.Method != "GET" {
		return fmt.Errorf("collection requires GET")
	}

	switch e.Idempotency {
	case "", "auto", "required", "exempt":
	default:
		return fmt.Errorf("idempotency must be auto, required or exempt")
	}

	if e.Idempotency == "required" &&
		(e.Public || !e.isWrite()) {
		return fmt.Errorf(
			"required idempotency needs an authenticated business write; public writes require explicit app policy",
		)
	}

	reserved := " break default func interface select case defer go map struct chan else goto package switch const fallthrough if range type continue for import return var init main internal paths app operations models features config domain transport context http strings testing io slog chi errors time fmt bytes json reflect "
	if !packageName.MatchString(e.Group) ||
		strings.Contains(reserved, " "+e.Group+" ") {
		return fmt.Errorf(
			"group must be a nonreserved lowercase Go package name",
		)
	}

	if !actionName.MatchString(e.Action) || e.Action == "Operations" {
		return fmt.Errorf("action must be an exported Go name, e.g. Create")
	}

	if e.Name == "" || e.Description == "" || len(e.Name) > 200 ||
		len(e.Description) > 2000 {
		return fmt.Errorf(
			"endpoint requires name (<=200 bytes) and description (<=2000 bytes)",
		)
	}

	switch e.Method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
	default:
		return fmt.Errorf("unsupported HTTP method %q", e.Method)
	}

	if !strings.HasPrefix(e.Path, "/") || e.Path == "/" || len(e.Path) > 500 {
		return fmt.Errorf(
			"path must be an absolute route such as /api/items/{id}",
		)
	}

	params := map[string]bool{}

	for _, p := range strings.Split(e.Path[1:], "/") {
		if !routePart.MatchString(p) || p == "." || p == ".." {
			return fmt.Errorf(
				"invalid route segment %q; use literals or {param}",
				p,
			)
		}

		if strings.HasPrefix(p, "{") {
			if params[p] {
				return fmt.Errorf("duplicate route parameter %s", p)
			}

			params[p] = true
		}
	}

	if e.Public && (e.Permission != "" || multi) {
		return fmt.Errorf(
			"public endpoints cannot require permissions or multi-tenant scope",
		)
	}

	if e.Permission != "" {
		_, err := rbac.New(rbac.Definition{
			Permissions: []rbac.Permission{
				{
					ID:          rbac.PermissionID(e.Permission),
					Name:        "Capability",
					Description: "Endpoint capability.",
				},
			},
		})
		if err != nil {
			return fmt.Errorf("invalid permission %q: %w", e.Permission, err)
		}
	}

	if e.Feature != "" && !metadataKey.MatchString(e.Feature) {
		return fmt.Errorf("invalid feature key %q", e.Feature)
	}

	if e.ClientVisible && e.Feature == "" {
		return fmt.Errorf("feature-client-visible requires --feature")
	}

	return nil
}

func canonicalRoute(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, "{") {
			parts[i] = "{}"
		}
	}

	return strings.Join(parts, "/")
}

func Discover(start string) (string, Manifest, error) {
	root, err := filepath.Abs(start)
	if err != nil {
		return "", Manifest{}, err
	}

	for {
		data, err := os.ReadFile(filepath.Join(root, "pluto.toml"))
		if err == nil {
			m, err := Decode(data)
			return root, m, err
		}

		if !os.IsNotExist(err) {
			return "", Manifest{}, err
		}

		parent := filepath.Dir(root)
		if root == parent {
			return "", Manifest{}, fmt.Errorf(
				"pluto.toml not found in current directory or parents",
			)
		}

		root = parent
	}
}

func Decode(data []byte) (Manifest, error) {
	var m Manifest

	decoder := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		var unknown *toml.StrictMissingError
		if errors.As(err, &unknown) {
			keys := make([]string, 0, len(unknown.Errors))
			for _, field := range unknown.Errors {
				keys = append(keys, strings.Join(field.Key(), "."))
			}

			return m, fmt.Errorf(
				"pluto.toml: unknown settings: %s",
				strings.Join(keys, ", "),
			)
		}

		return m, fmt.Errorf("pluto.toml: %w", err)
	}

	return m, m.Validate()
}

func Encode(m Manifest) ([]byte, error) { return toml.Marshal(m) }
