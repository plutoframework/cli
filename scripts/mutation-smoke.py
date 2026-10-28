#!/usr/bin/env python3
"""Bounded seeded security mutations in disposable copies, not a mutation engine."""
from pathlib import Path
import os
import shutil
import subprocess
import tempfile

root = Path(__file__).resolve().parent.parent
mutations = [
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
        "internal/project/templates/operation.go.tmpl",
        "if !principal.Can(operation.policy, access.{{.Symbol}}Permission) {",
        "if false && !principal.Can(operation.policy, access.{{.Symbol}}Permission) {",
        "TestGeneratedApplications/single",
    ),
]

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
            run = subprocess.run(
                ["go", "test", "./internal/cli", "-run", f"^{test}$", "-count=1"],
                cwd=checkout, capture_output=True, text=True, timeout=120,
                env={**os.environ, "GOWORK": "off"},
            )
            output = run.stdout + run.stderr
            if "[build failed]" in output or "syntax error" in output:
                result = "invalid"
            elif run.returncode and "--- FAIL:" in output:
                result = "killed"
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
