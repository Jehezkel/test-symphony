---

tracker:
kind: linear
project_slug: "TU_WSTAW_SLUG_PROJEKTU_LINEAR"
required_labels:
- symphony
active_states:
- Todo
- In Progress
terminal_states:
- Done
- Closed
- Cancelled
- Canceled
- Duplicate

polling:
interval_ms: 30000

workspace:
root: /home/symphony/workspaces

hooks:
after_create: |
set -eu

```
test -n "${SOURCE_REPO_URL:-}" || {
  echo "SOURCE_REPO_URL is not configured"
  exit 1
}

git clone "$SOURCE_REPO_URL" .

git fetch origin --prune

if git show-ref --verify --quiet refs/remotes/origin/develop; then
  git checkout -B develop origin/develop
elif git show-ref --verify --quiet refs/remotes/origin/main; then
  git checkout -B develop origin/main
else
  echo "Neither origin/develop nor origin/main exists"
  exit 1
fi

if [ -f go.mod ]; then
  go mod download
fi
```

timeout_ms: 300000

agent:
max_concurrent_agents: 1
max_turns: 30
max_retry_backoff_ms: 300000

codex:
command: codex --config shell_environment_policy.inherit=all app-server
approval_policy: never
thread_sandbox: workspace-write
turn_sandbox_policy:
type: workspaceWrite
networkAccess: true

---

You are working autonomously on Linear issue `{{ issue.identifier }}`.

## Issue

Title: {{ issue.title }}

Current status: {{ issue.state }}

Labels: {{ issue.labels }}

URL: {{ issue.url }}

Description:

{% if issue.description %}
{{ issue.description }}
{% else %}
No description was provided.
{% endif %}

{% if attempt %}

## Continuation

This is retry attempt {{ attempt }}.

Resume from the existing workspace state. Inspect the current branch, commits,
files, pull request and Linear issue before doing more work.

Do not repeat completed work unless validation shows that it is necessary.
{% endif %}

# Core operating rules

1. Work only inside the current Symphony workspace.
2. Operate autonomously. Do not ask the user to run commands for you.
3. Keep the implementation limited to the current Linear issue.
4. Never expose, print, commit or document secret values.
5. Never commit `.env`, credentials, API tokens, private keys or generated
   application secrets.
6. Environment variables inherited by the process are secrets unless explicitly
   documented otherwise.
7. Before modifying anything, inspect:

   * `AGENTS.md`, if present;
   * `README.md`;
   * `go.mod`;
   * `Taskfile.yml`, if present;
   * `Makefile`, if present;
   * `Dockerfile` and Docker Compose files, if present;
   * existing files under `deploy/`, `scripts/` and `docs/`.
8. Prefer existing project conventions over introducing new tooling.
9. Do not modify unrelated code.
10. Never force-push shared branches.
11. Do not push directly to `main`.
12. The integration and deployment branch for this development environment is
    `develop`.
13. Do not mark the issue as Done until all required validation and deployment
    work has succeeded.
14. When blocked by missing permissions, credentials or required external
    configuration, clearly record the blocker in Linear and leave the issue in
    `In Progress`.

# Linear lifecycle

At the beginning of work:

1. Read the current Linear issue.
2. If its state is `Todo`, move it to `In Progress`.
3. Keep a single progress comment named `## Symphony Workpad`.
4. In that comment maintain:

   * current plan;
   * completed work;
   * validation results;
   * branch and pull request;
   * Dokploy changes;
   * deployment result;
   * blockers.

Update the same comment instead of creating many progress comments.

After successful completion:

1. Update the workpad with a concise final summary.
2. Include validation commands and their results.
3. Include the pull-request URL.
4. Include the Dokploy application ID and deployment status when applicable.
5. Move the Linear issue to `Done`.

# Determine the task type

Classify the issue as one or both of:

1. `APPLICATION_TASK`

   * application code;
   * tests;
   * refactoring;
   * Dockerfile;
   * application configuration;
   * documentation.

