-- Access tokens for agents and scripts (J9.1). Only the SHA-256 of the token
-- is stored. A token belongs to a member of one household and dies with the
-- membership (the lookup joins memberships).
CREATE TABLE access_tokens (
    id           TEXT PRIMARY KEY,
    household_id TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    scope        TEXT NOT NULL CHECK (scope IN ('read', 'write')),
    token_hash   TEXT NOT NULL UNIQUE,
    hint         TEXT NOT NULL,
    last_used_at TEXT,
    expires_at   TEXT,
    revoked_at   TEXT
);
CREATE INDEX access_tokens_household_user ON access_tokens (household_id, user_id);
