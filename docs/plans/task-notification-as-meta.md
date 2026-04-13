# Plan: Classify task-notification user lines as meta

## Problem

Background command completion notifications (Claude Code の "Background command … completed/failed" 通知) currently render as normal "You" chat bubbles in the UI — making it look as if the human typed them.

Root cause is in `src/parser.ts`. The JSONL line for a completion notification looks like:

```json
{
  "type": "user",
  "message": { "role": "user", "content": "<task-notification>…<summary>Background command \"…\" failed with exit code 144</summary></task-notification>" },
  "origin": { "kind": "task-notification" },
  …
}
```

Key properties:
- `type: "user"` and `message.role: "user"` → passes `EXTRACTABLE_TYPES` in src/parser.ts:14
- `isMeta` は付いていない → meta 折り畳み分岐 (src/parser.ts:132) を通らない
- `content` が string なので src/parser.ts:147-155 で `blockType: "text"` として普通のユーザー発話扱いになる

結果:
1. UI の `ChatView` は `blockType: "text"` + `role: "user"` を "You" バブルで描画
2. セッションの **最初の** task-notification が `firstUserText` を上書きする可能性があり、サイドバーの firstPrompt が `<task-notification>…` になり得る
3. FTS もそのまま検索対象になる (これは悪影響ではないが、役割の違うコンテンツが混ざる)

`queue-operation` タイプ (同じ `<task-notification>` を運ぶ enqueue イベント) は既に `SKIP_TYPES` (src/parser.ts:17-24) で除外されているので、問題は **後続の `type: "user"` 行** だけ。

## Goal

- Task-notification 行を **meta 折り畳みバブル** として描画する (`MetaBubble`)
- `firstPrompt` / `firstUserText` を汚さない
- Settings の "show meta" トグル (`settings.showMeta`) で一括表示/非表示を切り替えられる (既存の挙動の継承)
- FTS 検索では引き続きヒットする (content は保持)

## Non-goals

- 新しい `blockType` の追加 (meta を再利用)
- 新しい UI コンポーネント追加 (`MetaBubble` を再利用)
- task-notification 以外の `origin.kind` ハンドリング (今見えてるのは `task-notification` のみ)
- DB スキーマ変更 (不要)

## Design

### 1. `JournalLine` 型に `origin` を追加

`src/types.ts`:

```ts
export interface JournalLine {
  …
  isMeta?: boolean;
  /**
   * System-injected lines that arrive as `type: "user"` but weren't typed by
   * the human. Currently the only observed kind is `"task-notification"`
   * (background command completion/failure), which we collapse into a meta
   * bubble just like `isMeta` expansions.
   */
  origin?: { kind?: string };
  …
}
```

### 2. `parser.ts` で meta 判定を拡張

現状 (src/parser.ts:132):

```ts
if (parsed.isMeta) {
  …
}
```

これを:

```ts
const isSystemInjected =
  parsed.isMeta === true || parsed.origin?.kind === "task-notification";

if (isSystemInjected) {
  const text = extractText(content); // string の場合は raw trim、array の場合は text block を join
  if (text) {
    messages.push({
      uuid: parsed.uuid,
      role,
      blockType: "meta",
      content: text,
      timestamp: ts,
      turnIndex: turnIndex++,
    });
  }
  continue;
}
```

注意: 既存の `extractText` は `string | ContentBlock[]` 両対応 (src/parser.ts:27-43)。task-notification の content は string なので問題なく動く。既存の isMeta テスト (parser_test.ts:474) では array 形式だったが、同じ関数で両方処理できる。

### 3. `firstUserText` / `lastTimestamp` を汚さない

- **firstUserText**: `if (!firstUserText && parsed.type === "user")` (src/parser.ts:151, 166) の判定は system-injected 分岐の `continue` の後ろなので、自動的に汚染されない (現状も isMeta は firstUserText を汚さない; parser_test.ts:493 がそれを担保)。追加作業なし。

- **lastTimestamp / endedAt**: 現状 src/parser.ts:117-119 の更新は isMeta / origin 判定より **前** にあり、`type === "user"` である限りどんな内容でも endedAt を押し上げる。task-notification をメタ扱いにしつつ endedAt だけ通知で動き続けると、サイドバーの並び順が通知の着信時刻で動いてしまい「人間が書いていないものは人間扱いしない」という修正の中核と矛盾する。よって **lastTimestamp 更新も system-injected 判定の後ろに移す**。

具体的には、parser.ts の 117-119 行の `lastTimestamp` 更新を削除し、今回導入する system-injected 分岐の外側 (= 通常の user text / content-block ループ) 内で行うように移動する。擬似コード:

