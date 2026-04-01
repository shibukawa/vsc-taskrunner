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

この簡易編集で向いているのは、公開対象のタスクやブランチの見直しなどです。認証認可や実行モードなどの細かい設定変更は`runtask-ui.yaml` を直接編集してください。`llms.txt`や、設定の変更用のskillsは[リポジトリ](https://github.com/shibukawa/vsc-taskrunner)にあります。

## WebUIの起動:

```sh
runtask ui
```

### タスクの設定詳細

WebUIではタスクごとに事前処理、成果物、履歴保持、作業フォルダの扱いを設定できます。代表的な設定例は次のとおりです。

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
        nameTemplate: frontend-{branch}-b{buildno}-{yyyymmdd}-{hhmmss}-{hash}.zip
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
- `artifacts`: 実行後にダウンロード対象として残すファイルやディレクトリです
- `historyKeepCount`: 実行履歴を何件残すかです。task ごとの上書きが無ければ `storage.historyKeepCount` を使います
- `worktree.disabled`: リポジトリ全体を展開しない task 向けのフラグです
- `worktree.keepOnSuccess` / `keepOnFailure`: 成功時・失敗時に作業フォルダをいくつ保持するかです

成果物名テンプレートでは次のプレースホルダが使えます。

- `{buildno}`: ビルド番号
- `{yyyymmdd}`: 年月日(UTC)
- `{hhmmss}`: 時分秒(UTC)
- `{yyyymmddhhmmss}`: 年月日時分秒(UTC)
- `{hash}`: Gitハッシュ(先頭7文字)
- `{longhash}`: Gitハッシュ
- `{branch}`: ブランチ

### API アクセス

WebUI はブラウザ操作だけでなく、アクセストークンを使った API 実行にも対応しています。定期実行や外部システムからの起動に向いています。

管理者ユーザーは WebUIの設定画面からトークンを発行できます。トークンには用途に応じてscopeを付けられます。

- `runs:read`: 実行履歴や結果の参照
- `runs:write`: task の起動

代表的な API は次のとおりです。トークン発行画面でcurlコマンドのコピーができます。

- `GET /api/me`: 現在の利用者情報と権限確認
- `GET /api/runs`: 実行履歴の取得
- `POST /api/runs`: task の起動
- `GET /api/runs/{runId}`: 実行詳細の取得
- `GET /api/runs/{runId}/artifacts`: 成果物一覧
- `GET /api/runs/{runId}/worktree.zip`: 保存済み作業フォルダのダウンロード

起動例:

```sh
curl -H 'Authorization: Bearer <token>' \
  -H 'Content-Type: application/json' \
  -X POST http://localhost:8080/api/runs \
  -d '{"branch":"main","taskLabel":"build","inputValues":{}}'
```

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

- `storage.backend: local` でローカルディスクに履歴や成果物を保存できます
- 同じマシンに履歴や repository cache が残るため、繰り返し実行でキャッシュを最大限に活かせます

#### サーバーレスモード

Google Cloud Run functions、AWS Lambda、AWS ECSなどを使い、アクセスがあった時のみサーバーリソースを使うモードです。ビルド作業などはブロックストレージがあるため問題なくできますが、アクティブなインスタンスがなくなるとブロックストレージもリセットされてしまうため、`.git`のクローンや、外部パッケージのキャッシュなどがなくなる可能性があります。巨大プロジェクトなどでは常駐モードを使うか、AWS EFSやGoogle Cloud Firestoreなどをマウントして作業フォルダとしてください。

サーバーレスモードでは実行結果、成果物、ワークの保存先としてS3互換のオブジェクトストレージを使います。

```yaml
storage:
  backend: object
  object:
    endpoint: https://s3.example.com
    bucket: runtask
    region: ap-northeast-1
```

## ライセンス

この project は GNU Affero General Public License v3.0 以降で提供します。
[LICENSE](LICENSE) を参照してください。


