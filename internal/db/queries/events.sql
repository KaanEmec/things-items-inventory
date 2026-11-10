-- History events (J3.6). Every query filters by household_id. Paging is by
-- (at, id) descending; an empty cursor_at means the first page.

-- name: InsertEvent :exec
INSERT INTO events (id, household_id, at, actor_kind, user_id, token_id, token_name, kind, item_id, place_id, data)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListEventsHousehold :many
SELECT * FROM events
WHERE household_id = sqlc.arg(household_id)
  AND (CAST(sqlc.arg(cursor_at) AS TEXT) = '' OR at < sqlc.arg(cursor_at) OR (at = sqlc.arg(cursor_at) AND id < sqlc.arg(cursor_id)))
ORDER BY at DESC, id DESC
LIMIT sqlc.arg(row_limit);

-- name: ListEventsItem :many
SELECT * FROM events
WHERE household_id = sqlc.arg(household_id) AND item_id = sqlc.arg(item_id)
  AND (CAST(sqlc.arg(cursor_at) AS TEXT) = '' OR at < sqlc.arg(cursor_at) OR (at = sqlc.arg(cursor_at) AND id < sqlc.arg(cursor_id)))
ORDER BY at DESC, id DESC
LIMIT sqlc.arg(row_limit);

-- A place's feed: events on the place, plus items that moved out of it
-- (their "before" place).
-- name: ListEventsPlace :many
SELECT * FROM events
WHERE household_id = sqlc.arg(household_id)
  AND (place_id = sqlc.arg(place_id) OR (item_id IS NOT NULL AND json_extract(data, '$.before.place_id') = sqlc.arg(place_id)))
  AND (CAST(sqlc.arg(cursor_at) AS TEXT) = '' OR at < sqlc.arg(cursor_at) OR (at = sqlc.arg(cursor_at) AND id < sqlc.arg(cursor_id)))
ORDER BY at DESC, id DESC
LIMIT sqlc.arg(row_limit);

-- name: GetUserNames :many
-- scope: global (users are not household rows; the caller passes only IDs found in this household's events)
SELECT id, name FROM users WHERE id IN (sqlc.slice(ids));

-- The places this person most recently put or moved items into (newest first).
-- name: ListRecentPlacesOf :many
SELECT e.place_id AS place_id, MAX(e.at) AS last_at
FROM events e JOIN places p ON p.id = e.place_id AND p.household_id = e.household_id AND p.deleted_at IS NULL
WHERE e.household_id = sqlc.arg(household_id) AND e.user_id = sqlc.arg(user_id) AND e.actor_kind = 'user'
  AND e.item_id IS NOT NULL AND e.kind IN ('created', 'moved')
GROUP BY e.place_id
ORDER BY last_at DESC, e.place_id DESC
LIMIT sqlc.arg(row_limit);
