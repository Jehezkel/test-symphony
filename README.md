# Product catalogue

A small persistent product CRUD and Allegro profitability data foundation using Go, templ, HTMX, DaisyUI, and SQLite.

```sh
go run .
```

The database is migrated automatically at startup. Set `DATABASE_PATH` to choose its location; the default is `data/app.db`.

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

## Symphony workflow

[workflow.md](workflow.md) is the single source of truth for Symphony task
selection, agent operating rules, validation, delivery, and Linear lifecycle.
