# Changelog

## [1.4.0](https://github.com/babarot/claude-recall/compare/1.3.1...1.4.0) - 2026-10-03
### New Features
- Set the archive and the web UI port in the config file by @babarot in https://github.com/babarot/claude-recall/pull/31
- Read transcripts from CLAUDE_CONFIG_DIR, add recall --all, and test the CLI by @babarot in https://github.com/babarot/claude-recall/pull/32
- Add folder: to the TUI filter, with in: for short by @babarot in https://github.com/babarot/claude-recall/pull/35
- Search the spread conversation with / by @babarot in https://github.com/babarot/claude-recall/pull/36
### Improvements
- Parse the CLI with cobra by @babarot in https://github.com/babarot/claude-recall/pull/30
- Keep the TUI moving while a long session's detail loads by @babarot in https://github.com/babarot/claude-recall/pull/37
### Others
- Add a demo GIF of the TUI, recorded with VHS by @babarot in https://github.com/babarot/claude-recall/pull/33

## [1.3.1](https://github.com/babarot/claude-recall/compare/1.3.0...1.3.1) - 2026-10-03
### Bug fixes
- Draw the TUI once the terminal's size settles by @babarot in https://github.com/babarot/claude-recall/pull/26

## [1.3.0](https://github.com/babarot/claude-recall/compare/1.2.0...1.3.0) - 2026-10-02
### New Features
- Search the conversation from the TUI filter and add field keys by @babarot in https://github.com/babarot/claude-recall/pull/21
- Move focus with Tab in the TUI and list the keys with ? by @babarot in https://github.com/babarot/claude-recall/pull/23
- Read the conversation over the TUI detail pane and pick the sort from a menu by @babarot in https://github.com/babarot/claude-recall/pull/24
- Ask Claude to find sessions from the TUI by @babarot in https://github.com/babarot/claude-recall/pull/25

## [1.2.0](https://github.com/babarot/claude-recall/compare/1.1.0...1.2.0) - 2026-10-02
### New Features
- Narrow the TUI to a folder and add a folder list by @babarot in https://github.com/babarot/claude-recall/pull/19

## [1.1.0](https://github.com/babarot/claude-recall/compare/1.0.1...1.1.0) - 2026-10-02
### New Features
- Improve the TUI's look, detail pane and mouse support by @babarot in https://github.com/babarot/claude-recall/pull/17

## [1.0.1](https://github.com/babarot/claude-recall/compare/1.0.0...1.0.1) - 2026-10-02
### Bug fixes
- Keep MCP servers up when several import at once by @babarot in https://github.com/babarot/claude-recall/pull/15

## [1.0.0](https://github.com/babarot/claude-recall/compare/0.2.0...1.0.0) - 2026-10-02
### Breaking Changes
- Rewrite in Go, add a TUI and rename to claude-recall by @babarot in https://github.com/babarot/claude-recall/pull/13

## [0.2.0](https://github.com/babarot/agent-recall/compare/0.1.1...0.2.0) - 2026-09-29

## [0.1.1](https://github.com/babarot/agent-recall/compare/0.1.0...0.1.1) - 2026-09-29
### Bug fixes
- Fix timer handle types that broke the release build, and catch type errors in CI by @babarot in https://github.com/babarot/agent-recall/pull/8
- Fix a flaky mtime check in the import resync test by @babarot in https://github.com/babarot/agent-recall/pull/10

## [0.1.0](https://github.com/babarot/agent-recall/commits/0.1.0) - 2026-09-29
### Breaking Changes
- Fix silent message drops by switching to uuid-based natural-key dedup by @babarot in https://github.com/babarot/agent-recall/pull/6
### New Features
- Add web UI for browsing sessions and chat history by @babarot in https://github.com/babarot/agent-recall/pull/2
- Add real-time web UI with FS watcher + SSE + incremental imports by @babarot in https://github.com/babarot/agent-recall/pull/3
### Improvements
- UI improvements: chat rendering, session list, and settings by @babarot in https://github.com/babarot/agent-recall/pull/4
- Extract sessions data layer into a signals-backed store by @babarot in https://github.com/babarot/agent-recall/pull/5
