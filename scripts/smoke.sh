#!/bin/sh
# End-to-end smoke test against the real binary: first-run setup, sign-in
# state, a place, an item, a photo upload, the thumbnail. Uses a temporary
# data directory and a free port, and stops the server it starts.
#
#   scripts/smoke.sh [path-to-binary]     (default: build bin/inventory-smoke)
set -eu

cd "$(dirname "$0")/.."
tmp=$(mktemp -d)
pid=
cleanup() {
	[ -n "$pid" ] && kill "$pid" 2>/dev/null && wait "$pid" 2>/dev/null || true
	rm -rf "$tmp"
}
trap cleanup EXIT INT TERM

fail() { echo "smoke: FAIL: $*" >&2; [ -f "$tmp/log" ] && tail -n 20 "$tmp/log" >&2; exit 1; }

bin=${1:-}
if [ -z "$bin" ]; then
	bin=$tmp/inventory
	CGO_ENABLED=0 go build -o "$bin" ./cmd/inventory || fail "build"
fi

# A free port in 20000-39999 (never 8080 or 8090): pick one nothing answers on.
port=
for try in 1 2 3 4 5 6 7 8; do
	p=$(awk 'BEGIN { srand(); print 20000 + int(rand() * 20000) }')
	curl -s -o /dev/null "http://127.0.0.1:$p/" 2>/dev/null || { port=$p; break; }
done
[ -n "$port" ] || fail "no free port"
base=http://127.0.0.1:$port/api/v1

INVENTORY_ADDR=127.0.0.1:$port INVENTORY_DATA=$tmp/data "$bin" >"$tmp/log" 2>&1 &
pid=$!

i=0
until curl -fs "http://127.0.0.1:$port/api/health" >/dev/null 2>&1; do
	i=$((i + 1)); [ $i -le 50 ] || fail "server did not start"
	kill -0 "$pid" 2>/dev/null || fail "server exited"
	sleep 0.2
done
INVENTORY_ADDR=127.0.0.1:$port "$bin" healthcheck || fail "healthcheck subcommand"

# json KEY: the first string value of "KEY" in stdin.
json() { grep -o "\"$1\":\"[^\"]*\"" | head -n 1 | cut -d'"' -f4; }
# call METHOD PATH [JSON]: print the body, fail on a non-2xx status.
call() {
	if [ $# -ge 3 ]; then
		curl -fsS -b "$tmp/jar" -c "$tmp/jar" -X "$1" -H 'Content-Type: application/json' -d "$3" "$base$2"
	else
		curl -fsS -b "$tmp/jar" -c "$tmp/jar" -X "$1" "$base$2"
	fi
}

out=$(call POST /setup '{"name":"Smoke","email":"smoke@example.com","password":"correct horse battery","household_name":"Smoke"}') || fail "setup"
me=$(call GET /me) || fail "me"
echo "$me" | grep -q '"role":"owner"' || fail "/me is not an owner: $me"

home=$(call GET /homes | json id)
[ -n "$home" ] || fail "no home after setup"
place=$(call POST /places "{\"home_id\":\"$home\",\"kind\":\"room\",\"name\":\"Smoke room\"}" | json id) || fail "create place"
[ -n "$place" ] || fail "place has no id"

item=$(call POST /items "{\"name\":\"Smoke item\",\"place_id\":\"$place\"}" | json id) || fail "create item"
[ -n "$item" ] || fail "item has no id"

# The new item is findable at once, and its history shows the create.
call GET "/search?q=Smoke%20item" | grep -q "\"id\":\"$item\"" || fail "item not found by search"
call GET "/items/$item/history" | grep -q '"created"' || fail "history has no create event"
call GET /me/recent-places | grep -q "\"id\":\"$place\"" || fail "recent-places misses the place"

printf '%s' '/9j/2wCEAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDIBCQkJDAsMGA0NGDIhHCEyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMv/AABEIABAAEAMBIgACEQEDEQH/xAGiAAABBQEBAQEBAQAAAAAAAAAAAQIDBAUGBwgJCgsQAAIBAwMCBAMFBQQEAAABfQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkqNDU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq8fLz9PX29/j5+gEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoLEQACAQIEBAMEBwUEBAABAncAAQIDEQQFITEGEkFRB2FxEyIygQgUQpGhscEJIzNS8BVictEKFiQ04SXxFxgZGiYnKCkqNTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqCg4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2dri4+Tl5ufo6ery8/T19vf4+fr/2gAMAwEAAhEDEQA/APP7HQunyV01hoXT5K6Ww0Lp8ldPY6F0+StMXmvmHD+d7an/2Q==' | base64 -d >"$tmp/p.jpg" 2>/dev/null || printf '%s' '/9j/2wCEAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDIBCQkJDAsMGA0NGDIhHCEyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMv/AABEIABAAEAMBIgACEQEDEQH/xAGiAAABBQEBAQEBAQAAAAAAAAAAAQIDBAUGBwgJCgsQAAIBAwMCBAMFBQQEAAABfQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkqNDU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq8fLz9PX29/j5+gEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoLEQACAQIEBAMEBwUEBAABAncAAQIDEQQFITEGEkFRB2FxEyIygQgUQpGhscEJIzNS8BVictEKFiQ04SXxFxgZGiYnKCkqNTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqCg4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2dri4+Tl5ufo6ery8/T19vf4+fr/2gAMAwEAAhEDEQA/APP7HQunyV01hoXT5K6Ww0Lp8ldPY6F0+StMXmvmHD+d7an/2Q==' | base64 -D >"$tmp/p.jpg"
up=$(curl -fsS -b "$tmp/jar" -F file=@"$tmp/p.jpg;type=image/jpeg" -F role=item -F item_id="$item" "$base/photos") || fail "upload photo"
photo=$(echo "$up" | json photo_id)
[ -n "$photo" ] || fail "upload returned no photo id: $up"

# The thumbnail is made by the job queue; the original is served until it is ready.
i=0
while :; do
	ct=$(curl -fsS -b "$tmp/jar" -o "$tmp/thumb" -w '%{content_type}' "$base/photos/$photo/thumb") || fail "fetch thumbnail"
	[ "$ct" = image/jpeg ] || fail "thumbnail content type $ct"
	[ -s "$tmp/thumb" ] || fail "empty thumbnail"
	curl -fsS -b "$tmp/jar" "$base/items/$item" | grep -q "\"photo_id\":\"$photo\"" && break
	i=$((i + 1)); [ $i -le 10 ] || fail "item does not list the photo"
	sleep 0.2
done

echo "smoke: ok (item $item, photo $photo)"
