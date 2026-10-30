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
go install github.com/plutoframework/cli/cmd/pluto@v0.13.0
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

### 3. Add your feature

```sh
pluto --no-input add endpoint reports List \
  --method GET --path /api/reports \
  --name 'List reports' --description 'List visible reports.' \
  --permission reports:read --feature reports-list
pluto fmt
pluto vet
```

Implement the generated operation, trusted membership resolver and tests before
calling the protected route. Business scaffolds intentionally return not
implemented; a permission declaration grants nobody access. `fmt`/`vet` require
the pinned tools; the generated mise setup provides them.

For React, start with `pluto frontend init --ui heroui-v3`; connected forms use
published action primitives and require explicit actor/session/tenant scope.

## See Pluto in motion

[![Pluto CLI scaffolds an API and curl receives HTTP 200 from its public health endpoint](https://raw.githubusercontent.com/plutoframework/pluto/main/assets/readme/demo.gif)](https://github.com/plutoframework/pluto/blob/main/docs/DEMO.md)

Recorded from the real CLI. Compilation waits are shortened. The demo uses the
built-in `/healthz` route; business operations still need implementation and auth.
[Text transcript and static image](https://github.com/plutoframework/pluto/blob/main/docs/DEMO.md).

## Read further

- [API and integration reference](docs/REFERENCE.md)
- [Field guide](https://plutoframework.github.io/pluto-site/docs/cli/reference/)
- [Pluto ecosystem](https://github.com/plutoframework/pluto#the-ecosystem)

## Boundaries

Generated app code remains editable; authored files are never silently migrated. Runtime integrations and production credentials remain application-owned.

## Contribute

Start with [CONTRIBUTING.md](CONTRIBUTING.md), then choose a [good first issue](https://github.com/plutoframework/pluto/blob/main/CONTRIBUTING.md#starter-tasks).

MIT licensed. See [LICENSE](LICENSE).
