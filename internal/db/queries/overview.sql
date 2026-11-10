-- Overview screen (J3.7). Every query filters by household_id. Dates are
-- passed in (never read from the clock here) so tests can fix "today".

-- name: OverviewCounts :one
SELECT
  CAST(COUNT(*) AS INTEGER) AS total,
  CAST(COALESCE(SUM(CASE WHEN draft = 1 THEN 1 ELSE 0 END), 0) AS INTEGER) AS drafts,
  CAST(COALESCE(SUM(CASE WHEN status = 'packed' THEN 1 ELSE 0 END), 0) AS INTEGER) AS packed,
  CAST(COALESCE(SUM(CASE WHEN status = 'to_sell' THEN 1 ELSE 0 END), 0) AS INTEGER) AS to_sell,
  CAST(COALESCE(SUM(CASE WHEN warranty_end >= sqlc.arg(today) THEN 1 ELSE 0 END), 0) AS INTEGER) AS in_warranty,
  CAST(COALESCE(SUM(CASE WHEN warranty_end >= sqlc.arg(today) AND warranty_end <= sqlc.arg(ending_to) THEN 1 ELSE 0 END), 0) AS INTEGER) AS warranty_ending
FROM items
WHERE household_id = sqlc.arg(household_id) AND deleted_at IS NULL;

-- name: OverviewPlaceCount :one
SELECT CAST(COUNT(*) AS INTEGER) FROM places
WHERE household_id = ? AND deleted_at IS NULL AND kind <> 'box';

-- name: OverviewDrafts :many
SELECT i.id, i.name, i.category, pl.path AS place_path, i.updated_at AS at
FROM items i
LEFT JOIN places pl ON pl.id = i.place_id AND pl.household_id = i.household_id
WHERE i.household_id = ? AND i.deleted_at IS NULL AND i.draft = 1
ORDER BY i.updated_at DESC, i.id DESC
LIMIT ?;

-- name: OverviewWarrantyEnding :many
SELECT i.id, i.name, i.category, pl.path AS place_path, i.warranty_end AS at
FROM items i
LEFT JOIN places pl ON pl.id = i.place_id AND pl.household_id = i.household_id
WHERE i.household_id = sqlc.arg(household_id) AND i.deleted_at IS NULL
  AND i.warranty_end >= sqlc.arg(today) AND i.warranty_end <= sqlc.arg(ending_to)
ORDER BY i.warranty_end, i.id
LIMIT sqlc.arg(row_limit);

-- name: OverviewUnseenCount :one
SELECT CAST(COUNT(*) AS INTEGER)
FROM items i
JOIN places pl ON pl.id = i.place_id AND pl.household_id = i.household_id
WHERE i.household_id = ? AND i.deleted_at IS NULL AND i.status NOT IN ('gone', 'lent')
  AND pl.daily_use = 1 AND pl.deleted_at IS NULL AND i.last_seen_at < ?;

-- name: OverviewUnseen :many
SELECT i.id, i.name, i.category, pl.path AS place_path, i.last_seen_at AS at
FROM items i
JOIN places pl ON pl.id = i.place_id AND pl.household_id = i.household_id
WHERE i.household_id = sqlc.arg(household_id) AND i.deleted_at IS NULL AND i.status NOT IN ('gone', 'lent')
  AND pl.daily_use = 1 AND pl.deleted_at IS NULL AND i.last_seen_at < sqlc.arg(cutoff)
ORDER BY i.last_seen_at, i.id
LIMIT sqlc.arg(row_limit);

-- AI jobs: every failed job except the photo derivative job (not AI).
-- name: OverviewFailedJobCount :one
SELECT CAST(COUNT(*) AS INTEGER) FROM jobs
WHERE household_id = ? AND state = 'failed' AND kind <> 'photo_derivatives';

-- name: OverviewFailedJobs :many
SELECT j.id, j.kind, j.item_id, i.name AS item_name, j.last_error, COALESCE(j.finished_at, j.updated_at) AS at
FROM jobs j
LEFT JOIN items i ON i.id = j.item_id AND i.household_id = j.household_id
WHERE j.household_id = ? AND j.state = 'failed' AND j.kind <> 'photo_derivatives'
ORDER BY COALESCE(j.finished_at, j.updated_at) DESC, j.id DESC
LIMIT ?;

-- name: OverviewBoxes :many
SELECT p.box_status AS box_status, p.goes_to_id AS room_id, g.name AS room_name, g.color AS room_color,
       CAST(COUNT(*) AS INTEGER) AS n
FROM places p
LEFT JOIN places g ON g.id = p.goes_to_id AND g.household_id = p.household_id
WHERE p.household_id = ? AND p.kind = 'box' AND p.deleted_at IS NULL
GROUP BY p.box_status, p.goes_to_id, g.name, g.color;

-- name: OverviewNotFoundCount :one
-- Items that were marked "not found" while unpacking (live, confirmed, in no place).
SELECT CAST(COUNT(*) AS INTEGER) FROM items
WHERE household_id = ? AND deleted_at IS NULL AND draft = 0 AND place_id IS NULL;

-- name: OverviewNotFound :many
SELECT id, name, category, updated_at AS at FROM items
WHERE household_id = ? AND deleted_at IS NULL AND draft = 0 AND place_id IS NULL
ORDER BY updated_at DESC, id DESC
LIMIT ?;
