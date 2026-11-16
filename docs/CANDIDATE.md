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
CLI knows: the current one, what the CLI itself last wrote (recorded as a digest
in `.pluto/managed.lock`, which every CLI command that writes managed files
updates; commit it), or an earlier generator's render recorded in the migrate
history. A template change in a new CLI is therefore an upgrade, never a "hand
edit", with no per-release snapshot. `pluto vet` reports such a file as stale
("run pluto generate"). A file that matches none was
edited by hand; `generate` prints `file:line` with the first differing line,
writes nothing and exits nonzero. Move the change into pluto.toml or an
authored file, restore the managed file and rerun. Applied SQL migrations are
never rewritten. Required migrations are proposed with framework numbering: the
forced-RLS migration of each new `[[tables]]` entry and the `[database]
runtime_role` migration; missing authored adapter stubs are created once. After
writing, `generate` runs OpenAPI generation and `pluto fmt`.

An application scaffolded before the lock existed has none: run
`pluto generate --adopt --dry-run`, review the diff, then `pluto generate
--adopt`. Adopt overwrites every managed file with the current render, hand
edits included (Git is the safety net), records the lock, and refuses once a
lock exists.

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

## Machine identity

Gateways, relays and other non-human callers are not users. A declared
principal gives them a lifecycle: an org admin registers one (a pending row plus
a one-time token), the machine enrolls once through a public generated endpoint
that binds its ed25519 public key, and from then on every request is verified by
signature. Tenant and subject come from the machine's own row, never from
request input.

```toml
[[tables]]
group = "gateways"
name = "gateways"
tenant_column = "org_id"
row_security = true

[[principals]]
id = "gateways.Gateway"            # <group>.<Name>
kind = "machine"
table = "gateways"                 # declared table of the same group
id_column = "id"                   # text-comparable (text, uuid)
key_column = "public_key"          # nullable text: base64url ed25519 key
state_column = "state"             # text
pending_value = "pending"          # state until enrollment
active_value = "active"            # the only state that authenticates
permissions = ["gateway.heartbeat", "gateway.status"]
enroll_ttl = "24h"                 # default 24h, max 168h
replay_window = "2m"               # default 2m, 5s to 10m

[[operations]]
group = "gateways"
action = "Enroll"
scope = "enroll"
principal = "gateways.Gateway"
public = true
write = true
idempotency = "exempt"
replay_reason = "A single-use token is the replay protection."
rate_limit = "12/1h"
body = "system"
response = "object"
input = "gateways.EnrollInput"    # exactly token:string, public_key:string
output = "gateways.Enrolled"      # exactly machine_id:string

[[operations]]
group = "gateways"
action = "Heartbeat"
auth = "machine:gateways.Gateway" # signature-verified, no user session
permission = "gateway.heartbeat"  # must be one of the principal's permissions
write = true
idempotency = "exempt"
replay_reason = "Signed nonces reject replays."
body = "authored"
resource = "gateways.heartbeat"
resource_policy = "tenant"
ports = ["gateways.Registry"]
response = "object"
input = "gateways.Beat"           # JSON body (writes) or [[operations.query]] (reads)
output = "gateways.Ack"
```

Rules, all enforced before any write:

- Each principal needs exactly one `enroll` operation; machine permissions are
  declared only on the principal and must not equal any user role or operation
  permission, so they never enter the user permission catalog. Machine
  operations use `body = "authored"`, `response = "object"`, `"page"` or
  `"stream"`, `resource_policy = "tenant"` and exactly one endpoint (POST for
  object writes, GET for reads, pages and streams). Machines require `tenancy = "multi"` and local credentials (the
  shared attempt limiter rate limits enrollment).
- Registration is authored: insert the pending row (`state = pending_value`,
  key null) and, in the same write transaction, call
  `tx.IssueEnrollment(ctx, "gateways.Gateway", id)`. It returns the token once;
  only its SHA-256 is stored, tokens are 256-bit random, and re-issuing removes
  the earlier unused token. It refuses a machine that is not a pending row of
  the transaction's tenant.
