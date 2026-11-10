-- Queries for homes and places (J3.1). Every query filters by household_id.

-- name: CreateHome :one
INSERT INTO homes (id, household_id, created_at, updated_at, name, sort)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetHome :one
SELECT * FROM homes WHERE household_id = ? AND id = ?;

-- name: ListHomes :many
SELECT * FROM homes
WHERE household_id = sqlc.arg(household_id)
  AND (archived_at IS NOT NULL) = CAST(sqlc.arg(archived) AS INTEGER)
ORDER BY sort, name, id;

-- name: NextHomeSort :one
SELECT CAST(COALESCE(MAX(sort), -1) + 1 AS INTEGER) FROM homes WHERE household_id = ?;

-- name: UpdateHome :one
UPDATE homes SET name = ?, sort = ?, archived_at = ?, updated_at = ?
WHERE household_id = ? AND id = ?
RETURNING *;

-- name: CreatePlace :one
INSERT INTO places (id, household_id, created_at, updated_at, home_id, parent_id, kind, name, sort,
                    path, lineage, goes_to_id, color, daily_use, note, box_number, box_status, handling)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetPlace :one
SELECT * FROM places WHERE household_id = ? AND id = ?;

-- name: UpdatePlace :one
UPDATE places
SET home_id = ?, parent_id = ?, name = ?, sort = ?, path = ?, lineage = ?,
    goes_to_id = ?, color = ?, daily_use = ?, note = ?, updated_at = ?
WHERE household_id = ? AND id = ?
RETURNING *;

-- name: SetPlaceSort :exec
UPDATE places SET sort = ? WHERE household_id = ? AND id = ?;

-- name: RewriteSubtree :exec
-- Rewrites path, lineage and home of every row below a place (deleted ones
-- too) after the place was renamed or moved. lineage_lo/lineage_hi bound the
-- old lineage prefix: "lineage >= '/a/b/' AND lineage < '/a/b0'".
UPDATE places
SET lineage = sqlc.arg(new_lineage) || substr(lineage, length(sqlc.arg(old_lineage)) + 1),
    path = substr(path, 1, length(path) - length(sqlc.arg(old_path))) || sqlc.arg(new_path),
    home_id = sqlc.narg(home_id)
WHERE household_id = sqlc.arg(household_id)
  AND lineage >= sqlc.arg(old_lineage) AND lineage < sqlc.arg(lineage_hi)
  AND id <> sqlc.arg(self_id);

-- name: ListHomeTree :many
-- The whole tree of a home in one query: every live place with its direct
-- item and line counts and the goes-to room's name and colour. Totals below a
-- place are rolled up in Go from parent_id.
SELECT p.id, p.kind, p.name, p.path, p.home_id, p.box_number, p.color, p.parent_id, p.sort,
       p.goes_to_id, g.name AS goes_to_name, g.color AS goes_to_color,
       p.daily_use, p.note, p.box_status, p.handling, p.packed_at, p.label_printed_at,
       p.created_at, p.updated_at,
       CAST(COALESCE(ic.n, 0) AS INTEGER) AS items_direct,
       CAST(COALESCE(lc.n, 0) AS INTEGER) AS lines_direct
FROM places p
LEFT JOIN places g ON g.household_id = p.household_id AND g.id = p.goes_to_id
LEFT JOIN (SELECT x.place_id, COUNT(*) AS n FROM items x
           WHERE x.household_id = sqlc.arg(household_id) AND x.deleted_at IS NULL AND x.place_id IS NOT NULL
           GROUP BY x.place_id) ic ON ic.place_id = p.id
LEFT JOIN (SELECT y.place_id, COUNT(*) AS n FROM contents_lines y
           WHERE y.household_id = sqlc.arg(household_id) AND y.deleted_at IS NULL
           GROUP BY y.place_id) lc ON lc.place_id = p.id
WHERE p.household_id = sqlc.arg(household_id) AND p.home_id = sqlc.arg(home_id) AND p.deleted_at IS NULL
ORDER BY p.sort, p.name, p.id;

-- name: ListChildPlaces :many
SELECT * FROM places
WHERE household_id = ? AND parent_id = ? AND deleted_at IS NULL
ORDER BY sort, name, id;

-- name: ListSiblings :many
SELECT * FROM places
WHERE household_id = sqlc.arg(household_id) AND deleted_at IS NULL
  AND home_id IS sqlc.narg(home_id) AND parent_id IS sqlc.narg(parent_id)
ORDER BY sort, name, id;

-- name: NextPlaceSort :one
SELECT CAST(COALESCE(MAX(sort), -1) + 1 AS INTEGER) FROM places
WHERE household_id = sqlc.arg(household_id) AND deleted_at IS NULL
  AND home_id IS sqlc.narg(home_id) AND parent_id IS sqlc.narg(parent_id);

-- name: GetPlaceCounts :one
-- Items directly in a place, items in it and everything below it (lineage
-- range), and its contents lines.
SELECT
  CAST((SELECT COUNT(*) FROM items a WHERE a.household_id = sqlc.arg(household_id)
          AND a.place_id = sqlc.arg(place_id) AND a.deleted_at IS NULL) AS INTEGER) AS items_direct,
  CAST((SELECT COUNT(*) FROM items i JOIN places p ON p.id = i.place_id
        WHERE i.household_id = sqlc.arg(household_id) AND i.deleted_at IS NULL
          AND p.household_id = sqlc.arg(household_id) AND p.deleted_at IS NULL
          AND p.lineage >= sqlc.arg(lineage_lo) AND p.lineage < sqlc.arg(lineage_hi)) AS INTEGER) AS items_below,
  CAST((SELECT COUNT(*) FROM contents_lines l WHERE l.household_id = sqlc.arg(household_id)
          AND l.place_id = sqlc.arg(place_id) AND l.deleted_at IS NULL) AS INTEGER) AS lines_direct,
  CAST((SELECT COUNT(*) FROM places c WHERE c.household_id = sqlc.arg(household_id)
          AND c.parent_id = sqlc.arg(place_id) AND c.deleted_at IS NULL) AS INTEGER) AS child_places;

-- name: MovePlaceItems :execrows
UPDATE items SET place_id = sqlc.arg(to_id), last_seen_at = sqlc.arg(now), updated_at = sqlc.arg(now),
                 updated_by = sqlc.narg(user_id)
WHERE household_id = sqlc.arg(household_id) AND place_id = sqlc.arg(from_id) AND deleted_at IS NULL;

-- name: MovePlaceLines :execrows
UPDATE contents_lines SET place_id = sqlc.arg(to_id), updated_at = sqlc.arg(now)
WHERE household_id = sqlc.arg(household_id) AND place_id = sqlc.arg(from_id) AND deleted_at IS NULL;

-- name: SoftDeletePlace :exec
UPDATE places SET deleted_at = ?, updated_at = ? WHERE household_id = ? AND id = ?;

-- name: RestorePlace :exec
UPDATE places SET deleted_at = NULL, updated_at = ? WHERE household_id = ? AND id = ?;

-- name: ClearPackingBox :exec
UPDATE memberships SET packing_box_id = NULL WHERE household_id = ? AND packing_box_id = ?;
