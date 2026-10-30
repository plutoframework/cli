<p align="center">
  <img src="https://raw.githubusercontent.com/plutoframework/pluto/main/assets/readme/banner.svg" alt="Pluto — a small framework, wide open possibilities" width="100%" />
</p>

# Pluto CLI

A clear path from an empty folder to an explicit Go API.

[Website](https://plutoframework.github.io/pluto-site/) · [Documentation](https://plutoframework.github.io/pluto-site/docs/cli/reference/) · [CLI demo](https://github.com/plutoframework/pluto/blob/main/docs/DEMO.md) · [First issues](https://github.com/plutoframework/pluto/blob/main/CONTRIBUTING.md#starter-tasks)

## What you get

- Scaffold APIs, endpoints, jobs, agents and domain state with Cobra commands.
- Add React frontends with native, HeroUI v3 or shadcn profiles.
- Keep TOML configuration, OpenAPI, formatting and project checks consistent.

## Get started

### 1. Install

Requires Go **1.25.5+** and Git:

```sh
go install github.com/plutoframework/cli/cmd/pluto@v0.14.0
pluto version
```

### 2. Create an API

```sh
pluto --no-input init orbit --module example.com/orbit --env-prefix ORBIT_
cd orbit
go run ./cmd/api
```

In another terminal:

```sh
curl -fsS http://127.0.0.1:8080/healthz
```

The response is `{"data":"ok"}`. Change `ORBIT_SERVER_ADDRESS` if the port is busy.
Docker, mise and fnox presets are included; use `--no-docker --no-mise --no-fnox`
to omit them. Hooks are opt-in. Run `pluto` in a terminal for the wizard.

### 3. Make a protected business endpoint return data

The [complete endpoint walkthrough](docs/FIRST-ENDPOINT.md) generates
`GET /api/greeting`, implements its operation and wires published authentication,
explicit membership and RBAC into the real generated router. Run its checked
script from a CLI checkout:

```sh
mise exec golangci-lint@2.15.0 -- python3 scripts/first-endpoint.py
```

It generates a disposable app with module `example.com/pluto-first-endpoint` and
uses this complete authored `internal/operations/greeting/read.go`:

<!-- example-source: docs/examples/first-endpoint/operation.go.txt -->

```go
package greeting

import (
	"context"

	"example.com/pluto-first-endpoint/internal/access"
	"example.com/pluto-first-endpoint/internal/domain"
	"example.com/pluto-first-endpoint/internal/models/greeting"

	rbac "github.com/plutoframework/pluto-rbac"
)

type Read struct{ policy *rbac.Policy }

func NewRead(policy *rbac.Policy) *Read { return &Read{policy: policy} }

func (operation *Read) Execute(
	_ context.Context,
	principal domain.Principal,
	_ greeting.ReadRequest,
) (greeting.ReadData, error) {
	if !principal.Verified() {
		return greeting.ReadData{}, domain.ErrUnauthenticated
	}

	if !principal.Can(operation.policy, access.GreetingReadPermission) {
		return greeting.ReadData{}, domain.ErrForbidden
	}

	return greeting.ReadData{Value: "Hello, " + principal.ActorID}, nil
}
```

<!-- /example-source -->

The linked [harness](docs/examples/first-endpoint/main.go.txt) supplies trusted
synthetic sessions, grants only `reader` the capability and explicitly enables
the feature. It exposes no session-issuance endpoint and leaves the generated
default resolver denying access. It calls the real API and verifies:

```text
anonymous: HTTP 401
authenticated without grant: HTTP 403
authorized: HTTP 200
Hello, reader
```

The script runs `pluto fmt`, `pluto vet`, operation tests and OpenAPI freshness,
then cleans up. Use real credential and membership integrations for an actual
application. For React, start with `pluto frontend init --ui heroui-v3`.

## See Pluto in motion

[![Pluto CLI scaffolds an API and curl receives HTTP 200 from its public health endpoint](https://raw.githubusercontent.com/plutoframework/pluto/main/assets/readme/demo.gif)](https://github.com/plutoframework/pluto/blob/main/docs/DEMO.md)

Recorded from the real CLI. Compilation waits are shortened. The demo uses the
built-in `/healthz` route; business operations still need implementation and auth.
[Text transcript and static image](https://github.com/plutoframework/pluto/blob/main/docs/DEMO.md).

## Account authentication upgrade

This revision uses Pluto Auth v0.4.0. Account-backed deployments explicitly apply
`db/migrations/006_authentication.sql` after the existing migrations, before
running the upgraded application. Constructors perform no DDL. Existing sessions
keep unknown authentication evidence until the next real sign-in; rotation does
not make them fresh. See [the authentication guide](https://plutoframework.github.io/pluto-site/docs/backend/authentication/).

The library foundation adds account policy, method evidence and freshness guards.
Native social sign-in, TOTP enrollment and their frontend flows remain next work;
no factor policy is enabled automatically by this upgrade.

## Read further

- [API and integration reference](docs/REFERENCE.md)
- [Field guide](https://plutoframework.github.io/pluto-site/docs/cli/reference/)
- [Pluto ecosystem](https://github.com/plutoframework/pluto#the-ecosystem)

## Boundaries

Generated app code remains editable; authored files are never silently migrated. Runtime integrations and production credentials remain application-owned.

## Contribute

Start with [CONTRIBUTING.md](CONTRIBUTING.md), then choose a [good first issue](https://github.com/plutoframework/pluto/blob/main/CONTRIBUTING.md#starter-tasks).

MIT licensed. See [LICENSE](LICENSE).
