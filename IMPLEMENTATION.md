# Implementation Rules

## Goals

- Keep the codebase organized by target rather than by feature slice where practical.
- Preserve external behavior while improving internal structure.
- Centralize target metadata so CLI, detection, and matcher registration do not drift.

## Target-Oriented Layout

- Put target-specific logic in a single file when the scope is manageable.
- Use file names like `lang_go.go`, `lang_typescript.go`, `lang_java_gradle.go`, and `lang_java_maven.go`.
- Keep shared helpers in neutral files such as `templates.go`, `templates_shared.go`, `problem_matcher.go`, or registry files.

## What Belongs In a Target File

- Task template builders such as `NewGoTasks` or `NewMavenTasks`.
- Target discovery helpers such as `FindGoModules` or `FindMavenProjects`.
- Built-in problem matcher definitions for that target.
- Candidate collection helpers used by detect flows.
- Target-specific tests in matching `lang_*.go` test files.

## Central Registries

- `internal/tasks/task_targets.go` is the source of truth for task targets.
- Add target metadata there first, then have other entry points consume it.
- The registry should drive at least these concerns:
  - detect candidate collection
  - built-in problem matcher aggregation
  - CLI add target definitions when the target supports `build` and `test`

## CLI Rules

- CLI command names may differ from internal target names.
- Keep aliases explicit. Current examples:
  - `gradle` command maps to `java-gradle`
  - `maven` command maps to `java-maven`
- Do not duplicate target metadata inside `internal/cli` if it already exists in `internal/tasks`.
- Subcommand routing should prefer registry-based dispatch over long `switch` statements.

## Behavior Preservation

- Refactors should not change saved task formats unless the task explicitly requires that change.
- Problem matcher IDs such as `$go`, `$tsc`, `$gradle`, and `$maven` must remain stable unless there is a deliberate compatibility change.
- Sort order in detect output should remain stable.

## Tests

- Co-locate target-specific tests with the same target naming convention.
- When adding a new target, add or update tests for:
  - task template defaults
  - detect candidate generation if applicable
  - CLI add integration if the target is exposed by `add`
  - matcher aggregation if the target introduces built-in matchers

## Refactoring Guidelines

- Prefer moving logic to the correct target file over introducing one-off indirection.
- Prefer registries when the same target metadata is used by multiple subsystems.
- Keep public behavior stable first, then simplify internal structure.