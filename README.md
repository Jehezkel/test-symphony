# Product catalogue

A small persistent product CRUD and Allegro profitability data foundation using Go, templ, HTMX, DaisyUI, and SQLite.

```sh
go run .
```

The database is migrated automatically at startup. Set `DATABASE_PATH` to choose its location; the default is `data/app.db`. Users can register in the browser. Optional `AUTH_EMAIL` and `AUTH_PASSWORD` create or update an initial account and must be set together; neither value is logged. `SESSION_TTL_HOURS` defaults to 24. Set `APP_ENV=production` in HTTPS deployments so the session cookie is marked `Secure`; it is always `HttpOnly` and `SameSite=Lax`.

All products, costs, Allegro integrations, synchronized offers and orders, dashboard calculations, and CSV exports are scoped to the authenticated user. Existing development databases are assigned to the original local user (`id=1`); the first `AUTH_EMAIL`/`AUTH_PASSWORD` bootstrap claims that account. To discard development data instead, stop the application, remove only the configured SQLite file and its `-wal`/`-shm` companions, and restart so migrations create an empty database. Never reuse a production database for this reset procedure.

Open <http://localhost:8080>. The service health endpoint is `GET /health`.

## Allegro OAuth

Copy the variable names from `.env.example` into your local secret-management
mechanism. The integration is enabled only when `ALLEGRO_CLIENT_ID`,
`ALLEGRO_CLIENT_SECRET`, `ALLEGRO_REDIRECT_URL`, and a base64-encoded random
32-byte `ALLEGRO_TOKEN_ENCRYPTION_KEY` are all present. Real values must never be
committed. Optional endpoint variables allow the same build to use Allegro's
sandbox.

Register the configured `ALLEGRO_REDIRECT_URL` exactly in the Allegro developer
application. The local default callback is
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