- Enrollment resolves the token before any tenant is known through the lookup
  pattern: a `security_invoker` view plus a SELECT policy admitting only the row
  whose hash equals a transaction-local setting. The token's tenant then selects
  the tenant of the rest of one transaction: the token is marked used
  (`used_at IS NULL AND expires_at > now()` in one UPDATE) and the machine row
  moves from pending to active with the key. Unknown, expired, used and foreign
  tokens, a non-pending machine and malformed input are the same
  `401 invalid_enrollment`; a failed attempt does not spend the token.
- A request is signed with ed25519 over the newline-joined string
  `pluto-machine-v1`, principal id, machine id, method, escaped path, raw query,
  hex SHA-256 of the body, unix seconds and nonce, sent as
  `X-Pluto-Machine-ID`, `X-Pluto-Timestamp`, `X-Pluto-Nonce` (16 to 64
  base64url characters) and `X-Pluto-Signature` (base64url, no padding). The
  verifier looks the machine up by id through its own lookup view and policy,
  requires `active_value`, a timestamp within `replay_window`, a valid
  signature, and a fresh nonce (consumed in the tenant's nonce table even if the
  operation later fails). Every failure is the same `401 unauthenticated`.
- The body receives a `scope.Execution` whose tenant is the machine row's and
  whose `SubjectID()` is the machine id. `DB.Read/Write` recheck inside the
  transaction that the machine is still active with the verified key, so a
  revoked or re-keyed machine is denied on its next data access.
- Generated storage uses forced-RLS framework tables (`pluto_machine_enrollments`,
  `pluto_machine_nonces`, migration `101_machine_identity.sql`) and the machine
  table's lookup policy/view (proposed with the table's RLS migration). ARCH612
  checks the policy exactly; authored queries cannot touch the framework tables
  or the lookup view. Authored code cannot construct `machineauth.Verified` or
  call `scope.BindMachine` (ARCH603/ARCH604) nor import the machine packages
  (ARCH610). Client helpers: `transport.SignMachineRequest` signs a request in
  Go; any ed25519 client can build the headers from the canonical string.

Credential columns are generated-only:

- The principal's `key_column` and `state_column` are protected: ARCH612
  rejects any authored `INSERT` that names them and any authored `UPDATE` that
  sets them, so authored code can neither forge a key nor revive a machine. A
  registering `INSERT` omits both; give the state column
  `DEFAULT '<pending_value>'` (a row in any other state is refused by
  `IssueEnrollment`) and leave the key null. The enrollment tokens live in the
  generated, forced-RLS `pluto_machine_enrollments` table, which authored SQL
  cannot touch either.
- Registration, rotation and revocation are generated `scope.Tx` methods an
  authored write calls inside its own transaction, so they commit or roll back
  with the business change: `tx.IssueEnrollment(ctx, principal, id)`,
  `tx.RotateMachine(ctx, principal, id)` (clears the key, which stops the
  machine authenticating at once, and returns a new single-use token) and
  `tx.RevokeMachine(ctx, principal, id)` (moves a pending or active machine to
  `revoked_value`, default `revoked`, and deletes its unused tokens). A revoked
  machine cannot be rotated back; both fail with a conflict
  (`scope.ErrMachineState`) for an unknown, foreign or revoked id.
- A verified machine has a request budget: `request_limit` (default 600, max
  100000) verified requests per two replay windows, counted from the nonce
  table so every replica shares it. Over budget answers `429 rate_limited`;
  only requests whose signature verified count, so an unauthenticated caller
  cannot spend or probe a machine's budget.
- With `[auth.events]` declared, a successful enrollment is recorded through
  the same operation with `event = "machine_enroll"`, bound to the machine's own
  tenant and attributed to the machine id (`e.SubjectID()`). Under the closed
  policy a failed record withdraws the enrollment (the machine is revoked) and
  the enroll call fails; best effort logs and proceeds. The event operation
  gains a generated `ExecuteMachine` entry used only by enrollment; the
  `RecordAuthEvent` body must therefore accept that kind.

