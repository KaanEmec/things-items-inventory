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

-- name: SetAIOutputModeIfUnset :exec
-- Remember the structured-output mode that auto mode found, unless a test or
-- an earlier request already did.
UPDATE ai_profiles SET output_mode = ?
WHERE household_id = ? AND role = ? AND output_mode IS NULL;

-- name: IsAIPaused :one
-- scope: global (households.id is the household; the table has no household_id column)
SELECT ai_paused FROM households WHERE id = ?;

-- name: InsertAIActivity :exec
INSERT INTO ai_activity (id, household_id, at, feature, role, model, tokens_in, tokens_out, cost, duration_ms, ok, error_code, error, job_id, item_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListAIActivity :many
-- Newest first. Keyset page: pass the (at, id) of the last row of the previous
-- page, or '9999-12-31T23:59:59.999Z' and '~' for the first page.
SELECT * FROM ai_activity
WHERE household_id = sqlc.arg(household_id)
  AND (at < sqlc.arg(before_at) OR (at = sqlc.arg(before_at) AND id < sqlc.arg(before_id)))
ORDER BY at DESC, id DESC
LIMIT sqlc.arg(max_rows);

-- name: AIUsageByRole :many
SELECT role,
       COUNT(*) AS requests,
       CAST(COALESCE(SUM(CASE WHEN ok = 0 THEN 1 ELSE 0 END), 0) AS INTEGER) AS failures,
       CAST(COALESCE(SUM(tokens_in), 0) AS INTEGER) AS tokens_in,
       CAST(COALESCE(SUM(tokens_out), 0) AS INTEGER) AS tokens_out,
       CAST(COALESCE(SUM(cost), 0) AS REAL) AS cost
FROM ai_activity
WHERE household_id = sqlc.arg(household_id) AND at >= sqlc.arg(from_at) AND at < sqlc.arg(to_at)
GROUP BY role
ORDER BY role;
