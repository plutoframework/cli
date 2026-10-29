package project

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"
)

//go:embed templates/*
var templates embed.FS

type templateData struct {
	Module, Name, Prefix string
	Multi                bool
	LocalCredentials     bool
	CacheBackend         string
	Valkey               bool
	Development
	Endpoint
	Symbol        string
	JobKind       string
	AgentProvider string
	Params        []string
	Body          bool
	TestPath      string
}

func dataFor(m Manifest, e Endpoint) templateData {
	cacheBackend := m.Capabilities.Cache
	if cacheBackend == "" {
		cacheBackend = "none"
	}

	params := []string{}

	testPath := e.Path
	for _, part := range strings.Split(e.Path, "/") {
		if strings.HasPrefix(part, "{") {
			params = append(params, strings.Trim(part, "{}"))
			testPath = strings.ReplaceAll(testPath, part, "test-value")
		}
	}

	return templateData{
		Module:           m.Project.Module,
		Name:             m.Project.Name,
		Prefix:           m.Project.EnvPrefix,
		Multi:            m.Capabilities.Tenancy == "multi",
		LocalCredentials: m.Capabilities.LocalCredentials,
		CacheBackend:     cacheBackend,
		Valkey:           cacheBackend == "valkey",
		Development:      m.Development,
		Endpoint:         e,
		Symbol:           symbol(e),
		Params:           params,
		Body: e.Method == "POST" || e.Method == "PUT" ||
			e.Method == "PATCH",
		TestPath: testPath,
	}
}

func symbol(e Endpoint) string {
	if e.Group == "" {
		return ""
	}

	return strings.ToUpper(e.Group[:1]) + e.Group[1:] + e.Action
}

func render(name string, data templateData) ([]byte, error) {
	source, err := templates.ReadFile("templates/" + name + ".tmpl")
	if err != nil {
		return nil, err
	}

	tmpl, err := template.New(name).
		Funcs(template.FuncMap{"q": strconv.Quote}).
		Parse(string(source))
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return nil, err
	}

	if strings.HasSuffix(name, ".go") {
		return format.Source(out.Bytes())
	}

	return out.Bytes(), nil
}

