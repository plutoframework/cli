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
        "lookup policy is select only",
        "internal/sqlcheck/migrations.go",
        'if policy.LookupOK && policy.Command == "select" && !policy.HasCheck {',
        "if policy.LookupOK && !policy.HasCheck {",
        "./internal/sqlcheck#TestCheckMigrationsLookupPolicy",
    ),
    (
        "lookup policy has no write check",
        "internal/sqlcheck/migrations.go",
        'if policy.LookupOK && policy.Command == "select" && !policy.HasCheck {',
        'if policy.LookupOK && policy.Command == "select" {',
        "./internal/sqlcheck#TestCheckMigrationsLookupPolicy",
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
        "table row-level security migration",
        "internal/project/candidate_table_migrations.go",
        "ALTER TABLE %[1]s FORCE ROW LEVEL SECURITY;\n",
        "",
        "./internal/project#TestTableMigrationForcesRowSecurityAndAppendOnly",
    ),
]

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
