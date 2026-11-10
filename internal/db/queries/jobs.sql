-- name: InsertJob :one
INSERT INTO jobs (id, household_id, created_at, updated_at, kind, payload, run_after, created_by, item_id, place_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetJob :one
SELECT * FROM jobs WHERE household_id = ? AND id = ?;

-- name: ClaimJob :one
-- scope: global (the worker pool serves every household; it picks the next queued job across all of them)
-- Round-robin across households (the one claimed from longest ago first),
-- newest job first within a household.
UPDATE jobs
SET state = 'running', attempts = attempts + 1, started_at = sqlc.arg(now), updated_at = sqlc.arg(now)
WHERE id = (
    SELECT j.id FROM jobs j
    WHERE j.state = 'queued' AND j.run_after <= sqlc.arg(now)
    ORDER BY COALESCE((SELECT MAX(x.started_at) FROM jobs x WHERE x.household_id = j.household_id), ''),
             j.created_at DESC, j.id DESC
    LIMIT 1
)
RETURNING *;

-- name: CompleteJob :one
-- scope: global (the worker pool serves every household and addresses jobs by id)
UPDATE jobs
SET state = 'done', result = sqlc.narg(result), last_error = NULL, finished_at = sqlc.arg(now), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND state = 'running'
RETURNING *;

-- name: FailJob :one
-- scope: global (the worker pool serves every household and addresses jobs by id)
UPDATE jobs
SET state = 'failed', last_error = sqlc.arg(last_error), finished_at = sqlc.arg(now), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND state = 'running'
RETURNING *;

-- name: DelayJob :one
-- scope: global (the worker pool serves every household and addresses jobs by id)
UPDATE jobs
SET state = 'queued', run_after = sqlc.arg(run_after), last_error = sqlc.arg(last_error), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND state = 'running'
RETURNING *;

-- name: DeferJob :one
-- scope: global (the worker pool serves every household and addresses a running job by id)
-- The handler asked to wait: back to queued, the attempt not counted.
UPDATE jobs
SET state = 'queued', attempts = MAX(attempts - 1, 0), deferrals = deferrals + 1,
    run_after = sqlc.arg(run_after), last_error = sqlc.arg(last_error), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND state = 'running'
RETURNING *;

-- name: ReleaseJob :one
-- scope: global (the worker pool serves every household and addresses jobs by id)
-- Puts a running job back without counting the attempt (shutdown).
UPDATE jobs
SET state = 'queued', attempts = MAX(attempts - 1, 0), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND state = 'running'
RETURNING *;

-- name: RequeueRunningJobs :many
-- scope: global (the worker pool serves every household and addresses jobs by id)
-- Start-up recovery: jobs a dead process left running.
UPDATE jobs
SET state = 'queued', updated_at = sqlc.arg(now)
WHERE state = 'running'
RETURNING *;

-- name: CancelJob :one
UPDATE jobs
SET state = 'cancelled', finished_at = sqlc.arg(now), updated_at = sqlc.arg(now)
WHERE household_id = sqlc.arg(household_id) AND id = sqlc.arg(id) AND state IN ('queued', 'running')
RETURNING *;

-- name: RetryJob :one
UPDATE jobs
SET state = 'queued', attempts = 0, deferrals = 0, run_after = sqlc.arg(now), last_error = NULL, result = NULL,
    finished_at = NULL, updated_at = sqlc.arg(now)
WHERE household_id = sqlc.arg(household_id) AND id = sqlc.arg(id) AND state = 'failed'
RETURNING *;

-- name: DeleteFinishedJobsBefore :execrows
-- scope: global (the worker pool serves every household and addresses jobs by id)
DELETE FROM jobs
WHERE state IN ('done', 'failed', 'cancelled') AND finished_at < ?;
