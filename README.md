# runtask

runtask is a CLI that aims to be compatible with VS Code tasks.json. It reads tasks.json files, lists tasks, shows dry-run output, runs tasks, and helps you add new ones.

It currently supports two families of tasks.

- custom tasks: `shell` and `process`
- provider-like tasks: `npm`, `typescript`, `gulp`, `grunt`, and `jake`

Provider-like tasks keep a VS Code compatible task definition when saved, and are resolved into concrete commands only at runtime.

## Basics

### List tasks

```sh
runtask list
runtask list --json
```

`list --json` also includes `group`, `workspaceRoot`, `taskFilePath`, and `sourceTaskId`.

### Dry-run a task

```sh
runtask npm-test --dry-run
```

Text dry-run output also shows the task group. `run --json --dry-run` includes `workspaceRoot`, `taskFilePath`, and `sourceTaskId` in the resolved task.

### Run a task

```sh
runtask go-test
runtask tsc-build-tsconfig.json
```

You can omit the `run` subcommand. `runtask run <task-name>` still works for compatibility.

### Add a task

Interactive custom task creation:

```sh
runtask add
```

Inspect detected tasks:

```sh
runtask add detect
runtask add detect --json
```

Save detected tasks:

```sh
runtask add detect --save --ecosystem npm
runtask add detect --save --label npm-test
runtask add detect --save --ecosystem gulp --all
```

## Add commands

### npm

```sh
runtask add npm --all
runtask add npm --task test
runtask add npm --path web --task build,test
```

- Detects `package.json`
- Saves tasks as `type: npm` plus `script`
- Resolves `npm`, `yarn`, or `pnpm` at runtime based on lockfiles and the `packageManager` field

### typescript

```sh
runtask add typescript
runtask add typescript --all
runtask add typescript --tsconfig packages/app/tsconfig.json --task build,watch
```

- Detects `tsconfig*.json`
- Adds `build` by default when no explicit mode is given
- Adds both `build` and `watch` for every config with `--all`
- Marks `build` tasks with the `build` group
- Automatically assigns `$tsc` or `$tsc-watch` at runtime

### gulp, grunt, jake

```sh
runtask add gulp --all
runtask add grunt --task build
runtask add jake --file tools/Jakefile --all
```

- Detects supported files:
  - gulp: `gulpfile.js`, `gulpfile.cjs`, `gulpfile.mjs`, `gulpfile.ts`
  - grunt: `Gruntfile.js`, `Gruntfile.cjs`, `Gruntfile.mjs`, `Gruntfile.ts`
  - jake: `Jakefile`, `Jakefile.js`, `Jakefile.cjs`, `Jakefile.mjs`, `Jakefile.ts`
- Extracts task names with simple static parsing:
  - gulp: `gulp.task('name', ...)`
  - grunt: `grunt.registerTask('name', ...)`
  - jake: `task('name', ...)`
- Saves them as provider-like task definitions

### go, rust, swift, gradle, maven

```sh
runtask add go
runtask add rust --path crates/core --task test
runtask add gradle --all
runtask add maven --path server
```

- Generates `process` tasks rather than provider tasks
- Adds both `build` and `test` by default
- Marks `build` or `watch` with the `build` group, and `test` with the `test` group
- Sets `options.cwd` to the detected project root
- Applies built-in problem matchers by default for rust, swift, gradle, and maven
- Uses matcher arrays for rust, gradle, and maven so panic output and common Kotlin compiler output are also covered

## Task naming and shortcuts

Generated task labels now use canonical hyphenated names.

- `npm-test`
- `npm-build-web`
- `go-build`
- `go-test`
- `tsc-build-tsconfig.json`
- `gulp-build`

Task lookup follows this order.

1. Exact label match
2. Default `build` or `test` task when you run `runtask build` or `runtask test`
3. A unique shorthand such as `runtask lint`

If a shorthand matches more than one task, runtask prints the matching candidates instead of guessing.

## Default build and test tasks

When `add` generates build or test tasks, runtask checks the existing tasks.json first.