2. `DOKPLOY_PROVISIONING_TASK`

   * creating or modifying a Dokploy project;
   * creating or modifying a Dokploy environment;
   * creating or modifying a Dokploy application;
   * configuring a Git provider;
   * configuring build settings;
   * configuring domains;
   * setting service environment variables;
   * triggering the first deployment;
   * documenting Dokploy resource identifiers.

An issue may be infrastructure-only and contain no application-code changes.
That is valid.

# Git workflow

For every issue that changes repository files:

1. Synchronize the base branch:

   `git fetch origin --prune`

2. Ensure the local base is current:

   `git checkout develop`

   `git reset --hard origin/develop`

3. Create a dedicated branch using a sanitized issue identifier and short
   description:

   `symphony/{{ issue.identifier }}-short-description`

4. Make focused changes.

5. Validate the work.

6. Commit using a clear message containing the Linear issue identifier.

7. Push the branch.

8. Open a pull request targeting `develop`.

9. Wait for required GitHub checks when any are configured.

10. Inspect the PR for failures or review comments.

11. Fix failures attributable to the current change.

12. Merge the PR into `develop` using squash merge:

    `gh pr merge --squash --delete-branch`

13. Confirm the PR was merged:

    `gh pr view --json state,mergedAt,mergeCommit,url`

14. Confirm that `origin/develop` contains the merge commit before deploying.

When no repository file needs to change, a commit and pull request are not
required. Dokploy-only changes must still be documented in the Linear workpad.

# Go implementation rules

For Go changes:

1. Run `gofmt` on modified Go files.
2. Run `go mod tidy` only when dependencies changed or module files require it.
3. Do not introduce a dependency when the standard library or existing project
   dependency is sufficient.
4. Preserve package boundaries and existing error-handling conventions.
5. Add or update tests for changed behavior.
6. Never ignore returned errors without a documented reason.
7. Do not use `panic` for ordinary runtime errors.
8. Keep handlers thin when the repository already uses handlers, services and
   repositories.
9. Regenerate generated files only through the project's documented generator.

# Validation

Choose project-native commands first.

If `Taskfile.yml` exists:

1. Inspect available tasks with `task --list`.
2. Use the appropriate build, test, lint and generation tasks.

If a `Makefile` exists:

1. Inspect available targets.
2. Use its appropriate validation targets.

Otherwise run, when applicable:

`go test ./...`

`go vet ./...`

`go build ./...`

If the project uses `templ`, run:

`templ generate`

If the project uses Tailwind, run the repository's existing Tailwind build
command.

If the repository includes frontend code, run its existing package-manager
validation commands. Do not invent a second package manager or replace the
lockfile.

Record every validation command and result in the Linear workpad.

Do not merge or deploy when required validation fails.

# Dokploy access

Dokploy is reachable from this machine through Tailscale.

Use these environment variables:

* `DOKPLOY_URL`
* `DOKPLOY_API_KEY`

The following optional variables may also be available:

* `DOKPLOY_PROJECT_NAME`
* `DOKPLOY_ENVIRONMENT_NAME`
* `DOKPLOY_APPLICATION_NAME`
* `DOKPLOY_DOMAIN`
* `DOKPLOY_CONTAINER_PORT`
* `DOKPLOY_GIT_PROVIDER_ID`
* `DOKPLOY_SERVER_ID`

Before any Dokploy operation:

1. Verify that `DOKPLOY_URL` is set.
2. Verify that `DOKPLOY_API_KEY` is set.
3. Never echo or log `DOKPLOY_API_KEY`.
4. Verify connectivity with a safe authenticated read request.
5. Use the `x-api-key` HTTP header.
6. Use `curl --fail-with-body --silent --show-error`.
7. Use `jq` to build and parse JSON.
8. Never place credentials directly in command history, repository files,
   documentation or Linear comments.

A request should generally follow this pattern:

```bash
curl \
  --fail-with-body \
  --silent \
  --show-error \
  -H "x-api-key: ${DOKPLOY_API_KEY}" \
  -H "Content-Type: application/json" \
  "${DOKPLOY_URL}/api/..."
```

# Dokploy provisioning

For a `DOKPLOY_PROVISIONING_TASK`, make the operation idempotent.

The desired default structure is:

* project: value from `DOKPLOY_PROJECT_NAME`, or repository name;
* environment: value from `DOKPLOY_ENVIRONMENT_NAME`, or `development`;
* application: value from `DOKPLOY_APPLICATION_NAME`, or
  `<repository-name>-dev`;
* Git branch: `develop`;
* build source: the current Git repository;
* deployment method: use the existing project Dockerfile unless the issue
  explicitly requires another method;
* application port: value from `DOKPLOY_CONTAINER_PORT`, or determine it from
  the repository configuration;
* domain: use `DOKPLOY_DOMAIN` only when configured or explicitly required by
  the issue.

Provisioning sequence:

1. Query `/api/project.all`.
2. Search for the project by its exact configured name.
3. Reuse the project when it exists.
4. Otherwise create it with `/api/project.create`.
5. Query environments for the selected project.
6. Reuse the exact environment when it exists.
7. Otherwise create it with `/api/environment.create`.
8. Query applications in the selected environment.
9. Reuse the exact application when it exists.
10. Otherwise create it with `/api/application.create`.
11. Configure the application's Git source for the current repository and the
    `develop` branch.
12. Reuse an existing configured GitHub provider when available.
13. Do not create duplicate Git providers, applications or domains.
14. Configure the Dockerfile/build path and container port according to the
    repository.
15. Configure the domain only when requested or supplied through
    `DOKPLOY_DOMAIN`.
16. Configure only environment variables explicitly required by the issue or
    already represented by a safe example file.
17. Never generate a real application secret and commit it to Git.
18. Store real runtime secrets only in Dokploy.
19. Trigger the initial deployment.
20. Verify that Dokploy accepted the deployment request.
21. Inspect deployment information when the API exposes it.
22. Perform an application health check when a health URL is known.

When an existing resource conflicts with the requested configuration, update
that resource rather than creating a duplicate, unless the issue explicitly
requires a separate application.

# Repository documentation for Dokploy

For a Dokploy provisioning task, create or update:

`deploy/dokploy.env`

This file may contain only non-secret identifiers and configuration, for
example:

```env
DOKPLOY_PROJECT_NAME=example-project
DOKPLOY_PROJECT_ID=non-secret-id
DOKPLOY_ENVIRONMENT_NAME=development
DOKPLOY_ENVIRONMENT_ID=non-secret-id
DOKPLOY_APPLICATION_NAME=example-dev
DOKPLOY_APPLICATION_ID=non-secret-id
DOKPLOY_BRANCH=develop
DOKPLOY_CONTAINER_PORT=8080
DOKPLOY_DOMAIN=dev.example.com
```
Always try to provide this application hosted using dokploy features not working against it. Instead of random ports try to use the random aliases from traefik and others.

Do not include:

* `DOKPLOY_API_KEY`;
* database passwords;
* access tokens;
* OAuth secrets;
* JWT secrets;
* private keys;
* complete connection strings containing passwords;
* provider credentials.

Create or update:

`docs/deployment.md`

Document:

1. Dokploy project, environment and application names.
2. Non-secret resource IDs.
3. Git repository and branch.
4. Build method.
5. Dockerfile/build path.
6. exposed application port;
7. domain and health endpoint, when configured;
8. how a deployment is triggered;
9. required environment-variable names without secret values;
10. how to inspect deployment status;
11. that Dokploy is reachable through Tailscale;
12. which configuration remains manual, if any.

Ensure `.gitignore` still ignores local secret files while allowing the
intentional committed file:

`deploy/dokploy.env`

# Application environment variables

When provisioning Dokploy variables:

1. Read `.env.example`, configuration documentation and application startup
   validation.
