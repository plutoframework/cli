#!/usr/bin/env python3
"""Run bounded security mutations in disposable copies."""

from pathlib import Path
import os
import shutil
import subprocess
import tempfile

root = Path(__file__).resolve().parent.parent
mutations = [
    (
        "candidate compiler directive integrity",
        "internal/project/candidate_integrity.go",
        "matches = candidateSourceEquivalent(actual, files[path])",
        "matches = Equivalent(actual, files[path])",
        (
            "./internal/project#TestCandidateIntegrityRejectsExcludedGateAndS"
            "QLChanges"
        ),
    ),
    (
        "candidate constructor ownership",
        "internal/project/candidate_boundary_source.go",
        'capabilityName := file.namespace + "." + value.Name',
        ('capabilityName := "disabled." + value.Name'),
        "./internal/project#TestCandidateBoundaryCapabilitiesAndForgery",
    ),
    (
        "candidate credential seam import restriction",
        "internal/project/candidate_credential_seam.go",
        "case path == module || strings.HasPrefix(path, module+\"/\"):\n"
        "\t\treturn false",
        "case path == module || strings.HasPrefix(path, module+\"/\"):\n"
        "\t\treturn true",
        "./internal/project#TestCandidateCredentialSeamRejectsAuthorityImports",
    ),
    (
        "candidate private authority fields",
        "internal/project/candidate_boundary.go",
        "owners != nil &&",
        "owners != nil && false &&",
        "./internal/project#TestCandidateOwnershipBranches",
    ),
    (
        "candidate unsafe confinement",
        "internal/project/candidate_boundary.go",
        'if path == "unsafe" {',
        'if path == "disabled/unsafe" {',
        "./internal/project#TestCandidateBoundaryCapabilitiesAndForgery",
    ),
    (
        "candidate closed production ownership",
        "internal/project/candidate_boundary.go",
        "if rules.managed[file.path] {",
        "if true {",
        "./internal/project#TestCandidateOwnershipBranches",
    ),
    (
        "domain normalization placement",
        "internal/project/primitive_boundaries.go",
        'if operationCode && dependency == "strings" {',
        'if false && operationCode && dependency == "strings" {',
        "./internal/project#TestPrimitivePlacementAndHelperBypasses",
    ),
    (
        "cache boundary placement",
        "internal/project/primitive_boundaries.go",
        'dependency == "github.com/plutoframework/pluto-core/cache"',
        'dependency == "disabled/cache"',
        "./internal/project#TestPrimitivePlacementAndHelperBypasses",
    ),
    (
        "scoped pagination ownership",
        "internal/project/primitive_boundaries.go",
        'capability == "Finish"',
        'capability == "disabledFinish"',
        "./internal/project#TestPrimitivePlacementAndHelperBypasses",
    ),
    (
        "required replay contract",
        "internal/project/contracts.go",
        'operation.Idempotency != "required"',
        "false",
        (
            "./internal/project#TestContractsRejectMissingAndContradictoryObl"
            "igations"
        ),
    ),
    (
        "declared query visibility",
        "internal/project/contracts.go",
        "operation.ResourcePolicy != resource.Visibility",
        "false",
        (
            "./internal/project#TestContractsRejectMissingAndContradictoryObl"
            "igations"
        ),
    ),
    (
        "owned worker construction",
        "internal/project/entry_boundary.go",
        'selector.Sel.Name == "NewWorker"',
        'selector.Sel.Name == "disabledWorker"',
        "TestCrossEntryOperationConsumer",
    ),
    (
        "public lifecycle field inspection",
        "internal/project/lifecycle_boundary.go",
        "if fieldName.IsExported() {",
        "if fieldName.IsExported() && false {",
        "TestLifecycleArchitectureCheck",
    ),
    (
        "raw lifecycle writer inspection",
        "internal/project/lifecycle_boundary.go",
        "lifecycleType{parameter.Type, file, false}",
        "lifecycleType{nil, file, false}",
        "TestLifecycleArchitectureCheck",
    ),
    (
        "managed operation gate integrity",
        "internal/project/operation_boundary.go",
        "if !Equivalent(actual, expected) {",
        "if !Equivalent(actual, expected) && false {",
        "TestManagedOperationGateConsumer",
    ),
    (
        "OIDC account profile guard",
        "internal/project/manifest.go",
        "if m.Capabilities.OIDC && !m.Capabilities.LocalCredentials {",
        "if false {",
        "TestOIDCProfileRequiresAccountBackend",
    ),
    (
        "lifecycle ownership guard",
        "internal/project/templates/lifecycle.go.tmpl",
        "facts.ActorID != facts.OwnerID",
        "false",
        "TestStateScaffoldConsumers/single",
    ),
    (
        "registry source confinement",
        "internal/frontend/registry.go",
        "if !project.RegistryPath(file.Source) {",
        "if false {",
        "TestFrontendRegistryPreflight/source-traversal",
    ),
    (
        "registry strict manifest fields",
        "internal/frontend/registry.go",
        "DisallowUnknownFields().\n",
        "",
        "TestFrontendRegistryPreflight/hook",
    ),
    (
        "theme validation preflight",
        "internal/project/frontend.go",
        "if err := f.ValidateTheme(); err != nil {",
        "if err := f.ValidateTheme(); false {",
        "TestFrontendThemePreflight",
    ),
    (
        "frontend auth capability guard",
        "internal/project/frontend.go",
        "if f.Auth && !m.Capabilities.LocalCredentials {",
        "if false {",
        "TestFrontendPreflight/auth-capability",
    ),
    (
        "job kind uniqueness",
        "internal/project/jobs.go",
        "identifiers[identifier] || kinds[job.Kind]",
        "identifiers[identifier]",
        "./internal/project#TestJobKindUniqueness",
    ),
    (
        "job tenant delivery guard",
        "internal/project/templates/job_handler.go.tmpl",
        (
            'if job.TenantID == nil || *job.TenantID == "" {\n'
            "\t\t\treturn jobs.Permanent(jobs.ErrInvalid)"
        ),
        "if false {\n\t\t\treturn jobs.Permanent(jobs.ErrInvalid)",
        "TestGeneratedJobs/multi",
    ),
    (
        "auth route namespace reservation",
        "internal/project/manifest.go",
        '"/auth/"',
        '"/disabled-auth/"',
        "TestGeneratedLocalApplications/single",
    ),
    (
        "app-name traversal validation",
        "internal/project/manifest.go",
        "if !appName.MatchString(m.Project.Name) {",
        "if false {",
        "TestInitPreflight/bad-name",
    ),
    (
        "route parameter conflict",
        "internal/project/manifest.go",
        'parts[i] = "{}"',
        "parts[i] = p",
        "TestGeneratedApplications/single",
    ),
    (
        "authentication guard",
        "internal/project/templates/register.go.tmpl",
        "if endpoint.Authenticated && !endpoint.SessionOnly {",
        "if false && endpoint.Authenticated && !endpoint.SessionOnly {",
        "TestGeneratedApplications/single",
    ),
    (
        "idempotency registration",
        "internal/project/templates/register.go.tmpl",
        "if endpoint.Idempotent {",
        "if false && endpoint.Idempotent {",
        "TestGeneratedApplications/single",
    ),
    (
        "security middleware registration",
        "internal/project/templates/app.go.tmpl",
        "router.Use(transport.SecurityHeaders)",
        "// Security middleware removed by mutation.",
        "TestGeneratedApplications/single",
    ),
    (
        "anti-sniffing header",
        "internal/project/templates/security.go.tmpl",
        'headers.Set("X-Content-Type-Options", "nosniff")',
        "// Anti-sniffing header removed by mutation.",
        "TestGeneratedApplications/single",
    ),
    (
        "route RBAC gate",
        "internal/project/templates/register.go.tmpl",
        "!principal.Can(reg.Policy, endpoint.Permission)",
        "false",
        "TestGeneratedApplications/single",
    ),
    (
        "direct operation RBAC gate",
        "internal/project/templates/operation_gate.go.tmpl",
        "}); err != nil {",
        "}); false {",
        "TestGeneratedApplications/single",
    ),
    (
        "candidate pinned module replacement",
        "internal/project/boundary_modules.go",
        "if _, pinned := pins[replacement.module]; pinned {",
        "if _, pinned := pins[replacement.module]; pinned && false {",
        "./internal/project#TestCandidatePinsRejectReplacementsAndDrift",
    ),
    (
        "candidate pinned requirement drift",
        "internal/project/boundary_modules.go",
        'if requires[module] != "" && requires[module] != pins[module] {',
        'if false && requires[module] != pins[module] {',
        "./internal/project#TestCandidatePinsRejectReplacementsAndDrift",
    ),
    (
        "candidate pin pruning requires no import",
        "internal/project/candidate_modules.go",
        "if !imported[module] && module != boundaryIdentityModule {",
        "if module != boundaryIdentityModule {",
        "./internal/project#TestCandidatePinsAllowOnlyUnimportedPruning",
    ),
    (
        "candidate pin API probe",
        "internal/project/candidate_pin_verify.go",
        'goModuleCommand(ctx, directory, "build", "./...")',
        'goModuleCommand(ctx, directory, "mod", "download")',
        "./internal/project#TestVerifyCandidatePinsAgainstModuleProxy",
    ),
    (
        "candidate pin selection consistency",
        "internal/project/candidate_pin_verify.go",
        "if pins[info.Path] != info.Version {",
        "if false {",
        "./internal/project#TestVerifyCandidatePinsAgainstModuleProxy",
    ),
    (
        "authored query tenant argument",
        "internal/sqlcheck/bind.go",
        "\treturn fmt.Sprintf(\n\t\t\"does not bind %s.%s to a sqlc.arg in "
        "WHERE.\",\n\t\ttable.Name,\n\t\ttable.TenantColumn,\n\t), "
        "bindingRepair(table)\n}",
        "\treturn \"\", \"\"\n}",
        "./internal/sqlcheck#TestAnalyzeQueryRejects",
    ),
    (
        "authored adapter pool import",
        "internal/project/candidate_adapter_role.go",
        "case candidateDataPackage(path):\n\t\treturn false\n\t}",
        "case candidateDataPackage(path):\n\t\treturn true\n\t}",
        "./internal/project#TestAuthoredRoleViolations",
    ),
    (
        "authored logic reachable only from its workflow",
        "internal/project/candidate_authored_rules.go",
        "file.directory != rolesLogicDir+role.group {",
        "file.directory != rolesLogicDir+role.group && role.group == \"\" {",
        "./internal/project#TestAuthoredRoleViolations",
    ),
    (
        "authored forged scope tenant",
        "internal/project/candidate_authored_rules.go",
        '"Execution", "Tenant", "DB", "Tx", "Provisioning",',
        '"Execution", "DB", "Tx", "Provisioning",',
        "./internal/project#TestAuthoredRoleViolations",
    ),
    (
        "authored rules purity",
        "internal/project/candidate_authored_rules.go",
        '"C", "unsafe", "reflect", "plugin", "os", "net", "syscall", "log",',
        '"C", "unsafe", "plugin", "os", "net", "syscall", "log",',
        "./internal/project#TestAuthoredRoleViolations",
    ),
    (
        "adapter sqlc transaction handle",
        "internal/project/candidate_adapter_role.go",
        'case "DBTX", "WithTx":',
        'case "DBTXdisabled", "WithTxDisabled":',
        "./internal/project#TestAuthoredRoleViolations",
    ),
    (
        "authored role shape",
        "internal/project/candidate_authored_rules.go",
        'inspectAuthoredShape(file, role.role+" role", emit)',
        "_ = inspectAuthoredShape",
        "./internal/project#TestAuthoredRoleViolations",
    ),
    (
        "tenant root insert confined to provisioning",
        "internal/sqlcheck/query.go",
        'if stmt.Kind != "insert" || env.provision {',
        'if stmt.Kind != "insert" || true {',
        "./internal/sqlcheck#TestAnalyzeQueryTenantRootInsert",
    ),
    (
        "lookup policy is keyed to its own column",
        "internal/sqlcheck/migrations_apply.go",
        "tableKey(lookup.Key),",
        "tableKey(s.columns[table]),",
        "./internal/sqlcheck#TestCheckMigrationsLookupPolicy",
    ),
    (
        "system managed output is generated",
        "internal/project/candidate_system.go",
        '"internal/storage/" + operation.Group + "/" + name +\n\t\t\t\t"_lookup_gen.go": "candidate_lookup_reader.go",',
        '"internal/storage/" + operation.Group + "/" + name +\n\t\t\t\t"_lookup_gen.go": "candidate_lookup_gate.go",',
        "./internal/project#TestSystemManagedOutput",
    ),
    (
        "tx write caller allowlist",
        "internal/project/manifest_txwrite.go",
        "!slices.Contains(port.TxCallers, o.ID()) {",
        "false {",
        "./internal/project#TestTxWriteManifestRejections",
    ),
    (
        "tx write caller must be a write",
        "internal/project/manifest_txwrite.go",
        "if !o.Write {",
        "if false {",
        "./internal/project#TestTxWriteManifestRejections",
    ),
    (
        "tx write narrow port",
        "internal/project/candidate_adapters.go",
        "if slices.Contains(port.TxMethods, method.Name) {",
        "if true {",
        "./internal/project#TestTxWriteGeneratesNarrowPortAndWiring",
    ),
    (
        "table row-level security migration",
        "internal/project/candidate_table_migrations.go",
        "ALTER TABLE %[1]s FORCE ROW LEVEL SECURITY;\n",
        "",
        "./internal/project#TestTableMigrationForcesRowSecurityAndAppendOnly",
    ),
    (
        "membership active predicate",
        "internal/project/candidate_memberships_app.go",
        'query += " AND " + quoteIdent(column) + " IS TRUE"',
        'query += ""',
        "./internal/project#TestCandidateAppMembershipsActivePredicate",
    ),
    (
        "runtime role append-only grants",
        "internal/project/candidate_runtime_role.go",
        'privileges = "SELECT, INSERT"',
        'privileges = "SELECT, INSERT, UPDATE, DELETE"',
        "./internal/project#TestRuntimeRoleMigrationIsLeastPrivilege",
    ),
    (
        "runtime role startup owner check",
        "internal/project/candidate_runtime_role.go",
        "pg_has_role(current_user, c.relowner, 'USAGE')",
        "false",
        "./internal/project#TestRuntimeRoleGuardGeneratedAndWired",
    ),
    (
        "operation feature reaches its entries",
        "internal/project/operations.go",
        "if operation.Feature != \"\" || operation.ClientVisible {",
        "if false {",
        "./internal/project#TestOperationOwnedFeatureReachesEntries",
    ),
    (
        "entry cannot change operation feature",
        "internal/project/feature_gate.go",
        "if entry.Feature == \"\" && !entry.ClientVisible {",
        "if true {",
        "./internal/project#TestEntryCannotChangeOperationFeature",
    ),
    (
        "stream websocket endpoint shape",
        "internal/project/manifest_stream.go",
        'if e.Method != "GET" || e.Collection || e.Public {',
        "if false {",
        "./internal/project#TestStreamManifestRejectsBadDeclarations",
    ),
    (
        "stream exposed only through websocket",
        "internal/project/manifest_stream.go",
        "case !websocket && operation.isStream():",
        "case false && operation.isStream():",
        "./internal/project#TestStreamManifestRejectsBadDeclarations",
    ),
    (
        "websocket library is never an authored dependency",
        "internal/project/manifest_stream.go",
        "if dependency.Module == library ||",
        "if false ||",
        "./internal/project#TestStreamManifestRejectsBadDeclarations",
    ),
    (
        "stream constructor ownership",
        "internal/project/candidate_authored_rules.go",
        'rules.addCapability(namespace, "RunStream", workflows...)',
        'rules.addCapability(namespace, "RunStreamDisabled", workflows...)',
        "./internal/project#TestStreamCapabilitiesStayWithGeneratedCode",
    ),
    (
        "stream gate recheck",
        "internal/project/templates/candidate_stream_scope.go.tmpl",
        "err := e.scopeGate.Use(",
        "err := e.scopeGate.Disabled(",
        "./internal/project#TestStreamManagedFiles",
    ),
    (
        "stream read limit",
        "internal/project/templates/candidate_stream_scope.go.tmpl",
        "s.scopeSocketConn.SetReadLimit(bytes)",
        "s.scopeSocketConn.SetReadLimit(-1)",
        "./internal/project#TestStreamManagedFiles",
    ),
    (
        "stream strict inbound decoding",
        "internal/project/templates/candidate_stream_scope.go.tmpl",
        "decoder.DisallowUnknownFields()",
        "decoder.UseNumber()",
        "./internal/project#TestStreamManagedFiles",
    ),
    (
        "stream origin verification",
        "internal/project/templates/candidate_stream_handler.go.tmpl",
        "OriginPatterns: {{.Action}}Origins,",
        "InsecureSkipVerify: true,",
        "./internal/project#TestStreamManagedFiles",
    ),
    (
        "tenant index policy is an exact switch",
        "internal/sqlcheck/migrations_policy.go",
        "tokens[0].Text == \"'on'\"",
        "tokens[0].Text != \"\"",
        "./internal/sqlcheck#TestCheckMigrationsTenantIndexPolicy",
    ),
    (
        "tenant job service role grants its permission",
        "internal/project/manifest_tenantjobs.go",
        "!ok || !containsString(role.Permissions, operation.Permission) {",
        "!ok || !containsString(role.Permissions, operation.Permission) &&\n"
        "\t\t\tfalse {",
        "./internal/project#TestTenantJobManifestRejections",
    ),
    (
        "tenant job has no http entry",
        "internal/project/manifest_tenantjobs.go",
        "if endpoint.Operation == operation.ID() {",
        "if endpoint.Operation == operation.ID() && false {",
        "./internal/project#TestTenantJobManifestRejections",
    ),
    (
        "tenant job delivery rechecks the tenant",
        "internal/project/templates/candidate_tenantjob_gate.go.tmpl",
        "if !found {",
        "if false {",
        "./internal/project#TestTenantJobManagedOutput",
    ),
    (
        "transactional enqueue accepts declared kinds only",
        "internal/project/templates/candidate_scope.go.tmpl",
        "if !declared {",
        "if false {",
        "./internal/project#TestScopeEnqueueIsBoundToDeclaredKinds",
    ),
    (
        "tenant job exemption is path specific",
        "internal/project/primitive_boundaries.go",
        "!effectsDeclared && !tenantJobFiles[file.path] {",
        "!effectsDeclared && false {",
        "./internal/project#TestTenantJobExemptionIsPathSpecific",
    ),
    (
        "runtime role reserved names",
        "internal/project/manifest_seams.go",
        'role == "postgres" || role == "public" ||',
        'role == "disabled" || role == "public" ||',
        "./internal/project#TestSeamManifestRejections",
    ),
    (
        "machine permissions separate from user roles",
        "internal/project/manifest_machine.go",
        "\t\tif user[permission] {",
        "\t\tif false && user[permission] {",
        "./internal/project#TestMachineManifestRejections",
    ),
    (
        "machine operation permission grant",
        "internal/project/manifest_machine.go",
        "if !slices.Contains(principal.Permissions, o.Permission) {",
        "if false && !slices.Contains(principal.Permissions, o.Permission) {",
        "./internal/project#TestMachineManifestRejections",
    ),
    (
        "machine permission outside the user catalog",
        "internal/project/operations.go",
        'permission, scope, machine = "", ScopeMachine, o.MachinePrincipal()',
        "permission, scope, machine = o.Permission, ScopeMachine, "
        "o.MachinePrincipal()",
        "./internal/project#TestMachineManifestValidatesAndRoundTrips",
    ),
    (
        "machine enrollment model shape",
        "internal/project/manifest_machine.go",
        "if !slices.Equal(model.Fields, fields) {",
        "if false {",
        "./internal/project#TestMachineManifestRejections",
    ),
    (
        "machine endpoint exposure",
        "internal/project/manifest_machine.go",
        "\t\tif count != 1 {",
        "\t\tif count > 1 {",
        "./internal/project#TestMachineManifestRejections",
    ),
    (
        "machine lookup text comparison",
        "internal/project/manifest_system.go",
        'return "::text"',
        'return ""',
        "./internal/project#TestMachineLookupMigrationShape",
    ),
    (
        "machine nonce replay protection",
        "internal/project/templates/candidate_machine.go.tmpl",
        "ON CONFLICT DO NOTHING",
        "ON CONFLICT (principal, machine_id, nonce) DO UPDATE SET "
        "nonce = EXCLUDED.nonce",
        "./internal/project#TestMachineManagedRender",
    ),
]

