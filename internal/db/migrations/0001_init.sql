CREATE TABLE households (
    id                     TEXT PRIMARY KEY,
    name                   TEXT NOT NULL,
    language               TEXT NOT NULL DEFAULT 'en',
    currency               TEXT NOT NULL DEFAULT 'EUR',
    default_warranty_months INTEGER NOT NULL DEFAULT 24,
    ai_paused              INTEGER NOT NULL DEFAULT 0,
    next_box_number        INTEGER NOT NULL DEFAULT 1,
    created_at             TEXT NOT NULL,
    updated_at             TEXT NOT NULL
);

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

-- packing_box_id references places(id); the places table arrives in a later
-- migration, which cannot add the foreign key to an existing column in SQLite.
CREATE TABLE memberships (
    household_id   TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role           TEXT NOT NULL,
    packing_box_id TEXT,
    last_active_at TEXT,
    created_at     TEXT NOT NULL,
    PRIMARY KEY (household_id, user_id)
);
CREATE INDEX memberships_user_id ON memberships (user_id);

CREATE TABLE invites (
    id           TEXT PRIMARY KEY,
    household_id TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('invite', 'password_reset')),
    role         TEXT,
    user_id      TEXT,
    token_hash   TEXT NOT NULL UNIQUE,
    expires_at   TEXT NOT NULL,
    created_by   TEXT NOT NULL,
    used_at      TEXT,
    used_by      TEXT,
    revoked_at   TEXT
);

CREATE TABLE sessions (
    id           TEXT PRIMARY KEY,
    token_hash   TEXT NOT NULL UNIQUE,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    household_id TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    user_agent   TEXT,
    ip           TEXT
);
CREATE INDEX sessions_user_id ON sessions (user_id);
