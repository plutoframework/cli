package project

import (
	"fmt"
	"strings"
)

// AuthVersion pins the published root, password and PostgreSQL modules.
const AuthVersion = "v0.3.0"

const CoreVersion = "v0.4.0"

const ValkeyVersion = "v0.1.0"

const CorePostgresVersion = "v0.1.0"

const RBACVersion = "v0.1.0"

const JobsVersion = "v0.1.1"

const JobsPostgresVersion = "v0.1.0"

// The published pgx adapter imports the local store in its package, so its
// password/crypto dependencies are transitive even in session-only PG apps.
func passwordRequirement(m Manifest) string {
	if m.Capabilities.LocalCredentials {
		return AuthVersion
	}

	return AuthVersion + " // indirect"
}

// ToolFiles contains module dependencies and optional development configuration.
func ToolFiles(m Manifest) map[string][]byte {
	files := map[string][]byte{}
	files["go.mod"] = []byte(fmt.Sprintf(`module %s

go 1.25.5

require (
 github.com/go-chi/chi/v5 v5.2.3
 github.com/jackc/pgx/v5 v5.11.0
 github.com/plutoframework/pluto-core %s
 github.com/plutoframework/pluto-core/stores/postgres %s
 github.com/plutoframework/pluto-rbac %s
 github.com/plutoframework/pluto-auth %s
 github.com/plutoframework/pluto-auth/stores/postgres %s
 github.com/plutoframework/pluto-jobs %s
 github.com/plutoframework/pluto-jobs/stores/postgres %s
)

require (
 github.com/plutoframework/pluto-auth/methods/password %s
 golang.org/x/crypto v0.55.0 // indirect
 golang.org/x/sys v0.47.0 // indirect
 github.com/jackc/pgpassfile v1.0.0 // indirect
 github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
 github.com/jackc/puddle/v2 v2.2.2 // indirect
 golang.org/x/sync v0.22.0 // indirect
 golang.org/x/text v0.41.0 // indirect
)
`, m.Project.Module, CoreVersion, CorePostgresVersion, RBACVersion, AuthVersion, AuthVersion, JobsVersion, JobsPostgresVersion, passwordRequirement(m)))

	files["go.sum"] = []byte(appGoSum)
	addTelemetryRequirement(files)

	if m.Capabilities.Cache == "valkey" {
		module := string(files["go.mod"])
		module = strings.Replace(
			module,
			"require (",
			fmt.Sprintf(
				"require (\n github.com/plutoframework/pluto-core/stores/valkey %s\n github.com/valkey-io/valkey-go v1.0.79 // indirect",
				ValkeyVersion,
			),
			1,
		)
		files["go.mod"] = []byte(module)
	}

	files[".golangci.yml"] = []byte(
		strings.ReplaceAll(lintConfig, "MODULE", m.Project.Module),
	)
	files[".gitignore"] = []byte(
		"/bin/\n.env\n.env.*\n!.env.example\nage.txt\n*.key\nfnox.local.toml\n.fnox.local.toml\n",
	)
	files[".env.example"] = []byte(
		fmt.Sprintf(
			"# Explicitly export desired values; no implicit dotenv loading.\n%sSERVER_ADDRESS=127.0.0.1:8080\n%sSERVER_GZIP=true\n%sLOG_LEVEL=info\n# %sDATABASE_URL=postgres://pluto:local_only@127.0.0.1:55432/pluto?sslmode=disable\n# Flags: %sFEATURE_<KEY>=false (dots/hyphens become underscores)\n",
			m.Project.EnvPrefix,
			m.Project.EnvPrefix,
			m.Project.EnvPrefix,
			m.Project.EnvPrefix,
			m.Project.EnvPrefix,
		),
	)

	files[".env.example"] = append(files[".env.example"], []byte(fmt.Sprintf(
		"%sCACHE_ENVIRONMENT=development\n# %sCACHE_URL=redis://:local_only@127.0.0.1:6379/0\n%sCACHE_ALLOW_INSECURE=false\n",
		m.Project.EnvPrefix,
		m.Project.EnvPrefix,
		m.Project.EnvPrefix,
	))...)

	files[".env.example"] = append(files[".env.example"], []byte(fmt.Sprintf(
		"%sAUTH_TRUSTED_ORIGINS=http://127.0.0.1:8080\n%sAUTH_SESSION_TTL=24h\n%sAUTH_IDLE_TIMEOUT=30m\n",
		m.Project.EnvPrefix,
		m.Project.EnvPrefix,
		m.Project.EnvPrefix,
	))...)
	if m.Capabilities.LocalCredentials {
		files[".env.example"] = append(files[".env.example"], []byte(
			"# Local credentials require DATABASE_URL and explicit app-owned Sender,\n# shared AttemptLimiter and trusted-client Ingress in cmd/api/credentials.go.\n# No credential defaults or delivery secrets are included.\n",
		)...)
	}

	files["scripts/check.sh"] = []byte(
		"#!/bin/sh\nset -eu\ncd \"$(dirname \"$0\")/..\"\nexec pluto vet\n",
	)
	files["scripts/fmt.sh"] = []byte(
		"#!/bin/sh\nset -eu\ncd \"$(dirname \"$0\")/..\"\nexec pluto fmt\n",
	)
	files["sqlc.yaml"] = []byte(`version: "2"
sql:
  - engine: postgresql
    schema: db/migrations
    queries: db/queries
    gen:
      go:
        package: db
        out: internal/storage/postgres/db
        sql_package: pgx/v5
`)
	files["db/migrations/001_initial.sql"] = []byte(
		"-- Foundation only: no application tables or migration runner yet.\nCREATE TABLE schema_metadata (version bigint PRIMARY KEY);\nINSERT INTO schema_metadata (version) VALUES (1);\n",
	)
	files["db/queries/schema.sql"] = []byte(
		"-- name: SchemaVersion :one\nSELECT version FROM schema_metadata ORDER BY version DESC LIMIT 1;\n",
	)

	if m.Development.Mise {
		content := `min_version = "2026.10.4"

[tools]
go = "1.25.5"
golangci-lint = "2.15.0"
"go:github.com/sqlc-dev/sqlc/cmd/sqlc" = "1.31.1"

[tasks.run]
run = "go run ./cmd/api"

[tasks.worker]
run = "go run ./cmd/worker"

[tasks.fmt]
run = "pluto fmt"

[tasks.check]
run = "pluto vet"

[tasks.openapi]
run = "go run ./cmd/openapi"

[tasks.sqlc]
run = "sqlc generate"
`
		if m.Development.Fnox {
			content = strings.Replace(
				content,
				"\"go:github.com/sqlc-dev/sqlc/cmd/sqlc\" = \"1.31.1\"",
				"\"go:github.com/sqlc-dev/sqlc/cmd/sqlc\" = \"1.31.1\"\nfnox = \"1.43.0\"",
				1,
			)
			content = strings.Replace(
				content,
				"run = \"go run ./cmd/api\"",
				"run = \"fnox exec -- go run ./cmd/api\"",
				1,
			)
			content = strings.Replace(
				content,
				"run = \"go run ./cmd/worker\"",
				"run = \"fnox exec -- go run ./cmd/worker\"",
				1,
			)
		}

		files["mise.toml"] = []byte(content)
	}

	if m.Development.Fnox {
		files["fnox.toml"] = []byte(
			fmt.Sprintf(
				"#:schema https://fnox.jdx.dev/schema.json\n# Nonsecret local-only value. NEVER use the plain provider for real secrets.\nroot = true\nenv = \"exec\"\nredact_exec = true\n\n[providers.local]\ntype = \"plain\"\n\n[secrets]\n%sLOG_LEVEL = { provider = \"local\", value = \"info\" }\n",
				m.Project.EnvPrefix,
			),
		)
	}

	if m.Development.Docker {
		files["Dockerfile"] = []byte(`FROM golang:1.25.5-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/api ./cmd/api
RUN CGO_ENABLED=0 go build -trimpath -o /out/worker ./cmd/worker

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 api
COPY --from=build /out/api /usr/local/bin/api
COPY --from=build /out/worker /usr/local/bin/worker
USER api
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api"]
`)
		files[".dockerignore"] = []byte(
			".git\n.env\n.env.*\nfnox*.toml\n.fnox*.toml\n*.key\nage.txt\nbin\n",
		)
		files["compose.yaml"] = []byte(fmt.Sprintf(`services:
  api:
    build: .
    environment:
      %sSERVER_ADDRESS: 0.0.0.0:8080
      %sDATABASE_URL: postgres://pluto:local_only@postgres:5432/pluto?sslmode=disable
    ports:
      - "127.0.0.1:8080:8080"
    depends_on:
      postgres:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/healthz"]
      interval: 5s
      timeout: 3s
      retries: 10
  worker:
    build: .
    profiles: ["worker"]
    entrypoint: ["/usr/local/bin/worker"]
    environment:
      %sDATABASE_URL: postgres://pluto:local_only@postgres:5432/pluto?sslmode=disable
    depends_on:
      postgres:
        condition: service_healthy
  postgres:
    image: postgres:18.6-alpine
    environment:
      POSTGRES_USER: pluto
      POSTGRES_PASSWORD: local_only
      POSTGRES_DB: pluto
    ports:
      - "127.0.0.1:55432:5432"
    volumes:
      - postgres-data:/var/lib/postgresql
      - ./db/migrations:/docker-entrypoint-initdb.d:ro
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U pluto -d pluto"]
      interval: 3s
      timeout: 3s
      retries: 10
volumes:
  postgres-data:
`, m.Project.EnvPrefix, m.Project.EnvPrefix, m.Project.EnvPrefix))
	}

	if m.Development.Docker && m.Capabilities.Cache == "valkey" {
		compose := string(files["compose.yaml"])
		compose = strings.Replace(
			compose,
			"      "+m.Project.EnvPrefix+"SERVER_ADDRESS:",
			"      "+m.Project.EnvPrefix+"CACHE_URL: redis://:local_only@valkey:6379/0\n      "+m.Project.EnvPrefix+"CACHE_ALLOW_INSECURE: \"true\"\n      "+m.Project.EnvPrefix+"SERVER_ADDRESS:",
			1,
		)
		compose = strings.Replace(
			compose,
			"    depends_on:\n",
			"    depends_on:\n      valkey:\n        condition: service_healthy\n",
			1,
		)
		compose = strings.Replace(
			compose,
			"volumes:\n  postgres-data:",
			`  valkey:
    image: valkey/valkey:9.1.3-alpine
    command: ["valkey-server", "--save", "", "--appendonly", "no", "--requirepass", "local_only", "--maxmemory", "64mb", "--maxmemory-policy", "allkeys-lru"]
    ports: ["127.0.0.1:6379:6379"]
    healthcheck:
      test: ["CMD-SHELL", "VALKEYCLI_AUTH=local_only valkey-cli ping"]
      interval: 2s
      timeout: 2s
      retries: 20
volumes:
  postgres-data:`,
			1,
		)
		files["compose.yaml"] = []byte(compose)
	}

	return files
}

const lintConfig = `version: "2"
run:
  timeout: 3m
  modules-download-mode: readonly
linters:
  default: none
  enable:
    - govet
    - ineffassign
    - staticcheck
    - unused
    - wsl_v5
  settings:
    wsl_v5:
      default: default
      allow-first-in-block: true
      allow-whole-block: false
      cuddle-max-statements: 1
      branch-max-lines: 2
      enable:
        - after-block
        - after-expr
  exclusions:
    generated: strict
    presets: []
formatters:
  enable:
    - gci
    - gofumpt
    - golines
  settings:
    gci:
      custom-order: true
      sections:
        - standard
        - prefix(MODULE)
        - default
    gofumpt:
      module-path: MODULE
    golines:
      max-len: 80
      tab-len: 4
      shorten-comments: false
  exclusions:
    generated: strict
issues:
  max-issues-per-linter: 0
  max-same-issues: 0
`
