#!/usr/bin/env bash
# Launch one Codex implementation lane from a brief, on gpt-6.1-sol.
#
#   scripts/run-lane.sh <name> [brief-dir]
#
# Lanes run on the current model only. HCMNEXT_LANE_MODEL overrides it for a
# newer model; never point a lane at an older one.
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
model="${HCMNEXT_LANE_MODEL:-gpt-6.1-sol}"
# The CLI on PATH can lag the Codex app's bundled build, and an old build
# refuses newer models ("not supported when using Codex with a ChatGPT
# account"). Use the newest bundled binary when there is one.
codex_bin="$(ls -t "${LOCALAPPDATA:-$HOME/AppData/Local}"/OpenAI/Codex/bin/*/codex.exe 2>/dev/null | head -1)"
[ -x "$codex_bin" ] || codex_bin="codex"
"$codex_bin" exec --sandbox workspace-write -m "$model" -C "$root" -o "$dir/codex_$name.out" - < "$brief" > "$dir/codex_$name.log" 2>&1
code=$?
echo "lane $name model=$model codex exit=$code"
# Keep only the short report after a clean run; the full log is kept for failures.
[ "$code" -eq 0 ] && [ -s "$dir/codex_$name.out" ] && rm -f "$dir/codex_$name.log"
tail -c 3500 "$dir/codex_$name.out" 2>/dev/null
exit "$code"