// Managed derives only files wholly owned by the generator. User source files
// are created once and never edited. No string replacement of user code occurs.
func Managed(m Manifest) (map[string][]byte, error) {
	files := map[string][]byte{}

	jobs, err := jobRegistry(m)
	if err != nil {
		return nil, err
	}

	files["internal/background/handlers_gen.go"] = jobs
	for path, source := range jobKinds(m) {
		files[path] = source
	}

	permissions, err := permissionDeclarations(m)
	if err != nil {
		return nil, err
	}

	files["internal/access/permissions_gen.go"] = permissions

	groups := map[string][]Endpoint{}
	for _, e := range m.Endpoints {
		groups[e.Group] = append(groups[e.Group], e)
	}

	names := []string{}
	for name := range groups {
		names = append(names, name)
	}

	sort.Strings(names)

	var catalog, composition, definitions strings.Builder
	catalog.WriteString(
		"package paths\nfunc All() []Endpoint { return []Endpoint{Health, GetFeatures, GetSession, DeleteSession,",
	)
	composition.WriteString(
		"package app\nimport (\n" + strconv.Quote(
			m.Project.Module+"/internal/transport",
		) + "\n",
	)

	for _, name := range names {
		composition.WriteString(
			name + "handlers " + strconv.Quote(
				m.Project.Module+"/internal/handlers/"+name,
			) + "\n",
		)
	}

	composition.WriteString(
		")\nfunc RegisterGenerated(reg transport.Registrar,dependencies Dependencies) {\n",
	)

	featureMap := map[string]bool{}

	for _, name := range names {
		var paths, register strings.Builder
		paths.WriteString(
			"package paths\nimport (" + strconv.Quote(
				m.Project.Module+"/internal/models/"+name,
			) + "\n" + strconv.Quote(m.Project.Module+"/internal/access") + ")\n",
		)
		register.WriteString(
			"package " + name + "\nimport " + strconv.Quote(
				m.Project.Module+"/internal/transport",
			) + "\n\ntype Operations struct {\n",
		)

		for _, endpoint := range groups[name] {
			fmt.Fprintf(
				&register,
				"%s %sOperations\n",
				endpoint.Action,
				endpoint.Action,
			)
		}

		register.WriteString(
			"}\nfunc Register(reg transport.Registrar,operations Operations,bodyLimit int64) {\n",
		)
		fmt.Fprintf(
			&composition,
			"%shandlers.Register(reg,%shandlers.Operations{\n",
			name,
			name,
		)

		for _, e := range groups[name] {
			d := dataFor(m, e)

			request := "nil"
			if d.Body {
				request = e.Group + "." + e.Action + "Request{}"
			}

			fmt.Fprintf(
				&paths,
				"var %s = Endpoint{\nID:%q,\nName:%q,\nDescription:%q,\nMethod:%q,\nPath:%q,\nAuthenticated:%t,\nPermission:access.%sPermission,\nFeature:%q,\nRequest:%s,\nResponse:%s.%sResponse{},\nSuccessStatus:200,\nCollection:%t,\nIdempotent:%t,\n}\n",
				d.Symbol,
				e.Group+"."+e.Action,
				e.Name,
				e.Description,
				e.Method,
				e.Path,
				!e.Public,
				d.Symbol,
				e.Feature,
				request,
				e.Group,
				e.Action,
				e.Collection,
				e.RequiresIdempotency(),
			)
			fmt.Fprintf(&catalog, "%s,\n", d.Symbol)
			fmt.Fprintf(
				&register,
				"Register%s(reg,operations.%s,bodyLimit)\n",
				e.Action,
				e.Action,
			)
			fmt.Fprintf(
				&composition,
				"%s:new%s(dependencies),\n",
				e.Action,
				d.Symbol,
			)

			if e.Feature != "" {
				featureMap[e.Feature] = e.ClientVisible
			}
		}

		register.WriteString("}\n")

		files["internal/paths/"+name+"_gen.go"] = []byte(paths.String())
		files["internal/handlers/"+name+"/register_gen.go"] = []byte(
			register.String(),
		)

		composition.WriteString("},dependencies.Config.Server.BodyLimit)\n")
	}

	if m.Capabilities.LocalCredentials {
		catalog.WriteString(
			"Enroll,RequestVerification,VerifyEmail,SignIn,RequestRecovery,ResetPassword,Logout,\n",
		)
	}

	catalog.WriteString("}\n}\n")
	composition.WriteString("}\n")
	definitions.WriteString(
		"package features\nfunc Declarations() []Definition { return []Definition{\n",
	)

	keys := []string{}
	for key := range featureMap {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		fmt.Fprintf(
			&definitions,
			"{Key:%q,ClientVisible:%t,Default:false},\n",
			key,
			featureMap[key],
		)
	}

	definitions.WriteString("}\n}\n")

	files["internal/paths/catalog_gen.go"] = []byte(catalog.String())
	files["internal/features/declarations_gen.go"] = []byte(
		definitions.String(),
	)

	files["internal/app/routes_gen.go"] = []byte(composition.String())
	for path, source := range files {
		formatted, err := format.Source(source)
		if err != nil {
			return nil, fmt.Errorf("generate %s: %w", path, err)
		}

		files[path] = append(
			[]byte("// Code generated by pluto; DO NOT EDIT.\n"),
			formatted...)
	}

	return files, nil
}

func EndpointFiles(m Manifest, e Endpoint) (map[string][]byte, error) {
	files := map[string][]byte{}

	for _, entry := range []struct{ dir, template string }{
		{"models", "models.go"},
		{"operations", "operation.go"},
		{"operations", "operation_test.go"},
		{"handlers", "handler.go"},
		{"handlers", "handler_test.go"},
	} {
		suffix := ".go"
		if strings.HasSuffix(entry.template, "_test.go") {
			suffix = "_test.go"
		}

		data, err := render(entry.template, dataFor(m, e))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.template, err)
		}

		files[filepath.Join("internal", entry.dir, e.Group, strings.ToLower(e.Action)+suffix)] = data
	}

	binding, err := render("binding.go", dataFor(m, e))
	if err != nil {
		return nil, err
	}

	files[filepath.Join("internal", "app", e.Group+"_"+strings.ToLower(e.Action)+".go")] = binding

	return files, nil
}

