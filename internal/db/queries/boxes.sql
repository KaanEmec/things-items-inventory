-- Moving boxes (J8.1). A box is a place of kind 'box'. Every query filters by household_id.

-- name: ReserveBoxNumbers :one
-- Takes count numbers from the household's counter and returns the counter
-- after them: the numbers are (result - count) .. (result - 1). The counter
-- only grows, so a number is never given out twice (also not after a delete).
UPDATE households SET next_box_number = next_box_number + sqlc.arg(count), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(household_id)
RETURNING next_box_number;

-- name: PeekBoxNumber :one
SELECT next_box_number FROM households WHERE id = ?;

-- name: SkipBoxNumbers :exec
-- Moves the counter up to at least next (a range that starts above it).
UPDATE households SET next_box_number = MAX(next_box_number, sqlc.arg(next)), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(household_id);

-- name: GetBoxByNumber :one
SELECT * FROM places
WHERE household_id = ? AND kind = 'box' AND box_number = ? AND deleted_at IS NULL;

-- name: ListBoxes :many
-- Live boxes by number with the destination room and direct counts, in one query.
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
WHERE p.household_id = sqlc.arg(household_id) AND p.kind = 'box' AND p.deleted_at IS NULL
  AND (CAST(sqlc.arg(status) AS TEXT) = '' OR p.box_status = sqlc.arg(status))
  AND (CAST(sqlc.arg(goes_to_id) AS TEXT) = '' OR p.goes_to_id = sqlc.arg(goes_to_id))
  AND p.box_number > sqlc.arg(after_number)
ORDER BY p.box_number
LIMIT sqlc.arg(row_limit);

-- name: UpdateBoxFields :one
UPDATE places SET goes_to_id = ?, handling = ?, note = ?, updated_at = ?
WHERE household_id = ? AND id = ? AND kind = 'box'
RETURNING *;

-- name: SetBoxStatus :one
UPDATE places SET box_status = ?, packed_at = ?, packed_by = ?, updated_at = ?
WHERE household_id = ? AND id = ? AND kind = 'box'
RETURNING *;

-- name: SetPackingBox :exec
UPDATE memberships SET packing_box_id = ? WHERE household_id = ? AND user_id = ?;
