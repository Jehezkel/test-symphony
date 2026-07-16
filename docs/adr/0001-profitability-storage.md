# ADR 0001: SQLite and integer minor units for profitability data

Status: accepted (2026-07-16)

## Context

The application is a small, single-process Go/templ service. Profitability must survive restarts, support idempotent Allegro synchronization, and calculate money without binary floating-point errors.

## Decision

Use SQLite through the pure-Go `modernc.org/sqlite` driver. Embedded, ordered SQL migrations run transactionally at startup and are recorded in `schema_migrations`. The database path is configured with `DATABASE_PATH` and defaults to `data/app.db`; the container uses `/data/app.db` on a volume.

Store every monetary value as a signed 64-bit integer in the currency's minor unit plus an ISO 4217 currency code. API decimal strings are parsed directly to integers without `float`. Signed Allegro billing values retain their source sign. Percentage margin is stored as integer basis points and remains nullable when revenue is zero or inputs are incomplete.

External Allegro identifiers are unique within an integration. Orders, line items, fees, and adjustments therefore support idempotent upserts without duplicate facts. Product costs are effective-dated. A profitability result records its input timestamp, calculation version, component totals, currency, and explicit completeness state.

OAuth token columns contain only encrypted ciphertext; encryption and OAuth synchronization are outside this issue.

## Consequences

This keeps deployment and local development simple and gives atomic constraints and migrations. A single SQLite writer is appropriate for the current service; horizontal replicas would require migration to a client/server database. Minor units assume currencies supported by the importer define their scale; the MVP parser accepts two fractional digits for PLN.