Machine operations take three response shapes. `response = "object"` (a read
with query parameters, or a write with a JSON input) as above. `response =
"page"` with `collection = true` and an `item` model is a signed paginated read:
the cursor is bound to the verified machine, its tenant, the resource and the
filters, so another machine cannot replay it. `response = "stream"` is a signed
WebSocket channel (see Streams): the upgrade request is verified once (signature
over `GET`, path and query, empty body), the body runs as the machine in its
tenant with `e.SubjectID()` = the machine id, every data access and a periodic
recheck (`recheck_seconds`) confirm the machine is still active with the
verified key, and revoking it closes the stream (1008). Machine streams have no
user Origin allowance: service clients send none, and a browser Origin is
refused unless `origins` lists it.

Limits: liveness is a normal tenant column (see the per-tenant job below); a
machine stream is verified at the upgrade, not per message; the registering
operation is an authored write exposed like any other (see authored write
endpoints).

## Authored write endpoints

An authored operation with `write = true` and an `input` model is exposed by an
ordinary `[[endpoints]]` entry using `POST`, `PUT`, `PATCH` or `DELETE` (a
write behind a read method is refused). The input is the request body, whatever
the method; the response is `response = "object"` with an `output` model.

```toml
[[operations]]
group = "gateways"
action = "Rename"
permission = "gateways.manage"
write = true
idempotency = "required"          # or exempt + replay_reason
body = "authored"
resource = "gateways.registry"
resource_policy = "tenant"
ports = ["gateways.Registry"]
response = "object"
input = "gateways.RenameGateway"
output = "gateways.GatewayInfo"
feature = "gateways"              # optional default-off flag

[[endpoints]]
group = "gateways"
action = "Rename"
operation = "gateways.Rename"
method = "PATCH"
path = "/api/gateways"
```

The generated handler, in order: the route gate (feature flag, session);
the verified caller and tenant; a body read limited by the server body limit
(`413 body_too_large`); a strict decode into the declared model (unknown fields,
trailing data, non-object JSON and wrong types are `400`); then the identity
gate (membership, permission, resource) before the authored body runs. The
OpenAPI request schema is the input model. Field values beyond their type
(lengths, formats, ranges) are domain rules: validate them in the body or the
`rules` package.

`idempotency = "required"` adds generated `Idempotency-Key` handling over the
durable store: the claim is taken only after the gate has authorized the exact
request (an unauthorized caller never creates or replays one), a replay of the
same key and body returns the stored response without running the body, and the
same key with another body is `409`. Claims are never released on error, so a
failed execution is not retried under its key. The stored response is replayed
verbatim: do not return secrets (such as an enrollment token) from an
idempotent write; declare those `exempt` with a `replay_reason`. `required`
needs an HTTP endpoint; an authored write that no endpoint exposes stays
`exempt`. A write takes no query parameters; identify the record with a typed
path parameter (next section) or carry identifiers in the body.

## Path parameters on authored endpoints

REST routes address one record: `GET|PATCH|PUT|DELETE /api/gateways/{id}`.
Each `{segment}` of an authored endpoint path is declared and typed on the
operation, and the entry names the one that selects the resource:

```toml
[[operations]]
group = "gateways"
action = "Edit"
permission = "gateways.manage"
write = true
idempotency = "required"
body = "authored"
resource = "gateways.registry"
resource_policy = "tenant"
ports = ["gateways.Registry"]
response = "object"
input = "gateways.GatewayName"     # JSON body; omit for a bodyless write
output = "gateways.GatewayInfo"
path = [{ name = "id", type = "uuid" }]

[[endpoints]]
group = "gateways"
action = "Edit"
operation = "gateways.Edit"
method = "PATCH"
path = "/api/gateways/{id}"
resource_source = "path:id"
```

