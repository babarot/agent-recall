# TUI

`recall` (or `recall tui`) lists every archived session, most recently ended first.

The title is the session's `/rename` name, or else the title Claude Code generated, or else its first prompt.

## Keys

| Key | Action |
|-----|--------|
| `↑` `↓` / `j` `k` | Move (`g` `G` for top and bottom, PgUp and PgDn or `ctrl+d` and `ctrl+u` by page) |
| `Enter` | Resume the session: `claude -r <id>` from the session's folder |
| `c` | Continue the session in a new claude (see [Continuing in a new claude](#continuing-in-a-new-claude)) |
| `y` | Copy the session ID, to hand it to another agent ("look this session up with claude-recall") |
| `Y` | Copy the resume command |
| `Space` | Read the conversation over the detail pane (see [Reading a conversation](#reading-a-conversation)) |
| `/` | Filter the list (see [Filter](#filter)); `Esc` clears it |
| `a` | Ask Claude to find sessions (see [Ask Claude](#ask-claude)) |
| `s` | Choose the sort order (ended, started, message count or size) from a menu: `↑` `↓` or a number and `Enter`, or a click; from any pane |
| `.` | Switch between the folder `recall` was started in and all folders |
| `←` `→` / `h` `l` | Show or hide the folder list (see [Folders](#folders)) |
| `+` `-` | Make the detail pane taller or shorter |
| `Tab` `Shift+Tab` (or `]` `[`) | Move focus along the folder list (when shown), the sessions and the detail pane's frames, in the order they are laid out; `↑` `↓`, `j` `k`, PgUp, PgDn, `g` and `G` then scroll it, `Esc` returns to the list |
| `?` | Show every key, grouped by where it works; `?`, `Esc` or `q` closes the list |
| `q` | Quit; over the spread conversation, put the pane back |

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

- While it works, the box shows each search it makes; `Esc` or `q` cancels.
- The answer lists the sessions found with why each matched, the model, the time taken and the cost claude reports.
- `Enter` jumps to one (clearing a folder or filter that hides it), `f` narrows the list to all of them in Claude's order with the reason under each row, and `r` asks again. `Esc` clears the narrowed list.
- The reason stays in Conversation for a session Claude picked.

## Continuing in a new claude

`claude -r` cannot resume a session whose folder is gone, such as one in a removed worktree (`Enter` says so and names this key), or whose transcript Claude Code has deleted. `c` continues any session another way: recall quits and starts a new claude in the folder recall was started in, with recall's MCP server, asking it to recall the session, the way you would ask in a session yourself:

```console
claude "Use the recall tools to recall session <id> ..." \
  --mcp-config <recall mcp> \
  --allowedTools mcp__recall__recall_search,mcp__recall__recall_list,mcp__recall__recall_export
```

- The box takes what to recall about it ("the retry policy"); left empty, Claude says what was being done and how far it got. Either way it then waits for you.
- It is an ordinary session: your settings, CLAUDE.md and MCP servers apply. Only the recall tools are allowed without asking.
- The new session starts from what the old one said, not from its files: a removed worktree's changes are not brought back.

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

`Space` spreads the conversation over the detail pane, wrapped, and the pane grows to leave the list a few rows (`+` `-` or dragging its edge change how many, and they are remembered). `Tab` back to the list and `j` `k` read the next session in place; `Space` or `Esc` puts the pane back, and so does `q` while the conversation has the focus (from the list, `q` quits and `Esc` first clears the filter and Claude's answer, if any).

`/` searches the spread conversation, all of it, the messages the pane skips too. While there is a search, the pane shows the messages that have it, with two on each side and a marker for the ones between; every line with it is highlighted, and the pane scrolls to the first. `Enter` keeps the search, `n` and `N` go to the next and previous match, and `Esc` drops it and shows the conversation as before, from the top (a second `Esc` puts the pane back). The search carries over to the next session read in place. With the list focused, `/` is the list's filter as before.

With `images = true` under `[tui]`, the images you pasted show in the spread conversation where their `[Image #N]` markers were. They are drawn with the Kitty graphics protocol's Unicode placeholders, which Ghostty and Kitty support; in a terminal that does not, they come out as stray characters, so the setting is off by default. An image shows as its marker while it is read, and stays one if it cannot be read.

## Mouse

Click a session to select it, click a frame to focus it, scroll the wheel over the list or over a frame, and drag the pane's top edge (or the row count line just above it) to resize the pane. While the TUI has the mouse, most terminals still select text when you hold Shift (Option in iTerm2) while dragging.

## Changing keys

Under `[keys]` in the [config file](../README.md#configuration), an operation takes a key or a list of keys in place of its own, or `[]` to turn it off. A key you give an operation leaves the operations that have it by default where they would meet: `continue = "enter"` makes `Enter` continue and leaves `resume` with no key, as if `Enter` itself were set, and `resume = "j"` leaves `down` its arrow and `ctrl+n`. The footer and the `?` key list show the keys you set. The file `recall` writes on first run lists every operation with its keys, commented out, so a key is changed by uncommenting its line and editing it; a file written by an older `recall` lacks them, and the README shows the whole file to copy them from.

```toml
[keys]
resume = "space"
read = "enter"

[keys.list]
folders_open = "o"
```

An operation that works in one pane only goes in that pane's table, `[keys.list]` or `[keys.folders]`, after the lines directly under `[keys]` (TOML reads a line after a table header as part of that table). A key written in the wrong place is reported with where it goes.

| Operation | Keys | What it does |
|---|---|---|
| `quit` | `q` | Quit |
| `help` | `?` | Show every key |
| `focus_next` | `tab` `]` | Focus the next pane |
| `focus_prev` | `shift+tab` `[` | Focus the previous pane |
| `ask` | `a` | Ask Claude to find sessions |
| `sort` | `s` | Choose the sort order |
| `scope` | `.` | Switch between the folder recall was started in and all folders |
| `resume` | `enter` | Resume the session |
| `continue` | `c` | Continue the session in a new claude |
| `read` | `space` | Read the conversation over the detail pane, or put it back |
| `copy_id` | `y` | Copy the session ID |
| `copy_command` | `Y` | Copy the resume command |
| `grow` | `+` `=` | Make the detail pane taller |
| `shrink` | `-` | Make the detail pane shorter |
| `up` `down` | `up` `k` `ctrl+p`, `down` `j` `ctrl+n` | Move in a list, scroll a frame |
| `page_up` `page_down` | `pgup` `ctrl+b` `ctrl+u`, `pgdown` `ctrl+f` `ctrl+d` | A page up or down |
| `top` `bottom` | `home` `g`, `end` `G` | The first, the last |
| `search` | `/` | Search what has focus: the list's filter, the folder search, the conversation search |
| `next_match` `prev_match` | `n`, `N` | The next, the previous match of the conversation search |
| `[keys.list]` `folders_open` | `left` `h` | Open the folder list, then move into it |
| `[keys.list]` `folders_close` | `right` `l` | Close the folder list |
| `[keys.folders]` `back` | `right` `l` `enter` | Go from the folder list back to the sessions |

Keys are written as key presses are read:

- A character as itself (`a`, `Y`, `?`), including a shifted one: `Y`, not `shift+y`.
- A named key: `enter`, `space`, `tab`, `backspace`, `up`, `down`, `left`, `right`, `home`, `end`, `pgup`, `pgdown`, `insert`, `delete`, `f1` to `f12`.
- With modifiers in the order `ctrl+`, `alt+`, `shift+`, and a letter lower-cased: `ctrl+d`, `ctrl+shift+y`, `alt+enter`, `shift+tab`.

`ctrl+c` and `esc` cannot be given to an operation, and neither can the keys inside the sort menu and the Ask box, or those of a field being typed in. An unknown operation, a key written another way and two operations set in the file that would share a key in one place (`resume = "j"` with `down = "j"`) are reported when `recall` starts, rather than ignored.

## Settings

The `[tui]` section of the config file holds the TUI's settings: where the detail pane goes and how tall it starts, the color scheme, which sessions to start with, and the model and display of `a`. See [Configuration](../README.md#configuration) in the README for the whole file.
