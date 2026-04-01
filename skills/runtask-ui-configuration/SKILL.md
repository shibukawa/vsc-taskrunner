---
name: runtask-ui-configuration
description: 'Create, review, and update runtask Web UI configuration in runtask-ui.yaml across any runtask-enabled repository. Use when directly editing branches, exposed tasks, preRunTask, artifacts, worktree retention, auth, OIDC, API tokens, storage backend, or repository settings for runtask ui.'
argument-hint: 'Direct-edit goal, for example: enable OIDC and API tokens, add build artifacts, switch storage to object, or expose release/* only'
user-invocable: true
---

# Runtask UI Configuration

Use this skill when you need to create or change runtask Web UI configuration in runtask-ui.yaml.

This skill is for direct YAML editing only. It does not use interactive helper commands as the primary path.

This skill is intended for any repository that uses runtask Web UI, not only this repository.

It does not redesign .vscode/tasks.json unless the requested UI change cannot work with the current task definitions.

## When to Use

- Create an initial runtask-ui.yaml for a repository
- Expose or restrict branches in the Web UI
- Expose specific tasks or add per-task settings
- Add preRunTask commands such as npm ci before build or test
- Configure artifacts, history retention, and worktree retention
- Switch between local storage and object storage
- Configure noAuth for local development or OIDC for shared environments
- Enable API tokens for automation
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

5. Task exposure model
   - Small allowlist: add exact task labels under tasks.
   - Pattern-based policy: use task-label glob patterns and remember that the first matching pattern wins.
   - Build-like tasks needing clean dependency setup: add preRunTask.
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
   - artifacts.path should point at files or directories produced by the main task.
   - historyKeepCount on a task overrides storage.historyKeepCount.
   - worktree.disabled does not disable the task; it switches to sparse tasks-only execution.

6. Validate cross-field rules.
   - If repository.source is remote, repository.cachePath is still required by Go validation.
   - If auth.apiTokens.enabled is true, auth.noAuth must be false.
   - publicURL must be an absolute http or https URL when used for external access or OIDC callbacks.
   - For object storage, endpoint and bucket are required.

7. Finish with a review checklist.
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

## Output Expectations

When using this skill, produce:

- A proposed or updated runtask-ui.yaml
- A short explanation of why each non-default section is present
- Any unresolved assumptions, especially around auth, storage, or task labels
- A concise validation summary listing the cross-field checks that were applied

## Examples

- Create a minimal local-development config with noAuth and one build task.
- Add OIDC, adminUsers, and API token issuance for a shared deployment.
- Convert a local-history setup to object storage while keeping task-level artifacts.
- Add preRunTask and artifact rules for a frontend build task.
- Restrict Web UI execution to main and release/* branches only.