mutations.extend(
    [
        (
            "auth events fail closed by default",
            "internal/project/manifest_authevents.go",
            "return e.OnError != AuthEventsBestEffort",
            "return e.OnError == AuthEventsClosed",
            "./internal/project#TestAuthEventsValidatesAndDefaultsClosed",
        ),
        (
            "auth events operation unreachable from endpoints",
            "internal/project/manifest_authevents.go",
            "if endpoint.Operation == operation.ID() {",
            "if endpoint.Operation == operation.ID() && false {",
            "./internal/project#TestAuthEventsRejections",
        ),
        (
            "auth events every role holds the permission",
            "internal/project/manifest_authevents.go",
            "if !holds(role.ID, map[string]bool{}) {",
            "if false {",
            "./internal/project#TestAuthEventsRejections",
        ),
        (
            "auth events member index subject setting",
            "internal/project/manifest_authevents.go",
            'return "app.auth_event_subject"\n}',
            'return "app.tenant_id"\n}',
            "./internal/project#TestAuthEventsMemberIndexSQL",
        ),
        (
            "auth events member index active predicate",
            "internal/project/candidate_table_migrations.go",
            'return " WHERE " + column + " IS TRUE"',
            'return ""',
            "./internal/project#TestAuthEventsMemberIndexSQL",
        ),
        (
            "auth events tenants bound to the verified subject",
            "internal/project/candidate_memberships_app.go",
            "\"SELECT set_config('{{SETTING}}', $1, true)\",",
            "\"SELECT set_config('{{SETTING}}x', $1, true)\",",
            "./internal/project#TestAuthEventsManagedOutput",
        ),
        (
            "auth events closed policy revokes the session",
            "internal/project/templates/candidate_auth_events.go.tmpl",
            "if authEventsClosed {\n\t\t\t\t_ = e.manager.Revoke(ctx, token)",
            "if authEventsClosed {\n\t\t\t\t_ = token",
            "./internal/project#TestAuthEventsManagedOutput",
        ),
        (
            "auth events member index is read only for the runtime role",
            "internal/project/candidate_runtime_role.go",
            'privileges = "SELECT"\n',
            'privileges = "SELECT, INSERT, UPDATE, DELETE"\n',
            "./internal/project#TestRuntimeRoleGrantsMemberIndexReadOnly",
        ),
    ]
)