Types: `uuid` (canonical hex, lowercased on bind), `string` (`max` 1..256,
letters, digits and `-._~`) and `int64` (canonical non-negative decimal,
optional `max`). Names are `[a-z][a-zA-Z0-9]*` like route segments. Validation
refuses a `{segment}` without a declaration, a declaration that is not in the
path, a missing or mismatched `resource_source = "path:<name>"`, and path
parameters on anything but an authored, protected, non-collection
`response = "object"` operation on the verified user path (not machine,
stream, enroll, provision, lookup or per-tenant).

The generated handler verifies the caller, then validates every path value
against its declared type (`400 invalid_path_parameter`, before the body is
read or any claim is taken), then proceeds as above. The request model is a
generated struct: a read's carries the path fields beside its query fields; a
write's has `Body` (the strictly decoded input model, absent when no `input`
is declared; a bodyless write refuses any body with `400 invalid_body`) and
one field per path parameter (`request.ID`). Path fields are never part of the
JSON body or the OpenAPI request schema; OpenAPI lists them as typed `path`
parameters.

The identity gate is bound to the resource key `"<resource>:<value>"` of the
parameter named by `resource_source`; the generated policy demands exactly that
shape, and every data access of the body revalidates the same key. The gate
authorizes membership and permission, not existence: look the record up
through `queriesOf` under the verified tenant and map an empty result with
`scope.NotFound(err)`. Another tenant's id is then a `404 not_found` identical
to a missing id, while a caller outside the tenant or without the permission
fails earlier with 401/403. With `idempotency = "required"` the stored claim is
keyed by the request path, so one key reused on another id is a `409`.

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

A `scope = "provision"` operation may list such a port too (it must appear in the
port's `tx_callers`). The provision body then receives the narrow interface and
`TxDB`; `TxDB.Provision(ctx, p, run)` opens the one transaction bound to the NEW
tenant, so the organization, its first admin and its first audit row commit or
roll back together, attributed to the verified subject (`tx.SubjectID()`). The
provisioning authority still mints at most one tenant per request.

## Authentication events

Managed credential handlers cannot call authored code, so events are declared
and generated instead of hooked by hand. `[auth.events]` names one authored
operation the generated glue runs when a session is issued or ended:

```toml
[auth.events]
operation = "audit.RecordAuthEvent"
on_error = "closed"   # or "best_effort"; default closed

[[models]]
id = "audit.AuthEvent"
fields = ["event:string"]

[[operations]]
group = "audit"
action = "RecordAuthEvent"
permission = "audit.record"
write = true
idempotency = "exempt"
replay_reason = "Authentication events are recorded once per sign-in."
body = "authored"
resource = "audit.events"
resource_policy = "tenant"
ports = ["audit.Events"]
input = "audit.AuthEvent"
```

The operation is an ordinary protected authored write: its body appends through
`queriesOf` under RLS (for example into an append-only audit group). Validation
requires local credentials and `[memberships]`; an input model of exactly
`event:string`; no response; no endpoint, job or agent (so only the generated
hook can run it); and every declared role must hold its permission, so a
fail-closed policy can never lock out a member whose role lacks it.

Recorded events, with `event` set to `login`, `oidc_login` (when OIDC is on) or
`logout`:

- **Sign-in / OIDC sign-in**: the handler's response is held until the record
  is written. The subject is verified through the new live session; its
  organizations come from its active memberships (`active_column` applies);
  the operation runs once in each, through the normal identity gate
  (membership, permission, RLS) and is attributed to the verified subject
  (`tx.SubjectID()`). More than 16 organizations is a failure. Nothing is read
  from the request: a presented `X-Tenant-ID` is ignored.
- **Sign-out**: recorded before revocation (a revoked session cannot prove the
  subject), after the same Origin and ingress checks as the handler.
- **Not recorded in any org**: sign-up, verification, recovery, reset, failed
  sign-in, and any event of a subject with no organization (for example sign-in
  before provisioning). With no verified subject and organization there is no
  scope to write into without letting a stranger choose or probe one, so they
  are logged by kind only. Failed sign-in is deliberately identical for known
  and unknown accounts; recording it per organization would need an
  unauthenticated account-to-org lookup. The provisioning transaction records
  the organization's first audit row itself (see above).
- **Policy**: `closed` (default) denies the sign-in with 503, revokes the
  just-issued session and delivers no credential; sign-out is refused with 503
  and the session stays. `best_effort` logs the failure by event kind and
  proceeds. A response too large to hold is never delivered.

Organizations are resolved through a generated subject index on the memberships
table, created by the proposed migration: a SELECT policy admitting only rows
whose subject equals the transaction-local `app.auth_event_subject`, and a
security-invoker view `pluto_auth_event_members` exposing only the organization
id of active memberships. Only the generated hook sets the setting, from a live
verified session; ARCH612 accepts exactly this policy beside tenant isolation,
and the runtime role gets SELECT on the view only. Without `[auth.events]`
neither the policy, the view, nor any read exists.

## Not-found semantics

A resource in another tenant is indistinguishable from a missing one. Row-level
security and the `tenant_id` predicate make foreign rows invisible, so generated
resource policies and repositories return `domain.ErrNotFound` and the generated
handler maps it to `404 {"error":{"code":"not_found"}}` for GET, PUT/transition
and DELETE alike. Missing membership in the caller's tenant (an `X-Tenant-ID`
the verified subject does not belong to) and missing permission still fail in
the gate before any lookup: 401 or 403, never 404. Owner-visibility rows owned
by someone else are not found too.

