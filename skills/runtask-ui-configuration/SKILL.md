---
name: runtask-ui-configuration
description: 'Create, review, and update runtask Web UI configuration in runtask-ui.yaml across any runtask-enabled repository. Use when directly editing branches, exposed tasks, preRunTask, schedules, heartbeat-based serverless execution, artifacts, worktree retention, auth, OIDC, API tokens, storage backend, or repository settings for runtask ui.'
argument-hint: 'Direct-edit goal, for example: enable OIDC and API tokens, add build artifacts, switch storage to object, or expose release/* only'
user-invocable: true
---

# Runtask UI Configuration

Use this skill when you need to create or change runtask Web UI configuration in runtask-ui.yaml.

This skill is for direct YAML editing only. It does not use interactive helper commands as the primary path.

This skill is intended for any repository that uses runtask Web UI, not only this repository.

It does not redesign .vscode/tasks.json unless the requested UI change cannot work with the current task definitions.

Important: runtime mode is not configured in `runtask-ui.yaml`. It is selected with the `runtask ui` startup flag `--runtime-mode=always-on|serverless`.

## When to Use

- Create an initial runtask-ui.yaml for a repository
- Expose or restrict branches in the Web UI
- Expose specific tasks or add per-task settings
- Add preRunTask commands such as npm ci before build or test
- Add schedules for cron-based execution and fixed task inputValues
- Configure artifacts, history retention, and worktree retention
- Switch between local storage and object storage
- Configure noAuth for local development or OIDC for shared environments
- Enable API tokens for automation
- Configure serverless heartbeat-driven schedule execution
- Document or adjust the `runtask ui --runtime-mode` startup flag alongside YAML changes
- Review a proposed runtask-ui.yaml for correctness and missing fields

## Decision Points

Before editing, determine these choices:

1. Initial setup or update
   - If the file does not exist, write the YAML directly unless the user explicitly asks for init-command output.
   - Treat direct YAML editing as the default path for both simple and advanced changes.

2. Repository type
   - Local repository path: repository.source is enough for the minimum config.
   - Remote repository URL: also require repository.cachePath.
   - Remote private repository: also configure repository.auth.

3. Authentication mode
   - Local verification only: auth.noAuth: true.
   - Shared environment: configure OIDC and sessionSecret.
   - API automation: if apiTokens.enabled is true, auth.noAuth must be false.

4. Storage mode
   - Local service with disk persistence: storage.backend: local is usually enough.
   - Serverless or shared artifact retention: use storage.backend: object and configure object storage fields.
   - If schedules must run in serverless mode, plan an external caller for /api/heartbeat.

5. Runtime mode at process startup
   - `runtask ui --runtime-mode=always-on`: enable the global `/api/runs/stream` SSE feed for immediate shared-history updates.
   - `runtask ui --runtime-mode=serverless`: disable the global run stream and rely on conditional polling of `/api/runs`.
   - Do not try to put runtime mode into YAML; it is a CLI startup flag.

6. Task exposure model
   - Small allowlist: add exact task labels under tasks.
   - Pattern-based policy: use task-label glob patterns and remember that the first matching pattern wins.
   - Build-like tasks needing clean dependency setup: add preRunTask.
   - Timed execution: add schedules with cron, branch, and optional inputValues.
   - Tasks that should not expand the full repository: set worktree.disabled: true.

## Workflow

1. Inspect the current state.
   - Check whether runtask-ui.yaml already exists.
   - Read config-schema.json when you need exact field names or shape validation.
   - Review existing tasks in .vscode/tasks.json if task labels or artifact paths are uncertain.

2. Work in YAML, not helper commands.
   - Edit runtask-ui.yaml directly even for branch and task exposure changes.
   - Use helper commands only if the user explicitly asks for them.

3. Preserve the schema comment.
   - Ensure the first line is exactly:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/shibukawa/vsc-taskrunner/main/config-schema.json
```

4. Build the configuration top-down.
   - repository: always set source first.
   - server: set host, port, and publicURL when the service is exposed externally.
   - auth: choose noAuth or OIDC, then add adminUsers and apiTokens if needed.
   - branches: limit to the branch patterns the UI should allow.
   - tasks: expose only the tasks intended for UI execution; add per-task overrides only where needed.
   - execution, metrics, logging, storage: add only non-default fields unless the user wants an explicit template.

5. Configure tasks carefully.
   - preRunTask runs once before the selected parent task.
   - preRunTask failure stops the parent task from starting.
   - schedules are attached to task patterns and still follow first-match-wins behavior.
   - schedules.branch must also be allowed by the top-level branches list.
   - schedules use the server's local time zone.
   - In serverless mode, schedules are evaluated only when /api/heartbeat is called.
   - artifacts.path should point at files or directories produced by the main task.
   - artifacts.nameTemplate may include {input:NAME} for sanitized task input values.
   - historyKeepCount on a task overrides storage.historyKeepCount.
   - worktree.disabled does not disable the task; it switches to sparse tasks-only execution.

7. Validate cross-field rules.
   - If repository.source is remote, repository.cachePath is still required by Go validation.
   - If auth.apiTokens.enabled is true, auth.noAuth must be false.
   - publicURL must be an absolute http or https URL when used for external access or OIDC callbacks.
   - For object storage, endpoint and bucket are required.
   - schedules.branch must match the branch allowlist.
   - /api/heartbeat is public and only evaluates YAML-defined schedules; it is not a generic task-start endpoint.
   - If documentation or deployment examples mention serverless vs always-on behavior, include the matching `runtask ui --runtime-mode=...` command.

8. Finish with a review checklist.
   - Remove fields that only restate defaults unless explicit verbosity is desired.
   - Replace real secrets with placeholders such as ${OIDC_CLIENT_SECRET}.
   - Keep task patterns in priority order because first match wins.
   - Confirm branch patterns and task labels match what the repository actually uses.

## Quality Checks

- The file name is runtask-ui.yaml.
- The schema comment is present on the first line.
- The YAML uses only supported top-level keys: server, repository, auth, branches, tasks, execution, metrics, logging, storage.
- repository.source is present.
- Remote repository configs also include cachePath.
- Shared environments do not use noAuth.
- API token setups do not combine apiTokens.enabled with noAuth: true.
- Artifact paths and preRunTask cwd values are plausible for the repository layout.
- Task settings are attached to real task labels or intended glob patterns.
- Schedule definitions use standard 5-field cron syntax.
- Serverless schedule deployments also document an external heartbeat caller.
- Runtime mode guidance, when relevant, uses the CLI startup flag instead of inventing a YAML field.

## Output Expectations

When using this skill, produce:

- A proposed or updated runtask-ui.yaml
- A short explanation of why each non-default section is present
- Any unresolved assumptions, especially around auth, storage, or task labels
- Any required startup command changes such as `runtask ui --runtime-mode=always-on` or `--runtime-mode=serverless`
- A concise validation summary listing the cross-field checks that were applied

## Examples

- Create a minimal local-development config with noAuth and one build task.
- Add OIDC, adminUsers, and API token issuance for a shared deployment.
- Convert a local-history setup to object storage while keeping task-level artifacts.
- Add preRunTask and artifact rules for a frontend build task.
- Add schedules for a daily build on main with fixed inputValues.
- Restrict Web UI execution to main and release/* branches only.
