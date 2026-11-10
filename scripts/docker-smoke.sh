#!/bin/sh
# Smoke test of the Docker image: empty volume, health, first-run setup,
# an item, restart (data persists), non-root, healthcheck status.
#
#   scripts/docker-smoke.sh [image]     (default: build inventory:smoke)
set -eu

cd "$(dirname "$0")/.."
img=${1:-}
if [ -z "$img" ]; then
	img=inventory:smoke
	docker build -t "$img" . || { echo "docker-smoke: FAIL: build" >&2; exit 1; }
fi

name=inventory-smoke-$$
vol=$name-data
tmp=$(mktemp -d)
cleanup() {
	docker rm -f "$name" >/dev/null 2>&1 || true
	docker volume rm "$vol" >/dev/null 2>&1 || true
	rm -rf "$tmp"
}
trap cleanup EXIT INT TERM
fail() { echo "docker-smoke: FAIL: $*" >&2; docker logs --tail 20 "$name" >&2 2>&1 || true; exit 1; }

port=$(awk 'BEGIN { srand(); print 20000 + int(rand() * 20000) }')
base=http://127.0.0.1:$port
start() {
	docker run -d --name "$name" -p "127.0.0.1:$port:8080" -v "$vol:/data" "$img" >/dev/null || fail "run"
	i=0
	until curl -fs "$base/api/health" >/dev/null 2>&1; do
		i=$((i + 1)); [ $i -le 100 ] || fail "no health answer"
		[ "$(docker inspect -f '{{.State.Running}}' "$name")" = true ] || fail "container exited"
		sleep 0.2
	done
}

start
curl -fs "$base/" | grep -qi '<html' || fail "no page at /"
curl -fs "$base/api/v1/setup" >/dev/null 2>&1 || true
uid=$(docker inspect -f '{{.Config.User}}' "$img")
case "$uid" in nonroot*|65532*) ;; *) fail "image user is '$uid', not non-root" ;; esac

jar=$tmp/jar
call() { curl -fsS -b "$jar" -c "$jar" -X "$1" -H 'Content-Type: application/json' ${3:+-d "$3"} "$base/api/v1$2"; }
json() { grep -o "\"$1\":\"[^\"]*\"" | head -n 1 | cut -d'"' -f4; }
call POST /setup '{"name":"Smoke","email":"smoke@example.com","password":"correct horse battery","household_name":"Smoke"}' >/dev/null || fail "setup"
home=$(call GET /homes | json id)
place=$(call POST /places "{\"home_id\":\"$home\",\"kind\":\"room\",\"name\":\"Smoke room\"}" | json id)
item=$(call POST /items "{\"name\":\"Persistent thing\",\"place_id\":\"$place\"}" | json id)
[ -n "$item" ] || fail "no item"

docker restart "$name" >/dev/null || fail "restart"
i=0
until curl -fs "$base/api/health" >/dev/null 2>&1; do
	i=$((i + 1)); [ $i -le 100 ] || fail "no health after restart"
	sleep 0.2
done
# Sessions live in the database, so the old cookie still works.
call GET "/search?q=Persistent%20thing" | grep -q "\"id\":\"$item\"" || fail "item lost after restart"

# The container's own healthcheck (binary probes /api/health) turns healthy.
i=0
until [ "$(docker inspect -f '{{.State.Health.Status}}' "$name")" = healthy ]; do
	i=$((i + 1)); [ $i -le 40 ] || fail "container never became healthy"
	sleep 1
done
echo "docker-smoke: ok ($img, user $uid, item $item survived restart)"
