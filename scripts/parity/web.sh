#!/usr/bin/env bash
# Compare the web server between the TypeScript and Go implementations.
#
# Each server gets its own copy of the archive and its own snapshot of the
# transcripts (both write while they run), then every API endpoint and a few
# static paths are fetched from both and compared: status, content type and
# body. Finally one transcript is appended to in both snapshots and the
# session_updated events pushed over /api/stream are compared.
#
# Needs ui/dist (cd ui && npm run build).
#
# Usage: scripts/parity/web.sh [path/to/vault.db]
set -euo pipefail

src_db="${1:-$HOME/.claude/vault.db}"
root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
pids=()
cleanup() { for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done; rm -rf "$work"; }
trap cleanup EXIT

deno_dir="$(deno info --json | sed -n 's/.*"denoDir": *"\([^"]*\)".*/\1/p')"
# One snapshot, cloned for each side, so both see the same transcripts even
# while sessions are being written.
clone() { cp -cR "$1" "$2" 2>/dev/null || cp -R "$1" "$2"; }
clone "$HOME/.claude/projects" "$work/projects"
sqlite3 "$src_db" ".backup '$work/vault.db'"
for side in ts go; do
  mkdir -p "$work/$side/home/.claude"
  clone "$work/projects" "$work/$side/home/.claude/projects"
  cp "$work/vault.db" "$work/$side/vault.db"
done

(cd "$root" && deno task ui:embed >/dev/null)
rm -rf "$root/internal/webui/dist" && cp -R "$root/ui/dist" "$root/internal/webui/dist"
go build -tags embedui -o "$work/recall-go" "$root/cmd/recall"

ts_port=16276 go_port=16277
HOME="$work/ts/home" DENO_DIR="$deno_dir" deno run --allow-read --allow-write --allow-env=HOME --allow-net --allow-run \
  "$root/src/main.ts" ui --foreground --port "$ts_port" --db "$work/ts/vault.db" >"$work/ts.log" 2>&1 &
pids+=($!)
HOME="$work/go/home" "$work/recall-go" ui --foreground --port "$go_port" --db "$work/go/vault.db" >"$work/go.log" 2>&1 &
pids+=($!)
for port in $ts_port $go_port; do
  for _ in $(seq 1 240); do curl -sf "http://localhost:$port/api/status" >/dev/null && break; sleep 0.5; done
done

db="$work/go/vault.db"
id=$(sqlite3 "$db" "SELECT session_id FROM sessions ORDER BY ended_at DESC LIMIT 1 OFFSET 3")
img=$(sqlite3 "$db" "SELECT session_id || '&message=' || message_uuid || '&index=' || image_index FROM images LIMIT 1")
png=$(find "$root/ui" -name '*.png' -not -path '*/node_modules/*' | head -1)
asset=$(cd "$root/ui/dist" && find . -type f -name '*.js' | head -1 | sed 's#^\.##')

paths=(
  "/api/sessions"
  "/api/sessions?limit=200&offset=10"
  "/api/sessions?project=dotfiles"
  "/api/sessions/$id"
  "/api/sessions/${id:0:8}"
  "/api/sessions/nonexistent"
  "/api/search?q=terraform"
  "/api/search?q=error&limit=100"
  "/api/search?q=%E3%82%BB%E3%83%83%E3%82%B7%E3%83%A7%E3%83%B3&project=agent"
  "/api/search?q="
  "/api/stats"
  "/api/stats?project=dotfiles"
  "/api/image?session=$img"
  "/api/image?session=nope&message=nope"
  "/api/file?path=relative.png"
  "/api/file?path=/nonexistent.png"
  "/api/nope"
  "/"
  "/index.html"
  "/some/spa/route"
  "$asset"
)
[[ -n $png ]] && paths+=("/api/file?path=$png")

fail=0
fetch() { curl -s -o "$3" -w '%{http_code} %{content_type}' "http://localhost:$1$2"; }
for p in "${paths[@]}"; do
  a=$(fetch $ts_port "$p" "$work/ts.body"); b=$(fetch $go_port "$p" "$work/go.body")
  if [[ $a == "$b" ]] && cmp -s "$work/ts.body" "$work/go.body"; then
    printf 'ok    %s (%s, %s bytes)\n' "$p" "$a" "$(wc -c <"$work/ts.body" | tr -d ' ')"
  else
    printf 'DIFF  %s\n  ts: %s\n  go: %s\n' "$p" "$a" "$b"
    cmp "$work/ts.body" "$work/go.body" | head -2 || true
    fail=1
  fi
done

# /api/status differs in pid, port and timestamps; compare the rest.
norm='del(.pid, .port) | .watcher |= (del(.projectsDir, .lastEventAt, .lastImportAt))'
a=$(curl -s "http://localhost:$ts_port/api/status" | jq -cS "$norm"); b=$(curl -s "http://localhost:$go_port/api/status" | jq -cS "$norm")
if [[ $a == "$b" ]]; then echo "ok    /api/status ($a)"; else printf 'DIFF  /api/status\n  ts: %s\n  go: %s\n' "$a" "$b"; fail=1; fi

# Live update: append one turn to the same transcript in both snapshots.
for port in $ts_port $go_port; do
  curl -sN "http://localhost:$port/api/stream" >"$work/stream.$port" &
  pids+=($!)
done
sleep 1
file=$(sqlite3 "$db" "SELECT project || '/' || session_id || '.jsonl' FROM sessions ORDER BY ended_at DESC LIMIT 1 OFFSET 5")
sid=$(basename -- "$file" .jsonl)
for side in ts go; do
  printf '{"type":"user","uuid":"web-parity","sessionId":"%s","timestamp":"2099-01-01T00:00:00Z","message":{"role":"user","content":"appended by the web parity check"}}\n' "$sid" \
    >>"$work/$side/home/.claude/projects/$file"
done
sleep 3
for port in $ts_port $go_port; do grep -o '"type":"session_updated".*' "$work/stream.$port" | sed 's/}$//' >"$work/events.$port" || true; done
first_ts=$(head -c 60 "$work/stream.$ts_port" | sed 's/[0-9]\{13\}/<ms>/'); first_go=$(head -c 60 "$work/stream.$go_port" | sed 's/[0-9]\{13\}/<ms>/')
if [[ $first_ts == "$first_go" ]]; then echo "ok    /api/stream hello frame"; else printf 'DIFF  hello\n  ts: %q\n  go: %q\n' "$first_ts" "$first_go"; fail=1; fi
if [[ -s $work/events.$ts_port ]] && cmp -s "$work/events.$ts_port" "$work/events.$go_port"; then
  echo "ok    session_updated event: $(cat "$work/events.$go_port")"
else
  printf 'DIFF  session_updated\n  ts: %s\n  go: %s\n' "$(cat "$work/events.$ts_port")" "$(cat "$work/events.$go_port")"
  fail=1
fi
exit "$fail"
