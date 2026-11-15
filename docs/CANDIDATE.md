# Candidate profile reference

The unreleased candidate boundary profile, migration to it, and the authored-code
seams it supports. The general CLI reference is [REFERENCE.md](REFERENCE.md).

## Explicit candidate boundary profile

Protected operations in the published compatibility profile are drafts. Canonical
`pluto architecture check` and `pluto vet` report `ARCH401`; compiling a draft does
not establish identity, resource or data-contract adoption. There is no flag to
waive these required obligations.

The supported unreleased candidate profile uses explicit local checkouts. It
keeps published dependency pins and writes validated local replacements; it never
pretends those candidate APIs have been released. Candidate initialization needs
`--no-input` and explicit multi tenancy. The wizard rejects this profile rather
than losing its source paths.

```sh
pluto --no-input init /absolute/path/records --module example.com/records \
  --multi-tenant --boundary-profile candidate \
  --candidate-auth-source /absolute/path/pluto-auth \
  --candidate-rbac-source /absolute/path/pluto-rbac \
  --candidate-core-source /absolute/path/pluto-core \
  --candidate-jobs-source /absolute/path/pluto-jobs
cd /absolute/path/records
pluto --no-input add blueprint --file record-contracts.toml
go mod tidy
pluto fmt
pluto openapi
pluto architecture check --directory . --format json
pluto vet
```

Local sources only build on the machine that has those checkouts. To build the
candidate profile anywhere the Go module proxy reaches (CI included), select a
library by a pushed commit instead of a checkout:

```sh
pluto --no-input init /absolute/path/records --module example.com/records \
  --multi-tenant --boundary-profile candidate \
  --candidate-auth-revision <40-hex commit> \
  --candidate-rbac-revision <40-hex commit> \
  --candidate-core-revision <40-hex commit> \
  --candidate-jobs-revision <40-hex commit>
```

A revision must be a full commit hash. It is resolved through the Go module
system to the exact version of every module the profile binds from that library
(submodules carry their own pseudo-versions), and nothing is written until the
pins verify: each module downloads at exactly its version with go.sum checksums,
declares its own module path, the pin set survives minimal version selection,
and a probe of the required candidate APIs compiles against it. Pins are saved
under `[boundary.pins]` in pluto.toml and become plain go.mod requirements with
verified go.sum lines and no replace directive. Each library uses either a source
or pins, never both. Blueprints restore the exact pins, and canonical checks and
`pluto vet` reject any pinned requirement at another version or any replacement
of a pinned module; `go mod tidy` may only prune pins that no package imports.

