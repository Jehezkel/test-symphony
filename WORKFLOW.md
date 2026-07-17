---
tracker:
  kind: linear
  project_slug: "test-symphony-409594645a94"
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
  timeout_ms: 300000

agent:
  max_concurrent_agents: 1
  max_turns: 12
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
files, pull request, Linear issue, Symphony Workpad and recent Linear comments
before doing more work.

Do not repeat completed work unless validation shows that it is necessary.

Before taking any action, determine whether the previous run ended because of
an external blocker. If the blocker has not materially changed, follow the
Repeated blocker protection rules and stop immediately.

{% endif %}

# Core operating rules

1. Work only inside the current Symphony workspace.
2. Operate autonomously. Do not ask the user to run commands unless work is
   blocked by an external permission, secret, credential, configuration value
   or human decision.
3. Keep changes limited to the current Linear issue.
4. Never expose, print, commit or document secret values.
5. Never commit `.env`, credentials, API tokens, private keys or generated
   application secrets.
6. Environment variables inherited by the process are secrets unless explicitly
   documented otherwise.
7. Inspect project instructions and existing conventions before changing code.
8. Do not modify unrelated code.
9. Never force-push shared branches.
10. Do not push directly to `main`.
11. The integration and deployment branch is `develop`.
12. Do not mark an issue Done until required validation and deployment succeed.
13. Do not keep an externally blocked issue in an active Symphony state.
14. `Needs Assistance` must not appear in `active_states` or `terminal_states`.
15. Do not retry an unchanged external blocker.

# Linear lifecycle

At the beginning of work:

1. Read the current Linear issue and its comments.
2. Locate the single comment named `## Symphony Workpad`.
3. If the issue is already in `Needs Assistance`, stop immediately.
4. If the issue is `Todo`, move it to `In Progress`.
5. Keep one `## Symphony Workpad` comment containing:
   * current plan;
   * completed work;
   * validation results;
   * branch and pull request;
   * Dokploy changes;
   * deployment result;
   * blockers.
6. Update that comment instead of creating repeated progress comments.

After successful completion:

1. Update the workpad with a concise final summary.
2. Include validation commands and results.
3. Include the pull-request URL.
4. Include Dokploy application ID and deployment status when applicable.
5. Move the issue to `Done`.

# Human assistance lifecycle

A human-assistance blocker includes:

* missing permissions;
* missing credentials or secrets;
* missing external configuration;
* unavailable third-party services;
* a required account, domain, certificate or provider configuration;
* a decision that cannot safely be inferred;
* any required action outside Symphony's authorized scope.

When such a blocker is found:

1. Verify that no safe in-scope action can resolve it.
2. Do not retry the same unchanged blocker.
3. Update `## Symphony Workpad` with:
   * what is blocked;
   * why Symphony cannot resolve it;
   * the exact human action required;
   * how work should be resumed;
   * branch and PR containing completed work, if any.
4. Create exactly one new top-level Linear comment beginning with:

   `🚨 HUMAN ASSISTANCE REQUIRED`

5. Include the issue identifier, concise blocker description, required action,
   and an instruction to move the issue back to `Todo` or `In Progress` after
   resolving it. Never include secret values.
6. Mention the issue assignee when possible.
7. Move the issue to `Needs Assistance`.
8. Stop immediately.
9. Do not perform more validation, deployment checks, infrastructure reads or
   continuation turns after moving the issue.

Issues in `Needs Assistance` must never be resumed automatically.

# Repeated blocker protection

Before work during a continuation or retry:

1. Read the Symphony Workpad and recent Linear comments.
2. Determine whether the previous run ended because of an external blocker.
3. Check only the minimum state needed to see whether it materially changed.
4. If unchanged:
   * do not rerun tests or builds;
   * do not repeat infrastructure audits;
   * do not append another long blocker report;
   * create the assistance comment only if it does not exist;
   * move the issue to `Needs Assistance` if still active;
   * stop immediately.
5. Never consume multiple continuation turns describing the same blocker.

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
   * creating or modifying a Dokploy project, environment or application;
   * configuring a Git provider, build settings, domains or variables;
   * triggering deployment;
   * documenting Dokploy resource identifiers.

# Git workflow

For every issue that changes repository files:

1. Run `git fetch origin --prune`.
2. Reset the base branch:

   `git checkout develop`

   `git reset --hard origin/develop`

3. Create `symphony/{{ issue.identifier }}-short-description`.
4. Make focused changes.
5. Validate the work.
6. Commit with the Linear issue identifier.
7. Push the branch.
8. Open a pull request targeting `develop`.
9. Wait for configured checks and inspect review comments.
10. Fix failures attributable to the change.
11. Merge using squash only when safe deployment configuration is available:

    `gh pr merge --squash --delete-branch`

12. Confirm merge state and that `origin/develop` contains the merge commit.

If completed code cannot be safely merged or deployed because of an external
blocker, preserve the branch and PR, document them, move the issue to
`Needs Assistance`, and stop.

# Go implementation rules

For Go changes:

1. Run `gofmt` on modified Go files.
2. Run `go mod tidy` only when dependencies changed.
3. Prefer the standard library and existing dependencies.
4. Preserve package and error-handling conventions.
5. Add or update tests for changed behavior.
6. Never ignore returned errors without a documented reason.
7. Do not use `panic` for ordinary runtime errors.
8. Regenerate generated files only through documented generators.

# Validation

Choose project-native commands first.

If `Taskfile.yml` exists, inspect `task --list` and use the relevant tasks.
If a `Makefile` exists, inspect and use its relevant targets.

Otherwise run when applicable:

`go test ./...`

`go vet ./...`

`go build ./...`

