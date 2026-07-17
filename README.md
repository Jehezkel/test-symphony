# Product catalogue

A small persistent product CRUD and Allegro profitability data foundation using Go, templ, HTMX, DaisyUI, and SQLite.

```sh
go run .
```

The database is migrated automatically at startup. Set `DATABASE_PATH` to choose its location; the default is `data/app.db`. Users can register in the browser. Optional `AUTH_EMAIL` and `AUTH_PASSWORD` create or update an initial account and must be set together; neither value is logged. `SESSION_TTL_HOURS` defaults to 24. Set `APP_ENV=production` in HTTPS deployments so the session cookie is marked `Secure`; it is always `HttpOnly` and `SameSite=Lax`.

Password recovery and email verification use short-lived, one-time links. Set `APP_BASE_URL` to the public origin. Development uses a discard email adapter by default (tokens are never logged); production requires `EMAIL_PROVIDER=smtp` with `SMTP_HOST`, `SMTP_PORT`, `EMAIL_FROM`, and optional `SMTP_USERNAME`/secret `SMTP_PASSWORD`.

All products, costs, Allegro integrations, synchronized offers and orders, dashboard calculations, and CSV exports are scoped to the authenticated user. Existing development databases are assigned to the original local user (`id=1`); the first `AUTH_EMAIL`/`AUTH_PASSWORD` bootstrap claims that account. To discard development data instead, stop the application, remove only the configured SQLite file and its `-wal`/`-shm` companions, and restart so migrations create an empty database. Never reuse a production database for this reset procedure.

Open <http://localhost:8080>. The service health endpoint is `GET /health`.

## Allegro OAuth

Copy the variable names from `.env.example` into your local secret-management
mechanism. `ALLEGRO_ENVIRONMENT=production|sandbox` selects the OAuth host, API
host, redirect URI, and matching credential set together. It defaults to
`sandbox`; any other value stops application startup with an error. The selected
environment is enabled only when its `ALLEGRO_<ENV>_CLIENT_ID`,
`ALLEGRO_<ENV>_CLIENT_SECRET`, `ALLEGRO_<ENV>_REDIRECT_URL`, and a base64-encoded
random 32-byte `ALLEGRO_TOKEN_ENCRYPTION_KEY` are all present. Keep Production
and Sandbox OAuth credentials separate and never commit real values.

Register the selected environment's `ALLEGRO_<ENV>_REDIRECT_URL` exactly in the
corresponding Allegro developer application. The local Sandbox callback is
`http://localhost:8080/oauth/allegro/callback`.

After connecting an account, offers, orders with their line items, and billing
entries can be synchronized manually from the integration page. The same
idempotent synchronization runs every 15 minutes by default. Set
`ALLEGRO_SYNC_INTERVAL_MINUTES` to a positive number to change the interval or
to `0` to disable scheduled runs. Each API page is committed atomically, and the
last successful checkpoint and run result are retained so interrupted runs can
be safely retried.

## Symphony workflow

[workflow.md](workflow.md) is the single source of truth for Symphony task
selection, agent operating rules, validation, delivery, and Linear lifecycle.
