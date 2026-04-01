# runtask

runtask は、VS Codeの`.vscode/tasks.json`をVS Code の外から扱うためのツールです。既存の`.vscode/tasks.json`をCLIから実行できるだけでなく、`.vscode/tasks.json`の作成支援と、ブラウザからtask を実行できるWebUIモードを提供します。

![screenshot](docs/screenshot.png)

## tasks.jsonの実行機能

- `.vscode/tasks.json`に定義されたタスクの一覧表示
- タスクの dry-run
- タスクの実行(inputの追加パラメータ、並行実行・逐次実行も含む、依存タスクを含む実行)

> [!NOTE]
> 単一ワークスペースのみ対応しています
> - `command:*`、`config:*`、`file*` 系の variable には未対応です
> - provider-like taskは事前に明示的なコマンド定義に変換しておく必要があります

### 基本的な使い方

task 一覧:

```sh
runtask list
runtask list --json
```

dry-run:

```sh
# npm-test は task label
runtask npm-test --dry-run
```

タスク実行:

```sh
runtask go-test
runtask tsc-build-tsconfig.json
```

`runtask run <task-name>` 形式でも実行できますが、通常は `runtask <task-name>` の短い書き方で使えます。依存関係がある task は、`dependsOn` の定義に従ってあわせて実行されます。

## tasks.json作成支援

VSCodeではタスク定義を行わなくても、拡張機能がプロジェクトの構成を見てコマンド実行の推測を行います。
このツールは拡張機能には対応していませんが、いくつかの言語やビルドツールの設定を読み込んで、`.vscode/tasks.json`のコマンドとして登録する支援機能を提供しています。

プロジェクトの雛形型できた段階でこの機能を使って登録しておくことで、VSCode上のデバッグなどもしやすくなります。

### `runtask add` でタスクを追加する

`runtask add` は、手書きせずに `.vscode/tasks.json` へ task を追加するための補助コマンドです。まずは対話式でカスタムタスクを作るか、既存プロジェクトから検出して保存する使い方が中心になります。

対話式の追加:

```sh
runtask add
```

検出結果の確認:

```sh
runtask add detect
runtask add detect --json
```

検出結果の保存:

```sh
runtask add detect --save --ecosystem npm
runtask add detect --save --label npm-test
runtask add detect --save --ecosystem gulp --all
```

### 対応している追加支援

現在は次のエコシステムに対応しています。

- provider-like task: `npm` / `typescript` / `gulp` / `grunt` / `jake`
- 生成系 task: `go` / `rust` / `swift` / `gradle` / `maven`

よく使う例:

```sh
# npm scripts をまとめて追加
runtask add npm --all

# TypeScript 用の build / watch task を追加
runtask add typescript --all

# Go 用の build / test / bench / cover / lint を追加
runtask add go

# Rust 用の build / test / check / bench を追加
runtask add rust
```

## WebUI モード

WebUIモードでは、Gitリポジトリを対象にブラウザ上でブランチやタスクを選んで実行し、履歴や成果物を確認できます。常駐サービスとして使う構成だけでなく、サーバーレス環境での実行も想定しています。

CLIモードは現在のブランチのワークスペース上にある`.vscode/tasks.json`をその場で実行します。一方、WebUIモードはCI的な環境を想定しています。画面上の設定などは提供していません。

> [!NOTE]
> git submoduleには対応していません。

### WebUIとCLIの動作の違いについて

* CLIはGit操作をせずに現在のワークの中で動作しますが、WebUIはブランチ選択を行い、そのブランチのHEADをもとにワークスペースを作成してから実行します。
* CLIはVSCode互換の`.vscode/tasks.json`のみを読み込んで利用しますが、WebUIはそれでは足りない設定を`runtask-ui.yaml`として持ちます。WebUIで操作可能なブランチやタスクの選択、タスクの前の事前実行タスク、成果物やワーク、履歴の管理、認証認可など
* CLIでは必要なライブラリなどは揃っている前提で書かれたタスクのみを実行しますが、WebUIはクリーンビルドなどのために追加のパッケージインストール処理などが定義できます
* 実行結果は履歴として保持され、成果物や保存された作業フォルダをあとから参照できます

リポジトリのワークスペースを作らずに`.vscode/tasks.json`に書かれたコマンドの単体実行だけを行うオプションもあります。

### WebUIの初期設定

WebUI 用の設定ファイルは `runtask-ui.yaml` です。最初のひな形は `ui init` で作れます。

```sh
# 対話式に初期設定を生成
runtask ui init

# 設定ファイルを書き出さず標準出力に確認
runtask ui init --write=false
```

生成後に、公開する task やブランチを簡易編集するコマンドもあります。

```sh
runtask ui edit task
runtask ui edit branch
```