If the project uses `templ`, run `templ generate`.
If it uses Tailwind or frontend tooling, use the repository's existing commands.

Record each validation command and result in the Linear workpad.
Do not merge or deploy when required validation fails.
Do not rerun unchanged validation during continuation unless relevant state
changed.

# Dokploy access

Dokploy is reachable through Tailscale.

Required environment variables:

* `DOKPLOY_URL`
* `DOKPLOY_API_KEY`

Optional variables may include:

* `DOKPLOY_PROJECT_NAME`
* `DOKPLOY_ENVIRONMENT_NAME`
* `DOKPLOY_APPLICATION_NAME`
* `DOKPLOY_DOMAIN`
* `DOKPLOY_CONTAINER_PORT`
* `DOKPLOY_GIT_PROVIDER_ID`
* `DOKPLOY_SERVER_ID`
* `DOKPLOY_APPLICATION_ID`

Before Dokploy operations:

1. Verify required variables are set without printing values.
2. Verify connectivity with one safe authenticated read.
3. Use the `x-api-key` header.
4. Use `curl --fail-with-body --silent --show-error` with bounded timeouts.
5. Use `jq` to build and parse JSON.
6. Never place credentials in history, files, docs or Linear comments.
7. Do not repeatedly poll when an external blocker is known.

Example:

```bash
curl \
  --fail-with-body \
  --silent \
  --show-error \
  --connect-timeout 10 \
  --max-time 60 \
  -H "x-api-key: ${DOKPLOY_API_KEY}" \
  -H "Content-Type: application/json" \
  "${DOKPLOY_URL}/api/..."
```

# Dokploy provisioning

Provisioning must be idempotent:

1. Reuse an existing project, environment, application, Git provider and domain
   when they match the requested configuration.
2. Otherwise create only the missing resource.
3. Track `develop` and use the repository Dockerfile unless requested otherwise.
4. Configure only known non-secret values and required existing secrets.
5. Never generate or commit real secrets.
6. Store real runtime secrets only in Dokploy.
7. Trigger deployment only after required configuration is available.
8. Verify deployment acceptance and run a bounded health check when known.
9. Do not create duplicate replacement resources after a failed update.

# Repository documentation for Dokploy

For provisioning tasks, create or update `deploy/dokploy.env` with non-secret
resource names, IDs, branch, port and domain.

Never include API keys, passwords, tokens, private keys, OAuth/JWT secrets or
credential-bearing connection strings.

Create or update `docs/deployment.md` with resource names and IDs, repository,
branch, build method, Dockerfile path, port, domain, health endpoint, deployment
procedure, required variable names and manual configuration.

Prefer Dokploy-native routing and generated aliases over unmanaged random ports.

# Application environment variables

When provisioning variables:

1. Read `.env.example`, documentation and startup validation.
2. Determine required variable names.
3. Reuse existing values.
4. Never overwrite a secret with empty or placeholder data.
5. Set known non-secret values automatically.
6. For a missing required secret or external value:
   * do not invent a placeholder;
   * document the missing name;
   * update the workpad;
   * create one `🚨 HUMAN ASSISTANCE REQUIRED` comment;
   * move the issue to `Needs Assistance`;
   * stop immediately.
7. Never copy real Dokploy values into repository files.

Do not retry until a human provides the missing configuration and reactivates
the issue.

# Development deployment

After code is merged into `develop`:

1. Determine application ID from process environment, `deploy/dokploy.env`, or
   a safe Dokploy lookup.
2. Confirm the application tracks `develop`.
3. Confirm all required runtime configuration is present.
4. Trigger `POST /api/application.redeploy`.
5. Include application ID, issue identifier and concise description.
6. Verify acceptance.
7. Do not repeatedly trigger ambiguous deployments.
8. Inspect current deployment before retrying.
9. Poll known health endpoints with bounded timeout.
10. Record deployment and health results in the workpad.
11. Do not mark Done when health validation fails.
12. If deployment needs missing secrets, external configuration or valid TLS,
    follow the Human assistance lifecycle and stop.

# Completion criteria

An application task is complete only when acceptance criteria pass, validation
passes, changes are committed and pushed, a PR exists and required checks pass,
the PR is merged into `develop`, Dokploy redeployment succeeds, available health
checks pass, the workpad is updated and the issue is moved to `Done`.

A provisioning task is complete only when requested resources and settings
exist without duplicates, deployment and health checks succeed, non-secret IDs
and deployment documentation are committed and merged, the workpad is updated
and the issue is moved to `Done`.

When completion cannot proceed because of an external blocker, preserve
completed work, move to `Needs Assistance`, and stop instead of retrying.

# Failure handling

When a command or API request fails:

1. Capture sanitized status and response.
2. Never log authorization headers or secret values.
3. Inspect current state before retrying.
4. Retry only transient network or server failures.
5. Retry a transient failure at most twice.
6. Do not retry validation errors without correcting the request.
7. Do not create replacement resources because an update failed.
8. Do not merge or deploy partially validated changes.

For an external blocker requiring human action:

1. Update the Symphony Workpad.
2. Create one top-level `🚨 HUMAN ASSISTANCE REQUIRED` comment.
3. Describe the exact action required.
4. Move the issue to `Needs Assistance`.
5. Stop immediately.
6. Do not continue retries or audits for the unchanged blocker.

If the Linear comment or status mutation fails, retry that mutation once, record
the sanitized failure when possible, then stop all further work.

# Final response

The final response must contain only:

* completed repository changes;
* Git branch, commit and pull request;
* validation results;
* Dokploy resources created or updated;
* deployment and health-check results;
* blockers, if any;
* exact human action required when moved to `Needs Assistance`.
