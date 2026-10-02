#!/usr/bin/env bash
# Compare `import` between the TypeScript and Go implementations.
#
# Both import the same snapshot of ~/.claude/projects into their own empty
# database, then every row is compared (autoincrement ids and imported_at
# excluded). A second run of each must report every session unchanged.
#
# Usage: scripts/parity/import.sh [path/to/projects]
set -euo pipefail

src="${1:-$HOME/.claude/projects}"
root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

mkdir -p "$work/home/.claude"
# APFS clone when available, so the snapshot is instant and takes no space.
cp -cR "$src" "$work/home/.claude/projects" 2>/dev/null || cp -R "$src" "$work/home/.claude/projects"
go build -o "$work/recall-go" "$root/cmd/recall"

# Keep Deno's module cache when HOME points at the snapshot.
deno_dir="$(deno info --json | sed -n 's/.*"denoDir": *"\([^"]*\)".*/\1/p')"
ts() {
  HOME="$work/home" DENO_DIR="$deno_dir" deno run --allow-read --allow-write --allow-env=HOME --allow-net --allow-run \
    "$root/src/main.ts" import --db "$work/ts.db"
}
go_() {
  HOME="$work/home" "$work/recall-go" import --db "$work/go.db"
}

time_it() { local s=$SECONDS; "$@"; echo "  ($((SECONDS - s))s)"; }

echo "TypeScript:"; time_it ts | sed 's/^/  /'
echo "Go:";         time_it go_ | sed 's/^/  /'

fail=0
compare() {
  local name=$1 cols=$2
  local only_ts only_go
  only_ts=$(sqlite3 "$work/ts.db" "ATTACH '$work/go.db' AS g; SELECT COUNT(*) FROM (SELECT $cols FROM main.$name EXCEPT SELECT $cols FROM g.$name);")
  only_go=$(sqlite3 "$work/ts.db" "ATTACH '$work/go.db' AS g; SELECT COUNT(*) FROM (SELECT $cols FROM g.$name EXCEPT SELECT $cols FROM main.$name);")
  local n
  n=$(sqlite3 "$work/ts.db" "SELECT COUNT(*) FROM $name")
  if [[ $only_ts == 0 && $only_go == 0 ]]; then
    echo "ok    $name ($n rows)"
  else
    echo "DIFF  $name: $only_ts rows only in TypeScript, $only_go only in Go"
    sqlite3 "$work/ts.db" "ATTACH '$work/go.db' AS g; SELECT 'ts', * FROM (SELECT $cols FROM main.$name EXCEPT SELECT $cols FROM g.$name) LIMIT 3; SELECT 'go', * FROM (SELECT $cols FROM g.$name EXCEPT SELECT $cols FROM main.$name) LIMIT 3;" | cut -c1-300
    fail=1
  fi
}

compare sessions "session_id, project, project_path, git_branch, first_prompt, summary, message_count, started_at, ended_at, claude_version, file_mtime, file_size"
compare messages "session_id, uuid, role, block_type, block_index, content, tool_name, tool_input, timestamp, turn_index"
compare images "session_id, message_uuid, image_index, media_type, hex(data)"

fts_ts=$(sqlite3 "$work/ts.db" "SELECT COUNT(*) FROM messages_fts")
fts_go=$(sqlite3 "$work/go.db" "SELECT COUNT(*) FROM messages_fts")
if [[ $fts_ts == "$fts_go" ]]; then echo "ok    messages_fts ($fts_ts rows)"; else echo "DIFF  messages_fts: $fts_ts vs $fts_go"; fail=1; fi

second_ts=$(ts); second_go=$(go_)
if [[ $second_ts == "$second_go" ]]; then
  echo "ok    second run: $(tail -1 <<<"$second_go")"
else
  printf 'DIFF  second run\n  ts: %s\n  go: %s\n' "$second_ts" "$second_go"
  fail=1
fi

# Change three transcripts the ways Claude Code does: a /compact rewrite
# that drops lines, an appended turn, and a touch without new content.
python3 - "$work/home/.claude/projects" <<'PY'
import os, sys, glob, json
files = sorted(glob.glob(os.path.join(sys.argv[1], "*", "*.jsonl")), key=os.path.getsize, reverse=True)[:3]
shrink, grow, touch = files
lines = open(shrink, encoding="utf-8").read().split("\n")
open(shrink, "w", encoding="utf-8").write("\n".join(lines[: len(lines) // 2]) + "\n")
sid = os.path.basename(grow)[:-6]
with open(grow, "a", encoding="utf-8") as f:
    f.write(json.dumps({"type": "user", "uuid": "parity-appended", "sessionId": sid,
                        "timestamp": "2099-01-01T00:00:00Z",
                        "message": {"role": "user", "content": "appended by the parity check"}}) + "\n")
st = os.stat(touch)
os.utime(touch, (st.st_atime, st.st_mtime + 5))
PY
third_ts=$(ts); third_go=$(go_)
if [[ $third_ts == "$third_go" ]]; then
  echo "ok    resync run: $(tail -1 <<<"$third_go")"
else
  printf 'DIFF  resync run\n  ts: %s\n  go: %s\n' "$third_ts" "$third_go"
  fail=1
fi
compare sessions "session_id, project, project_path, git_branch, first_prompt, summary, message_count, started_at, ended_at, claude_version, file_mtime, file_size"
compare messages "session_id, uuid, role, block_type, block_index, content, tool_name, tool_input, timestamp, turn_index"
compare images "session_id, message_uuid, image_index, media_type, hex(data)"
exit "$fail"
