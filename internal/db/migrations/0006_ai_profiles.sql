-- One row per household and role. A role with no row uses the server-wide
-- defaults from the environment (if any); those are never stored here.
-- token_enc is AES-256-GCM (see internal/secret), bound to household and role.
CREATE TABLE ai_profiles (
    household_id   TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    role           TEXT NOT NULL CHECK (role IN ('vision', 'text')),
    base_url       TEXT NOT NULL DEFAULT 'https://openrouter.ai/api/v1',
    token_enc      BLOB,
    token_hint     TEXT,
    model          TEXT,
    same_as_vision INTEGER NOT NULL DEFAULT 1 CHECK (same_as_vision IN (0, 1)),
    output_mode    TEXT CHECK (output_mode IS NULL OR output_mode IN ('json_schema', 'json_object', 'prompt')),
    capabilities   TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(capabilities)),
    test_result    TEXT CHECK (test_result IS NULL OR json_valid(test_result)),
    tested_at      TEXT,
    failing_since  TEXT,
    updated_by     TEXT,
    updated_at     TEXT NOT NULL,
    PRIMARY KEY (household_id, role)
);
