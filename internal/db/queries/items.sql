-- Queries for items and contents lines (J3.2). Every query filters by
-- household_id. Listing items with filters and paging is built in
-- internal/inventory/itemlist.go (one dynamic SELECT) and not here.

-- name: GetHouseholdCurrency :one
SELECT currency FROM households WHERE id = ?;

-- name: CreateItem :one
INSERT INTO items (id, household_id, created_at, updated_at, place_id, name, category, brand, model, serial, color,
                   quantity, condition, notes, status, attributes, ai_fields, draft, source, import_id,
                   purchase_date, price_minor, currency, seller, warranty_end, warranty_source,
                   last_seen_at, created_by, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetItemRow :one
SELECT * FROM items WHERE household_id = ? AND id = ?;

-- name: UpdateItemRow :one
UPDATE items
SET place_id = ?, name = ?, category = ?, brand = ?, model = ?, serial = ?, color = ?, quantity = ?,
    condition = ?, notes = ?, status = ?, attributes = ?, ai_fields = ?, draft = ?,
    purchase_date = ?, price_minor = ?, currency = ?, seller = ?, warranty_end = ?, warranty_source = ?,
    last_seen_at = ?, updated_by = ?, updated_at = ?
WHERE household_id = ? AND id = ?
RETURNING *;

-- name: SetItemAIMeta :exec
UPDATE items SET ai_state = ?, ai_draft = ?, updated_at = ? WHERE household_id = ? AND id = ?;

-- name: SetItemDeleted :exec
UPDATE items SET deleted_at = ?, updated_by = ?, updated_at = ? WHERE household_id = ? AND id = ?;

-- name: CreateLine :one
INSERT INTO contents_lines (id, household_id, created_at, updated_at, place_id, name, quantity, category,
                            worth_item, photo_id, ai, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetLine :one
SELECT * FROM contents_lines WHERE household_id = ? AND id = ?;

-- name: ListLines :many
SELECT * FROM contents_lines
WHERE household_id = ? AND place_id = ? AND deleted_at IS NULL
ORDER BY created_at, id;

-- name: UpdateLine :one
UPDATE contents_lines SET name = ?, quantity = ?, category = ?, ai = 0, updated_at = ?
WHERE household_id = ? AND id = ?
RETURNING *;

-- name: SetLineDeleted :exec
UPDATE contents_lines SET deleted_at = ?, updated_at = ? WHERE household_id = ? AND id = ?;

-- name: ConfirmLines :execrows
UPDATE contents_lines SET ai = 0, updated_at = ?
WHERE household_id = ? AND place_id = ? AND deleted_at IS NULL AND ai = 1;

-- name: SetLinePromoted :exec
UPDATE contents_lines SET item_id = ?, deleted_at = ?, updated_at = ? WHERE household_id = ? AND id = ?;
