-- Homes and places (J3.1). Boxes are places of kind 'box' with no home and no
-- parent; their numbering columns are used by the boxes job.
CREATE TABLE homes (
    id           TEXT PRIMARY KEY,
    household_id TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    name         TEXT NOT NULL,
    sort         INTEGER NOT NULL DEFAULT 0,
    archived_at  TEXT
);
CREATE INDEX homes_household_sort ON homes (household_id, sort);

CREATE TABLE places (
    id               TEXT PRIMARY KEY,
    household_id     TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    deleted_at       TEXT,
    home_id          TEXT REFERENCES homes(id) ON DELETE CASCADE,
    parent_id        TEXT REFERENCES places(id),
    kind             TEXT NOT NULL CHECK (kind IN ('floor', 'room', 'furniture', 'spot', 'box')),
    name             TEXT NOT NULL,
    sort             INTEGER NOT NULL DEFAULT 0,
    path             TEXT NOT NULL,
    lineage          TEXT NOT NULL,
    goes_to_id       TEXT REFERENCES places(id) ON DELETE SET NULL,
    color            TEXT CHECK (color IS NULL OR color IN
                         ('red', 'orange', 'yellow', 'green', 'teal', 'blue', 'purple', 'pink', 'brown', 'grey')),
    daily_use        INTEGER NOT NULL DEFAULT 0,
    note             TEXT,
    box_number       INTEGER,
    box_status       TEXT CHECK (box_status IS NULL OR box_status IN ('empty', 'open', 'packed', 'arrived', 'unpacked')),
    handling         TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(handling)),
    packed_at        TEXT,
    packed_by        TEXT,
    label_printed_at TEXT,
    CHECK ((kind = 'box') = (home_id IS NULL))
);
CREATE INDEX places_tree ON places (household_id, home_id, parent_id, sort);
CREATE INDEX places_lineage ON places (household_id, lineage);
CREATE INDEX places_kind_status ON places (household_id, kind, box_status);
CREATE UNIQUE INDEX places_box_number ON places (household_id, box_number) WHERE box_number IS NOT NULL;
