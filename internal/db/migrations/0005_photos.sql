-- Photos (J3.3). One row per household per file; the bytes on disk are shared
-- by sha256 and removed when no row points at them. photo_links point at an
-- item or a place, with no foreign key to either: those tables belong to
-- another migration, and the photos package checks the target in code.
CREATE TABLE photos (
    id           TEXT PRIMARY KEY,
    household_id TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    sha256       TEXT NOT NULL,
    mime         TEXT NOT NULL CHECK (mime IN ('image/jpeg', 'image/png', 'image/webp')),
    ext          TEXT NOT NULL CHECK (ext IN ('jpg', 'png', 'webp')),
    bytes        INTEGER NOT NULL,
    width        INTEGER,
    height       INTEGER,
    derivatives  INTEGER NOT NULL DEFAULT 0,
    created_by   TEXT,
    UNIQUE (household_id, sha256)
);
CREATE INDEX photos_sha256 ON photos (sha256);

CREATE TABLE photo_links (
    id           TEXT PRIMARY KEY,
    household_id TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    photo_id     TEXT NOT NULL REFERENCES photos(id) ON DELETE CASCADE,
    item_id      TEXT,
    place_id     TEXT,
    role         TEXT NOT NULL CHECK (role IN ('item', 'label', 'receipt', 'contents', 'place')),
    sort         INTEGER NOT NULL DEFAULT 0,
    CHECK ((item_id IS NOT NULL) <> (place_id IS NOT NULL))
);
-- NULLs are distinct in a plain UNIQUE, so one partial index per target kind
-- makes a retried upload or link a no-op.
CREATE UNIQUE INDEX photo_links_item_unique ON photo_links (photo_id, item_id, role) WHERE item_id IS NOT NULL;
CREATE UNIQUE INDEX photo_links_place_unique ON photo_links (photo_id, place_id, role) WHERE place_id IS NOT NULL;
CREATE INDEX photo_links_item ON photo_links (household_id, item_id, role, sort) WHERE item_id IS NOT NULL;
CREATE INDEX photo_links_place ON photo_links (household_id, place_id, role, sort) WHERE place_id IS NOT NULL;
CREATE INDEX photo_links_photo ON photo_links (photo_id);
