# Feature flags

Feature flags were already wired into endpoint scaffolding. The CLI saves each
endpoint's feature key and client visibility in pluto.toml, generates declarations,
loads prefixed environment overrides, gates the route and exposes explicitly
client-visible values through the authenticated /api/features contract.

```sh
pluto add endpoint reports Export --method GET --path /api/reports/export \
  --permission reports.export --feature reports-export --feature-client-visible
```

Generated apps now import github.com/plutoframework/pluto-core/features. Core owns
immutable local evaluation, default/override precedence, environment normalization
and boolean parsing, optional bool presence conversion and visible projection.
The generated internal/features package is only declarations and a small adapter;
its startup MustSnapshot call uses declarations validated by CLI metadata. Invalid
application-owned declarations are programmer errors, not runtime flag values.

Flags default false for new generated declarations. Environment names combine the
saved app prefix with FEATURE_ and an uppercase key with dots/hyphens normalized
to underscores. Explicit false is preserved. Missing values use defaults; malformed
values fail startup without exposing their raw content. Undeclared overrides are
ignored and undeclared flags evaluate false. Environment name collisions are
rejected during endpoint validation and by Core snapshot construction.

For custom configuration providers, Core NewSnapshot accepts ordered override maps;
rightmost present values win. This is precedence support, not a runtime config-file
loader, remote provider protocol, rollout/targeting engine or OpenFeature adapter.
Declarations and overrides are copied and visible maps are fresh per call.

Application declarations, verified identity, tenant scope and authorization remain
application-owned. A flag is never a permission. Client-visible results support UI
presentation only; route and business operation checks remain authoritative.
Internal flags never appear in the client projection. The example retains its
principal-aware adapter, so an unverified actor cannot enable a feature.

Existing apps are not rewritten by a CLI update. Their internal/features provider
can be adapted to Core while preserving application declarations and contracts.
No database migration or backend manifest schema change is needed.