Authored adapters map an empty single-row result with
`return scope.NotFound(err)` (or `scope.NoRows(err)` then `scope.ErrNotFound`);
any other error passes through unchanged and stays a 500. Do not return a
forbidden error for a missing row: that leaks existence.

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

## Feature flags on operations

An `[[operations]]` entry (authored, generated, system) may declare
`feature = "<key>"` and optionally `feature_client_visible = true`. The operation
owns the flag: its `[[endpoints]]` entries inherit it, and an entry that declares
a different or additional flag fails validation (route-only flags stay rejected).
Keys are the existing flag keys; the flag is declared by use, defaults to off and
is set through `<PREFIX>FEATURE_<KEY>` (dots and hyphens become underscores).

- The generated route gate checks the flag after session verification and before
  the handler, the identity gate, membership lookups, rate limits, idempotency or
  the authored body run. A disabled flag answers `403 feature_disabled` to
  every verified caller, authorized or not, and produces no database effect.
  Anonymous callers still get `401`.
- An enabled flag changes nothing else: authentication, membership, permission
  and resource checks apply unchanged. A flag never grants authorization.
- OpenAPI marks the operation with `x-feature`. `GET /api/features` (now served
  by the candidate runtime) lists only keys declared `feature_client_visible`,
  to authenticated callers.
- Flags gate transports. Code that calls an operation directly (workers, other
  authored code) is not flag gated; gate exposure, not authorization.

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

## Streams (WebSocket)

An authored operation with `response = "stream"` is a long-lived bidirectional
channel for gateway job pushes, relay fan-out or live replay. It is exposed
only through one endpoint with `transport = "websocket"`; the body never sees a
connection.

```toml
[[models]]
id = "audit.Cursor"                 # client -> server
fields = ["seq:int64"]

[[models]]
id = "audit.Tick"                   # server -> client
fields = ["seq:int64", "type:string"]

[[operations]]
group = "audit"
action = "Watch"
permission = "audit.read"
idempotency = "none"
body = "authored"
resource = "audit.events"
resource_policy = "tenant"
ports = ["audit.Events"]
response = "stream"
item = "audit.Tick"                 # server message; required
input = "audit.Cursor"              # client message; omit for push-only
query = [{ name = "after", type = "int64" }]

[operations.stream]                 # optional; these are the defaults
max_message_bytes = 65536           # 1..1048576, both directions
write_seconds = 10                  # slow-consumer deadline per Send
ping_seconds = 20
idle_seconds = 60                   # must exceed ping_seconds
recheck_seconds = 15                # gate recheck interval
max_connections = 256               # per operation, per process

[[endpoints]]
group = "audit"
action = "Watch"
operation = "audit.Watch"
transport = "websocket"
origins = ["app.example.com"]       # optional browser host patterns
method = "GET"
path = "/api/audit/watch"
name = "Watch audit"
description = "Replay audit events over a WebSocket."
```

