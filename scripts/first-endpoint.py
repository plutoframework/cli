"""Generate and exercise a protected business endpoint in a disposable consumer."""
from pathlib import Path
import argparse
import os
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument('--refresh', action='store_true', help='Refresh canonical Go formatting of tutorial source files')
args = parser.parse_args()
with tempfile.TemporaryDirectory(prefix='pluto-first-endpoint-') as directory:
    root = Path(directory)
    env = {**os.environ, 'GOWORK': 'off'}
    binary = root / 'pluto'
    subprocess.run(['go', 'build', '-o', str(binary), './cmd/pluto'], cwd=ROOT, env=env, check=True)
    def run(*args, cwd=root, capture=False):
        return subprocess.run(args, cwd=cwd, env=env, check=True, text=True,
                              stdout=subprocess.PIPE if capture else None)
    run(str(binary), '--no-input', 'init', 'app', '--module', 'example.com/pluto-first-endpoint',
        '--name', 'first-endpoint', '--no-docker', '--no-mise', '--no-fnox')
    app = root / 'app'
    run(str(binary), '--no-input', 'add', 'endpoint', 'greeting', 'Read', '--method', 'GET',
        '--path', '/api/greeting', '--name', 'Read your greeting', '--description', 'Greet the verified caller.',
        '--permission', 'greeting:read', '--feature', 'greeting-read', cwd=app)
    source = ROOT / 'docs/examples/first-endpoint'
    files = [
        ('operation.go.txt', 'internal/operations/greeting/read.go'),
        ('operation_test.go.txt', 'internal/operations/greeting/read_test.go'),
        ('main.go.txt', 'cmd/tutorial/main.go'),
    ]
    for filename, target in files:
        path = app / target
        path.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source / filename, path)
    run(str(binary), 'fmt', cwd=app)
    if args.refresh:
        for filename, target in files:
            shutil.copy2(app / target, source / filename)
    run(str(binary), 'vet', cwd=app)
    result = run('go', 'run', './cmd/tutorial', cwd=app, capture=True)
    expected = 'anonymous: HTTP 401\nauthenticated without grant: HTTP 403\nauthorized: HTTP 200\nHello, reader\n'
    assert result.stdout == expected, result.stdout
    print(result.stdout, end='')
    print('PASS: protected generated route, published auth, explicit membership, operation and RBAC')
