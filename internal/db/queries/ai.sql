-- Queries for AI profiles (J5.1). Every query filters by household_id.

-- name: GetAIProfile :one
SELECT * FROM ai_profiles WHERE household_id = ? AND role = ?;

-- name: ListAIProfiles :many
SELECT * FROM ai_profiles WHERE household_id = ? ORDER BY role;

-- name: UpsertAIProfile :one
INSERT INTO ai_profiles (household_id, role, base_url, token_enc, token_hint, model, same_as_vision, updated_by, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (household_id, role) DO UPDATE SET
    base_url = excluded.base_url,
    token_enc = excluded.token_enc,
    token_hint = excluded.token_hint,
    model = excluded.model,
    same_as_vision = excluded.same_as_vision,
    updated_by = excluded.updated_by,
    updated_at = excluded.updated_at
RETURNING *;

-- name: ClearAITest :exec
-- Forget what the last test found (the endpoint, model or token changed).
UPDATE ai_profiles
SET output_mode = NULL, capabilities = '{}', test_result = NULL, tested_at = NULL, failing_since = NULL
WHERE household_id = ? AND role = ?;

-- name: SaveAITest :exec
UPDATE ai_profiles
SET output_mode = ?, capabilities = ?, test_result = ?, tested_at = ?, failing_since = NULL
WHERE household_id = ? AND role = ?;

-- name: MarkAIFailing :exec
UPDATE ai_profiles SET failing_since = COALESCE(failing_since, ?)
WHERE household_id = ? AND role = ?;

-- name: ClearAIFailing :exec
UPDATE ai_profiles SET failing_since = NULL WHERE household_id = ? AND role = ? AND failing_since IS NOT NULL;

-- name: DeleteAIProfile :execrows
DELETE FROM ai_profiles WHERE household_id = ? AND role = ?;
