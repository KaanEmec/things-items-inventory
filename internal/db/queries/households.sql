-- name: CreateHousehold :one
INSERT INTO households (id, name, language, currency, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetHousehold :one
SELECT * FROM households WHERE id = ?;
