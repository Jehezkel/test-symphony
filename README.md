# Product catalogue

A small persistent product CRUD and Allegro profitability data foundation using Go, templ, HTMX, DaisyUI, and SQLite.

```sh
go run .
```

The database is migrated automatically at startup. Set `DATABASE_PATH` to choose its location; the default is `data/app.db`.

Open <http://localhost:8080>. The service health endpoint is `GET /health`.

## Symphony workflow

[workflow.md](workflow.md) is the single source of truth for Symphony task
selection, agent operating rules, validation, delivery, and Linear lifecycle.
