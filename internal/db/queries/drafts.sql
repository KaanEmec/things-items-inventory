-- Queries for the label, receipt and contents AI jobs (J6.4-J6.6). Every
-- query filters by household_id.

-- name: SetLineWorth :exec
UPDATE contents_lines SET worth_item = 1 WHERE household_id = ? AND id = ?;

-- name: SetWarrantySourceDefault :exec
UPDATE items SET warranty_source = 'default' WHERE household_id = ? AND id = ? AND warranty_end IS NOT NULL;

-- name: ClearDefaultWarranty :exec
-- A receipt's stated term replaces a warranty end that was only the default.
UPDATE items SET warranty_end = NULL, warranty_source = NULL
WHERE household_id = ? AND id = ? AND warranty_source = 'default';
