# ADR-003: SQLite mirrors the JSONL; no independent archive layer

## Status

Accepted

## Context

After PR that switched the message uniqueness key from `(session_id, turn_index)` to `(session_id, uuid, block_index)` and simplified import to `resetSession + full reparse`, the `messages` / `images` tables became an exact mirror of the session's current JSONL file on disk.

A concern was then raised: if Claude Code's `/compact` command shrinks a JSONL, the mirror DB will shrink too — and for a tool named `agent-recall`, silently losing history to compaction is a functional regression. The working assumption was that `/compact` is destructive to the JSONL, the same way it is destructive to the in-flight session context.

Two alternative designs were considered:

1. **Two-tier archive** (`messages` + `messages_archive`): before each `resetSession`, move disappeared rows to an archive table. Rejected — it fragments every read path (UI ChatView, `/api/search`, MCP `recall_search`, FTS5) and breaks conversational continuity for an LLM consumer reading the transcript.

2. **Explicit append-only with natural-key dedup** (claude-mem-style): drop `resetSession`, rely on `INSERT OR IGNORE` on `(session_id, uuid, block_index)`. All historical rows stay even if the JSONL shrinks. Requires switching ordering from `turn_index` to timestamp-based, UPSERT logic for sessions with NULL/empty-string guards on MIN/MAX, UNIQUE on `images`, and careful handling if `/compact` emits same-content under new uuids (content_hash dedup as a safety net).

Before committing to option 2, the assumption that `/compact` shrinks the JSONL was verified empirically against 456 local sessions.

### Empirical findings

Scanning `~/.claude/projects/**/*.jsonl` for compact markers:

- 7 sessions contain `subtype: "compact_boundary"` system lines
- 7 sessions contain `isCompactSummary: true` flags on a user message
- 7 `compactMetadata` objects with `{trigger: "auto", preTokens: ~170K}`

Inspecting one session (`f4b2a51e-...`, 770 lines, 2.3 MB):

```
line 567  type=assistant                               ← last pre-compact message
line 568  type=system, subtype=compact_boundary        ← marker only, no content
line 569  type=user, isCompactSummary=true             ← synthesized summary, new uuid
line 570  type=assistant                               ← new conversation resumes
...
```

Pre-compact user+assistant lines: 296, timestamps from 2026-03-17 03:22 onward.
Post-compact user+assistant lines: 95, timestamps from 2026-03-18 04:16 onward (~25 hours later).
File size: 2.3 MB, strictly growing across the compact event.

**`/compact` is additive to the JSONL, not destructive.** It appends a boundary marker and a synthesized summary with new uuids, then the conversation resumes normally. Pre-compact content stays in the file with its original uuids. The file never shrinks.

This invalidates the premise that motivated the archive work. The mirror DB does not lose data on compact because the JSONL does not lose data on compact.

### When can the JSONL actually shrink?

| Scenario | Frequency | Mirror behaviour | Desired? |
|---|---|---|---|
| `/compact` (auto or explicit) | common | no shrink — file grows | n/a |
| `/clear` | occasional | no shrink — new session file | n/a |
| User manually edits JSONL to remove content | rare, deliberate | shrinks with file | yes (user intent) |
| Claude Code bug / crash mid-write | unobserved | possibly shrinks | out of scope |

The only realistic case where the mirror drops data is user-initiated deletion, which is the correct response.

### agent-recall vs claude-mem on this axis

claude-mem's observation table has no DELETE path and carries content-hash dedup plus a file-shrinkage-reset heuristic. This makes sense for claude-mem because observations are **derived** from the JSONL (semantic extraction, LLM classification) and the derivation pipeline has to be idempotent on its own terms regardless of source behaviour.

agent-recall has no derivation step. The JSONL *is* the archive, and SQLite is its searchable index. When the source is already append-only, mirroring it is functionally equivalent to explicit append-only storage for every observed scenario. The implementation divergence only matters if the source violates append-only, which does not normally occur.

## Decision

**`messages` and `images` remain a mirror of the current JSONL, rebuilt via `resetSession + full reparse` on every import. No explicit append-only logic, no archive table, no content-hash dedup, no retention policy.**

The design rests on one invariant: **Claude Code's JSONL files are append-only in practice**. If that invariant breaks in a future Claude Code version, this decision must be re-opened.

### What this explicitly accepts

- If the user manually edits a JSONL to remove content, that content leaves the DB on the next import. This is desired behaviour, not a bug.
- If Claude Code introduces a JSONL-shrinking operation in a future version, the mirror will follow and potentially lose data. We'll revisit when observed.
- We do not defend against hypothetical bugs that would cause same-content-different-uuid duplication inside a single JSONL. If it starts happening, the dedup strategy is a separate decision.

### What this explicitly rejects

- A `messages_archive` table or any two-tier storage model.
- A UI affordance for "archived" vs "live" messages — the distinction is not meaningful under this model.
- Tracking `observed_at` / `last_seen_at` metadata on message rows.
- content_hash-based secondary dedup.
- Any retention policy on message rows; they live exactly as long as the source JSONL does.

## Consequences

### Benefits

- **One read path for everything.** UI, MCP, and search hit a single `messages` table. No UNION, no archived-vs-live branching, no per-consumer policy choices.
- **Conversational continuity preserved.** An LLM reading a session through MCP sees a single linear transcript, which is the shape LLMs consume best.
- **Minimal code surface.** Import is one branch: parse, reset, re-insert. Every failure mode (new session, append, interrupted prior run, `/compact`, watcher+CLI concurrency) resolves through the same path.
- **DB size tracks disk.** `du -sh ~/.claude/projects ~/.claude/vault.db` tells a consistent story. No "why is my vault bigger than the source" surprises.
- **Small mental model.** "`messages` is a searchable copy of the current JSONL." That's the whole contract.

### Drawbacks

- **Design depends on Claude Code's append-only JSONL invariant.** A regression in Claude Code's write behaviour breaks the design premise. This is the single load-bearing assumption.
- **No defense against disk-level truncation.** If a user's JSONL is corrupted or an errant script truncates one, the next import propagates the damage into the vault. Mitigation (file-level backup) is out of scope here.
- **Not a forensic log.** If a user removes sensitive content from a JSONL manually, we cannot reconstruct it. Arguably a feature.

### Not decided here

- **A `--prune` CLI command** to explicitly drop a session's rows. Deferred. Does not conflict with this ADR.
- **A file-level backup layer** (e.g., watcher snapshots `*.jsonl` to a separate archive directory before each import). Deferred. Would sit beside the DB, not inside it.
- **Migrating from `resetSession` to an `INSERT OR IGNORE`-only flow** as a micro-optimisation. Under this ADR's invariant it would be behaviour-equivalent; not worth the diff.

## History

- The mirror design was established implicitly in the initial importer and made explicit in the PR that switched to `(session_id, uuid, block_index)` uniqueness.
- Two review rounds with `codex` converged on an explicit append-only proposal with non-trivial implementation concerns (ordering, NULL handling in UPSERT, content-hash risk).
- Empirical investigation of 456 local sessions (7 with compact events) showed `/compact` is additive to the JSONL. The append-only refactor was cancelled; this ADR records why.
