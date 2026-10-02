---
name: recall
description: Open the claude-recall web UI, the archive of past coding agent sessions, in the browser. Opens the current session by default. Use for "/recall", "open recall", "show this session in claude-recall", "過去のセッションを UI で見たい".
argument-hint: "[list | stats | stop | <session-id>]"
---

# recall

Open the claude-recall web UI. It serves on http://localhost:6276 and has these pages:

- `/session/<id>`: one session as a chat
- `/`: the session list with search
- `/stats`: statistics

## Steps

1. Pick the page from the arguments: `$ARGUMENTS`
   - empty: the current session, `/session/${CLAUDE_SESSION_ID}`. If the session ID above was not substituted (an agent other than Claude Code), open `/` instead.
   - `list`: `/`
   - `stats`: `/stats`
   - `stop`: run `recall ui stop`, report its output and stop here.
   - anything else: treat it as a session ID and open `/session/<it>`.
2. Start the server. It runs in the background and only prints the URL if one is already running, so run it every time:
   ```bash
   recall ui
   ```
3. Open the page in the browser: `open <url>` on macOS, `xdg-open <url>` on Linux.
4. Reply with the URL in one line.

For the current session, the UI imports new sessions when it starts and watches `~/.claude/projects` while it runs, so a session that has just begun shows up without an import.
