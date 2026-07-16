CREATE TABLE allegro_oauth_states (
    state_hash BLOB PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_allegro_oauth_states_expiry ON allegro_oauth_states(expires_at);