mutations.extend(
    [
        (
            "protected columns rejected on update",
            "internal/sqlcheck/query.go",
            'named := stmt.Kind == "update" && setsColumn(stmt.Set, column)',
            'named := false && setsColumn(stmt.Set, column)',
            "./internal/sqlcheck#TestAnalyzeQueryProtectedColumns",
        ),
        (
            "protected columns rejected on insert",
            "internal/sqlcheck/query.go",
            "named = named || tableKey(inserted) == tableKey(column)",
            "named = named || false",
            "./internal/sqlcheck#TestAnalyzeQueryProtectedColumns",
        ),
        (
            "machine credential columns protected",
            "internal/project/manifest_machine.go",
            "columns = append(columns, principal.KeyColumn, principal.StateColumn)",
            "columns = append(columns, principal.KeyColumn)",
            "./internal/project#TestMachineProtectedColumnsReachSQLCheck",
        ),
        (
            "authored write endpoint needs an input model",
            "internal/project/candidate_seam_models.go",
            'if out.Input == "" || o.Response != ResponseObject || o.Collection {',
            'if false {',
            "./internal/project#TestAuthoredWriteEndpointRejections",
        ),
        (
            "authored write claims idempotency after the gate",
            "internal/project/templates/candidate_seam_write_handler.go.tmpl",
            "reg.Idempotency.Serve(",
            "reg.Idempotency.Do(",
            "./internal/project#TestAuthoredWriteEndpointIdempotent",
        ),
        (
            "authored write body strictly decoded",
            "internal/project/templates/candidate_seam_write_handler.go.tmpl",
            "transport.Decode(w, r, &input, bodyLimit)",
            "json.NewDecoder(r.Body).Decode(&input)",
            "./internal/project#TestAuthoredWriteEndpointExempt",
        ),
        (
            "unexposed authored write cannot claim idempotency",
            "internal/project/candidate_authored.go",
            "!(operation.isAuthoredWriteBody() && m.hasEndpoint(operation)) {",
            "false {",
            "./internal/project#TestInternalAuthoredWriteCannotRequireIdempotency",
        ),
        (
            "machine stream rechecks the machine",
            "internal/project/templates/candidate_machine_stream_workflow.go.tmpl",
            "Verifier.Recheck(ctx, verified)",
            "Verifier.Check(ctx, verified)",
            "./internal/project#TestMachinePageAndStreamAreGenerated",
        ),
    ]
)

