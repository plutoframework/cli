# Local component registries

Configure a local source in the project's pluto.toml:

```toml
[frontend.registries.company]
path = ".pluto/components"
```

Copy examples/component-registry into .pluto/components to try the included
HeroUI approval panel, then run:

```sh
pluto frontend components list
pluto frontend add component purchase-review --from company/approval-panel --dry-run
pluto frontend add component purchase-review --from company/approval-panel
```

Use --no-install for offline file generation, followed by pluto frontend install.
The normal installation phase runs the existing pinned npm install, formatting,
tests and build workflow. Generation itself executes no registry hooks or scripts.
Existing frontend scaffolds remain compatible; no backend schema migration is
required. The original schema-1 pluto-example prototype still cannot be rewritten
by CLI; use a CLI-initialized app for these commands.

## Authoring

A registry.toml contains a versioned list of items:

```toml
schema_version = 1

[[items]]
name = "approval-panel"
description = "A reusable approval summary."
profiles = ["heroui-v3"]
files = [
  { source = "approval-panel/component.tsx.tmpl", target = "components/<%.Symbol%>.tsx" },
  { source = "approval-panel/component.test.tsx.tmpl", target = "components/<%.Symbol%>.test.tsx" },
]
```

All targets are relative to web/src/features/<requested-name>. Only .ts, .tsx and
.css files are accepted. The item must supply components/<Symbol>.tsx exporting
a zero-required-prop component named <Symbol>, suitable for the existing component
catalog. Pluto writes public.ts; registry files cannot replace it or write into
other features, app composition, runtime config or build scripts. Components can
include forms, dialogs, helper files, styles and meaningful tests. Import your
local CSS from the component so it participates in the build.

Templates use Go text/template with <% and %> delimiters and only these values:

- .Name: requested lowercase kebab-case component/feature name.
- .Symbol: derived exported PascalCase component name.
- .UI: saved UI profile.
- .Dependencies: map of required registry item references to public import paths.

There are no host file, network or command template functions. Missing template
fields fail. Custom free-form input parameters are outside this first version.
Templates and rendered files must remain within the source size and line limits.
Unknown metadata fields, including install hooks, are rejected.

## Dependencies and reuse

An item can declare exact npm pins in dependencies and dev_dependencies:

```toml
[items.dependencies]
clsx = "2.1.1"
```

No ranges, latest, URLs, local npm references or shell commands are accepted.
Existing application dependencies, framework pins and dependency-section conflicts
are checked before any write. Normal npm lockfiles retain integrity information.

`requires = ["company/status-badge"]` declares other local registry items. Put
requires on the item before its dependency tables. Dependencies are resolved first
and receive stable feature names such as company-status-badge. Templates can use:

```tsx
import { CompanyStatusBadge } from "<% index .Dependencies "company/status-badge" %>";
```

For shadcn items, `primitives = ["button", "input", "label"]` requests the existing
curated application-owned primitives and their pinned dependencies. Available
primitives are button, input, label, card and dialog. Imported feature contracts
still go through public.ts and must pass the existing architecture checks.

Cycles, missing items, incompatible profiles, duplicate destinations and name
collisions fail before writes. Installed items record source reference, SHA-256
digest, required primitives and npm pins in frontend.components. The digest covers
normalized item metadata plus template paths and bytes. A shared dependency is
reused only when name, reference and digest match. Modifying registry templates
never rewrites installed code. Changed shared sources require deliberate manual
migration; no automatic update/overwrite command is provided.

## Scope and ownership

Local registries support native, heroui-v3 and shadcn profile variants. They extend
these profiles with custom compositions; arbitrary new UI-library profiles,
remote/published packs, shadcn registry imports and custom input schemas are future
work. Registry installation currently uses the raw CLI; the existing wizard still
offers built-in kinds. List the registry and pass --from explicitly. Use
--interactive with --from to prefill a registry choice and enter its name. Built-ins can
also be requested as --from pluto/button, etc.; pluto is a reserved namespace.

All source paths remain inside the configured registry, output paths remain inside
the new feature, and symlinks/traversal are refused. Existing authored files are
never overwritten. A single preflight/rollback plan covers feature files, catalog,
package.json and manifest. Neither registry templates nor npm pins grant server
permissions; application operations and Go authorization remain authoritative.

## Verification

Focused Cobra tests cover listing, dry-run stability, provenance, dependency reuse
and drift, cycles, incompatible profiles, malformed pins, framework conflicts,
source/target traversal, symlinks, unknown hooks and late-file conflicts.
`scripts/frontend-matrix.py --keep` installs the real approval-panel registry in a
fresh HeroUI app and runs its interaction test, architecture checks and build.
Run the CLI check script, race tests and bounded mutation smoke after changes.
