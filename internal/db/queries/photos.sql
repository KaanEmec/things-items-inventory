-- Queries for photos and photo links (J3.3).

-- name: InsertPhoto :one
INSERT INTO photos (id, household_id, created_at, updated_at, sha256, mime, ext, bytes, width, height, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetPhoto :one
SELECT * FROM photos WHERE household_id = ? AND id = ?;

-- name: GetPhotoBySha :one
SELECT * FROM photos WHERE household_id = ? AND sha256 = ?;

-- name: GetPhotoByRef :one
-- The URL names a photo by id or by sha256.
SELECT * FROM photos WHERE household_id = sqlc.arg(household_id) AND (id = sqlc.arg(ref) OR sha256 = sqlc.arg(ref)) LIMIT 1;

-- name: SetPhotoDerivatives :exec
UPDATE photos SET derivatives = 1, updated_at = ? WHERE household_id = ? AND id = ?;

-- name: DeletePhoto :exec
DELETE FROM photos WHERE household_id = ? AND id = ?;

-- name: CountPhotosBySha :one
-- scope: global (the file on disk is shared by every household that uploaded the same bytes)
SELECT COUNT(*) FROM photos WHERE sha256 = ?;

-- name: InsertPhotoLink :one
INSERT INTO photo_links (id, household_id, created_at, updated_at, photo_id, item_id, place_id, role, sort)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetPhotoLink :one
SELECT * FROM photo_links WHERE household_id = ? AND id = ?;

-- name: FindPhotoLink :one
SELECT * FROM photo_links
WHERE household_id = sqlc.arg(household_id) AND photo_id = sqlc.arg(photo_id) AND role = sqlc.arg(role)
  AND item_id IS sqlc.narg(item_id) AND place_id IS sqlc.narg(place_id);

-- name: ListLinksInGroup :many
-- The links of one target and role, in display order.
SELECT * FROM photo_links
WHERE household_id = sqlc.arg(household_id) AND role = sqlc.arg(role)
  AND item_id IS sqlc.narg(item_id) AND place_id IS sqlc.narg(place_id)
ORDER BY sort, created_at, id;

-- name: SetPhotoLinkSort :exec
UPDATE photo_links SET sort = ?, updated_at = ? WHERE household_id = ? AND id = ?;

-- name: SetPhotoLinkRole :exec
UPDATE photo_links SET role = ?, sort = ?, updated_at = ? WHERE household_id = ? AND id = ?;

-- name: DeletePhotoLink :exec
DELETE FROM photo_links WHERE household_id = ? AND id = ?;

-- name: CountLinksOfPhoto :one
SELECT COUNT(*) FROM photo_links WHERE household_id = ? AND photo_id = ?;

-- name: ListLinksForItems :many
SELECT l.id AS link_id, l.photo_id, p.sha256, l.role, l.sort, l.item_id, l.place_id
FROM photo_links l JOIN photos p ON p.id = l.photo_id AND p.household_id = l.household_id
WHERE l.household_id = sqlc.arg(household_id) AND l.item_id IN (sqlc.slice(item_ids))
ORDER BY l.item_id, l.role, l.sort, l.id;

-- name: ListLinksForPlaces :many
SELECT l.id AS link_id, l.photo_id, p.sha256, l.role, l.sort, l.item_id, l.place_id
FROM photo_links l JOIN photos p ON p.id = l.photo_id AND p.household_id = l.household_id
WHERE l.household_id = sqlc.arg(household_id) AND l.place_id IN (sqlc.slice(place_ids))
ORDER BY l.place_id, l.role, l.sort, l.id;

-- name: ListLinksOfTarget :many
SELECT * FROM photo_links
WHERE household_id = sqlc.arg(household_id) AND item_id IS sqlc.narg(item_id) AND place_id IS sqlc.narg(place_id);
