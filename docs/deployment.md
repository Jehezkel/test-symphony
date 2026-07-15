# Development deployment

The development service is hosted by Dokploy, which is reachable through Tailscale.

| Resource | Name | ID |
| --- | --- | --- |
| Project | `test-symphony` | `t9NfQOsqdLeaQzHGhNXV8` |
| Environment | `development` | `vO9VaKVAgIDo7ShJWs5AI` |
| Application | `test-symphony-dev` | `SdDyZWMDhe-80YdK5VqmC` |

Dokploy tracks `Jehezkel/test-symphony` on the `develop` branch through the existing GitHub provider. It builds the root `Dockerfile` with the Dockerfile builder and routes the container's port `8080` through Traefik.

- Application: <https://app-bypass-haptic-sensor-4ooyls-ed57d3-85-215-207-20.traefik.me>
- Health check: <https://app-bypass-haptic-sensor-4ooyls-ed57d3-85-215-207-20.traefik.me/health>
- Required environment variables: none. `PORT` is optional and defaults to `8080` in the image.

Deployments are triggered automatically by pushes to `develop`. An operator can also call `POST /api/application.redeploy` with the application ID. Inspect the application and its deployment history in the Dokploy UI or through the authenticated Dokploy API. No manual configuration remains.
