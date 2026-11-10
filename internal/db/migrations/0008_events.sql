-- History (J3.6): who did what and when. Written only by the inventory data
-- layer, in the transaction of the change. item_id and place_id carry no
-- foreign key: history outlives the daily purge of soft-deleted rows.
CREATE TABLE events (
    id           TEXT PRIMARY KEY,
    household_id TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    at           TEXT NOT NULL,
    actor_kind   TEXT NOT NULL CHECK (actor_kind IN ('user', 'ai', 'token')),
    user_id      TEXT REFERENCES users(id) ON DELETE SET NULL,
    token_id     TEXT,
    token_name   TEXT,
    kind         TEXT NOT NULL CHECK (kind IN ('created', 'edited', 'moved', 'status_changed', 'photo_added', 'confirmed',
                                               'seen', 'packed', 'opened', 'arrived', 'unpacked', 'deleted', 'restored',
                                               'settings_changed')),
    item_id      TEXT,
    place_id     TEXT,
    data         TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(data))
);
CREATE INDEX events_household_at ON events (household_id, at DESC);
CREATE INDEX events_item_at ON events (item_id, at DESC);
CREATE INDEX events_place_at ON events (place_id, at DESC);
