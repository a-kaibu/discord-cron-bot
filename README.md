# discord-cron-bot

GitHub Actions で毎日 JST 0:00 に実行し、その日付に一致する設定があれば Discord Webhook にメッセージを送信する Bot です。

## 仕組み

- スケジュールは `config.json` で管理します
- メッセージ本文は GitHub Actions の Variables または Secrets で管理します
- `config.json` に書く値はメッセージキーではなく、GitHub Actions の secret/var 名です
- 当日が `YYYY-MM-DD` または `MM-DD` に一致したら、その名前の secret/var の中身を送信します

## 必要な環境変数

次の2種類が必要です。

### 1. Discord Webhook URL

- `DISCORD_WEBHOOK_URL`

GitHub Actions では `secrets.DISCORD_WEBHOOK_URL` のみを使います。

### 2. メッセージ定義

`config.json` に書いた名前と同じ名前の GitHub Actions Variables または Secrets を用意します。

例:

- `ANNOUNCEMENT=本日の告知です`
- `CHRISTMAS=メリークリスマス`
- `NEW_YEAR_GREETING=あけましておめでとうございます`
- `NEW_YEAR_NOTICE=今年もよろしくお願いします`

同じ名前が Secret と Variable の両方にある場合は Secret を優先します。

改行を本文に入れたい場合は値の中で `\n` を使えます。

## config.json

`config.json` は実運用用です。例は [config.example.json](/home/kota/prj/discord-cron-bot/config.example.json) を参照してください。

形式:

```json
{
  "schedules": {
    "2026-03-15": ["ANNOUNCEMENT"],
    "12-25": ["CHRISTMAS"],
    "01-01": ["NEW_YEAR_GREETING", "NEW_YEAR_NOTICE"]
  }
}
```

- `YYYY-MM-DD`: その年だけ送る
- `MM-DD`: 毎年送る
- 値: 送信する GitHub Actions secret/var 名の配列

例えば `2026-03-15` に `"ANNOUNCEMENT"` が設定されていれば、その日に `ANNOUNCEMENT` という名前の Secret または Variable の中身を Discord に送ります。

## GitHub Actions

- 実行 cron: `0 15 * * *`
- これは UTC 15:00 で、JST では翌日 0:00 です
- 手動実行 `workflow_dispatch` にも対応しています
- workflow は `toJson(vars)` と `toJson(secrets)` を Go プロセスに渡し、設定名に一致する値を引きます

## ローカル実行

```bash
DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/... \
ACTIONS_VARS_JSON='{"ANNOUNCEMENT":"本日の告知です"}' \
go run .
```

別ファイルを使う場合は `CONFIG_FILE` を指定します。

```bash
CONFIG_FILE=config.json go run .
```

## セットアップ手順

1. `config.example.json` を元に `config.json` を編集する
2. Discord Webhook URL を `secrets.DISCORD_WEBHOOK_URL` に設定する
3. `config.json` に書いた名前と同じ名前の Variables または Secrets を作る
4. GitHub Actions を有効化する
