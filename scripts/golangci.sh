#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
if command -v golangci-lint >/dev/null 2>&1; then
  if [ "$(golangci-lint version --short)" = 2.15.0 ]; then
    exec golangci-lint "$@"
  fi
fi
if command -v mise >/dev/null 2>&1; then
  exec mise exec golangci-lint@2.15.0 -- golangci-lint "$@"
fi
echo 'Install golangci-lint 2.15.0 from official releases, or mise install.' >&2
exit 1
