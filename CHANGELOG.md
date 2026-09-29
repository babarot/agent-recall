# Changelog

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
