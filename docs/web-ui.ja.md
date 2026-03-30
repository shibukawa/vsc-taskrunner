# Web UI

## 概要

Web UI は、Git ブランチごとの `.vscode/tasks.json` を読み、そのブランチ用の run workspace 上で task を実行するための画面です。実行結果は run 単位で履歴に保存され、親 run の状態に加えて child task ごとの状態とログを後から参照できます。

現在の実装では、画面の選択状態は URL に反映されます。branch、task、run、child task のログ選択まで含めて直接リンクできます。

## 前提と配置

- Web UI は対象リポジトリのルートを基点に動きます。
- 対象リポジトリは `runtask-ui.yaml` の `repository` で指定します。
- task の一覧表示は、選択した branch 上の `.vscode/tasks.json` を読み取って行います。
- task の実行は、対象 branch 用に準備した run workspace 上で `.vscode/tasks.json` を解決して行います。
- 設定ファイルは `runtask-ui.yaml` を使います。

リポジトリ内の代表的な配置は次のとおりです。

- 設定ファイル: `runtask-ui.yaml`
- 実行対象 task 定義: `<repo>/.vscode/tasks.json`
- 履歴保存先: `storage.historyDir/<runId>/`
- 履歴インデックス: `storage.historyDir/run-history-index.json` または object storage 上の同名オブジェクト

## `runtask-ui.yaml`

`runtask-ui.yaml` は人間向けの詳細仕様です。AI 補助やエディタ補完を使う場合は、次の 3 つを併用します。

- `config-schema.json`: editor 向けの構造定義と補完
- `llms.txt`: 生成 AI 向けの要点整理
- `.codex/skills/taskrun-ui-config/SKILL.md`: Codex 向けの生成・編集・レビュー手順

source of truth は `internal/uiconfig/config.go` の Go validation です。schema は補完と構造検査、`llms.txt` と skill は AI 支援に使います。

生成・保存する YAML の先頭には次の schema コメントを付ける前提です。

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/shibukawa/vsc-taskrunner/main/config-schema.json
```

設定ファイルの主な項目は次のとおりです。

### `server`

- `host`: HTTP サーバの listen address の host 部分です。
- `port`: HTTP サーバの listen port です。
- `publicURL`: 外部公開 URL です。OIDC の callback URL 生成にも使います。

### `auth`

- `oidcIssuer`: OIDC issuer URL です。
- `oidcClientID`: OIDC client ID です。
- `oidcClientSecret`: OIDC client secret です。
- `noAuth`: `true` の場合は認証を無効化します。
- `sessionSecret`: セッション cookie 署名キーです。
- `allowUsers`: アクセスを許可するユーザー条件です。`claim: glob` または `claim: [glob, ...]` の形式で指定します。
- `adminUsers`: 管理者扱いにするユーザー条件です。`claim: glob` または `claim: [glob, ...]` の形式で指定します。
- `apiTokens.enabled`: バッチ実行用のアクセストークン機能を有効にします。
- `apiTokens.defaultTTLHours`: 発行する token の既定 TTL です。
- `apiTokens.maxPerUser`: 1 ユーザーが同時に保持できる有効 token 数の上限です。
- `apiTokens.store.backend`: token 保存先です。`local` または `object` を指定します。
- `apiTokens.store.localPath`: `backend: local` のときの保存ファイルです。
- `apiTokens.store.object.*`: `backend: object` のときの token 専用オブジェクトストレージ設定です。履歴用 `storage.object.*` とは別に指定できます。

例:

```yaml
auth:
  allowUsers:
    email: "*@example.com"
    role:
      - runner
      - ops-*
  adminUsers:
    role:
      - administrator
  apiTokens:
    enabled: true
    defaultTTLHours: 720
    maxPerUser: 10
    store:
      backend: local
      localPath: .runtask/api-tokens.json
