#!/usr/bin/env bash
# Launch one Codex GPT-5.6 Luna implementation lane from a brief.
#
#   scripts/run-lane.sh <name> [brief-dir]
#
# Reads <brief-dir>/codex_<name>.md (default: .artifacts/lanes), runs codex
# exec against the repository root with workspace-write sandboxing, writes the
# lane's final report to <brief-dir>/codex_<name>.out and its full log to
# <brief-dir>/codex_<name>.log. The report file only appears when the lane
# exits, so a monitor can wait on its existence. Every brief starts with the
# standing rules in .claude/lanes/luna-lane-preamble.md (see AGENTS.md,
# "Delivery loop and model routing").
set -u
name="${1:?usage: scripts/run-lane.sh <name> [brief-dir]}"
dir="${2:-.artifacts/lanes}"
root="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$dir"
brief="$dir/codex_$name.md"
if [ ! -f "$brief" ]; then
  echo "run-lane: brief not found: $brief" >&2
  exit 2
fi
cd "$root" || exit 1
# Lanes leak temp directories and caches wherever their environment points;
# point everything at the artifact root so the checkout stays clean.
art="$(cd "$root" && (pwd -W 2>/dev/null || pwd))/.artifacts"
mkdir -p "$art/tmp" "$art/gocache" "$art/pg"
export GOTMPDIR="$art/tmp" TMP="$art/tmp" TEMP="$art/tmp" GOCACHE="$art/gocache" HCMNEXT_TEST_PG_CACHE="$art/pg"
codex exec --sandbox workspace-write -m gpt-5.6-luna -C "$root" -o "$dir/codex_$name.out" - < "$brief" > "$dir/codex_$name.log" 2>&1
code=$?
echo "lane $name codex exit=$code"
# Keep only the short report after a clean run; the full log is kept for failures.
[ "$code" -eq 0 ] && [ -s "$dir/codex_$name.out" ] && rm -f "$dir/codex_$name.log"
tail -c 3500 "$dir/codex_$name.out" 2>/dev/null
exit "$code"
