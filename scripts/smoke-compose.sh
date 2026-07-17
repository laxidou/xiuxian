#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"

base_url=${BASE_URL:-http://localhost}
cookie_jar=$(mktemp "${TMPDIR:-/tmp}/xiuxian-smoke-cookies.XXXXXX")
response_file=$(mktemp "${TMPDIR:-/tmp}/xiuxian-smoke-response.XXXXXX")
cleanup() {
  rm -f "$cookie_jar" "$response_file"
}
trap cleanup EXIT INT TERM

require() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

require curl
require docker
require jq

if [ "${SKIP_BUILD:-0}" = "1" ]; then
  docker compose up -d
else
  docker compose up -d --build
fi
docker compose restart caddy >/dev/null

attempt=0
until curl -fsS "$base_url/api/v1/healthz" | jq -e '.status == "ok" and .service == "game-server"' >/dev/null; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 60 ]; then
    docker compose ps >&2
    docker compose logs --tail=100 game-server caddy >&2
    exit 1
  fi
  sleep 1
done

curl -fsS "$base_url/" | grep -q '<div id="root"></div>'

suffix=$(date +%s)-$$
account="smoke-$suffix"
role_name="验收-$suffix"
password="compose smoke password $suffix"

jq -n --arg account "$account" --arg password "$password" --arg role_name "$role_name" \
  '{account: $account, password: $password, role_name: $role_name}' |
  curl -fsS -c "$cookie_jar" -H 'Content-Type: application/json' --data-binary @- \
    "$base_url/api/v1/auth/register" >"$response_file"

role_id=$(jq -er --arg role_name "$role_name" 'select(.name == $role_name and .life_number == 1 and .status == "alive") | .id' "$response_file")
life_number=$(jq -er '.life_number' "$response_file")
state_version=$(jq -er '.state_version' "$response_file")

curl -fsS -b "$cookie_jar" "$base_url/api/v1/state" |
  jq -e --arg role_id "$role_id" '.id == $role_id and .life_number == 1' >/dev/null

jq -n '{}' |
  curl -fsS -b "$cookie_jar" -H 'Content-Type: application/json' --data-binary @- \
    "$base_url/xiuxian.v1.WorldService/GetState" |
  jq -e --arg role_id "$role_id" '.id == $role_id and .lifeNumber == "1"' >/dev/null

curl -fsS -b "$cookie_jar" -X POST "$base_url/api/v1/mcp-key/rotate" >"$response_file"
api_key=$(jq -er '.api_key | select(startswith("xiu_"))' "$response_file")

jq -n '{jsonrpc:"2.0", id:1, method:"tools/call", params:{name:"get_state", arguments:{}}}' |
  curl -fsS -H "Authorization: Bearer $api_key" -H 'Content-Type: application/json' --data-binary @- "$base_url/mcp" |
  jq -e --arg role_id "$role_id" '.result.isError == false and (.result.content[0].text | fromjson | .id == $role_id)' >/dev/null

curl -fsS -b "$cookie_jar" "$base_url/api/v1/state" >"$response_file"
life_number=$(jq -er '.life_number' "$response_file")
state_version=$(jq -er '.state_version' "$response_file")

move_key="smoke-move-$suffix"
jq -n '{x: 1, y: 0}' |
  curl -fsS -b "$cookie_jar" -H 'Content-Type: application/json' -H "Idempotency-Key: $move_key" \
    -H "X-Expected-Life-Number: $life_number" -H "X-Expected-State-Version: $state_version" --data-binary @- \
    "$base_url/api/v1/movement/move" >"$response_file"
first_version=$(jq -er '.state_version' "$response_file")
jq -n '{x: 1, y: 0}' |
  curl -fsS -b "$cookie_jar" -H 'Content-Type: application/json' -H "Idempotency-Key: $move_key" \
    -H "X-Expected-Life-Number: $life_number" -H "X-Expected-State-Version: $state_version" --data-binary @- \
    "$base_url/api/v1/movement/move" |
  jq -e --argjson first_version "$first_version" '.state_version == $first_version' >/dev/null

database_counts=$(docker compose exec -T postgres psql -U xiuxian -d xiuxian -Atc \
  "SELECT (SELECT count(*) FROM world_snapshots),(SELECT count(*) FROM accounts WHERE account_identifier='$account'),(SELECT count(*) FROM roles WHERE id='$role_id');")
[ "$database_counts" = "1|1|1" ]
[ "$(docker compose exec -T redis redis-cli ping)" = "PONG" ]

attempt=0
until [ "$(docker compose exec -T postgres psql -U xiuxian -d xiuxian -Atc 'SELECT count(*) FROM outbox WHERE completed_at IS NULL;')" = "0" ]; do
  attempt=$((attempt + 1))
  [ "$attempt" -lt 30 ] || exit 1
  sleep 1
done

docker compose exec -T redis redis-cli FLUSHDB >/dev/null
docker compose restart worker mcp-gateway >/dev/null

attempt=0
until [ "$(docker compose exec -T redis redis-cli zcard world:death_deadlines)" -gt 0 ]; do
  attempt=$((attempt + 1))
  [ "$attempt" -lt 30 ] || exit 1
  sleep 1
done

jq -n '{jsonrpc:"2.0", id:2, method:"tools/call", params:{name:"get_state", arguments:{}}}' |
  curl -fsS -H "Authorization: Bearer $api_key" -H 'Content-Type: application/json' --data-binary @- "$base_url/mcp" |
  jq -e --arg role_id "$role_id" '.result.isError == false and (.result.content[0].text | fromjson | .id == $role_id)' >/dev/null

echo "compose smoke passed for $role_id"
