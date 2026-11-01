#!/usr/bin/env python3
"""Bounded seeded security mutations in disposable copies, not a mutation engine."""
from pathlib import Path
import os
import shutil
import subprocess
import tempfile

root = Path(__file__).resolve().parent.parent
mutations = [
    ("candidate compiler directive integrity", "internal/project/candidate_integrity.go", 'matches = candidateSourceEquivalent(actual, files[path])', 'matches = Equivalent(actual, files[path])', './internal/project#TestCandidateIntegrityRejectsExcludedGateAndSQLChanges'),
    ("candidate constructor ownership", "internal/project/candidate_boundary_source.go", 'rules.capabilities[file.namespace+"."+value.Name]; restricted &&', 'rules.capabilities[file.namespace+"."+value.Name]; restricted && false &&', './internal/project#TestCandidateBoundaryCapabilitiesAndForgery'),
    ("candidate private authority fields", "internal/project/candidate_boundary.go", 'owners != nil &&', 'owners != nil && false &&', './internal/project#TestCandidateOwnershipBranches'),
    ("candidate unsafe confinement", "internal/project/candidate_boundary.go", 'if path == "unsafe" {', 'if path == "disabled/unsafe" {', './internal/project#TestCandidateBoundaryCapabilitiesAndForgery'),
    ("candidate closed production ownership", "internal/project/candidate_boundary.go", 'if rules.managed[file.path] {', 'if true {', './internal/project#TestCandidateOwnershipBranches'),
    ("domain normalization placement", "internal/project/primitive_boundaries.go", 'if operationCode && dependency == "strings" {', 'if false && operationCode && dependency == "strings" {', './internal/project#TestPrimitivePlacementAndHelperBypasses'),
    ("cache boundary placement", "internal/project/primitive_boundaries.go", 'dependency == "github.com/plutoframework/pluto-core/cache"', 'dependency == "disabled/cache"', './internal/project#TestPrimitivePlacementAndHelperBypasses'),
    ("scoped pagination ownership", "internal/project/primitive_boundaries.go", 'capability == "Finish"', 'capability == "disabledFinish"', './internal/project#TestPrimitivePlacementAndHelperBypasses'),
    ("required replay contract", "internal/project/contracts.go", 'operation.Idempotency != "required"', 'false', './internal/project#TestContractsRejectMissingAndContradictoryObligations'),
    ("declared query visibility", "internal/project/contracts.go", 'operation.ResourcePolicy != resource.Visibility', 'false', './internal/project#TestContractsRejectMissingAndContradictoryObligations'),
    ("owned worker construction", "internal/project/entry_boundary.go", 'selector.Sel.Name == "NewWorker"', 'selector.Sel.Name == "disabledWorker"', 'TestCrossEntryOperationConsumer'),

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
        'if job.TenantID==nil || *job.TenantID=="" {return jobs.Permanent(jobs.ErrInvalid)}',
        'if false {return jobs.Permanent(jobs.ErrInvalid)}',
        "TestGeneratedJobs/multi",
    ),
    (
        "auth route namespace reservation",
        "internal/project/manifest.go",
        'strings.HasPrefix(e.Path, "/auth/")',
        "false",
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
        '// Anti-sniffing header removed by mutation.',
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
        "});err!=nil {",
        "});false {",
        "TestGeneratedApplications/single",
    ),
]

if os.environ.get("PLUTO_CLI_CANDIDATE_PROFILE") == "1":
    mutations.extend([
        ("candidate authoritative transition guard", "internal/project/templates/candidate_data.go.tmpl", 'return domain.Authorize{{ .Resource.Name }}Actor(operation.event, request.SubjectID, actualOwner)', 'return nil', 'TestCandidateGeneratedConsumer'),
        ("candidate downstream stable delivery key", "internal/project/templates/candidate_effect.go.tmpl", 'request.Header.Set("Idempotency-Key",command.Key())', 'request.Header.Set("X-Idempotency-Key",command.Key())', 'TestCandidateGeneratedConsumer'),
    ])

required_assertions = {
    "candidate private authority fields": "managed sibling altered opaque caller field",
    "candidate closed production ownership": "unowned initializer accepted",
    "candidate authoritative transition guard": "FORGED_ACTOR_FACTS_COMMITTED",
    "candidate downstream stable delivery key": "TYPED_DELIVERY_SINK_REJECTED_PAYLOAD",
    "candidate compiler directive integrity": "expected precise semantic integrity rejection",
}

results = []
for name, relative, old, new, test in mutations:
    with tempfile.TemporaryDirectory(prefix="pluto-mutation-") as temporary:
        checkout = Path(temporary).resolve() / "cli"
        shutil.copytree(
            root, checkout,
            ignore=shutil.ignore_patterns(".git", "bin", "pluto", "__pycache__"),
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
                cwd=checkout, capture_output=True, text=True, timeout=120,
                env={**os.environ, "GOWORK": "off"},
            )
            output = run.stdout + run.stderr
            if "[build failed]" in output or "syntax error" in output:
                result = "invalid"
            elif run.returncode and "--- FAIL:" in output:
                expected = required_assertions.get(name)
                result = "killed" if expected is None or expected in output else "invalid"
            elif run.returncode:
                result = "invalid"
            else:
                result = "survived"
        except subprocess.TimeoutExpired:
            result = "timed-out"
        results.append(result)
        print(f"{name}: {result}", flush=True)
        if result != "killed":
            print(output if 'output' in locals() else "timeout", flush=True)

print("; ".join(f"{kind}={results.count(kind)}" for kind in
                ["killed", "survived", "invalid", "timed-out"]))
raise SystemExit(0 if all(result == "killed" for result in results) else 1)
