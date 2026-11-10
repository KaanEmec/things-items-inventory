-- Queries for accounts, sessions, memberships and reset links (J2.1, J2.2).
-- Every query on a table with a household_id column filters or sets it, or
-- says "-- scope: global" (checked by internal/db/scope_test.go).

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: UpdateUserProfile :one
UPDATE users SET name = ?, email = ?, updated_at = ? WHERE id = ? RETURNING *;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?;

-- name: CreateMembership :exec
INSERT INTO memberships (household_id, user_id, role, created_at) VALUES (?, ?, ?, ?);

-- name: GetMembership :one
SELECT * FROM memberships WHERE household_id = ? AND user_id = ?;

-- name: TouchMembership :exec
UPDATE memberships SET last_active_at = ? WHERE household_id = ? AND user_id = ?;

-- name: ListUserHouseholds :many
-- scope: global (lists one user's memberships across households)
SELECT m.household_id, h.name, m.role
FROM memberships m JOIN households h ON h.id = m.household_id
WHERE m.user_id = ?
ORDER BY m.last_active_at DESC, m.created_at, m.household_id;

-- name: UpdateHousehold :one
UPDATE households
SET name = ?, language = ?, currency = ?, default_warranty_months = ?, ai_paused = ?, updated_at = ?
WHERE id = ?
RETURNING *;

-- name: CreateSession :exec
-- scope: global (a session belongs to a user; household_id is its active household)
INSERT INTO sessions (id, token_hash, user_id, household_id, created_at, last_seen_at, expires_at, user_agent, ip)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetSessionByTokenHash :one
-- scope: global (resolving the cookie is how the household becomes known)
SELECT s.id AS session_id, s.user_id, s.household_id, s.last_seen_at, s.expires_at,
       u.name AS user_name, u.email AS user_email, m.role
FROM sessions s
JOIN users u ON u.id = s.user_id
JOIN memberships m ON m.household_id = s.household_id AND m.user_id = s.user_id
WHERE s.token_hash = ?;

-- name: TouchSession :exec
-- scope: global
UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?;

-- name: SetSessionHousehold :exec
-- scope: global (the caller has already checked the user's membership)
UPDATE sessions SET household_id = ? WHERE id = ? AND user_id = ?;

-- name: DeleteSession :exec
-- scope: global
DELETE FROM sessions WHERE id = ?;

-- name: DeleteUserSessions :exec
-- scope: global
DELETE FROM sessions WHERE user_id = ?;

-- name: DeleteOtherUserSessions :exec
-- scope: global
DELETE FROM sessions WHERE user_id = ? AND id <> ?;

-- name: DeleteExpiredSessions :exec
-- scope: global
DELETE FROM sessions WHERE expires_at < ?;

-- name: CreateResetInvite :exec
INSERT INTO invites (id, household_id, created_at, updated_at, kind, user_id, token_hash, expires_at, created_by)
VALUES (?, ?, ?, ?, 'password_reset', ?, ?, ?, ?);

-- name: RevokeResetInvites :exec
UPDATE invites SET revoked_at = ?, updated_at = ?
WHERE household_id = ? AND kind = 'password_reset' AND user_id = ? AND used_at IS NULL AND revoked_at IS NULL;

-- name: GetResetInviteByTokenHash :one
-- scope: global (a reset link is opened without a session)
SELECT id, household_id, user_id, expires_at, used_at, revoked_at
FROM invites WHERE kind = 'password_reset' AND token_hash = ?;

-- name: UseResetInvite :execrows
-- scope: global
UPDATE invites SET used_at = ?, used_by = ?, updated_at = ?
WHERE id = ? AND used_at IS NULL AND revoked_at IS NULL;