この簡易編集で向いているのは、公開対象のタスクやブランチの見直しなどです。認証認可、実行モード、スケジュール実行、heartbeat ベースの起動などの細かい設定変更は`runtask-ui.yaml` を直接編集してください。`llms.txt`や、設定の変更用のskillsは[リポジトリ](https://github.com/shibukawa/vsc-taskrunner)にあります。

## WebUIの起動:

```sh
runtask ui
```

起動時に実行モードを選べます。

```sh
# 既定値。常駐サービスとして動かし、共有 run 更新をリアルタイム配信
runtask ui --runtime-mode=always-on

# グローバル live stream を無効にし、条件付きポーリングを使う
runtask ui --runtime-mode=serverless
```

- `--runtime-mode=always-on`: `/api/runs/stream` の SSE を有効にし、他ユーザーの run サマリ更新を即時にブラウザへ反映します。ほかのユーザーの実行中 run を選んだ場合は、従来どおり個別 run のログ stream を使います
- `--runtime-mode=serverless`: グローバル live stream を無効にし、`GET /api/runs` の `ETag` / `If-None-Match` を使った条件付きポーリングで履歴を更新します
- 既定値は `always-on` です
- これは `runtask ui` の起動フラグであり、`runtask-ui.yaml` の設定項目ではありません

### タスクの設定詳細

WebUIではタスクごとに事前処理、定期実行、成果物、履歴保持、作業フォルダの扱いを設定できます。代表的な設定例は次のとおりです。

```yaml
tasks:
  build:
    # 事前実行タスク
    preRunTask:
      - command: npm
        args:
          - ci
        cwd: ${workspaceFolder}
    # 成果物
    artifacts:
      - path: dist
        format: zip
        nameTemplate: frontend-{branch}-{input:env}-b{buildno}-{yyyymmdd}-{hhmmss}-{hash}.zip
    # 定期実行
    schedules:
      - cron: "0 6 * * 1-5"
        branch: main
        inputValues:
          env: staging
    # タスク単位の履歴保持数
    historyKeepCount: 10
    # 作業フォルダの保存数
    worktree:
      # リポジトリ展開を抑えた軽量実行にする場合は true
      disabled: false
      keepOnSuccess: 0
      keepOnFailure: 5

# 全体のストレージ設定（タスク定義が優先）
storage:
  historyKeepCount: 50
  worktree:
    keepOnSuccess: 0
    keepOnFailure: 2
```

- `preRunTask`: 本体 task の前に一度だけ実行する前処理です。`npm ci` などの依存取得に向いています
- `schedules`: この task を cron 形式で定期実行する設定です。各 schedule ごとに対象ブランチと固定 input を持てます
- `artifacts`: 実行後にダウンロード対象として残すファイルやディレクトリです。`nameTemplate` は `format: zip` のときだけ使われ、`format: file` は一致したファイル名のまま保持します
- `historyKeepCount`: 実行履歴を何件残すかです。task ごとの上書きが無ければ `storage.historyKeepCount` を使います
- `worktree.disabled`: リポジトリ全体を展開しない task 向けのフラグです
- `worktree.keepOnSuccess` / `keepOnFailure`: 成功時・失敗時に作業フォルダをいくつ保持するかです

スケジュール実行の補足:

- `schedules[].cron` は通常の 5 フィールド cron 形式で、サーバーのローカル時刻で評価されます
- `schedules[].branch` は top-level の `branches` 設定でも許可されている必要があります
- `schedules[].inputValues` は task 実行時の固定 input 値です
- 常駐モードでは runtask プロセスがバックグラウンドで自動評価します
- サーバーレスモードでは常時タイマーを持たず、heartbeat リクエスト到着時に評価します
- サービス停止などで遅れた場合は、取りこぼした全回ではなく直近 1 回分だけを起動します

`format: zip` の成果物名テンプレートでは次のプレースホルダが使えます。

- `{buildno}`: ビルド番号
- `{yyyymmdd}`: 年月日(UTC)
- `{hhmmss}`: 時分秒(UTC)
- `{yyyymmddhhmmss}`: 年月日時分秒(UTC)
- `{hash}`: Gitハッシュ(先頭7文字)
- `{longhash}`: Gitハッシュ
- `{branch}`: ブランチ
- `{input:NAME}`: task input `NAME` の値です。ファイル名に使える形に整形され、未指定時は `unknown` になります

### API アクセス

WebUI はブラウザ操作だけでなく、アクセストークンを使った API 実行にも対応しています。手動起動や外部システムからの明示的な起動に向いています。

管理者ユーザーは WebUIの設定画面からトークンを発行できます。トークンには用途に応じてscopeを付けられます。

- `runs:read`: 実行履歴や結果の参照
- `runs:write`: task の起動

代表的な API は次のとおりです。トークン発行画面でcurlコマンドのコピーができます。

- `GET /api/me`: 現在の利用者情報と権限確認
- `GET /api/runs`: 実行履歴の取得
- `GET /api/runs/stream`: `--runtime-mode=always-on` で有効な共有 run サマリの live stream
- `POST /api/runs`: task の起動
- `GET /api/heartbeat`: 設定済み schedule を評価して due な task を起動
- `GET /api/runs/{runId}`: 実行詳細の取得
- `GET /api/runs/{runId}/artifacts`: 成果物一覧
- `GET /api/runs/{runId}/worktree.zip`: 保存済み作業フォルダのダウンロード

履歴更新の補足:

- `GET /api/runs` は `ETag` と `If-None-Match` に対応し、変更が無い場合は `304 Not Modified` を返します
- `always-on` モードではブラウザが `/api/runs/stream` にも接続し、共有履歴を即時更新します
- `serverless` モードでは `/api/runs/stream` は無効で、条件付きポーリングだけを使います

起動例:

```sh
curl -H 'Authorization: Bearer <token>' \
  -H 'Content-Type: application/json' \
  -X POST http://localhost:8080/api/runs \
  -d '{"branch":"main","taskLabel":"build","inputValues":{}}'
```

heartbeat の呼び出し例:

```sh
curl http://localhost:8080/api/heartbeat
```

補足:

- `/api/heartbeat` は認証不要です
- request body で branch や task を自由に指定して起動する用途には使えません
- `runtask-ui.yaml` に書かれた schedule だけを評価して due なものを起動します
- 任意の branch と task を指定する明示起動は、従来どおり認証付きの `POST /api/runs` を使います

### 認証と認可

ローカル確認だけなら `auth.noAuth: true` で起動できます。共有環境用にOIDCをサポートしています。

```yaml
auth:
  oidcIssuer: https://issuer.example.com
  oidcClientID: runtask
  oidcClientSecret: ${OIDC_CLIENT_SECRET}
  sessionSecret: ${SESSION_SECRET}
  allowUsers:
    role:
      - runner
  adminUsers:
    role:
      - admin
  apiTokens:
    enabled: true
```

- `allowUsers`: WebUI を利用できるユーザー条件です。未設定なら認証済みユーザー全員を許可します
- `adminUsers`: 設定確認や API トークン管理を許可するユーザー条件です。クレームのキーとマッチする条件(glob形式)で書けます
- `apiTokens.enabled`: API トークン機能の有効化です。共有環境で API 連携を行う場合に使います

### 環境構築

- `server.host` / `server.port` / `server.publicURL` を設定して公開します

#### 常駐モード

シンプルな1台のサーバーでWebUIを常駐させる構成です。

推奨起動コマンド:

```sh
runtask ui --runtime-mode=always-on
```

- `storage.backend: local` でローカルディスクに履歴や成果物を保存できます
- 同じマシンに履歴や repository cache が残るため、繰り返し実行でキャッシュを最大限に活かせます
- schedule は runtask プロセスがだいたい 1 分ごとに自動評価します
- ブラウザはグローバル run stream で、他ユーザーの run サマリ更新をリアルタイムに受け取れます

#### サーバーレスモード

Google Cloud Run functions、AWS Lambda、AWS ECSなどを使い、アクセスがあった時のみサーバーリソースを使うモードです。ビルド作業などはブロックストレージがあるため問題なくできますが、アクティブなインスタンスがなくなるとブロックストレージもリセットされてしまうため、`.git`のクローンや、外部パッケージのキャッシュなどがなくなる可能性があります。巨大プロジェクトなどでは常駐モードを使うか、AWS EFSやGoogle Cloud Firestoreなどをマウントして作業フォルダとしてください。

推奨起動コマンド:

```sh
runtask ui --runtime-mode=serverless
```

サーバーレスモードでは実行結果、成果物、ワークの保存先としてS3互換のオブジェクトストレージを使います。

```yaml
storage:
  backend: object
  object:
    endpoint: https://s3.example.com
    bucket: runtask
    region: ap-northeast-1
```

サーバーレス環境で定期実行したい場合は、Cloud Scheduler、EventBridge、cron、GitHub Actions などの外部スケジューラから heartbeat endpoint を呼び出します。

```sh
curl -fsS https://runtask.example.com/api/heartbeat
```

heartbeat はその時点で due な schedule だけを評価して起動します。

このモードではグローバル live run stream は使わず、共有履歴は `/api/runs` の条件付きポーリングで更新されます。

## ライセンス

この project は GNU Affero General Public License v3.0 以降で提供します。
[LICENSE](LICENSE) を参照してください。