if os.environ.get("PLUTO_CLI_CANDIDATE_PROFILE") == "1":
    mutations.extend(
        [
            (
                "candidate authoritative transition guard",
                "internal/project/templates/candidate_data.go.tmpl",
                (
                    "return domain.Authorize{{ .Resource.Name }}Actor(\n"
                    "\t\t\t\t\toperation.event,\n"
                    "\t\t\t\t\trequest.SubjectID,\n"
                    "\t\t\t\t\tactualOwner)"
                ),
                "return nil",
                "TestCandidateGeneratedConsumer",
            ),
            (
                "candidate shared credential attempt window",
                "internal/project/templates/candidate_limiter.go.tmpl",
                "if count > l.attempts {",
                "if count > l.attempts+1 {",
                "TestCandidateLocalCredentialConsumer",
            ),
            (
                "candidate downstream stable delivery key",
                "internal/project/templates/candidate_effect.go.tmpl",
                'request.Header.Set("Idempotency-Key", command.Key())',
                'request.Header.Set("X-Idempotency-Key", command.Key())',
                "TestCandidateGeneratedConsumer",
            ),
            (
                "tx write foreign logic imports",
                "internal/project/candidate_authored_rules.go",
                "if ok && r.foreignPorts[role.group][owner] {",
                "if ok {",
                "TestCandidateAuthoredSeamEndToEnd",
            ),
            (
                "authored page cursor binding",
                "internal/project/templates/candidate_authored_workflow.go.tmpl",
                "execution.SubjectID(),\n\t\t\texecution.TenantID()))",
                '""))',
                "TestCandidateAuthoredSeamEndToEnd",
            ),
            (
                "scope queries constructor ownership",
                "internal/project/candidate_authored_rules.go",
                'rules.addCapability(scopeNamespace, "Queries", queryOwners...)',
                "_ = queryOwners",
                "TestCandidateAuthoredSeamEndToEnd",
            ),
            (
                "scope transaction always rolls back",
                "internal/project/templates/candidate_scope.go.tmpl",
                "defer func() {\n\t\t_ = tx.Rollback(context.WithoutCancel(ctx))\n\t}()",
                "defer func() {\n\t}()",
                "./internal/project#TestScopeAlwaysRollsBack",
            ),
            (
                "provisioning mints one tenant",
                "internal/project/templates/candidate_scope.go.tmpl",
                "if state.used {",
                "if false {",
                "TestCandidateSystemSeamEndToEnd",
            ),
            (
                "lookup binds its policy setting",
                "internal/project/candidate_system.go",
                'data.LookupBind = "SELECT set_config(\'" + lookup.Setting() +',
                'data.LookupBind = "SELECT set_config(\'app.unbound" + lookup.Setting() +',
                "TestCandidateSystemSeamEndToEnd",
            ),
            (
                "tenant index enumeration switch",
                "internal/project/templates/candidate_tenant_index.go.tmpl",
                "set_config('app.tenant_index', 'on', true)",
                "set_config('app.tenant_index_off', 'on', true)",
                "TestCandidateTenantJobsEndToEnd",
            ),
            (
                "tenant job service role resolver",
                "internal/project/candidate_tenantjob_runtime.go",
                "return []rbac.RoleID{rbac.RoleID(%[2]s)}, nil",
                "return []rbac.RoleID{}, nil",
                "TestCandidateTenantJobsEndToEnd",
            ),
            (
                "machine request budget",
                "internal/project/templates/candidate_machine.go.tmpl",
                "if recent >= cfg.limit {",
                "if false {",
                "TestCandidateGatewayEndToEnd",
            ),
            (
                "machine revoke only pending or active",
                "internal/project/candidate_machine.go",
                '"::text = $2 AND " + state + "::text IN ($3, $4)",\n\t\t\tReset:',
                '"::text = $2",\n\t\t\tReset:',
                "TestCandidateGatewayEndToEnd",
            ),
            (
                "failed enrollment audit withdraws the machine",
                "internal/project/templates/candidate_enroll_gate.go.tmpl",
                "context.WithoutCancel(ctx),\n\t\t\tverified)",
                "context.WithoutCancel(ctx),\n\t\t\tmachineauth.Verified{})",
                "TestCandidateGatewayEndToEnd",
            ),
            (
                "machine stream closes on revocation",
                "internal/project/templates/candidate_machine_stream_workflow.go.tmpl",
                "return operation.{{lowerFirst .Action}}Verifier.Recheck(ctx, verified)",
                "return nil",
                "TestCandidateGatewayEndToEnd",
            ),
            (
                "machine page cursor binds the machine",
                "internal/project/templates/candidate_machine_workflow.go.tmpl",
                "verified.MachineID(),\n\t\t\tverified.TenantID()))",
                '"",\n\t\t\tverified.TenantID()))',
                "TestCandidateGatewayEndToEnd",
            ),
            (
                "authored tenant transaction binding",
                "internal/project/templates/candidate_scope.go.tmpl",
                "\"SELECT set_config('app.tenant_id', $1, true)\",",
                "\"SELECT set_config('app.tenant_id_unbound', $1, true)\",",
                "TestCandidateAuthoredSeamEndToEnd",
            ),
        ]
    )

