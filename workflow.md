---
<<<<<<< HEAD

tracker:
  kind: linear
  project_slug: "test-symphony"
=======
tracker:
  kind: linear
  project_slug: "test-symphony-409594645a94"
>>>>>>> main
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
<<<<<<< HEAD
  max_turns: 30
=======
  max_turns: 12
>>>>>>> main
  max_retry_backoff_ms: 300000

codex:
  command: codex --config shell_environment_policy.inherit=all app-server
  approval_policy: never
  thread_sandbox: workspace-write
  turn_sandbox_policy:
    type: workspaceWrite
    networkAccess: true
<<<<<<< HEAD

=======
>>>>>>> main
---

You are working autonomously on Linear issue `{{ issue.identifier }}`.

<<<<<<< HEAD
# Source of truth and task selection

This file is the single, durable source of truth for Symphony's tracker
configuration and delivery process in this repository. Do not create a second
process document. When the Symphony working model, Linear tracker configuration,
or delivery process changes, update this file in the same change that introduces
the new behavior.

Symphony polls the Linear project `test-symphony` every 30 seconds. It may take
an issue only when all of the following are true:

1. The issue belongs to `test-symphony`.
2. It has the `symphony` label.
3. Its state is one of the configured `active_states`: `Todo` or `In Progress`.

Issues in `In Review` or a terminal state are not eligible for implementation.
The `required_labels` and `active_states` values in the front matter are the
executable configuration; this section explains, but does not override, them.

The product scope is defined in [the MVP profitability specification](docs/mvp-profitability.md),
created as part of EZE-10. Read it before making product or domain decisions.

=======
>>>>>>> main
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
<<<<<<< HEAD
files, pull request and Linear issue before doing more work.

Do not repeat completed work unless validation shows that it is necessary.
=======
files, pull request, Linear issue, Symphony Workpad and recent Linear comments
before doing more work.

Do not repeat completed work unless validation shows that it is necessary.

Before taking any action, determine whether the previous run ended because of
an external blocker. If the blocker has not materially changed, follow the
Repeated blocker protection rules and stop immediately.

>>>>>>> main
{% endif %}

# Core operating rules

1. Work only inside the current Symphony workspace.
<<<<<<< HEAD
2. Operate autonomously. Do not ask the user to run commands for you.
3. Keep the implementation limited to the current Linear issue.
=======
2. Operate autonomously. Do not ask the user to run commands unless work is
   blocked by an external permission, secret, credential, configuration value
   or human decision.
3. Keep changes limited to the current Linear issue.
>>>>>>> main
4. Never expose, print, commit or document secret values.
5. Never commit `.env`, credentials, API tokens, private keys or generated
   application secrets.
6. Environment variables inherited by the process are secrets unless explicitly
   documented otherwise.
<<<<<<< HEAD
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
=======
7. Inspect project instructions and existing conventions before changing code.
8. Do not modify unrelated code.
9. Never force-push shared branches.
10. Do not push directly to `main`.
11. The integration and deployment branch is `develop`.
12. Do not mark an issue Done until required validation and deployment succeed.
13. Do not keep an externally blocked issue in an active Symphony state.
14. `Needs Assistance` must not appear in `active_states` or `terminal_states`.
15. Do not retry an unchanged external blocker.
>>>>>>> main

# Linear lifecycle

At the beginning of work:

<<<<<<< HEAD
1. Read the current Linear issue.
2. If its state is `Todo`, move it to `In Progress`.
3. Keep a single progress comment named `## Symphony Workpad`.
4. In that comment maintain:

=======
1. Read the current Linear issue and its comments.
2. Locate the single comment named `## Symphony Workpad`.
3. If the issue is already in `Needs Assistance`, stop immediately.
4. If the issue is `Todo`, move it to `In Progress`.
5. Keep one `## Symphony Workpad` comment containing:
>>>>>>> main
   * current plan;
   * completed work;
   * validation results;
   * branch and pull request;
   * Dokploy changes;
   * deployment result;
   * blockers.
<<<<<<< HEAD

Update the same comment instead of creating many progress comments.
=======
6. Update that comment instead of creating repeated progress comments.
>>>>>>> main

After successful completion:

1. Update the workpad with a concise final summary.
<<<<<<< HEAD
2. Include validation commands and their results.
3. Include the pull-request URL.
4. Include the Dokploy application ID and deployment status when applicable.
5. Move the Linear issue to `Done`.