func InitialFiles(m Manifest) (map[string][]byte, error) {
	files, err := Managed(m)
	if err != nil {
		return nil, err
	}

	layout := map[string]string{
		"cmd/api/telemetry.go":                       "telemetry_main.go",
		"cmd/api/telemetry_test.go":                  "telemetry_test.go",
		"docs/TELEMETRY.md":                          "telemetry.md",
		"internal/caching/cache.go":                  "cache.go",
		"cmd/worker/main.go":                         "worker_main.go",
		"internal/background/worker.go":              "worker.go",
		"db/migrations/005_jobs.sql":                 "jobs_schema.sql",
		"internal/access/policy.go":                  "policy.go",
		"internal/access/memberships.go":             "memberships.go",
		"internal/transport/pagination.go":           "pagination.go",
		"internal/transport/idempotency.go":          "idempotency.go",
		"db/migrations/003_idempotency.sql":          "idempotency_schema.sql",
		"internal/transport/sessions.go":             "sessions.go",
		"internal/transport/session_routes.go":       "session_routes.go",
		"internal/transport/sessions_test.go":        "sessions_test.go",
		"db/migrations/002_auth_sessions.sql":        "auth_schema.sql",
		"internal/access/roles.go":                   "roles.go",
		"internal/domain/principal.go":               "domain.go",
		"internal/features/features.go":              "features.go",
		"internal/config/config.go":                  "config.go",
		"internal/paths/endpoint.go":                 "endpoint.go",
		"internal/transport/recovery_test.go":        "recovery_test.go",
		"internal/transport/recovery.go":             "recovery.go",
		"internal/transport/security.go":             "security.go",
		"internal/transport/security_test.go":        "security_test.go",
		"internal/transport/http.go":                 "http.go",
		"internal/transport/register.go":             "register.go",
		"internal/app/dependencies.go":               "dependencies.go",
		"internal/app/app.go":                        "app.go",
		"internal/storage/postgres/db/db.go":         "sqlc_db.go",
		"internal/storage/postgres/db/models.go":     "sqlc_models.go",
		"internal/storage/postgres/db/schema.sql.go": "sqlc_schema.sql.go",
		"internal/storage/postgres/pool.go":          "postgres.go",
		"cmd/api/main.go":                            "main.go",
		"internal/api/schema.go":                     "schema.go",
		"internal/api/schema_test.go":                "schema_test.go",
		"internal/api/openapi.go":                    "openapi.go",
		"cmd/openapi/main.go":                        "openapi_main.go",
		"internal/transport/http_test.go":            "http_test.go",
		"internal/app/app_test.go":                   "app_test.go",
		"internal/config/config_test.go":             "config_test.go",
		"README.md":                                  "README.md",
		"AGENTS.md":                                  "AGENTS.md",
	}
	if m.Capabilities.LocalCredentials {
		layout["internal/transport/credentials.go"] = "credentials.go"
		layout["internal/paths/credentials.go"] = "credentials_paths.go"
		layout["cmd/api/credentials.go"] = "credentials_main.go"
		layout["db/migrations/004_local_credentials.sql"] = "credentials_schema.sql"
	}

	for path, name := range layout {
		data, err := render(name, dataFor(m, Endpoint{}))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}

		files[path] = data
	}

	for path, data := range ToolFiles(m) {
		files[path] = data
	}

	manifest, err := Encode(m)
	if err != nil {
		return nil, err
	}

	files["pluto.toml"] = manifest
	files["openapi/openapi.json"] = []byte("{}\n")

	return files, nil
}
