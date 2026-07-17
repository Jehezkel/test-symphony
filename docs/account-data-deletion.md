# Account data deletion policy

Deleting an account is immediate and irreversible. The user must submit the
exact confirmation phrase shown by the application and re-enter the current
password. The request is protected by the authenticated session's CSRF token.

The application deletes the `users` row inside a single database transaction.
SQLite foreign keys are enabled at connection startup, and cascading relations
remove all data owned by that user: sessions, authentication tokens, OAuth
states, encrypted Allegro access and refresh tokens, products and costs, synced
offers, orders, line items, fees, adjustments, profitability results, sync runs,
sync checkpoints, and onboarding state. A failed transaction leaves the account
and its data unchanged, so there is no partially deleted state to resume.

After commit, the browser session cookie is expired. Existing cookies no longer
authenticate because their database records were deleted. Removing the Allegro
integration also removes the credentials and checkpoints needed by manual or
scheduled synchronization, so no subsequent synchronization can start for the
deleted account. Allegro does not expose a token-revocation endpoint in the API
surface configured by this application; disconnect therefore destroys the
locally stored encrypted tokens and integration record.
