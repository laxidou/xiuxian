#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"

base_url=${BASE_URL:-http://localhost}

restore_system_clock() {
  ALLOW_TEST_CLOCK=false TEST_CLOCK_START_UNIX_MILLIS=0 docker compose up -d --force-recreate game-server mcp-gateway
  docker compose restart caddy
  attempt=0
  until curl -fsS "$base_url/healthz" >/dev/null; do
    attempt=$((attempt + 1))
    [ "$attempt" -lt 60 ] || return 1
    sleep 1
  done
  [ "$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$base_url/test/clock/advance?milliseconds=1")" = "404" ]
}

on_exit() {
  status=$?
  trap - EXIT INT TERM
  if ! restore_system_clock; then
    echo "failed to restore system clock configuration" >&2
    exit 1
  fi
  exit "$status"
}
trap on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [ "${SKIP_BUILD:-0}" = "1" ]; then
  ALLOW_TEST_CLOCK=true TEST_CLOCK_START_UNIX_MILLIS=0 docker compose up -d --force-recreate game-server mcp-gateway
else
  ALLOW_TEST_CLOCK=true TEST_CLOCK_START_UNIX_MILLIS=0 docker compose up -d --build --force-recreate game-server mcp-gateway
fi
docker compose restart caddy >/dev/null

attempt=0
until curl -fsS -X POST "$base_url/test/clock/advance?milliseconds=0" >/dev/null; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 60 ]; then
    docker compose ps >&2
    docker compose logs --tail=100 game-server caddy >&2
    exit 1
  fi
  sleep 1
done

BASE_URL="$base_url" node scripts/smoke-web-browser.mjs
