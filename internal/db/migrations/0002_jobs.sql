CREATE TABLE jobs (
    id           TEXT PRIMARY KEY,
    household_id TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    kind         TEXT NOT NULL,
    payload      TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload)),
    state        TEXT NOT NULL DEFAULT 'queued'
                 CHECK (state IN ('queued', 'running', 'done', 'failed', 'cancelled')),
    attempts     INTEGER NOT NULL DEFAULT 0,
    deferrals    INTEGER NOT NULL DEFAULT 0, -- consecutive jobs.Later() waits
    run_after    TEXT NOT NULL,
    last_error   TEXT,
    result       TEXT CHECK (result IS NULL OR json_valid(result)),
    created_by   TEXT,
    item_id      TEXT,
    place_id     TEXT,
    started_at   TEXT,
    finished_at  TEXT
);
-- The claim query filters on (state, run_after); fairness looks up each
-- household's latest started_at.
CREATE INDEX jobs_state_run_after ON jobs (state, run_after);
CREATE INDEX jobs_household_state ON jobs (household_id, state);
CREATE INDEX jobs_household_started ON jobs (household_id, started_at);
CREATE INDEX jobs_item_id ON jobs (item_id);
