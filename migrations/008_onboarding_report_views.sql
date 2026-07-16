CREATE TABLE onboarding_report_views (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    first_viewed_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
