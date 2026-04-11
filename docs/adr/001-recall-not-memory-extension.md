# ADR-001: Raw-text pull-based recall over LLM-summarized memory extension

## Status

Accepted

## Context

Claude Code writes each session's conversation as JSONL files under `~/.claude/projects/`. These files persist for a limited time before Claude Code deletes them, and `/compact` compresses running sessions, discarding detail in the process. The problem to solve is: how do we make past sessions reachable after they would otherwise be lost?

A prominent reference implementation for this problem is [claude-mem](https://github.com/thedotmack/claude-mem). It takes a "memory extension" approach:

- A `PostToolUse` hook captures every tool invocation in real time
- An LLM (Anthropic / Gemini / OpenRouter) summarizes each observation into structured facts, classifies it (bugfix / feature / decision / ...), and extracts title / subtitle / concepts / files touched
- Results are stored in SQLite plus a Chroma vector database for hybrid FTS + semantic search
- A `SessionStart` hook automatically injects relevant past observations into the next session's prompt
- A resident worker service on `:37777` ties it all together

The effect is that the agent silently "picks up where it left off" without being asked. From the agent's perspective, its working memory appears to span sessions.

This approach is technically coherent and solves a real user need. It also has real costs:

1. **LLM summaries drift from the source.** By the time you want the exact wording of a past exchange, all that's left is a summary. The original context may already be gone.
2. **Non-determinism.** What gets stored depends on the LLM's run-time judgement. The same session indexed twice can produce different observations. You can't `git log` that.
3. **API cost and runtime footprint.** Per-tool-use LLM calls cost money. The runtime grows to include Bun, Python (uv), Chroma, a resident HTTP worker, and at least one external AI provider account.
4. **Phantom memory.** Auto-injection is useful until the agent confidently references a hallucinated summary of something that never happened the way it "remembers". The user has no warning that this is happening.
5. **Privacy surface.** Every tool use flows through an external LLM during indexing.

Given these costs, it's worth asking whether the right problem framing for this project is "extend the agent's memory" at all.

### Two framings of the same problem

| | Memory extension | Recall tool |
|---|---|---|
| Goal | Make the agent appear to remember across sessions | Make past sessions reachable when explicitly looked up |
| Layer | Intervenes at the agent's memory layer | Stays at the filesystem layer |
| Metaphor | RAG + auto-memory | A better `grep` for JSONL files |
| Interaction model | Push: system injects context at `SessionStart` | Pull: agent calls a tool when it realizes it needs past context |
| Who triggers retrieval | The framework, automatically | The agent, deliberately |
| Storage format | LLM-derived observations (summary / classification / concepts) | Raw conversation text, noise-stripped |

agent-recall chooses the second framing.

## Decision

**Archive raw JSONL content into SQLite with FTS5 full-text search. Do not summarize with an LLM, do not auto-inject past context into new sessions, and do not run any resident process during indexing. Past context is reached by explicit MCP tool calls (`recall_search` and friends) that the agent issues when it realizes it doesn't know something.**

In short: agent-recall is a recall tool, not a memory system.

### Rationale

1. **Raw logs don't lie.** What's stored is literally what happened in the session. There is no interpretation layer to audit, retrain, or second-guess. If a search result looks wrong, the fix is a better query, not a better summarizer.

2. **Deterministic indexing.** Importing the same JSONL always produces the same rows. This makes the archive behave like `git log`: something you can trust as a source of truth, re-run freely, and reason about without worrying about model drift.

3. **Zero LLM calls on the hot path.** Indexing, dedup, and search run entirely on local SQLite. No API keys are required to use the tool. No per-tool-use cost. No network latency. No privacy surface beyond the user's own machine.

4. **Pull-based matches how agents already work.** A well-designed agent is already comfortable with "I don't know X, let me look it up". Adding a `recall_search` MCP tool slots into that existing pattern exactly. It's just one more tool, no different from `Grep` or `Read`. By contrast, auto-injection fights the agent's judgement: the framework decides what's relevant before the agent has even seen the prompt.

5. **No phantom memory.** Because nothing is auto-injected, the agent never has the experience of "confidently remembering" something it didn't actually see in the current context. If it needs to know what happened last time, it asks explicitly, reads the result, and cites it. This keeps the failure mode honest: "I don't have that, let me search" instead of "I'm pretty sure we decided X last week".

6. **Layer separation.** This project operates at the filesystem layer: JSONL in, SQLite out, plus a search interface. The agent's memory layer is left entirely to the agent and its framework. Mixing the two layers couples every search improvement to an LLM inference and every indexing run to an API bill. Keeping them separate lets each part evolve independently.

7. **Aligned with the "zero dependencies" constraint.** agent-recall distributes as a single `deno compile` binary with no runtime dependencies. Adding an LLM pipeline would require network access, an API key, a vector database, and a resident process, none of which fit this constraint. The recall-tool framing is compatible with the existing distribution model; the memory-extension framing is not.

### What this decision explicitly rejects

These are things agent-recall will not do, even as optional features, unless this ADR is superseded:

- **LLM-based summarization** of observations during indexing (no classification into bugfix / feature / decision, no extracted facts/concepts fields)
- **Vector or semantic search** as a core capability. FTS5 full-text match is the contract.
- **Automatic context injection** at `SessionStart`, `UserPromptSubmit`, or any other hook. Past context is only surfaced when the agent or user asks.
- **Real-time per-tool-use ingestion.** The archive boundary is a whole session; `PostToolUse` is not the right granularity for this project. Real-time UI updates are a separate concern addressed differently.
- **A resident background service for indexing.** The importer is a short-lived process that runs, writes, and exits.

### Why not offer both modes

Supporting memory extension as an opt-in mode alongside the recall-tool core was considered and rejected. Two reasons:

1. **It splits the positioning.** A tool that is "sometimes a recall tool and sometimes a memory system" has to explain both philosophies and both failure modes to every user. The value of the recall-tool framing is that the mental model is small and consistent.
2. **The dependencies leak.** The moment LLM summarization becomes supported, even optionally, the project inherits API keys, network dependencies, non-determinism, and vendor choice debates. The "zero dependencies, one binary, MIT" story erodes regardless of whether a given user enables the mode.

If a future user wants memory extension, claude-mem is a good answer. The two tools are not mutually exclusive on the same machine.

## Consequences

### Benefits

- **Zero dependencies, offline, deterministic.** Indexing and search require nothing beyond the single binary and the local SQLite file. Works on air-gapped machines.
- **No API cost, no privacy surface.** Conversations never leave the user's machine during indexing.
- **Trustable archive.** Because what's stored is the literal conversation text, the archive can serve as a source of truth for past work, the same role `git log` plays for code history.
- **Simple mental model.** "Claude Code writes JSONL, agent-recall makes it searchable." That's the whole contract. Users and contributors don't need to understand an observation taxonomy, a summarization prompt, or a vector index.
- **Composability.** CLI / MCP / Web UI all read the same SQLite file with FTS5. Each interface is a thin adapter; nothing is duplicated across them.
- **License freedom.** No derivative-LLM-output questions, no AGPL contagion from upstream dependencies. agent-recall is MIT.

### Drawbacks

- **No silent "picks up where it left off" experience.** The user or the agent has to explicitly invoke recall. First-time users may expect auto-continuation and be surprised when it doesn't happen. This is a deliberate choice but it's a real UX cost.
- **No semantic search.** Queries that don't share vocabulary with the stored text won't match. "How did we handle auth errors?" may miss sessions that said "401 response" without the word "auth". Users have to learn to phrase queries in terms the past session actually used.
- **Raw text uses more storage than summaries.** Noise-stripping reduces size to about 7% of raw JSONL, but that's still larger than a few hundred bytes of LLM-distilled observations. For very heavy users this may matter eventually.
- **The agent has to know when to look.** If the agent doesn't realize past context would help, it won't call `recall_search`, and the archive goes unused. Good prompting (via CLAUDE.md or the MCP tool description) partially mitigates this, but it's a softer guarantee than auto-injection.
- **No cross-session "understanding".** agent-recall cannot answer questions like "summarize the last two weeks of work on this project" without the agent doing the summarization itself at query time. There is no pre-computed summary layer.

### Not decided here

- **Whether to add an optional `agent-recall summarize <session>` command** that runs an LLM over a stored session on demand. Deferred. If added, it must remain a pure read side-channel: the summary is not written back into the main archive, and the core indexing path must stay LLM-free.
- **Whether to add richer search operators** (date ranges, project scoping, role filters) on top of FTS5. Deferred. These are additive to the recall-tool framing and can be handled case by case.
- **Whether to support agents other than Claude Code** (Cursor, Gemini CLI, and so on) by watching additional JSONL directories. Deferred, but this decision does not block it: the recall-tool framing is inherently agent-agnostic, and the filesystem-layer approach makes adding new sources a matter of pointing the importer at a new path.