# Definition of Ready

An issue is ready only when:

1. It meets every task-selection condition above.
2. Its objective, scope, acceptance criteria, and explicit exclusions are clear
   enough to validate a result.
3. Required dependencies, designs, owner decisions, and external access are
   available, or missing items are explicitly identified as blockers.
4. The repository and target branch are known and the requested work does not
   conflict with another active change.
5. Product behavior is consistent with the MVP specification, or the issue
   explicitly requests an approved change to that specification.

If these conditions are not met, do not guess at materially different product
behavior. Record the missing requirement or blocker in the Symphony Workpad and
follow the reporting rules below.

# Required work cycle

For each eligible issue:

1. Read the full issue, its comments and dependencies; inspect the repository
   instructions and relevant implementation before changing files.
2. Move `Todo` to `In Progress` and create or update the single
   `## Symphony Workpad` comment with the plan.
3. Synchronize `develop` and create `symphony/<issue>-<short-description>`.
4. Implement only the issue scope using current project conventions.
5. Run the project-native generation, tests, vet/lint, and build checks relevant
   to the change; record every command and result in the Workpad.
6. Commit a focused, reviewable change with the Linear identifier in the commit
   message. Never include secrets or unrelated files.
7. Push the issue branch and open a pull request targeting `develop`, linking
   the Linear issue and explaining the change and validation.
8. Wait for configured checks, inspect failures and review comments, and fix
   problems attributable to the change.
9. Squash-merge the approved PR, confirm `origin/develop` contains the merge,
   and perform the configured development deployment and health check.
10. Add the final evidence, PR, deployment, and health status to the same
    Workpad; move the issue to `Done` only after the Definition of Done is met.

# Owner decisions (`decision-required`)

An issue carrying `decision-required` requests analysis, not dependent
implementation. Symphony must:

1. Investigate enough to describe the decision and its constraints.
2. Prepare two or more viable options with consequences, risks, cost, and a
   clear recommendation when evidence supports one.
3. Post the options in Linear, preferably in the existing Symphony Workpad,
   naming the exact owner decision needed.
4. Move the issue to `In Review` and stop all implementation that depends on the
   answer. Independent, already-authorized work may continue only if it cannot
   prejudice the decision.
5. Resume dependent work only after the owner records a decision and the issue
   returns to a configured active state.

# Reporting errors, blockers, and incomplete requirements

- Put the sanitized command or operation, observed result, impact, and next safe
  step in `## Symphony Workpad`; never include credentials or secret values.
- Retry only transient failures. For validation or configuration errors,
  inspect current state and correct the cause before retrying.
- For incomplete or contradictory requirements, identify the exact gap and the
  affected acceptance criterion. If a materially different owner choice is
  required, apply `decision-required` or request that it be applied, document
  options, move to `In Review`, and pause dependent work.
- For missing permissions, credentials, or external configuration, document
  the missing capability and evidence, keep the issue `In Progress`, and do not
  merge, deploy, or mark partially validated work as complete.
- Keep one Workpad current rather than creating a stream of progress comments.
=======
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
>>>>>>> main

# Determine the task type

Classify the issue as one or both of:

1. `APPLICATION_TASK`
<<<<<<< HEAD

=======
>>>>>>> main
   * application code;
   * tests;
   * refactoring;
   * Dockerfile;
   * application configuration;
   * documentation.

2. `DOKPLOY_PROVISIONING_TASK`
<<<<<<< HEAD

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

=======
   * creating or modifying a Dokploy project, environment or application;
   * configuring a Git provider, build settings, domains or variables;
   * triggering deployment;
   * documenting Dokploy resource identifiers.

>>>>>>> main
# Git workflow

For every issue that changes repository files:

<<<<<<< HEAD
1. Synchronize the base branch:

   `git fetch origin --prune`

2. Ensure the local base is current:
=======
1. Run `git fetch origin --prune`.
2. Reset the base branch:
>>>>>>> main

   `git checkout develop`

   `git reset --hard origin/develop`

<<<<<<< HEAD
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

# Go implementation rules

For Go changes:

1. Run `gofmt` on modified Go files.
<<<<<<< HEAD
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
=======
2. Run `go mod tidy` only when dependencies changed.
3. Prefer the standard library and existing dependencies.
4. Preserve package and error-handling conventions.
5. Add or update tests for changed behavior.
6. Never ignore returned errors without a documented reason.
7. Do not use `panic` for ordinary runtime errors.
8. Regenerate generated files only through documented generators.
>>>>>>> main

# Validation

Choose project-native commands first.

<<<<<<< HEAD
If `Taskfile.yml` exists:

1. Inspect available tasks with `task --list`.
2. Use the appropriate build, test, lint and generation tasks.

If a `Makefile` exists:

1. Inspect available targets.
2. Use its appropriate validation targets.

Otherwise run, when applicable:
=======
If `Taskfile.yml` exists, inspect `task --list` and use the relevant tasks.
If a `Makefile` exists, inspect and use its relevant targets.

Otherwise run when applicable:
>>>>>>> main

`go test ./...`

`go vet ./...`

`go build ./...`

<<<<<<< HEAD
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
=======
If the project uses `templ`, run `templ generate`.
If it uses Tailwind or frontend tooling, use the repository's existing commands.

Record each validation command and result in the Linear workpad.
Do not merge or deploy when required validation fails.
Do not rerun unchanged validation during continuation unless relevant state
changed.

# Dokploy access

Dokploy is reachable through Tailscale.

Required environment variables:
>>>>>>> main

* `DOKPLOY_URL`
* `DOKPLOY_API_KEY`

<<<<<<< HEAD
The following optional variables may also be available:
=======
Optional variables may include:
>>>>>>> main

* `DOKPLOY_PROJECT_NAME`
* `DOKPLOY_ENVIRONMENT_NAME`
* `DOKPLOY_APPLICATION_NAME`
* `DOKPLOY_DOMAIN`
* `DOKPLOY_CONTAINER_PORT`
* `DOKPLOY_GIT_PROVIDER_ID`
* `DOKPLOY_SERVER_ID`
<<<<<<< HEAD

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
=======
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
>>>>>>> main

```bash
curl \
  --fail-with-body \
  --silent \
  --show-error \
  --connect-timeout 10 \
  --max-time 60 \
<<<<<<< HEAD
  -X POST \
  "${DOKPLOY_URL}/api/application.redeploy" \
  -H "x-api-key: ${DOKPLOY_API_KEY}" \
  -H "Content-Type: application/json" \
  -d "$payload"
```
There is full swagger available:
http://my-vps-tailscale:3000/swagger

# Completion criteria for application tasks

# Definition of Done

An issue is done only when all applicable conditions hold:

1. Every acceptance criterion is satisfied and no out-of-scope changes remain.
2. Required generated artifacts are current and all relevant tests, vet/lint,
   and builds pass.
3. The change has a focused commit on an issue branch and is pushed to the
   remote without secrets.
4. A pull request targeting `develop` documents the issue, change, and
   validation; required checks and review feedback are resolved.
5. The PR is squash-merged and the merge is confirmed on `origin/develop`.
6. Applicable Dokploy deployment succeeds and the documented health check
   passes.
7. The single Symphony Workpad contains the final validation commands, PR URL,
   deployment result, non-secret resource IDs when applicable, and no unresolved
   blocker.
8. Linear is moved to `Done` only after conditions 1-7 are met. For a
   no-repository-change or infrastructure-only issue, omit only the steps that
   genuinely do not apply and record why.

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
=======
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
>>>>>>> main

# Failure handling

When a command or API request fails:

<<<<<<< HEAD
1. Capture the HTTP status and sanitized response.
2. Never include authorization headers or secret values in logs.
3. Inspect the current state before retrying.
4. Retry only transient network or server failures.
5. Do not retry validation errors without correcting the request.
6. Do not create replacement resources merely because an update failed.
7. Document the blocker in the Linear workpad.
8. Leave the issue in `In Progress`.
9. Do not merge or deploy partially validated application changes.
=======
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
>>>>>>> main

# Final response

The final response must contain only:

* completed repository changes;
* Git branch, commit and pull request;
* validation results;
* Dokploy resources created or updated;
* deployment and health-check results;
<<<<<<< HEAD
* blockers, if any.

Do not ask the user for follow-up actions unless work is blocked by a missing
external permission or secret.
=======
* blockers, if any;
* exact human action required when moved to `Needs Assistance`.
>>>>>>> main
