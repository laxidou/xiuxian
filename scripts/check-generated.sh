#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"

hash_generated() {
  find gen/go api/openapi web/src/generated -type f -print0 |
    sort -z |
    xargs -0 shasum -a 256 |
    shasum -a 256
}

scripts/generate-contracts.sh
first=$(hash_generated)
scripts/generate-contracts.sh
second=$(hash_generated)

if [ "$first" != "$second" ]; then
  echo "generated contracts are not deterministic" >&2
  exit 1
fi

echo "generated contracts are deterministic: $second"