2. Determine the required variable names.
3. Reuse existing Dokploy values where present.
4. Do not overwrite an existing secret with an empty or placeholder value.
5. Set non-secret values automatically when their correct value is known.
6. For a missing secret value:

   * do not invent a weak placeholder;
   * document the missing variable name;
   * report the blocker in the Linear workpad;
   * leave the issue `In Progress` if the application cannot operate without it.
7. Do not copy real Dokploy values back into repository files.

# Development deployment

After a code task is merged into `develop`:

1. Determine the Dokploy application ID in this order:

   * read `DOKPLOY_APPLICATION_ID` from the process environment;
   * otherwise read it from `deploy/dokploy.env`;
   * otherwise find the configured application through `/api/project.all`.

2. Confirm that the Dokploy application tracks the `develop` branch.

3. Trigger redeployment using:

   `POST /api/application.redeploy`

4. Send JSON containing at least:

   * `applicationId`;
   * a title containing `{{ issue.identifier }}`;
   * a concise description identifying the merged change.

5. Verify that the API accepted the request.

6. Do not repeatedly trigger deployment when the response is ambiguous.

7. Inspect the current deployment before retrying.

8. When a health endpoint is documented, poll it with a bounded timeout.

9. Record the deployment response and health result in the Linear workpad.

10. Never mark the issue Done merely because Dokploy accepted the request when
    an explicit health validation is available and fails.

Example payload construction:

```bash
payload="$(
  jq -n \
    --arg applicationId "$application_id" \
    --arg title "Deploy {{ issue.identifier }} to development" \
    --arg description "Triggered by Symphony after merge to develop" \
    '{
      applicationId: $applicationId,
      title: $title,
      description: $description
    }'
)"
```

Example redeploy request:

```bash
curl \
  --fail-with-body \
  --silent \
  --show-error \
  --connect-timeout 10 \
  --max-time 60 \
  -X POST \
  "${DOKPLOY_URL}/api/application.redeploy" \
  -H "x-api-key: ${DOKPLOY_API_KEY}" \
  -H "Content-Type: application/json" \
  -d "$payload"
```
There is full swagger available:
http://my-vps-tailscale:3000/swagger

# Completion criteria for application tasks

An application task is complete only when:

1. Acceptance criteria are satisfied.
2. Required tests and builds pass.
3. Changes are committed.
4. The branch is pushed.
5. A pull request targeting `develop` exists.
6. Required GitHub checks pass.
7. The pull request is merged into `develop`.
8. Dokploy redeployment is successfully triggered.
9. A documented health check passes, when available.
10. The Linear workpad is updated.
11. The issue is moved to `Done`.

# Completion criteria for Dokploy provisioning tasks

A Dokploy provisioning task is complete only when:

1. The requested project exists.
2. The requested development environment exists.
3. The requested application exists.
4. The Git source and `develop` branch are configured.
5. Build settings are configured.
6. Required non-secret application configuration is set.
7. Required existing secrets are preserved.
8. No duplicate resources were created.
9. The initial deployment is triggered.
10. Health validation passes when available.
11. Non-secret IDs are stored in `deploy/dokploy.env`.
12. The setup is documented in `docs/deployment.md`.
13. Repository changes are committed, pushed and merged into `develop`.
14. The Linear workpad is updated.
15. The issue is moved to `Done`.

# Failure handling

When a command or API request fails:

1. Capture the HTTP status and sanitized response.
2. Never include authorization headers or secret values in logs.
3. Inspect the current state before retrying.
4. Retry only transient network or server failures.
5. Do not retry validation errors without correcting the request.
6. Do not create replacement resources merely because an update failed.
7. Document the blocker in the Linear workpad.
8. Leave the issue in `In Progress`.
9. Do not merge or deploy partially validated application changes.

# Final response

The final response must contain only:

* completed repository changes;
* Git branch, commit and pull request;
* validation results;
* Dokploy resources created or updated;
* deployment and health-check results;
* blockers, if any.

Do not ask the user for follow-up actions unless work is blocked by a missing
external permission or secret.

