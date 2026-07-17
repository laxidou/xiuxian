#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"

# Let the selected Go binary discover its matching toolchain. This avoids a
# stale shell-level GOROOT pointing at a different installed Go version.
unset GOROOT

unformatted=$(gofmt -l $(find cmd internal -name '*.go' -type f))
if [ -n "$unformatted" ]; then
  echo "Go files need formatting:" >&2
  echo "$unformatted" >&2
  exit 1
fi

go test ./...
go vet ./...

(cd web && npm run typecheck && npm run build)

scripts/check-generated.sh
docker compose config --quiet
scripts/smoke-compose.sh
scripts/smoke-web-browser.sh
