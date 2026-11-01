package project

import "fmt"

const initialMigrationSQL = "-- Foundation only: no application tables or migration runner yet.\nCREATE TABLE schema_metadata (version bigint PRIMARY KEY);\nINSERT INTO schema_metadata (version) VALUES (1);\n"

// candidateFoundation renders the same initial production foundation without
// calling Managed or InitialFiles. CandidateManaged replaces the authority and
// composition files with candidate implementations before formatting/checking.
// Tests, tooling, documentation and application metadata are generated separately.
func candidateFoundation(m Manifest) (map[string][]byte, error) {
	if m.Boundary.Mode != "candidate" {
		return nil, fmt.Errorf(
			"candidate foundation requires an explicit candidate profile",
		)
	}

	layout := map[string]string{
		"cmd/api/telemetry.go":                       "telemetry_main.go",
		"internal/caching/cache.go":                  "cache.go",
		"cmd/worker/main.go":                         "worker_main.go",
		"internal/app/worker.go":                     "worker.go",
		"db/migrations/005_jobs.sql":                 "jobs_schema.sql",
		"internal/access/policy.go":                  "policy.go",
		"internal/access/memberships.go":             "memberships.go",
		"internal/transport/pagination.go":           "pagination.go",
		"internal/transport/idempotency.go":          "idempotency.go",
		"db/migrations/003_idempotency.sql":          "idempotency_schema.sql",
		"internal/transport/sessions.go":             "sessions.go",
		"internal/transport/session_routes.go":       "session_routes.go",
		"db/migrations/002_auth_sessions.sql":        "auth_schema.sql",
		"internal/access/roles.go":                   "roles.go",
		"internal/domain/principal.go":               "domain.go",
		"internal/features/features.go":              "features.go",
		"internal/config/config.go":                  "config.go",
		"internal/paths/endpoint.go":                 "endpoint.go",
		"internal/transport/recovery.go":             "recovery.go",
		"internal/transport/security.go":             "security.go",
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
		"internal/api/openapi.go":                    "openapi.go",
		"cmd/openapi/main.go":                        "openapi_main.go",
	}
	if m.Capabilities.LocalCredentials {
		layout["internal/transport/credentials.go"] = "credentials.go"
		layout["internal/paths/credentials.go"] = "credentials_paths.go"
		layout["cmd/api/credentials.go"] = "credentials_main.go"
		layout["db/migrations/004_local_credentials.sql"] = "credentials_schema.sql"
		layout["db/migrations/006_authentication.sql"] = "authentication_schema.sql"
		layout["db/migrations/007_external_accounts.sql"] = "external_accounts.sql"
	}

	if m.Capabilities.OIDC {
		layout["cmd/api/oidc.go"] = "oidc_main.go"
		layout["internal/config/oidc.go"] = "oidc_config.go"
		layout["internal/paths/oidc.go"] = "oidc_paths.go"
	}

	files := map[string][]byte{}
	files["db/migrations/001_initial.sql"] = []byte(initialMigrationSQL)

	for path, name := range layout {
		source, err := render(name, dataFor(m, Endpoint{}))
		if err != nil {
			return nil, fmt.Errorf("candidate foundation %s: %w", path, err)
		}

		files[path] = source
	}

	return files, nil
}
