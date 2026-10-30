# Generate a protected endpoint and make it return data

This example uses the real CLI to create a temporary app, implements one authored
operation, and calls its generated HTTP route through a real loopback server.
It demonstrates 401, 403 and 200 without adding a public login shortcut.

## 1. Run the complete walkthrough

Requires Go 1.25.5+, Python 3 and golangci-lint 2.15.0 on PATH:

```sh
git clone https://github.com/plutoframework/cli.git
cd cli
mise exec golangci-lint@2.15.0 -- python3 scripts/first-endpoint.py
```

Review the repository's tool configuration before trusting it with mise. You can
instead put the pinned tools on PATH and run the Python command directly.

The script cleans up its temporary project and loopback server. It downloads only
published Go dependencies and makes no paid or external business API calls.

## 2. Inspect the generated contract

The script dispatches the equivalent commands:

```sh
pluto --no-input init app --module example.com/pluto-first-endpoint \
  --name first-endpoint --no-docker --no-mise --no-fnox
cd app
pluto --no-input add endpoint greeting Read --method GET \
  --path /api/greeting --name 'Read your greeting' \
  --description 'Greet the verified caller.' \
  --permission greeting:read --feature greeting-read
```

The CLI owns route metadata, registration and OpenAPI. The operation starts as
not implemented. A declared permission grants no role access and the feature
starts off; neither is bypassed by the tutorial.

## 3. Implement the authored operation

[`operation.go.txt`](examples/first-endpoint/operation.go.txt) is the complete
replacement for `internal/operations/greeting/read.go`. It checks verified
identity and the injected policy, then returns a typed `ReadData` response.
The accompanying test replaces the incomplete assertion with a successful
result while retaining anonymous, missing-grant and unknown-role denials.

## 4. Supply trusted test dependencies

[`main.go.txt`](examples/first-endpoint/main.go.txt) is a standalone tutorial
harness copied to `cmd/tutorial/main.go`. It compiles explicit role grants,
constructs published session authentication, and injects a resolver for two
synthetic verified accounts. Only `reader` receives `greeting:read`. The feature
is explicitly enabled in this harness.

Session issuance happens inside trusted test setup, never an HTTP endpoint. The
generated default membership resolver and role catalog remain deny-by-default.
Use the credential service and persistent membership checks for an actual app;
this memory-backed harness is not a production authentication implementation.

## 5. Validate the real request path

The script runs `pluto fmt`, `pluto vet`, operation tests and OpenAPI freshness,
then starts the generated app and makes real HTTP requests. Expected output:

```text
anonymous: HTTP 401
authenticated without grant: HTTP 403
authorized: HTTP 200
Hello, reader
```

The greeting comes from the server-verified principal, not a request body, tenant
header or client-provided role. For tenant-backed resources, the operation must
also apply verified tenant/resource predicates in its repository.
