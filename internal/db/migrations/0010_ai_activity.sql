-- One row per AI request (J5.6). It never holds a prompt, a photo or a token.
-- item_id and job_id are plain references: the item or job may be gone later.
CREATE TABLE ai_activity (
    id           TEXT PRIMARY KEY,
    household_id TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    at           TEXT NOT NULL,
    feature      TEXT NOT NULL CHECK (feature IN ('item_draft', 'label', 'receipt', 'contents', 'row_completion', 'text_to_items', 'csv_mapping', 'test')),
    role         TEXT NOT NULL CHECK (role IN ('vision', 'text')),
    model        TEXT NOT NULL,
    tokens_in    INTEGER,
    tokens_out   INTEGER,
    cost         REAL,
    duration_ms  INTEGER NOT NULL,
    ok           INTEGER NOT NULL CHECK (ok IN (0, 1)),
    error_code   TEXT,
    error        TEXT,
    job_id       TEXT,
    item_id      TEXT
);
CREATE INDEX ai_activity_household_at ON ai_activity (household_id, at DESC, id DESC);
