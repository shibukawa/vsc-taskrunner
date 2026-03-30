# runtask

VS Code の tasks.json 互換を目指した CLI です。`tasks.json` を読み込み、task の一覧表示、dry-run、実行、追加を行います。

現状は以下の 2 系統を扱います。

- custom task: `shell` / `process`
- provider-like task: `npm` / `typescript` / `gulp` / `grunt` / `jake`

provider-like task は保存時には VS Code 互換の definition を維持し、実行時に concrete command へ resolve されます。

## Background Attribution

Copyright (c) 2024 by glitchworker (https://codepen.io/glitchworker/pen/jENZGOV)
Released under the MIT license
https://opensource.org/licenses/mit-license.php

## 基本動作

Web UI 用の `taskrun-ui.yaml` では、`runtask ui init` / `runtask ui edit task` / `runtask ui edit branch` は基本設定に限定しています。複雑な UI config は直接 YAML を編集し、次の補助資産を使う前提です。

- `config-schema.json`: エディタ補完と構文検査
- `llms.txt`: 生成 AI 向けガイド
- `.codex/skills/taskrun-ui-config/SKILL.md`: Codex 向け手順

実装上の最終判定は Go 側 validation、editor support は schema、AI support は `llms.txt` / skill が担当します。

### task 一覧

```sh
runtask list
runtask list --json
```

`list --json` では task の `group` に加えて、`workspaceRoot` / `taskFilePath` / `sourceTaskId` も確認できます。

### task の dry-run

```sh
runtask npm-test --dry-run
```

`--dry-run` のテキスト出力でも `group` を表示します。`run --json --dry-run` では resolved task に `workspaceRoot` / `taskFilePath` / `sourceTaskId` を含めます。

### task の実行

```sh
runtask go-test
runtask tsc-build-tsconfig.json
```

### task の追加

対話式の custom task 追加:

```sh
runtask add
```

detect 結果の確認:

```sh
runtask add detect
runtask add detect --json
```

detect 結果の保存:

```sh
runtask add detect --save --ecosystem npm
runtask add detect --save --label npm-test
runtask add detect --save --ecosystem gulp --all
```

## add コマンド

### npm

```sh
runtask add npm --all
runtask add npm --task test
runtask add npm --path web --task build,test
```

- `package.json` を検出します
- 保存形式は `type: npm` と `script` です
- 実行時は lockfile と `packageManager` を見て `npm` / `yarn` / `pnpm` を解決します
- 候補に含める script 名は `pre` / `post` で始まるもの、または `:` を含まないものに限定します

### typescript

```sh
runtask add typescript
runtask add typescript --all
runtask add typescript --tsconfig packages/app/tsconfig.json --task build,watch
```

- `tsconfig*.json` を検出します
- 引数なしでは `build` を追加します
- `--all` では各 tsconfig について `build` と `watch` を追加します
- workspace root の `package.json` に `build` または `watch` script がある場合、同名の TypeScript task は追加しません
- `build` には `group: build` を付けます
- 実行時は `$tsc` / `$tsc-watch` を自動設定します

### gulp / grunt / jake

```sh
runtask add gulp --all
runtask add grunt --task build
runtask add jake --file tools/Jakefile --all
```

- 対応ファイルを検出します
  - gulp: `gulpfile.js`, `gulpfile.cjs`, `gulpfile.mjs`, `gulpfile.ts`
  - grunt: `Gruntfile.js`, `Gruntfile.cjs`, `Gruntfile.mjs`, `Gruntfile.ts`
  - jake: `Jakefile`, `Jakefile.js`, `Jakefile.cjs`, `Jakefile.mjs`, `Jakefile.ts`
- task 名はファイルから簡易抽出します
  - gulp: `gulp.task('name', ...)`
  - grunt: `grunt.registerTask('name', ...)`
  - jake: `task('name', ...)`
- 保存形式は provider-like task definition です

### go / rust / swift / gradle / maven

```sh
runtask add go
runtask add rust --path crates/core --task test
runtask add gradle --all
runtask add maven --path server
```

- これらは provider type ではなく `process` task を生成します
- 引数なしでは、Go は `build` / `test` / `bench` / `cover` / `lint` を、Rust は `build` / `test` / `check` / `bench` を、Swift は `build` / `test` / `clean` / `run` を、Gradle/Maven は `build` / `test` / `clean` / `lint` を追加します
- `build` / `watch` には `group: build`、`test` には `group: test` を付けます
- `options.cwd` は検出された project root に設定します
- rust, swift, gradle, maven には基本的な built-in matcher を既定付与します
- rust / gradle / maven は複数 matcher を配列で既定設定し、panic 系や Kotlin compiler 系の代表的な出力も拾います
- Go の既定引数は build が `go build -trimpath -ldflags=-s -w ./...`、test が `go test -v ./...`、bench が `go test -run=^$ -bench=. -benchmem ./...`、cover が `go test -coverprofile=coverage.out ./...`、lint が `gofmt -l -w . && go vet ./...` です

## サポート状況

| Ecosystem | Detect | Add | Saved form | Run | Default matcher | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| shell/process | なし | 対話式のみ | shell/process | 対応 | 明示時のみ | custom task |
| npm | 対応 | 対応 | provider-like | 対応 | なし | script 単位、`--all` 対応。候補は `pre*` / `post*` または `:` を含まない名前 |
| typescript | 対応 | 対応 | provider-like | 対応 | `$tsc`, `$tsc-watch` | workspace root の npm scripts に同名が無い場合のみ `build/watch` を生成 |
| gulp | 対応 | 対応 | provider-like | 対応 | なし | task 名をファイルから簡易抽出 |
| grunt | 対応 | 対応 | provider-like | 対応 | なし | task 名をファイルから簡易抽出 |
| jake | 対応 | 対応 | provider-like | 対応 | なし | task 名をファイルから簡易抽出 |
| go | 対応 | 対応 | process/shell | 対応 | `$go` | `go build -trimpath -ldflags=-s -w ./...`, `go test -v ./...`, `go test -run=^$ -bench=. -benchmem ./...`, `go test -coverprofile=coverage.out ./...`, `gofmt -l -w . && go vet ./...` |
| rust | 対応 | 対応 | process | 対応 | `$cargo`, `$cargo-panic` | `cargo build`, `cargo test`, `cargo check`, `cargo bench` |
| swift | 対応 | 対応 | process | 対応 | `$swift` | `swift build`, `swift test`, `swift package clean`, `swift run` |
| gradle | 対応 | 対応 | process | 対応 | `$gradle`, `$gradle-kotlin` | `gradlew` 優先。`build`, `test`, `clean`, `lint` を公開 |
| maven | 対応 | 対応 | process | 対応 | `$maven`, `$maven-kotlin` | `mvnw` 優先。`build`, `test`, `clean`, `lint` を公開 |

## 互換性の前提

- 単一 workspace 前提です
- `command:*`, `config:*`, `file*` 系 variable は未対応です
- auto-detect は VS Code の常駐 provider ではなく、CLI 実行時の one-shot scan です
- provider-like task の `provideTasks` を完全再現しているわけではなく、CLI から保存するための検出を提供しています
- `gulp` / `grunt` / `jake` の task 抽出は静的な簡易検出です。動的定義や高度な JS 実行は対象外です
- `cargo` / `swift` / `gradle` / `maven` の matcher は代表的な出力形式を対象にした基本実装です。可能な範囲で line / column / code を抽出しますが、ツールチェインやプラグイン固有の出力差分は今後の拡張対象です
- Rust は rustc 形式に加えて panic 行を、Gradle/Maven は Java 系に加えて Kotlin compiler 系の典型フォーマットを対象にしています

## 例

React / Node ワークスペースで npm scripts を追加:

```sh
runtask add npm --all
runtask npm-test
```

`lint:fix` のような script は候補から外し、`prebuild` や `postdeploy:prod` は候補に含めます。

TypeScript project の build/watch を追加:

```sh
runtask add typescript --all
runtask tsc-watch-tsconfig.json
```

workspace root の `package.json` に `build` または `watch` script がある場合は、対応する TypeScript task を追加せず、npm script を優先します。

Go project で build/test/bench/cover/lint task を生成:

```sh
runtask add go
runtask go-test
runtask go-bench
runtask go-cover
runtask go-lint
```

Rust project で build/test/check/bench task を生成:

```sh
runtask add rust
runtask cargo-check
runtask cargo-bench
```

出力例:

```text
$ runtask add rust
added 4 tasks to .vscode/tasks.json: cargo-bench, cargo-build, cargo-check, cargo-test
```

Swift Package project で build/test/clean/run task を生成:

```sh
runtask add swift
runtask swift-clean
runtask swift-run
```

出力例:

```text
$ runtask add swift
added 4 tasks to .vscode/tasks.json: swift-build, swift-clean, swift-run, swift-test
```

`swift-run` は executable target を含む package が必要です。

detect した gulp task をそのまま保存:

```sh
runtask add detect --save --ecosystem gulp --all
```

## 配布

### Release バイナリ

`v1.2.3` 形式のタグを push すると、GitHub Releases に次の target 向け archive を公開します。

- `linux/amd64`
- `linux/arm64`
- `darwin/arm64`
- `windows/amd64`
- `windows/arm64`

archive 名は次の形式です。

- `runtask_<version>_<os>_<arch>.tar.gz`
- `runtask_<version>_<os>_<arch>.zip`

例:

```sh
curl -L -o runtask_1.2.3_linux_amd64.tar.gz \
  https://github.com/<owner>/vsc-taskrunner/releases/download/v1.2.3/runtask_1.2.3_linux_amd64.tar.gz
tar -xzf runtask_1.2.3_linux_amd64.tar.gz
./runtask --help
```

各 release には `checksums.txt` も含まれます。

### Docker イメージ

タグ push 時に GHCR にもコンテナイメージを公開します。

```sh
docker pull ghcr.io/<owner>/vsc-taskrunner:v1.2.3
docker run --rm ghcr.io/<owner>/vsc-taskrunner:v1.2.3 --help
```

現状の image manifest は `linux/amd64` と `linux/arm64` を公開します。

## Continuous Integration

GitHub Actions は pull request と `main` への push で次を実行します。

- `gofmt -l .`
- `go vet ./...`
- `go test ./...`
- `go build ./...`

release 自動化は `v1.2.3` 形式のタグ push を起点に動きます。

## License

この project は GNU Affero General Public License v3.0 以降で提供します。
[LICENSE](LICENSE) を参照してください。
