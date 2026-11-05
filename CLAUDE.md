# Claude Code Repository Entry Point

Before beginning work in this repository:

1. read and follow [`AGENTS.md`](AGENTS.md);
2. read the todo entries you are closing in [`planning/archive/todos-2026-10-22.md`](planning/archive/todos-2026-10-22.md) and the packages their Refs name;
3. inspect the current source, tests and git state before assuming planned components exist.

`AGENTS.md` is the authoritative repository-wide agent instruction file. This file is intentionally a thin Claude Code compatibility entry point so the same rules are not duplicated and allowed to drift.

Everything a Claude Code session needs beyond that lives in `AGENTS.md`: the quality gates, the tracked configuration under `.claude/`, the gate-runner subagent, and the vendored Karpathy guidelines skill.
