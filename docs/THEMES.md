# TOML-driven frontend themes

New CLI frontends include the theme pipeline automatically. Existing frontends
remain authored code: there is no implicit migration of their scripts, HTML,
styles or Vite config. Backend schema 7 remains unchanged; older CLI schemas are
still rejected. The example app explicitly adopts the pipeline while retaining
its original schema-1 prototype and therefore uses npm theme commands directly.

## Configuration and ownership

```toml
[frontend.theme]
mode = "system"
radius = "0.75rem"
font_family = "Manrope, sans-serif"
overrides = "web/src/styles/overrides.css"

[frontend.theme.light]
accent = "#006FEE"
accent_foreground = "#FFFFFF"

[frontend.theme.dark]
accent = "#338EF7"
background = "#09090B"
```

All values are optional. Omitting the theme table uses system mode and the selected
profile's colors. Mode can also be light or dark. The documented color token names
and supported value formats are in the main README. Fonts must already be supplied
by the application; this setting never fetches assets. CSS remains available for
advanced colors, selectors, effects and responsive layouts.

- `src/generated/theme.css`: configured tokens only, generated and committed.
- `src/generated/theme-overrides.css`: generated import of the authored stylesheet.
- `public/pluto-theme.js`: generated external prepaint bootstrap, mode only.
- `src/styles/overrides.css`: initially empty and application-owned, never rewritten.
- `scripts/theme.mjs`: build-time TOML compiler, never imported into browser code.
- `scripts/theme-vite.mjs`: Vite build/start generation and TOML watch/reload.

Import the library/default styles and application layout first, then generated
CSS and finally the generated overrides import. Template layout/defaults use CSS
layers; custom overrides are unlayered and win last. HeroUI uses its own palette,
shadcn maps semantic tokens to its variables, and native controls consume semantic
CSS variables directly. Empty token tables preserve existing defaults.

The override path must be project-relative under the configured frontend's src
folder, outside generated directories, with safe lowercase names and a .css suffix.
It must already exist when changed. Validation refuses unknown settings/tokens,
malformed values and symlink traversal before output writes. The compiler never
edits the manifest, authored CSS or feature files. Generated output is replaceable;
checks detect drift without repairing it. Backend environment settings and secrets
are never emitted as assets. The browser follows OS preference changes only in
system mode and writes no browser storage.

## Commands

```sh
pluto frontend theme
pluto frontend theme --check
pluto vet
# From the frontend directory:
npm run theme:generate
npm run theme:check
```

Vite regenerates on start, build and saved TOML changes. Invalid edits show a Vite
error until corrected. A production preview serves the existing build. Pre-lifecycle
npm hooks are also supplied, but Vite handles generation even when .npmrc disables
lifecycle scripts. `npm run check` checks freshness before building; regenerate
explicitly when CI reports drift. CLI vet explicitly checks theme freshness when
the theme table is present, including on older frontend scaffolds that lack the
new check integration.

## Explicit adoption in an existing frontend

Review the matching version of this repository's templates and preserve authored
files. Add the pinned smol-toml dev dependency and the four theme compiler/plugin
files plus theme.test.mjs from internal/frontend/templates/scripts. These files
have no application template substitutions. Then:

1. Create the authored override CSS file and configure its project-relative path.
2. Add `theme:generate` / `theme:check` npm scripts, include the check in npm check,
   and include the Node theme tests in npm test.
3. Import `plutoTheme` from scripts/theme-vite.mjs in Vite and add `plutoTheme()` to
   plugins. Preserve existing proxies, HTTPS configuration and CSP guards.
4. Import src/generated/theme.css and src/generated/theme-overrides.css after the
   library/application styles. Remove competing hardcoded palette overrides.
5. Add `<script src="/pluto-theme.js"></script>` in the HTML head, after the CSP
   declaration. Remove any hardcoded light/dark root class or data attribute.
6. Exclude only those three generated assets from Prettier rewrites, generate,
   check and review light/dark rendering and authored CSS precedence.

The compiler assets are build tooling and can be reviewed/adapted by the app.
Changing UI libraries still requires an explicit component migration.

## Validation performed

- CLI formatter/lint/vet/build/tests and race checks.
- Fresh native, HeroUI, shadcn and auth-free native consumers through Cobra;
  dependency installation, API generation, theme tests/builds and Go compilation.
- Example repository checks and five browser workflows against the real API.
  The theme workflow edits TOML, changes browser OS preference, verifies authored
  CSS precedence and restores the original config/CSS bytes in a finally block.
- 13 seeded Go security/validation mutations killed, including theme preflight.
- Four Node compiler mutations killed: unknown fields, token allowlist, stale
  output detection and symlink refusal. Reproduce against an installed frontend:

```sh
python3 scripts/mutation-smoke.py
python3 scripts/theme-mutation-smoke.py /absolute/path/to/app/web
python3 scripts/frontend-matrix.py --keep
```

These are bounded seeded checks, not a complete mutation/security audit. Template
source stays within the existing 1,000-line limit. TOML parsing adds one pinned,
build-time npm dependency; it adds no Go runtime or browser package dependency.
