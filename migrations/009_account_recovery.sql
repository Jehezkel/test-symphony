ALTER TABLE users ADD COLUMN email_verified_at TEXT;

CREATE TABLE auth_tokens (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose TEXT NOT NULL CHECK (purpose IN ('password_reset', 'email_verification')),
    token_hash BLOB NOT NULL UNIQUE,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_auth_tokens_user_purpose ON auth_tokens(user_id, purpose, created_at);
CREATE INDEX idx_auth_tokens_expiry ON auth_tokens(expires_at);
