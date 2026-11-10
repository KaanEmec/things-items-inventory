-- Items and contents lines (J3.2). contents_lines.photo_id has no foreign key
-- because the photos table arrives in a later migration; the photos package
-- checks it in code.
CREATE TABLE items (
    id               TEXT PRIMARY KEY,
    household_id     TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    deleted_at       TEXT,
    place_id         TEXT REFERENCES places(id) ON DELETE SET NULL,
    name             TEXT NOT NULL,
    category         TEXT NOT NULL DEFAULT 'other',
    brand            TEXT,
    model            TEXT,
    serial           TEXT,
    color            TEXT,
    quantity         INTEGER NOT NULL DEFAULT 1 CHECK (quantity >= 0),
    condition        TEXT CHECK (condition IS NULL OR condition IN ('new', 'like_new', 'good', 'worn', 'broken')),
    notes            TEXT,
    status           TEXT NOT NULL DEFAULT 'stored'
                     CHECK (status IN ('in_use', 'stored', 'packed', 'to_sell', 'lent', 'gone')),
    attributes       TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(attributes)),
    ai_fields        TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(ai_fields)),
    ai_draft         TEXT CHECK (ai_draft IS NULL OR json_valid(ai_draft)),
    ai_state         TEXT CHECK (ai_state IS NULL OR ai_state IN ('queued', 'reading', 'ready', 'failed')),
    draft            INTEGER NOT NULL DEFAULT 0,
    source           TEXT NOT NULL DEFAULT 'manual'
                     CHECK (source IN ('manual', 'capture', 'quick_add', 'text', 'import', 'agent')),
    import_id        TEXT,
    purchase_date    TEXT,
    price_minor      INTEGER,
    currency         TEXT,
    seller           TEXT,
    warranty_end     TEXT,
    warranty_source  TEXT CHECK (warranty_source IS NULL OR warranty_source IN ('stated', 'default')),
    last_seen_at     TEXT NOT NULL,
    created_by       TEXT REFERENCES users(id) ON DELETE SET NULL,
    updated_by       TEXT REFERENCES users(id) ON DELETE SET NULL
);
CREATE INDEX items_place ON items (household_id, place_id);
CREATE INDEX items_status ON items (household_id, status);
CREATE INDEX items_category ON items (household_id, category);
CREATE INDEX items_updated ON items (household_id, updated_at);
CREATE INDEX items_brand_model ON items (household_id, brand, model);
CREATE INDEX items_draft ON items (household_id) WHERE draft = 1;
CREATE INDEX items_warranty ON items (household_id, warranty_end) WHERE warranty_end IS NOT NULL;
CREATE INDEX items_import ON items (household_id, import_id) WHERE import_id IS NOT NULL;

CREATE TABLE contents_lines (
    id           TEXT PRIMARY KEY,
    household_id TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    deleted_at   TEXT,
    place_id     TEXT NOT NULL REFERENCES places(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    quantity     INTEGER NOT NULL DEFAULT 1 CHECK (quantity >= 0),
    category     TEXT,
    worth_item   INTEGER NOT NULL DEFAULT 0,
    photo_id     TEXT,
    ai           INTEGER NOT NULL DEFAULT 0,
    item_id      TEXT REFERENCES items(id) ON DELETE SET NULL,
    created_by   TEXT
);
CREATE INDEX contents_lines_place ON contents_lines (household_id, place_id);