required_assertions = {
    "authored page cursor binding": "status=200 want=400",
    "candidate private authority fields": (
        "managed sibling altered opaque caller field"
    ),
    "candidate closed production ownership": "unowned initializer accepted",
    "candidate credential seam import restriction": (
        "restricted seam import accepted"
    ),
    "candidate shared credential attempt window": (
        "shared attempt window not enforced across replicas"
    ),
    "candidate authoritative transition guard": "FORGED_ACTOR_FACTS_COMMITTED",
    "candidate downstream stable delivery key": (
        "TYPED_DELIVERY_SINK_REJECTED_PAYLOAD"
    ),
    "candidate compiler directive integrity": (
        "expected precise semantic integrity rejection"
    ),
}

results = []
for name, relative, old, new, test in mutations:
    with tempfile.TemporaryDirectory(prefix="pluto-mutation-") as temporary:
        checkout = Path(temporary).resolve() / "cli"
        shutil.copytree(
            root,
            checkout,
            ignore=shutil.ignore_patterns(
                ".git", "bin", "pluto", "__pycache__"
            ),
        )
        source = checkout / relative
        text = source.read_text()
        if text.count(old) != 1:
            raise SystemExit(f"Mutation target drifted: {name}")
        source.write_text(text.replace(old, new, 1))
        try:
            package = "./internal/cli"
            if "#" in test:
                package, test = test.split("#", 1)
            run = subprocess.run(
                ["go", "test", package, "-run", f"^{test}$", "-count=1"],
                cwd=checkout,
                capture_output=True,
                text=True,
                timeout=120,
                env={**os.environ, "GOWORK": "off"},
            )
            output = run.stdout + run.stderr
            if "[build failed]" in output or "syntax error" in output:
                result = "invalid"
            elif run.returncode and "--- FAIL:" in output:
                expected = required_assertions.get(name)
                result = (
                    "killed"
                    if expected is None or expected in output
                    else "invalid"
                )
            elif run.returncode:
                result = "invalid"
            else:
                result = "survived"
        except subprocess.TimeoutExpired:
            result = "timed-out"
        results.append(result)
        print(f"{name}: {result}", flush=True)
        if result != "killed":
            print(output if "output" in locals() else "timeout", flush=True)

print(
    "; ".join(
        f"{kind}={results.count(kind)}"
        for kind in ["killed", "survived", "invalid", "timed-out"]
    )
)
raise SystemExit(0 if all(result == "killed" for result in results) else 1)