The body is `func Watch(ctx, e scope.Execution, p WatchPorts, in
models.WatchRequest, stream scope.Stream[models.Cursor, models.Tick]) error`.
`stream.Send(ctx, msg)` writes one JSON text frame; `stream.Receive(ctx)`
returns the next client message decoded strictly (one object, unknown fields
and trailing data rejected) or `scope.ErrStreamClosed` when the stream ends,
which a loop treats as its normal end. Returning nil closes normally. Push-only
streams (no `input`) use `scope.Stream[scope.NoMessage, Out]`; any client data
message disconnects the peer. Query parameters are the usual typed `query`
declarations and arrive in `in`.

Generated code owns everything else:

- Authentication and the gate run before the upgrade (`Prepare`): a denied
  caller gets an ordinary 401/403/400 and never a socket; a full operation gets
  503 `stream_capacity`. Serve then reruns the gate and runs the body inside
  it, so `scope.DB` and every adapter call recheck identity, membership,
  permission and resource per access, exactly as for HTTP bodies.
- Origin: a request carrying an `Origin` header must be same-host or match
  `origins` (host patterns; `*.example.com` allowed, a bare `*` is rejected).
  Clients without an Origin header are service clients, not browsers. The
  session transport's trusted origin may differ from the Host seen behind a
  proxy; list it in `origins`.
- Tenant: the `X-Tenant-ID` header, or for browsers (no custom headers on a
  WebSocket) the `tenant` query parameter, verified like any tenant selection.
  Credentials are the session cookie or bearer token, so a machine principal
  works wherever the application accepts it for HTTP (the stream only needs an
  `access.Caller`).
- Limits: read limit and outbound size check at `max_message_bytes`; text
  frames only; `Send` blocks while the peer is slow and fails the stream after
  `write_seconds` (backpressure, no unbounded queue); the inbound queue holds
  16 messages, so a body that declares input must keep calling `Receive`.
- Liveness: the runtime pings every `ping_seconds` and closes (1001) a peer
  that has not answered within `idle_seconds`. Quiet but responsive streams
  stay open.
- Revocation: every `recheck_seconds` the live gate is re-verified. Any
  failure, including a transient one, closes the stream (1008 for denied or
  unauthenticated, 1011 otherwise) and cancels the body's context, so a
  suspended member, expired session or removed role loses the stream within
  one interval.
- Close codes are fixed strings, never error text: 1000 done, 1001 idle or
  going away, 1003 binary frame, 1007 invalid message, 1008 unauthorized or
  unexpected message, 1009 too big, 1011 internal error.

A stream may write (`write = true`, `idempotency = "exempt"` with a
`replay_reason`; the upgrade is still a GET protected by the Origin check).
Cross-tenant data stays impossible: tenant comes only from the verified
execution and RLS binds every transaction.

Test bodies with `scopetest.RunStream(ctx, harness, subject, tenant, resource,
drive, body)`: it runs the body under a real gate execution through the same
runtime over an in-memory peer (`Send`, `SendRaw`, `Receive`, `Close`).

Generated OpenAPI documents the upgrade as a GET with a 101 response and an
`x-websocket` extension holding the message schemas, the size limit and the
allowed origins. Blueprints and `pluto generate` need the WebSocket module:
blueprints pin `github.com/coder/websocket` v1.8.14 in go.mod/go.sum; after
adding a stream by editing pluto.toml run `go mod tidy`. Authored code may not
declare any websocket library in `[[authored.dependencies]]`, and the library
is imported only by generated files.

Limits of this phase: connection caps are per process; streams are not closed
by `http.Server.Shutdown` (hijacked connections), so rely on the process exit
or the recheck interval; there is no per-subject connection quota or message
rate limit beyond the size, write and idle bounds.

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

## Device authorization and scoped delegated tokens

