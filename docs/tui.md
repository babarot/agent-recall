# TUI

`recall` (or `recall tui`) lists every archived session, most recently ended first.

The title is the session's `/rename` name, or else the title Claude Code generated, or else its first prompt.

## Keys

| Key | Action |
|-----|--------|
| `↑` `↓` / `j` `k` | Move (`g` `G` for top and bottom, PgUp and PgDn by page) |
| `Enter` | Resume the session: `claude -r <id>` from the session's folder |
| `y` | Copy the session ID, to hand it to another agent ("look this session up with claude-recall") |
| `Y` | Copy the resume command |
| `Space` | Read the conversation over the detail pane (see [Reading a conversation](#reading-a-conversation)) |
| `/` | Filter the list (see [Filter](#filter)); `Esc` clears it |
| `a` | Ask Claude to find sessions (see [Ask Claude](#ask-claude)) |
| `s` | Choose the sort order (ended, started, message count or size) from a menu: `↑` `↓` or a number and `Enter`, or a click; on the session list only |
| `.` | Switch between the folder `recall` was started in and all folders |
| `←` `→` / `h` `l` | Show or hide the folder list (see [Folders](#folders)) |
| `+` `-` | Make the detail pane taller or shorter |
| `Tab` `Shift+Tab` (or `]` `[`) | Move focus along the folder list (when shown), the sessions and the detail pane's frames, in the order they are laid out; `↑` `↓`, `j` `k`, PgUp, PgDn, `g` and `G` then scroll it, `Esc` returns to the list |
| `?` | Show every key, grouped by where it works; `?`, `Esc` or `q` closes the list |
| `q` | Quit |

## Filter

`/` filters by title, folder, branch, ID or what was said in the conversation. Words of two letters or more are also looked up in the conversation, in the background.

| Filter | Matches |
|--------|---------|
| `<word>` | Title, folder, branch, ID or the conversation |
| `text:<word>` | Only the conversation |
| `title:<part>` | Part of the title |
| `branch:<part>` | Part of the branch |
| `worktree:<part>` | Part of the worktree name |
| `id:<prefix>` | The start of the session ID |
| `folder:<name>` | Folders whose name matches fuzzily, as the folder list's search does (`folder:bdot` for babarot/dotfiles), within the folder the list is narrowed to; `in:` is the same, for short |

Several of one key match any of them.

While typing:

- Two letters of a key, such as `bra`, show the rest faintly; `Tab` or `→` types it.
- `folder:` (and `in:`), `branch:` and `worktree:` suggest their values as you type. While the suggestions show, `↑` `↓` and `Enter` pick one, `Tab` completes the highlighted one and `Esc` closes them. The mouse clicks and scrolls them too.

## Ask Claude

`a` opens a box for a question, for when you remember what a session was about but not what to type in the filter. Claude Code looks through the archive with recall's search:

```console
claude -p <question> --model <ask_model> --no-session-persistence \
  --strict-mcp-config --mcp-config <recall mcp> \
  --tools "" --allowedTools mcp__recall__recall_search,mcp__recall__recall_list
```

It runs signed in as you, a Claude plan included, so recall needs no API key. It runs from a directory of its own, so no project's settings apply, and no session is saved.

- While it works, the box shows each search it makes; `Esc` cancels.
- The answer lists the sessions found with why each matched, the model, the time taken and the cost claude reports.
- `Enter` jumps to one (clearing a folder or filter that hides it), `f` narrows the list to all of them in Claude's order with the reason under each row, and `r` asks again. `Esc` clears the narrowed list.
- The reason stays in Conversation for a session Claude picked.

## Folders

Started inside a repository (or one of its worktrees, or a subdirectory), `recall` lists only that repository's sessions, with a Worktree column in place of Folder; `.` shows every folder again. A folder with no sessions starts with all of them. `recall --all` starts with every folder for one run, and `--all=false` with the folder when `scope = "all"` is set.

The folder list on the left narrows the list to any folder: a repository together with its worktrees, or a directory outside git.

- `←` opens it and, pressed again, moves into it. `↑` `↓` there pick a folder.
- `/` searches the folders by fuzzy match (`bdot` finds babarot/dotfiles); `Enter` keeps the search, `Esc` clears it.
- `→` (or `Enter`) returns to the sessions, and `→` again closes it.
- The focused side has accent rules and the selection bar.

It needs a terminal at least 100 columns wide with the detail pane below, and whether it is open is remembered.

A worktree that has since been removed is shown struck through, with its repository and name guessed from where herdr (`~/.herdr/worktrees/<repo>/worktree-<name>`) or Claude Code (`<repo>/.claude/worktrees/<name>`) put it.

## Detail pane

The detail pane has three frames:

- Conversation: the first request, a `⋮ N messages` marker for what lies between, and the latest messages, always including the last thing you said
- What was done: activity over the session, then bars for the tools used most and the commands run most, and the edited files grouped by repository
- Details: when, how much, where: times, counts, size, branch, ID, version, and the folder's full path

Below the list, Details sits under Conversation in a few wide lines and What was done runs down the right. A taller pane shows more of the conversation and of What was done. The height you pick is remembered in `~/.local/state/claude-recall/state.json`.

### Reading a conversation

`Space` spreads the conversation over the detail pane, wrapped, and the pane grows to leave the list a few rows (`+` `-` or dragging its edge change how many, and they are remembered). `Tab` back to the list and `j` `k` read the next session in place; `Space` or `Esc` puts the pane back.

## Mouse

Click a session to select it, click a frame to focus it, scroll the wheel over the list or over a frame, and drag the pane's top edge (or the row count line just above it) to resize the pane. While the TUI has the mouse, most terminals still select text when you hold Shift (Option in iTerm2) while dragging.

## Settings

The `[tui]` section of the config file holds the TUI's settings: where the detail pane goes and how tall it starts, the color scheme, which sessions to start with, and the model and display of `a`. See [Configuration](../README.md#configuration) in the README for the whole file.
