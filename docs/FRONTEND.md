# Frontend profiles and verification

The CLI supports three curated React profiles: native, heroui-v3 and shadcn.
Templates and dependency pins are owned here; business features remain owned by
applications. Template version 1 is independent of additive schema-7 metadata.

## Upstream component provenance

The shadcn Button, Input, Label, Card and Dialog source was obtained using the
published shadcn CLI **4.23.0**, new-york style, neutral base, React/TypeScript,
Tailwind 4 and local shared/ui aliases. No upstream component source was
hand-downloaded. The registry is live; a pinned CLI alone does not freeze registry
content. Pluto embeds the reviewed source snapshot, so ordinary generation is
offline and repeatable regardless of later registry changes.

Adaptations replace `cn` package imports with the application-local clsx +
tailwind-merge utility and apply Pluto's expanded formatting. Original upstream
MIT attribution is retained in internal/frontend/profiles/shadcn/LICENSE.txt and
copied into generated applications. Source: https://github.com/shadcn-ui/ui.
Future refreshes must use the upstream CLI in a disposable project, inspect the
complete diff/dependencies, preserve licensing and validate every consuming kind.

HeroUI uses the published @heroui/react and @heroui/styles **3.2.6** packages,
following their v3 composition API. Source: https://heroui.com/en/docs/react.
No HeroUI source is vendored. HeroUI's transitive dependencies, including React
Aria peers, are resolved into the generated application's npm lockfile. Commit
that lockfile; direct pins alone do not freeze transitive resolution.

## Reproduce the consumer matrix

```sh
./scripts/check.sh
go test -race ./...
python3 scripts/mutation-smoke.py
python3 scripts/frontend-matrix.py --keep
```

The matrix prints its temporary directory. It initializes fresh apps through
Cobra, exercises auth both at init and as a later addition, generates all five
component kinds, installs later prerequisites, checks API freshness, lint/format,
architecture, TypeScript, behavior tests and production builds. It compiles the Go
backend too. A fourth native consumer has no local-credential capability or auth UI.
All files stay in owned disposable directories. The script removes them unless
--keep is supplied. It never installs hooks or touches a user's running stack.

## Browser QA

Use an installed Chrome and Node 24. Install pinned Playwright in a disposable
verification-tool directory (not in generated runtime dependencies):

```sh
npm install --prefix /path/to/temporary-browser-tools \
  --ignore-scripts --no-save @playwright/test@1.64.1
node scripts/frontend-browser.mjs /path/printed/by/matrix \
  /path/to/temporary-browser-tools/node_modules/@playwright/test/index.mjs
```

This starts only owned consumer preview servers on numeric loopback ports
5181-5183. It checks form feedback, dialog keyboard dismissal/focus restoration,
sign-in/session refresh/logout using explicit HTTP contract mocks, actual browser
Origin headers, absent credential/idempotency headers, empty browser storage,
375px/320px viewport bounds and JavaScript page errors. Non-credential screenshots
are saved inside the matrix directory. These are frontend contract checks; they
do not claim a production credential delivery provider or real SQL lifecycle test.
The CLI's existing generated-backend lifecycle tests cover its Go integration.

Local TLS is self-signed. The product-native preview may report
ERR_CERT_AUTHORITY_INVALID; isolated Playwright contexts accept untrusted TLS only
for these fixed loopback servers. Production trust and backend cookie settings are
not changed. Traces, video and credential storage-state artifacts are not enabled.

## Deliberate limits

- Supported kinds are curated, not an arbitrary remote-registry execution engine.
- Switching component libraries requires a deliberate source/dependency migration.
- Backend local credentials still require application-owned delivery, shared limits
  and trusted ingress. Frontend scaffolding never supplies demo issuance routes.
- Existing UI source is preserved; old generator output is not silently upgraded.
- HTML security policy, SPA fallback and trusted origins need production hosting
  configuration. The generated Vite proxy and certificates are local tools.
- Static import checks guard conventions, not all possible semantics or malicious
  dynamic code. Generated bindings describe compile-time API shapes.


## Validation results for v0.5.0

- Fresh native, HeroUI v3, shadcn and native-without-auth consumer matrix passed.
- Each authenticated consumer passed five behavior tests and twelve architecture
  checks; the auth-free consumer passed its URL-state test and architecture checks.
- Type generation/freshness, lint/format, TypeScript, production builds and Go
  consumer compilation passed, including dependency installation on a later add.
- Chrome contract walkthrough passed all three profiles: callbacks remain local
  until wired, dialogs dismiss with Escape and restore focus, sign-in refreshes
  session state, logout clears it, and 320px/375px layouts stay within the viewport.
- Combined pluto fmt/vet passed on an installed Go + React consumer.
- CLI formatting/lint/vet/build, the complete Go test/race suites and all five PTY
  wizard scenarios passed. The mutation smoke killed all twelve seeded guards,
  with no survivors, invalid builds or timeouts.

Visual samples: [native](evidence/native-components.png),
[HeroUI v3](evidence/heroui-v3-components.png),
[shadcn](evidence/shadcn-components.png).
