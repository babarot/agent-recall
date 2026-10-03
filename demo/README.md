# Demo

`demo.gif` in the README is recorded with [VHS](https://github.com/charmbracelet/vhs) from `demo.tape`:

```bash
make demo
```

It builds `recall`, runs `demo/gen` and then `vhs demo/demo.tape`. Re-record it after a change to how the TUI looks, and commit the new `demo.gif`.

- `gen/` writes a demo home directory with a few git repositories and worktrees, Claude Code transcripts of the sessions in `gen/scenario.go`, and an archive imported from them. It lives under the temporary directory, outside any git repository, and `demo/.out/env.sh` points a shell at it, so nothing of your own archive, config or home shows up.
- `bin/claude` stands in for Claude Code: `a` in the TUI runs it, and it replays the answer `gen` wrote.
- Dates are relative to when `gen` runs, so the demo always reads "Today" and "Yesterday".