`[auth.device]` declares the OAuth 2.0 device authorization grant (RFC 8628) for
a CLI or an agent acting for a user. It needs the candidate profile, multi
tenancy and `--local-credentials` (the owner approves in a signed-in browser
session). Everything is generated; the generated app mounts these routes under
`/auth/device` (the `/auth` namespace is reserved) and applies
`db/migrations/090_device_grants.sql` (`pluto_auth_device_*` tables; codes and
tokens are stored only as SHA-256 digests).

```toml
[auth.device]
verification_uri = "https://app.example.com/device"   # browser page
# optional, bounded: code_ttl 10m, poll_interval 5s, access_ttl 15m,
# refresh_ttl 720h, max_lifetime 2160h (absolute per grant)

[[auth.device.scopes]]
name = "requests.write"
permissions = ["requests.create", "requests.read"]    # declared operation permissions

[[auth.device.clients]]
id = "gryt-cli"
name = "gryt CLI"
actor_type = "agent"            # carried on every delegated identity; never "user"
scopes = ["requests.write"]
```

Flow: `POST /auth/device/code` (public, tenant-less, ingress limited) returns
`device_code`, `user_code`, `verification_uri(_complete)`, `expires_in` and
`interval`. The owner opens the page and the browser calls `GET
/auth/device/request?user_code=` (client and requested scopes) then `POST
/auth/device/approve` with `user_code`, a `tenant` where the owner holds a
membership with a role, and a subset of the requested `scope`s (or `/deny`).
The client polls `POST /auth/device/token` with the device-code grant; polling
faster than the interval returns `slow_down` and raises the interval, and codes
are single use. The token response carries `access_token`, `refresh_token`,
`scope`, `tenant` and `grant_id`. The refresh grant rotates both tokens and may
narrow scopes, never widen them. Presenting an already rotated refresh token
revokes the whole token family (`invalid_grant`). `POST /auth/device/revoke`
(RFC 7009), `DELETE /auth/device/grants/{id}` (owner), password reset, account
disabling and `DELETE /auth/session` all revoke families.

Enforcement: an access token is an ordinary opaque session through the single
runtime manager, so requests run the normal gates with the owner's verified
identity. Each generated gate first calls `caller.Permits(permission)`: the
operation permission must be named by an approved scope and the tenant must be
the token's tenant (the `X-Tenant-ID` header may be omitted, never differ).
The identity gate then still requires the owner's live membership, role and
resource policy, so the effective authority is the intersection and never
exceeds the owner's. Delegated tokens are refused on every owner route
(approve, deny, grants) and on tenant provisioning, and the generic
`/api/features` front door, which has no permission to intersect, still applies
only the owner's membership.

Delegated tokens go through every generated gate that has a human caller:
HTTP operations, path-parameter endpoints (the gate runs `Permits` before
the lookup, so a scope miss is 403 and a tenant-invisible id is the same
`404 not_found` the owner gets), and WebSocket streams (`Permits` runs in
`Prepare` before the upgrade and again at every recheck, so revoking the
grant closes an open stream with close code 1008, written before the
connection is torn down). A delegated token naming any tenant other than the
one it was approved for is `403`, before and regardless of membership, so it
reveals nothing about the owner's other tenants. Machine routes and per-tenant jobs
have no human caller and never accept a delegated token.

Metadata: authored bodies read `e.Actor()` on `scope.Execution` (`Kind()` is
`user` or the client's `actor_type`; `ClientID()`, `GrantID()`). Authored code
cannot construct or set it (`scope.Actor` is protected; `BindActor` and
`ContextWithActor` are generated-only capabilities), and cannot import
`pluto-auth/device`. Generated code that receives an `access.Caller` (for
example the auth-events hook) can read `Caller.Delegation()`.

Operational notes: user codes are 8 characters from a 20-letter alphabet and
live for `code_ttl`; lookups and decisions are limited per subject through the
shared credential attempt limiter (so each approval uses a few of its attempts)
and the public routes through `Ingress`. Expired requests are pruned
opportunistically on creation. Requires a pluto-auth revision containing the
`device` package; the candidate pin probe does not yet check it.