```ts
// (旧) ここで lastTimestamp を更新しない
// if (parsed.timestamp && parsed.type === "user") { lastTimestamp = ... }

if (!parsed.uuid || !parsed.message?.content) continue;

const content = parsed.message.content;
const role = parsed.message.role;
const ts = parsed.timestamp ?? "";

const isSystemInjected =
  parsed.isMeta === true || parsed.origin?.kind === "task-notification";

if (isSystemInjected) {
  // meta push ... (lastTimestamp は触らない)
  continue;
}

// ここ以降で初めて lastTimestamp を更新する
if (ts && parsed.type === "user") {
  lastTimestamp = ts;
}
```

副作用として **既存の isMeta 行も lastTimestamp を動かさなくなる**。これは本来望ましい挙動だが、従来通りのソート順に依存するユーザーはごく稀のはずで、かつ sessions-index.json 由来の `modified` 等で補完される経路は無いので、パース側の挙動が変わる点をテストで明示する。

### 4. `summarizeMeta` に task-notification 判定を追加

`ui/src/lib/chat-utils.ts` の `summarizeMeta` (L173):

```ts
// Background task notifications
if (trimmed.startsWith("<task-notification>")) {
  const status = extractTag(content, "status") ?? "";
  const summary = extractTag(content, "summary") ?? "";
  // summary 例: Background command "Ask codex for architecture opinion" failed with exit code 144
  const short = summary.length > 80 ? summary.slice(0, 80) + "…" : summary;
  return short || (status ? `Background task ${status}` : "Background task");
}
```

`extractTag` は既に同ファイルに存在 (L20)。

### 5. DB 再構築

既存 DB には task-notification 行が `blockType: "text"` で入っているため、本修正を反映するには `rm ~/.claude/vault.db` して再ビルドが必要 (CLAUDE.md のポリシー通り)。README / CHANGELOG には載せず、コミットメッセージで触れる。

## Tests

### parser_test.ts

新規テスト (META_MSG と同じ形で TASK_NOTIFICATION_MSG ヘルパを足す):

1. `parseJournalLines collapses task-notification origin into blockType=meta`
   - `origin: { kind: "task-notification" }` かつ `message.content` が `<task-notification>…</task-notification>` の string を 1 本投入
   - messages[i].blockType === "meta"
   - content は元の XML をそのまま保持 (summarizeMeta は UI 側の責務)

2. `parseJournalLines task-notification does not pollute firstUserText`
   - 1 本目が本物の user text、2 本目が task-notification
   - firstUserText === 本物の user text

3. `parseSession firstPrompt is not a task-notification`
   - セッション冒頭に task-notification、その後に本物の user text
   - meta.firstPrompt === 本物の user text

4. `parseJournalLines task-notification does not advance lastTimestamp`
   - 本物の user text (t1) → task-notification (t2, t1 より後)
   - result.lastTimestamp === t1 (通知側の t2 ではない)

5. `parseJournalLines isMeta does not advance lastTimestamp` (既存挙動の変更を明示)
   - 本物の user text (t1) → isMeta user line (t2)
   - result.lastTimestamp === t1

### chat-utils.test.ts

既存のファイルに `summarizeMeta` の task-notification ケースを追加:

1. `<task-notification>…<summary>Background command "X" failed with exit code 144</summary></task-notification>` → `Background command "X" failed with exit code 144`
2. summary タグが無いケース → `Background task <status>` fallback
3. summary が 80 文字超 → 末尾 `…`

## Files changed

- `src/types.ts` — `JournalLine.origin` 追加
- `src/parser.ts` — `isMeta` 分岐を `isSystemInjected` に拡張
- `src/parser_test.ts` — テスト 3 本追加
- `ui/src/lib/chat-utils.ts` — `summarizeMeta` に task-notification 判定追加
- `ui/src/lib/chat-utils.test.ts` — テスト追加

## Risks / edge cases

- **`origin.kind` が想定外の値** のとき: 今のところ `task-notification` しか観測されていない。未知の kind は従来通り `blockType: "text"` のままにしておく (完全な allow-list マッチ)。`origin.kind` が存在するだけで meta 扱いにしてしまうと、将来別用途の origin が出たときに UI が勝手に隠してしまうため避ける。
- **`message.content` が array のケース**: 仕様上 string で来ているが、防御的に array も `extractText` で処理できる (既存 isMeta ロジックと同じ)。
- **既存 DB に残った text 行**: rebuild が必要。schema 変更はないので `CREATE IF NOT EXISTS` の再実行は無害。
- **`lastTimestamp` の挙動変更**: system-injected 行 (isMeta / task-notification の両方) では endedAt を前進させなくなる。既存 DB をリビルドすると、既存セッションの endedAt が「最後の本物の user 発話」の時刻にリセットされ、サイドバーの並び順が若干変わる可能性あり。これは意図した修正。
