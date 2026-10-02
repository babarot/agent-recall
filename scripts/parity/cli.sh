#!/usr/bin/env bash
# Compare the output of the read-only CLI commands (search, list, export,
# stats) between the TypeScript and Go implementations, colors included.
#
# Usage: scripts/parity/cli.sh [path/to/vault.db]
set -euo pipefail

src_db="${1:-$HOME/.claude/vault.db}"
root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

sqlite3 "$src_db" ".backup '$work/vault.db'"
go build -o "$work/recall-go" "$root/cmd/recall"

ts() { deno run --allow-read --allow-write --allow-env=HOME --allow-net --allow-run "$root/src/main.ts" "$@" --db "$work/vault.db"; }
go_() { "$work/recall-go" "$@" --db "$work/vault.db"; }

# Pick real sessions to export: the newest, the largest, and one prefix.
newest=$(sqlite3 "$work/vault.db" "SELECT session_id FROM sessions ORDER BY ended_at DESC LIMIT 1")
largest=$(sqlite3 "$work/vault.db" "SELECT session_id FROM sessions ORDER BY message_count DESC LIMIT 1")
# A value starting with "-" would be read as a flag by parseArgs, so take the
# last path segment.
project=$(sqlite3 "$work/vault.db" "SELECT project FROM sessions GROUP BY project ORDER BY COUNT(*) DESC LIMIT 1 OFFSET 2" | awk -F- '{print $NF}')

cases=(
  "list"
  "list --limit 500"
  "list --format json --limit 300"
  "list --project dotfiles"
  "stats"
  "stats --project ${project}"
  "search terraform"
  "search error --limit 100"
  "search セッション --limit 50"
  "export ${newest}"
  "export ${largest} --format text"
  "export ${largest} --format json"
  "export ${newest:0:8} --format json"
  "export nonexistent-session"
  "search"
  "export"
)

fail=0
for c in "${cases[@]}"; do
  eval "args=($c)"
  ts "${args[@]}" >"$work/ts.out" 2>"$work/ts.err" && tc=0 || tc=$?
  go_ "${args[@]}" >"$work/go.out" 2>"$work/go.err" && gc=0 || gc=$?
  # Usage lines name the binary, which differs on purpose.
  sed -i '' 's/agent-recall /recall /' "$work/ts.err"
  if cmp -s "$work/ts.out" "$work/go.out" && cmp -s "$work/ts.err" "$work/go.err" && [[ $tc == "$gc" ]]; then
    printf 'ok    %s (%s bytes, exit %s)\n' "$c" "$(wc -c <"$work/ts.out" | tr -d ' ')" "$tc"
  else
    printf 'DIFF  %s (exit ts=%s go=%s)\n' "$c" "$tc" "$gc"
    diff "$work/ts.out" "$work/go.out" | head -10 || true
    diff "$work/ts.err" "$work/go.err" | head -5 || true
    fail=1
  fi
done
exit "$fail"
