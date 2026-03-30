# taskrun-ui-config

Use this skill when creating, editing, or reviewing `taskrun-ui.yaml`.

## Load these files first

1. `llms.txt`
2. `config-schema.json`
3. `docs/web-ui.ja.md`

Do not expand to other files unless those three are insufficient.

## Purpose

Support direct editing of `taskrun-ui.yaml` for settings that are intentionally too complex for:

- `runtask ui init`
- `runtask ui edit task`
- `runtask ui edit branch`

Interactive UI commands remain basic setup only. For advanced configuration, edit YAML directly.

## Generation workflow

When asked to create or rewrite `taskrun-ui.yaml`:

1. Read `llms.txt` for generation rules and default-handling expectations.
2. Use `config-schema.json` as the structural contract.
3. Write the first line exactly as:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/shibukawa/vsc-taskrunner/main/config-schema.json
```

4. Prefer omitting fields that are only repeating defaults.
5. Use placeholder secrets such as `${OIDC_CLIENT_SECRET}` and `${SESSION_SECRET}` unless the user provides concrete values.
6. Keep `tasks` patterns in priority order because the first matching pattern wins.
7. If the requested change involves auth, token storage, object storage, artifact capture, pre-run hooks, or sparse worktree execution, edit YAML directly rather than trying to force those through `ui edit`.

## Review workflow

When reviewing an existing `taskrun-ui.yaml`, check:

- Whether branch and task glob ordering matches the intended priority.
- Whether remote repositories have the cache/auth settings needed by Go validation.
- Whether `auth.apiTokens.enabled` is consistent with `auth.noAuth=false`.
- Whether object storage blocks include at least `endpoint` and `bucket`.
- Whether `preRunTask` side effects are intentional and understood.
- Whether `worktreeDisabled` is being used intentionally for sparse execution.
- Whether real secrets were inlined where placeholders or environment references should be used instead.

## Output rules

- Explain risky config changes and why they are needed.
- Mention when a requested change depends on Go validation rather than schema-only validation.
- If you change YAML, preserve or restore the schema comment at the top.
