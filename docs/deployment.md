# Development deployment

The development service is hosted by Dokploy, which is reachable through Tailscale.

| Resource | Name | ID |
| --- | --- | --- |
| Project | `test-symphony` | `t9NfQOsqdLeaQzHGhNXV8` |
| Environment | `development` | `vO9VaKVAgIDo7ShJWs5AI` |
| Application | `test-symphony-dev` | `SdDyZWMDhe-80YdK5VqmC` |

Dokploy tracks `Jehezkel/test-symphony` on the `develop` branch through the existing GitHub provider. It builds the root `Dockerfile` with the Dockerfile builder and routes the container's port `8080` through Traefik.

- Application: <http://app-bypass-haptic-sensor-4ooyls-ed57d3-85-215-207-20.traefik.me>
- Health check: <http://app-bypass-haptic-sensor-4ooyls-ed57d3-85-215-207-20.traefik.me/health>
- Required environment selector: `ALLEGRO_ENVIRONMENT=production|sandbox`
  (defaults safely to `sandbox`; development must explicitly use `sandbox`).
  The selector atomically chooses OAuth, token, and REST API hosts. Required
  OAuth variables for the selected environment are
  `ALLEGRO_<ENV>_CLIENT_ID`, `ALLEGRO_<ENV>_CLIENT_SECRET`,
  `ALLEGRO_<ENV>_REDIRECT_URL`, and
  `ALLEGRO_TOKEN_ENCRYPTION_KEY` (base64-encoded 32-byte key). Store their real
  values only in Dokploy; never copy them into repository files or deployment
  logs. Keep the `PRODUCTION` and `SANDBOX` credential sets separate. `PORT`
  defaults to `8080`;
  `ALLEGRO_SYNC_INTERVAL_MINUTES` is optional and defaults to `15` (`0`
  disables periodic synchronization);
  `DATABASE_PATH` defaults to `/data/app.db` in the image. The image declares
  `/data` as the persistent SQLite volume.

Authentication requires `AUTH_EMAIL` and secret `AUTH_PASSWORD`. Optional
`AUTH_DISPLAY_NAME` labels the initial account, and `SESSION_TTL_HOURS` defaults
to `24`. Dokploy must set `APP_ENV=production` so session cookies use `Secure`;
the public Traefik route must use HTTPS with a certificate valid for its host.
Passwords and session tokens are never stored in deployment files or logs.

Account recovery additionally requires `APP_BASE_URL`, `EMAIL_PROVIDER=smtp`,
`SMTP_HOST`, `SMTP_PORT`, `EMAIL_FROM`, and provider-dependent
`SMTP_USERNAME`/secret `SMTP_PASSWORD`. One-time tokens are never stored in
plain text or written to application logs.

Deployments are triggered automatically by pushes to `develop`. For a new application with no checkout yet, call `POST /api/application.deploy` with the application ID so Dokploy clones the repository before building it. Use `POST /api/application.redeploy` only after a successful deployment has created the application checkout. Inspect the application and its deployment history in the Dokploy UI or through the authenticated Dokploy API. Before deploying authentication, configure its required environment variables and a valid HTTPS route in Dokploy.
