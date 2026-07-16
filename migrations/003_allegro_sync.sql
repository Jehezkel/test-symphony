CREATE TABLE allegro_sync_runs (
    id INTEGER PRIMARY KEY,
    integration_id INTEGER NOT NULL REFERENCES allegro_integrations(id) ON DELETE CASCADE,
    trigger TEXT NOT NULL CHECK(trigger IN ('manual', 'scheduled')),
    status TEXT NOT NULL CHECK(status IN ('running', 'success', 'failed')),
    phase TEXT NOT NULL DEFAULT 'offers',
    processed_count INTEGER NOT NULL DEFAULT 0,
    error_message TEXT,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_allegro_sync_runs_latest ON allegro_sync_runs(integration_id, started_at DESC);

CREATE TABLE allegro_sync_checkpoints (
    integration_id INTEGER NOT NULL REFERENCES allegro_integrations(id) ON DELETE CASCADE,
    resource TEXT NOT NULL CHECK(resource IN ('offers', 'orders', 'fees')),
    cursor TEXT NOT NULL DEFAULT '',
    last_success_at TEXT,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(integration_id, resource)
);