A blueprint adds `resources`, `operations`, `operation_contracts`, `effects`,
`roles`, `endpoints`, `jobs` and `agents` atomically. Unknown fields, conflicting
filenames, missing authority or behavior obligations and unsupported combinations
fail before writes. Existing authored files require explicit migration (see
[Migrating existing applications](#migrating-existing-applications)); generation never silently replaces them. Adding metadata never assigns an actor to a role.

`resources` declare `group`, exported `name`, safe SQL `table`, cache `namespace`
and positive `version`, `visibility` (`owner` or `tenant`), `initial_state`,
`states`, deterministic `transitions` (`from`, `event`, `to`, explicit `guard`:
`none`, `owner`, `not_owner`), `normalize` (`trim` or `none`), `max_value_bytes`,
`page_limit`, `export_limit` and `cache_ttl_seconds`. Page bounds are at most 100;
exports at most 10000; effect-producing values at most 8192 bytes. SQL identifiers
are validated and quoted. Migrations are explicit, never constructor side effects.

Each independent operation needs exactly one `operation_contracts` entry with
matching `id`, `permission`, `public` and `idempotency`. It names a `resource`
(`group.Name`), `kind` (`create`, `load`, `list`, `export`, `transition`, `effect`),
`resource_policy` (`create`, `owner`, `tenant`), transition `event`, write replay
contract (`required` or justified `exempt` using `exemption_reason`) and emitted
`effects`. Reads require `idempotency = "none"`. List/export policy must match
resource visibility; incompatible ownership guards fail validation.

An effect declares `name`, target `operation`, typed delivery `kind` and finite
`max_attempts` (1..100). Jobs declare the same operation reference and explicit
`max_attempts`; effect-kind budgets must match. Worker identity comes from a
configured session credential, not payload actor fields. Agent writes require
`approval = "host"` and required replay: the host supplies the exact opaque
operation intent; model arguments cannot choose identity, resource, input or key.

HTTP endpoints declare `resource_source`: `body`, `path:name`, `query:name` or
`collection`. Conflicting resource/key bindings fail. Collections forward bounded
limit/cursor input into the scoped data port. The data transaction owns replay;
candidate routes do not create a second HTTP response-capture claim.

The scoped-record candidate owns all production Go files, including foundations,
resource domain, operation workflow, ports, SQL adapters and entry adapters.
Unsupported authored hooks, raw SQL clients, alternate handlers, reflection or
unsafe escape hatches fail canonical checks. Extend the framework contract for
new business patterns instead of editing generated files. Declared role catalogs
compile against actual RBAC rules; current memberships come from explicit trusted
SQL assignments. No users, sessions or assignments are automatically created.

Source inspection checks this supported shape and exact managed integrity. The
candidate test profile additionally exercises real SQL, current identity/resource
checks, all three write entries and bounded delivery. It does not prove arbitrary
business policy, remote provider idempotency or external dependency internals.
Effects use stable downstream idempotency keys and finite retries; external
exactly-once delivery is not claimed.

## Regenerating managed files

`pluto generate` renders every managed file from pluto.toml with the installed
CLI. Edit pluto.toml (or upgrade the CLI), then run it; never hand-edit managed
files. It works in the published and candidate profiles and is idempotent.

```sh
pluto generate --dry-run   # unified diff, refusals, new proposals
pluto generate             # needs a clean Git tree; no hooks run
```

A managed file is overwritten only while it is pristine relative to a render the
CLI knows: the current one, or an earlier generator's render recorded in the
migrate history (drift caused by a CLI upgrade). A file that matches none was
edited by hand; `generate` prints `file:line` with the first differing line,
writes nothing and exits nonzero. Move the change into pluto.toml or an
authored file, restore the managed file and rerun. Applied SQL migrations are
never rewritten. Required migrations are proposed with framework numbering: the
forced-RLS migration of each new `[[tables]]` entry and the `[database]
runtime_role` migration; missing authored adapter stubs are created once. After
writing, `generate` runs OpenAPI generation and `pluto fmt`.

## Migrating existing applications

`pluto migrate` moves an application between manifest states (schema version and
boundary profile) through ordered, versioned steps. Each step is deterministic and
idempotent; a chain is planned completely before any write, and an application
already at the target is a no-op.

```sh
pluto --no-input migrate --to candidate --dry-run \
  --candidate-auth-source /absolute/path/pluto-auth \
  --candidate-rbac-source /absolute/path/pluto-rbac \
  --candidate-core-source /absolute/path/pluto-core \
  --candidate-jobs-source /absolute/path/pluto-jobs
pluto --no-input migrate --to candidate --contracts contracts.toml \
  --candidate-auth-source ...   # same sources; applies the plan
```

The same `--candidate-<lib>-revision` flags adopt verified pins instead of local
sources; published requirements of the pinned modules move to their pins.

`--to` takes a profile (`published`, `candidate`) or a schema version; the default
is the latest schema in the current profile. `--dry-run` prints the step list,
every changed path, the full unified diff and all manual items without writing.
Applying requires a Git work tree with no uncommitted or untracked changes, so the
result is reviewable and reversible with Git; no hooks run.

A step may change only:

- `pluto.toml`, through a typed manifest transform;
- `go.mod`, keeping application requirements and adding profile requirements
  (moving pinned candidate modules to their verified versions);
- `go.sum`, adding verified checksums of pinned candidate modules;
- scaffold files that are still pristine. A file is pristine when it matches the
  previous profile's render token for token, comments included; formatter-only
  differences (layout, trailing commas) do not count as edits.

Everything else becomes a manual item with `file:line`, the reason and the action:
edited scaffold files at paths the target profile generates, edited scaffold files
with no target counterpart, and authored Go code that uses package-level names the
target removes (found by parsing with `go/ast`). Existing SQL migrations are never
rewritten or removed; differences are reported. Edited README, AGENTS and `docs/`
files are kept and listed as notes. The command exits nonzero while any manual item
remains; a blocked step (an unmet precondition) writes nothing.

After writing, migrate runs `go mod tidy` (when modules changed), OpenAPI
generation, `pluto fmt`, `pluto architecture check` and `pluto vet`, and reports
each result. A failed check fails the command; nothing is skipped or waived.

Files from older scaffold versions are recognized as pristine: migrate embeds
historical renders and a file matching any earlier scaffold counts as unedited.
A scaffold file that was edited, and is not generated, is three-way merged
(old scaffold, your file, new scaffold) with `git merge-file`; a clean merge is
applied, and only real conflicts or merges that do not parse as Go become manual
items. Generated sqlc output is kept with a note. Pristine scaffold tests that
exercise removed scaffold APIs (for example the old `app.New` test) are deleted
and reported. Authored files outside role directories get a concrete target in
the ARCH602 message (role packages, or a declared `[[operations.query]]`). When
`[memberships]` binds an app table, the framework migration `099_memberships.sql`
is no longer rendered; in an existing app it is reported and never deleted.
Generated RLS migrations get a `db/down` counterpart so up/down/up tests pass.

Steps:

- `010-published-to-candidate` (schema 10): requires explicit candidate sources
  and multi tenancy; switching tenancy is not mechanical. Every declared operation
  needs a typed behavior contract, which endpoint metadata cannot define, so supply
  `--contracts`: a TOML file with `[[resources]]`, `[[operation_contracts]]`,
  `[[effects]]`, `[[roles]]` (as in a blueprint) and `[[jobs]]` entries of `kind`
  and `max_attempts`. Route-only feature flags block. Local credentials and OIDC
  block while the candidate profile rejects them; the step re-checks them with the
  candidate validator, so they become migratable when that support lands. Scaffold
  tests specific to the published composition are removed while pristine.

## Same-transaction writes across groups

A port method that takes `scope.Tx` first may be called by another group's
operation inside that operation's own `DB.Write` transaction, so both writes
commit or roll back together under the one verified tenant.

```toml
[[ports]]
id = "audit.Events"
adapter = "audit.EventStore"
methods = ["List(...)", "AppendTx(scope.Tx, ...)"]
tx_methods = ["AppendTx"]
tx_callers = ["approvals.Approve"]

[[operations]]
id = "approvals.Approve"
ports = ["approvals.Requests", "audit.Events"]
```

Both sides declare it: the port lists `tx_methods` (each must be a declared
method taking `scope.Tx` first) and `tx_callers` (write operations only), and the
caller lists the port. The caller receives a generated narrow interface holding
only the tx methods, never the callee's execution methods, so the callee's gate
cannot be skipped or re-entered and no nested transaction opens. The callee
adapter uses only `queriesOf(tx)`; the tenant is the caller's. Any other
operation naming the port, or a read operation, fails validation.
`scope.Tx.Enqueue` stays fail-closed.

## Tenant-less system operations

Everything is tenant scoped except two explicit shapes. Declare the tenant root
table with `tenant_root = true` in `[[tables]]` (at most one; its tenant column
is its own id).

- `scope = "provision"`: an authored write that creates a new tenant root row
  and its first rows (for example org plus first admin) in one transaction.
  Generated code mints the new tenant id and sets `app.tenant_id` to it; the body
  cannot choose it or write into an existing tenant. The gate requires a verified
  identity but no membership. Requires `body = "authored"`, `write = true`, a
  protected operation, `idempotency = "exempt"` with `replay_reason`, an input and
  output model, ports in its own group. No permission, resource or collection.
- `scope = "lookup"` with a `[[lookups]]` declaration (`id`, `table`, `key`,
  `columns`): a generated public read resolving one root row by a unique key (for
  example slug before login). Safest option chosen: a `security_invoker` view
  exposing only the declared columns plus an RLS select policy admitting only the
  row whose key equals a transaction-local setting, so other columns, tables and
  rows are unreachable. The operation declares exactly one string query parameter
  named like the key, `body = "system"`, no ports or input.

`ARCH612` understands these two query kinds; no general tenant-less escape hatch
exists. Inserts into the tenant root are accepted only in provision queries.

## Memberships, active state and the runtime role

Candidate `[memberships]` binds roles to an app-owned declared table. Optional
`active_column` limits membership to active rows: with `active_values` the column
is compared as text against that set, without it the column must be boolean and
true. The predicate is part of every per-request membership query (nothing is
cached), so a suspended subject is denied on the next request and a reactivated
one is allowed. `[database] runtime_role` names the least-privilege role: the
blueprint proposes a NOLOGIN, NOBYPASSRLS role migration granting SELECT/INSERT
(append-only tables) or SELECT/INSERT/UPDATE/DELETE on declared and `pluto_*`
tables; enable login and set the credential in the deployment. Generated
`postgres.VerifyRuntimeRole` runs in the API and worker mains and refuses to
start when the connected role is a superuser, has BYPASSRLS, owns or inherits
ownership of a declared table, is not the declared runtime role, or a declared
table lacks forced row-level security. There is no override.

## Per-tenant background jobs

Offline detection, expiry sweeps and similar maintenance must run for every
tenant without any cross-tenant query. Declare an operation with
`scope = "each_tenant"`:

```toml
[[roles]]
id = "janitor"
name = "Tenant janitor"
description = "Service role of the per-tenant sweeps."
permissions = ["gateways.sweep"]

[[operations]]
group = "gateways"
action = "MarkStale"
scope = "each_tenant"
schedule = "every 30s"          # "every <5s..24h>" or "triggered"
timeout = "10s"                 # per tenant, 1s..5m, default 30s
concurrency = 2                 # simultaneous tenants, 1..32, default 4
max_attempts = 2                # pluto-jobs budget per tenant run, default 3
write = true
idempotency = "exempt"
replay_reason = "Marking a gateway offline twice is a no-op."
permission = "gateways.sweep"
service_role = "janitor"
body = "authored"
resource = "gateways.sweep"
resource_policy = "tenant"
ports = ["gateways.Registry"]
```

The body is `logic.MarkStale(ctx, e scope.Execution, ports MarkStalePorts) error`.
It has no request, no HTTP entry, no caller and no tenant input; `e` is the
execution of one tenant and `e.Tenant()`, `scope.DB.Read/Write` and
`queriesOf(tx)` behave as in any authored body, so row-level security and
ARCH612 bind it to that tenant alone. `write = true` needs
`idempotency = "exempt"` and a `replay_reason`: a run may repeat per tenant and
schedule slot, so the body must be idempotent.

How it runs (all generated):

- The tenant root table (`tenant_root = true`, required) gains a migration with
  a SELECT policy `USING (current_setting('app.tenant_index', true) = 'on')` and
  a security-invoker view `pluto_tenant_index` exposing the tenant column alone.
  Only the generated index reader sets the switch, in a read-only transaction
  that queries only that view. There is no BYPASSRLS role and no tenant-less
  escape hatch; sqlcheck requires the exact policy shape and rejects any wider
  permissive policy on the root, and authored SQL may not touch the view.
- `Schedule(ctx)` on the runtime (started by the generated worker main) ticks
  each scheduled job, pages the index and enqueues one pluto-jobs delivery per
  tenant with kind `tenant.<group>.<action>`, a delivery id derived from the
  kind, tenant and schedule slot (replicas enqueue each run once) and the
  declared budget. `triggered` jobs have no loop; call
  `runtime.Operations.<Group><Action>.Enqueue(ctx)` from your own trigger.
- Each delivery is input, not authority: the handler checks kind and budget,
  decodes the payload, re-checks the tenant against the index (a vanished tenant
  is a permanent failure), takes a concurrency slot (`concurrency`), applies
  the per-tenant `timeout`, authenticates the configured service credential live
  and runs the identity gate. The gate's membership check is "the tenant exists"
  and its RBAC resolver grants exactly the declared `service_role`, which must
  directly grant the operation's `permission`; every `scope.DB` call rechecks
  the session, tenant, permission and resource policy. Retries and backoff are
  pluto-jobs'; handler timeout and lease are sized for the slowest job.
- The service credential is a session token in
  `<PREFIX>JOB_<GROUP>_<ACTION>_CREDENTIAL`. The subject behind it needs no
  membership in any tenant; its authority is the service role alone, so give
  that role only what the sweep needs.

Delivered rows stay in `pluto_jobs` (pluto-jobs has no retention); plan pruning
for high-frequency schedules across many tenants.
