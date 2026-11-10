-- Search index (J4.1). search_docs holds one row per searchable thing with the
-- text to show and the folded keys used for ranking; search_fts is the
-- trigram index over the folded text, with the same rowid as search_docs.id.
-- Both are written only by internal/search, in the transaction of the change
-- they describe, and can be rebuilt from the real tables (`inventory reindex`).
CREATE TABLE search_docs (
    id           INTEGER PRIMARY KEY,
    household_id TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('item', 'line', 'place', 'box')),
    ref_id       TEXT NOT NULL,
    touched      TEXT NOT NULL,
    display      TEXT NOT NULL,
    detail       TEXT NOT NULL DEFAULT '',
    path         TEXT NOT NULL DEFAULT '',
    name_fold    TEXT NOT NULL,
    key_fold     TEXT NOT NULL DEFAULT '',
    brand_fold   TEXT NOT NULL DEFAULT '',
    box_number   INTEGER,
    UNIQUE (household_id, kind, ref_id)
);
CREATE INDEX search_docs_box ON search_docs (household_id, box_number) WHERE box_number IS NOT NULL;
CREATE INDEX search_docs_key ON search_docs (household_id, kind, key_fold) WHERE key_fold <> '';

CREATE VIRTUAL TABLE search_fts USING fts5(
    household_id UNINDEXED,
    kind UNINDEXED,
    ref_id UNINDEXED,
    title,
    body,
    tokenize = 'trigram'
);
