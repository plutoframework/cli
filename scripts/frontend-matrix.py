#!/usr/bin/env python3
"""Build fresh, disposable frontend consumers through the actual Cobra CLI."""
import argparse
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

parser = argparse.ArgumentParser()
parser.add_argument("--keep", action="store_true", help="Keep consumers/logs for browser QA")
parser.add_argument("--oidc", action="store_true", help="Exercise published OIDC sign-in in every auth profile")
args = parser.parse_args()
checkout = Path(__file__).resolve().parent.parent
binary = checkout / "bin/pluto"
root = Path(tempfile.mkdtemp(prefix="pluto-frontend-matrix-")).resolve()
environment = os.environ.copy()
version = subprocess.check_output(["node", "--version"], text=True).strip()
if not version.startswith("v24."):
    node = subprocess.check_output(["mise", "where", "node@24.22.1"], cwd=checkout, text=True).strip()
    environment["PATH"] = str(Path(node) / "bin") + os.pathsep + environment["PATH"]


def run(directory, *command):
    with (root / "commands.log").open("a") as log:
        log.write(f"\n{directory.name}: {' '.join(command)}\n")
        log.flush()
        result = subprocess.run(command, cwd=directory, env=environment,
                                stdout=log, stderr=subprocess.STDOUT, timeout=240)
    if result.returncode:
        print((root / "commands.log").read_text()[-6000:], flush=True)
        raise RuntimeError(f"Command failed: {command}")


try:
    run(checkout, "go", "build", "-o", str(binary), "./cmd/pluto")
    for profile in ("native", "heroui-v3", "shadcn", "native-bare"):
        print(f"Checking fresh consumer: {profile}", flush=True)
        app = root / profile
        local = [] if profile == "native-bare" else ["--local-credentials"]
        if args.oidc and local:
            local.append("--oidc")
        run(checkout, str(binary), "init", str(app), "--name", "orbital",
            "--no-mise", "--no-fnox", "--no-docker", "--no-input", *local)
        ui = "native" if profile == "native-bare" else profile
        auth = [] if profile in ("heroui-v3", "native-bare") else ["--auth"]
        run(app, str(binary), "frontend", "init", "--ui", ui,
            "--no-install", "--no-input", *auth)
        if profile == "heroui-v3":
            run(app, str(binary), "frontend", "add", "auth", "--no-install", "--no-input")
        if profile != "native-bare":
            for kind in ("button", "input", "card", "form"):
                run(app, str(binary), "frontend", "add", "component", f"sample-{kind}",
                    "--kind", kind, "--no-install", "--no-input")
        if profile == "heroui-v3":
            shutil.copytree(checkout / "examples/component-registry", app / ".pluto/components")
            with (app / "pluto.toml").open("a") as manifest:
                manifest.write('\n[frontend.registries.company]\npath=".pluto/components"\n')
            run(app, str(binary), "frontend", "components", "list", "--no-input")
            run(app, str(binary), "frontend", "add", "component", "approval-review",
                "--from", "company/approval-panel", "--dry-run", "--no-input")
            run(app, str(binary), "frontend", "add", "component", "approval-review",
                "--from", "company/approval-panel", "--no-install", "--no-input")
        run(app, str(binary), "frontend", "install", "--no-input")
        if profile != "native-bare":
            # Exercises dependency installation and checks after a later add.
            run(app, str(binary), "frontend", "add", "component", "sample-dialog",
                "--kind", "dialog", "--no-input")
        run(app, "go", "build", "./cmd/...", "./internal/...")
        print(f"Passed: {profile}", flush=True)
    print(f"Frontend consumers passed; artifacts: {root}", flush=True)
finally:
    if not args.keep:
        shutil.rmtree(root)