- If no default task exists for that group, the first generated task in that group is saved with `isDefault: true`
- If a default task already exists, the new task is saved without `isDefault: true`
- In that case, runtask prints an English warning message

This check is not limited to one ecosystem. A default npm build task will block a newly generated Go build task from becoming the default build task, and vice versa.

## Support matrix

| Ecosystem | Detect | Add | Saved form | Run | Default matcher | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| shell/process | No | Interactive only | shell/process | Yes | Only when explicitly set | Custom tasks |
| npm | Yes | Yes | provider-like | Yes | None | Per-script, supports `--all` |
| typescript | Yes | Yes | provider-like | Yes | `$tsc`, `$tsc-watch` | Generates `build` and `watch` |
| gulp | Yes | Yes | provider-like | Yes | None | Task names are extracted from files |
| grunt | Yes | Yes | provider-like | Yes | None | Task names are extracted from files |
| jake | Yes | Yes | provider-like | Yes | None | Task names are extracted from files |
| go | Yes | Yes | process | Yes | `$go` | `go build ./...`, `go test ./...` |
| rust | Yes | Yes | process | Yes | `$cargo`, `$cargo-panic` | `cargo build`, `cargo test` |
| swift | Yes | Yes | process | Yes | `$swift` | `swift build`, `swift test` |
| gradle | Yes | Yes | process | Yes | `$gradle`, `$gradle-kotlin` | Prefers `gradlew` |
| maven | Yes | Yes | process | Yes | `$maven`, `$maven-kotlin` | Prefers `mvnw` |

## Compatibility notes

- Assumes a single workspace
- Does not support `command:*`, `config:*`, or `file*` variables yet
- Auto-detection is a one-shot CLI scan, not a resident VS Code task provider
- Provider-like task handling is not a full implementation of VS Code `provideTasks`; it is focused on detection and persistence from the CLI
- `gulp`, `grunt`, and `jake` task extraction is intentionally simple static parsing and does not execute JavaScript
- Matchers for `cargo`, `swift`, `gradle`, and `maven` cover common output formats, but toolchain-specific variations may still need future work

## Examples

Add npm scripts in a Node or React workspace:

```sh
runtask add npm --all
runtask npm-test
```

Add TypeScript build and watch tasks:

```sh
runtask add typescript --all
runtask tsc-watch-tsconfig.json
```

Generate Go build and test tasks:

```sh
runtask add go
runtask go-test
```

Save detected gulp tasks directly:

```sh
runtask add detect --save --ecosystem gulp --all
```

## Distribution

### Release binaries

GitHub Releases publish archives for these targets when a `v1.2.3` style tag is pushed.

- `linux/amd64`
- `linux/arm64`
- `darwin/arm64`
- `windows/amd64`
- `windows/arm64`

Archive names follow this pattern:

- `runtask_<version>_<os>_<arch>.tar.gz`
- `runtask_<version>_<os>_<arch>.zip`

Example:

```sh
curl -L -o runtask_1.2.3_linux_amd64.tar.gz \
  https://github.com/<owner>/vsc-taskrunner/releases/download/v1.2.3/runtask_1.2.3_linux_amd64.tar.gz
tar -xzf runtask_1.2.3_linux_amd64.tar.gz
./runtask --help
```

Every release also includes `checksums.txt`.

### Docker image

Tag pushes also publish a container image to GHCR:

```sh
docker pull ghcr.io/<owner>/vsc-taskrunner:v1.2.3
docker run --rm ghcr.io/<owner>/vsc-taskrunner:v1.2.3 --help
```

The image currently publishes `linux/amd64` and `linux/arm64` manifests.

## Continuous integration

GitHub Actions runs on every pull request and on pushes to `main`.

- `gofmt -l .`
- `go vet ./...`
- `go test ./...`
- `go build ./...`

Release automation runs when a `v1.2.3` style tag is pushed.

## License

This project is licensed under the GNU Affero General Public License v3.0 or later.
See [LICENSE](LICENSE).