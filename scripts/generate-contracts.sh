#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"

export PATH="$PATH:$HOME/go/bin"
rm -rf gen/go api/openapi web/src/generated
mkdir -p api/openapi

buf lint
buf generate
wire ./cmd/game-server

(cd web && npm run generate:api)
