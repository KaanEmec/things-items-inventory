-- Whole-household reads for "export everything" (J10.2). Every query filters
-- by household_id. None of them touches secrets: the AI profile, session,
-- invite and access-token tables are deliberately not read here.

-- name: ExportHomes :many
SELECT * FROM homes WHERE household_id = ? ORDER BY sort, id;

-- name: ExportPlaces :many
SELECT * FROM places WHERE household_id = ? ORDER BY path, id;

-- name: ExportItems :many
SELECT * FROM items WHERE household_id = ? ORDER BY created_at, id;

-- name: ExportLines :many
SELECT * FROM contents_lines WHERE household_id = ? ORDER BY created_at, id;

-- name: ExportEvents :many
SELECT * FROM events WHERE household_id = ? ORDER BY at, id;

-- name: ExportPhotos :many
SELECT * FROM photos WHERE household_id = ? ORDER BY created_at, id;

-- name: ExportPhotoLinks :many
SELECT * FROM photo_links WHERE household_id = ? ORDER BY created_at, id;

-- name: ExportMembers :many
SELECT u.name AS name, m.role AS role, m.created_at AS joined_at
FROM memberships m JOIN users u ON u.id = m.user_id
WHERE m.household_id = ? ORDER BY m.created_at, u.id;
