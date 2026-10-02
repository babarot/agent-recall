#!/usr/bin/env bash
# Compare `search --format json` between the TypeScript and Go implementations.
#
# Both run against one snapshot of the archive, one after the other, so the
# live database is never opened by the Go port.
#
# Usage: scripts/parity/search.sh [path/to/vault.db]
set -euo pipefail

src_db="${1:-$HOME/.claude/vault.db}"
root="$(cd "$(dirname "$0")/../.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

sqlite3 "$src_db" ".backup '$work/vault.db'"
go build -o "$work/recall-go" "$root/cmd/recall"

ts() {
  deno run --allow-read --allow-write --allow-env=HOME --allow-net --allow-run \
    "$root/src/main.ts" search "$@" --format json --db "$work/vault.db"
}
go_() {
  "$work/recall-go" search "$@" --format json --db "$work/vault.db"
}

# Each line is one set of search arguments, split on whitespace.
queries=(
  "terraform"
  "migration --limit 50"
  "nix --limit 100"
  "worktree --project dotfiles"
  "deploy --from 2026-06-01 --to 2026-07-01"
  "43968160-3681"
  "terraform AND module"
  "\"state migration\""
  "ANDROID"
  "running"
  "セッション"
  "herdr のパッチ"
  "nonexistentwordxyz"
  "error --limit 200"
  "the --limit 2000"
  "claude --limit 5000"
)

fail=0
for q in "${queries[@]}"; do
  # eval keeps quoted phrases like "state migration" as one argument.
  eval "args=($q)"
  ts "${args[@]}" >"$work/ts.json" 2>&1 || true
  go_ "${args[@]}" >"$work/go.json" 2>&1 || true
  if cmp -s "$work/ts.json" "$work/go.json"; then
    printf 'ok    %s (%s bytes)\n' "$q" "$(wc -c <"$work/ts.json" | tr -d ' ')"
  else
    printf 'DIFF  %s\n' "$q"
    diff "$work/ts.json" "$work/go.json" | head -20 || true
    fail=1
  fi
done
exit "$fail"