```

### `repository`

- `source`: 対象リポジトリです。remote URL またはローカル Git パスを指定します。
- `localPath`: `source` が remote URL の場合に clone を作るローカル保存先です。
- `auth`: remote URL を token で読む場合の認証設定です。

現在の実装では tasks-sparse 実行時の展開パスは `.vscode` に固定、通常 run workspace はフル展開、fetch depth は `1` 固定です。これらは設定ファイルからは変更できません。

`source` が remote URL の場合、現在の実装では UI 起動時に `git clone --no-checkout` を行い、既存 clone があればそのまま使います。`/api/git/fetch` 実行時は `origin/*` から対応する local branch を同期します。

`repository.auth.type=envToken` を設定すると、token が環境変数に入っている場合にだけ、起動時と `fetch` 前に token の検査を行います。環境変数が未設定なら認証ヘッダは付けず、そのまま public repository としてアクセスを試みます。token が設定されている場合は、検査不能な token や、repo 範囲が広すぎる token を fail-close で拒否します。

主な項目は次のとおりです。

- `type`: 現在は `envToken` のみ対応です。
- `provider`: `github` / `gitlab` / `bitbucket`
- `tokenEnv`: token を読む環境変数名です。
- `baseURL`: API のベース URL です。GitLab Self-Managed では通常 `https://<host>/api/v4` を指定します。
- `repo`: 許可する repo 識別子です。GitHub/Bitbucket は `owner/repo`、GitLab は `group/project` です。
- `allowedHosts`: 許可する remote/API host の allowlist です。
- `rejectBroadScope`: `true` の場合、対象 repo 以外も読める token を拒否します。
- `requireReadOnly`: `true` の場合、write/admin 相当の権限を持つ token を拒否します。

注意:

- 現在は HTTPS token 認証のみ対応です。SSH は対象外です。
- GitHub は fine-grained PAT のみ許可します。
- GitLab は strict validation のため `read_repository` に加えて `read_api` を要求します。
- Bitbucket は repository-scoped token を想定します。

`allowUsers` が空の場合は、認証に成功したユーザーは全員アクセスできます。`adminUsers` が空の場合は管理者ユーザーはいません。

`apiTokens.enabled=true` の場合、`adminUsers` に一致したユーザーは Web UI の user menu からアクセストークンを発行できます。発行された token は `Authorization: Bearer <token>` で `GET /api/me`、`GET /api/runs`、`POST /api/runs`、および run 詳細参照 API に利用できます。

### `branches`

- 表示および実行を許可する branch 名の glob パターンです。

`branches` が空の場合は top-level branch のみ許可します。

### `tasks`

- task label の glob をキーにした task ごとの UI 設定です。
- `artifacts`: artifact rule の配列です。空でなければ artifact 有効として扱います。
- `artifacts[].path`: 回収対象パスまたは glob です。
- `artifacts[].format`: `zip` または `file` です。既定値は `zip` です。
- `artifacts[].nameTemplate`: `format: zip` のときの zip artifact 名です。既定値は `artifacts.zip` です。`format: file` では無視されます。
- `preRunTask`: 親 task 実行前に 1 回だけ走らせる前処理です。
- `worktreeDisabled`: `true` の task は `.vscode` だけを展開した tasks-sparse workspace で実行します。

`tasks` が空の場合、許可される task はありません。

### `execution`

- `maxParallelRuns`: 同時に走らせる親 run 数の上限です。0 以下ではなく、0 は無制限扱いです。

### `storage`

- `historyDir`: run 保存のルートです。
- `historyKeepCount`: `branch + task` ごとに完了済み run を保持する件数です。
- `backend`: 履歴インデックスの保存先です。`local` または `object` を指定します。
- `object.*`: `backend: object` のときに使う S3 互換オブジェクトストレージ接続設定です。

既定値は `internal/uiconfig/config.go` にあり、代表的には次の値です。

- `execution.maxParallelRuns`: `4`
- `storage.historyDir`: `.runtask/history`
- `storage.historyKeepCount`: `100`
- `storage.worktree.keepOnSuccess`: `0`
- `storage.worktree.keepOnFailure`: `3`

## 認証と権限

`auth.noAuth=false` の場合、Web UI は OIDC ログインを要求します。ログイン済みユーザーの claims は `allowUsers` に対して評価され、一致しない場合はアクセスできません。

`allowUsers` が空の場合は、認証に成功したユーザーは全員アクセスできます。task は `tasks` に定義されたものだけが表示・実行されます。
- `POST /api/runs` は `403` で拒否される

`auth.noAuth=true` の場合は認証を行わず、`/api/me` では `authenticated=false` かつ `canRun=true` で扱われます。

## 画面と URL

現在の画面は、おおむね次の領域で構成されています。

- Branches: branch 一覧
- Tasks: task 一覧
- Inputs: 実行前入力
- Runs: 親 run の履歴一覧
- Run Detail: 選択 run の概要
- 依存表示: 選択 task から辿れる依存関係の表示
- child task log: 選択した child task のログ

正規 URL は次の形です。

- `/branches/:branch`
- `/branches/:branch/runs/:runId`
- `/branches/:branch/tasks/:task`
- `/branches/:branch/tasks/:task/runs/:runNumber`
- `/branches/:branch/tasks/:task/runs/:runNumber/logs/:childTask`
- `/runs/:runId`
- `/runs/:runId/logs/:childTask`

現在の実装では次の性質があります。

- `branch`、`task`、`childTask` は path segment 全体を URL エンコードして扱います。
- branch または task を切り替えると、現在表示中の `runNumber` と child task 選択はクリアされます。
- `run` 詳細表示は左側の選択状態と分離できます。`/runs/:runId` は完全な run 単独表示、`/branches/:branch/runs/:runId` は branch 選択だけを維持した run 詳細表示です。
- `runNumber` 付き URL はその `branch + task + runNumber` 固定で表示します。最新 run に自動で切り替えることはありません。
- 現在の branch の `.vscode/tasks.json` に存在しない task や child task を URL で指定すると warning 表示になります。
- ただし過去 run に保存済みの child task 状態やログがあれば、履歴表示は継続できます。

## 実行フロー

### task 一覧の取得

branch を選ぶと、フロントエンドは `GET /api/git/branches/{branch}/tasks` を呼び、対象 branch 上の `.vscode/tasks.json` を読みます。branch に `tasks.json` がない場合、現在の実装では空配列を返します。

この API は task ごとに次の情報を返します。

- `label`
- `type`
- `group`
- `dependsOn`
- `dependsOrder`
- `hidden`
- `background`
- `inputs`

`inputs` は、その task が実際に `${input:...}` として参照している input だけが返されます。

### 実行時入力

現在の実装で Web UI が対応している input は次の 2 種類です。

- `promptString`
- `pickString`

入力値は `inputValues` として `POST /api/runs` に送られ、task 解決時の `${input:...}` に使われます。

### 実行

task 実行時には `POST /api/runs` に次の JSON を送ります。

```json
{
  "branch": "feature/demo",
  "taskLabel": "npm-build",
  "inputValues": {
    "target": "web"
  }
}
```

サーバ側では次の順で処理します。

1. 履歴インデックスを原子的に更新して `runId` を生成し、`branch + task` ごとの `runNumber` を採番して `running` 状態を登録する
2. local では `history/<runId>/`、object storage では `runs/<runId>/` を実体保存先として確保する
3. 対象 branch 用の run workspace を準備する
4. run workspace 内の `.vscode/tasks.json` を読み、`inputValues` を適用して解決する
5. 選択 task の `tasks.<pattern>.preRunTask` を順に実行する
6. 選択 task を実行する
7. 実行後に artifact rule ごとに artifact を回収する
8. `meta.yaml` と履歴インデックスを完了状態に更新し、保持数超過で除外された `runId` 実体を削除する

依存 task は既存 Runner の `dependsOn` / `dependsOrder` に従って処理されます。

- `dependsOrder` 未指定時は `parallel`
- `dependsOrder: sequence` 指定時は直列実行

依存失敗で実行されなかった child task は `skipped` として保存されます。

`preRunTask` は child task としては保存されません。統合ログにだけ出力され、失敗した場合は本体 task を起動せずに run 全体を失敗扱いにします。

artifact 回収は `artifacts` の各 rule を定義順に独立して処理します。`format: zip` は rule ごとに 1 つの zip を作ります。`format: file` はマッチした各ファイルを個別保存します。同じファイルが複数 rule にマッチした場合でも、それぞれの出力に含まれます。

## 履歴と保存物

各 run の保存先は local では `storage.historyDir/<runId>/`、object storage では `<prefix>/runs/<runId>/` です。run 配下には次の内容が作られます。

- `meta.yaml`
- `stdout.log`
- `tasks/*.log`
- `worktree/`
- `artifacts/`

### 履歴インデックス

トップページの `GET /api/runs` は run ディレクトリ全走査ではなく、単一の履歴インデックスだけを読みます。

インデックスには `branch + task` ごとの次の情報を保持します。

- `nextRunNumber`
- 最新 run の一覧
- 各 run の一覧用要約: `runId`, `runKey`, `branch`, `taskLabel`, `runNumber`, `status`, `startTime`, `endTime`, `exitCode`, `user`, `hasArtifacts`

artifact の実パス一覧や child task 詳細はインデックスには入れません。詳細は各 run の `meta.yaml` と artifact ディレクトリを参照します。

ローカルモードではインデックス更新時にファイルロックを取り、単一ホスト内の並列実行を排他します。object storage モードでは version/ETag を使って compare-and-swap で更新します。

### `meta.yaml`

`meta.yaml` には親 run と child task のメタ情報が入ります。主な項目は次のとおりです。

- `runKey`
- `runId`
- `branch`
- `taskLabel`
- `runNumber`
- `status`
- `startTime`
- `endTime`
- `exitCode`
- `worktreeKept`
- `artifacts`
- `user`
- `inputValues`
- `tasks`

`tasks` には child task ごとの状態が入り、主な項目は次のとおりです。

- `label`
- `dependsOn`
- `dependsOrder`
- `status`
- `startTime`
- `endTime`
- `exitCode`
- `logPath`
- `historical`

### ログファイル

- `stdout.log`: 親 run の統合ログです。
- `tasks/*.log`: child task ごとのログです。

child task のログファイル名は task label をそのまま使うのではなく、保存用にサニタイズした名前で作られます。

## worktree と保持ポリシー

各 run は専用 workspace を使います。workspace の実体は local では `historyDir/<runId>/worktree/`、object storage では `runs/<runId>/worktree/` に保存されます。

既定ではフル run workspace を準備します。選択 task の `worktreeDisabled: true` では、フル run workspace の代わりに `.vscode` だけを展開した tasks-sparse workspace を準備します。これは `.vscode/tasks.json` と最小限の補助ファイルだけで成立するジョブ向けです。

tasks-sparse workspace では、`.vscode` 以外のファイルへ依存する task や pre-run task は失敗する可能性があります。

run 完了後、worktree の保持は `storage.worktree.keepOnSuccess` と `storage.worktree.keepOnFailure` に従って管理されます。保持対象から外れた古い worktree は prune 時に削除され、`meta.yaml` の `worktreeKept` も更新されます。

`storage.historyKeepCount` は `branch + task` ごとの完了済み run の履歴保持数です。古い run ディレクトリはこの上限に従って削除されます。

## アーティファクト

artifact は task ごとの `artifacts` rule に従って、run の worktree から保存対象として回収したものです。`artifacts` が空でなければ artifact 有効として扱います。各 rule は `path`、`format`、`nameTemplate` を持てます。

`zip` ではディレクトリ指定を再帰的に展開します。

- `dist`: zip 内に `dist/...` を保持したまま再帰的に格納します。
- `dist/*`: `dist` の直下要素を zip 直下へ展開し、それぞれの配下は再帰的に格納します。

`file` ではディレクトリ指定は許可されません。`dist/index.html` のように明示的なファイルを指定してください。wildcard を使った場合でも、結果にディレクトリが含まれると run は失敗します。

- `zip`: その rule に一致したファイルを zip にまとめて保存します。既定の名前は `artifacts.zip` です。
- `file`: その rule に一致したファイルを、相対パスを保ったまま個別ファイルとして保存します。

`zip` のときは rule ごとの `nameTemplate` でアーカイブ名を変更できます。使えるプレースホルダは次の 6 つです。

- `{yyyymmdd}`: run 開始日の UTC 日付
- `{hhmmss}`: run 開始時刻の UTC 時刻
- `{yyyymmddhhmmss}`: run 開始日時の UTC 値
- `{hash}`: worktree の HEAD 先頭 7 文字の短縮コミットハッシュ
- `{longhash}`: worktree の HEAD フルコミットハッシュ
- `{branch}`: branch 名をファイル名安全に正規化した文字列

例: `frontend-{branch}-{yyyymmdd}-{hhmmss}-{hash}.zip`

同じファイルが複数 rule にマッチした場合でも、それぞれの出力に含まれます。`nameTemplate` は `format: file` では無視されます。

artifact のコピー先は各 run ディレクトリ配下の次の場所です。

- local: `storage.historyDir/<runId>/artifacts/`
- object storage: `<prefix>/runs/<runId>/artifacts/`

`zip` の場合は `artifacts/artifacts.zip` が保存されます。`files` の場合は相対パスを保ったまま `artifacts/` 配下に保存され、どちらも `meta.yaml` の `artifacts` に記録されます。

## ログとリアルタイム表示

ブラウザは `GET /api/runs/{runId}/log` の SSE を購読します。ページ URL 自体は `runNumber` を維持しますが、UI は一覧 API から `runId` を解決して detail/SSE/worktree API を呼びます。

主なイベントは次のとおりです。

- `task-start`
- `task-line`
- `task-finish`
- `task-skip`
- `done`

UI 側では event payload の `taskLabel` を使って child task ごとにフィルタし、選択中の child task ログとして表示します。run 完了後も、保存済みの `tasks/*.log` を元に再表示できます。

## 補助 API と閲覧

閲覧用途で使われる主な API は次のとおりです。

- `GET /api/runs`
  - 履歴インデックスから親 run の一覧を返します。
- `GET /api/runs/{runId}`
  - 1 件の run の詳細を返します。child task 状態も含みます。
- `GET /api/runs/{runId}/worktree`
  - 保存済み worktree のファイル一覧を返します。
- `GET /api/runs/{runId}/worktree/{path...}`
  - 保存済み worktree 内のファイル内容を返します。

worktree が prune 済みで存在しない場合、worktree 閲覧 API は使えません。

## Compose での検証

ローカル履歴モード:

- `docker compose up -f docker-compose.yml`

RustFS を使う object storage モード:

- `docker compose -f compose.object.yaml up`

object storage モードでは `runtask-ui.object.yaml` を使い、RustFS の S3 endpoint に履歴インデックスと `runs/<runId>/...` を書き込みます。実行中は local staging を使い、完了時や worktree prune 後に object storage 側へ再同期します。

現在の `compose.object.yaml` はローカル検証用の single drive layout です。可用性、冗長性、ディスク障害耐性はありません。S3 互換 API の簡易確認用としてのみ使ってください。

## 補足

このドキュメントは、現在の実装の事実に合わせた運用説明です。将来の理想仕様ではなく、`internal/web` と `internal/uiconfig` の実装で実際に行っている動作を基準に記述しています。
