# 2026-09-22 documentation hub run

Completed the 15 remaining actionable hub todos (HUB-017, HUB-022–029, HUB-033, HUB-035, HUB-037–039, HUB-042); all 30 hub todos with satisfied dependencies now ticked in `planning/todos.md`. Everything stays uncommitted per the session constraint.

## What was built

All in `internal/data/documenthubstore` unless noted, each with its matrix tests green:

- HUB-023 stable block anchors (`blocks.go`, migration 00011): heading-derived slugs survive redeploys; only heading edits move them. PRIMARY + PROPERTY + GOLDEN.
- HUB-024 safe attachments (`attachments.go`, migration 00012): admitted-quarantine-only attach, version-hash/classification binding, per-read authorization, descriptors carry no URL.
- HUB-025 lexical search (`search.go`, migration 00013): title/heading/body terms over deployed versions with grant prefilter + per-hit recheck.
- HUB-026 section embeddings (`embeddings.go`, migration 00014): approved-model + egress policy gate before any provider call; versioned, hashed vectors.
- HUB-027 hybrid fusion (`hybrid.go`): lexical priority, bounded semantic leg, explanations, keyword fallback. PRIMARY + GOLDEN + BENCHMARK (~50us/op).
- HUB-028 constrained retrieval (`retrieval.go`): exact authorized baseline + `RequireIndexConformance` gate for approximate indexes.
- HUB-033 editor + compare UI (`tools/uxqual/render/docs`, fixtures, `docs-editor.spec.mjs` 2/2 Chromium): candidate form with expected base, conflict alert, immutable compare at desktop + 390px, en-US/de-DE/RTL-ar.
- HUB-037 records (`records.go`, migration 00015): holds freeze disposal; disposal removes derivatives, retains the immutable core, reports verified inventory.
- HUB-017 history (`history.go`): history capability gate; versions above current policy redact to id + classification.
- HUB-022 backlinks (`backlinks.go`, migration 00016): jointly-readable reverse index; checker marks broken/stale + owner outbox alerts, once per transition.
- HUB-038 export (`export.go`): read+export bundle with versions, deployments, artifacts, link map, holds, records policy.
- HUB-039 import (`import.go`): admitted-only payloads, validation, link/artifact remap, candidate + provenance on the normal review path. FUZZ 280k execs clean.
- HUB-029 reconcile (`reconcile.go`, migration 00017): idempotent outbox consumer with per-document watermarks; shared tx helpers extracted from search/blocks/links/embeddings.
- HUB-035 picker + backlinks UI (same docs renderer package, `docs-picker.spec.mjs` 2/2 Chromium): stable-ID picker, title-free restricted rows.
- HUB-042 backup/restore (`backup.go`): custodian-scoped snapshot, byte-faithful restore with hash verification into a fresh database, reconcile to a consistent watermark.

## Defects found and fixed

- `headingLevel` returns 3 values (blocks RED caught at vet).
- Search recheck ran under an open cursor (conn busy); rows are now drained before re-authorizing.
- Withdraw needs `Reason` + `ExpectedLive`; deploy-as-u-deployer needs target read (HUB-020 working as designed).
- HUB-027 primary had a tie-score flake (two titles, UUID order); assertion now checks rank classes deterministically.
- `document_link` state CHECK rejected the new broken/stale states (migration 00016).
- Import fuzz found an over-broad assertion (bare prose is not a link); invariant narrowed to extracted targets, failing input kept as regression seed.
- Restore into a second tenant collides on globally-unique ids; restore targets a fresh database instead.
- Disposition `Disposition` name collided with HUB-003's registry type; records types renamed `Records*`.

## Verification

- `go test -count=1 ./internal/data/documenthubstore/` PASS (84.1% statements, floor is 70%), `go vet` clean, `gofmt -l` clean on touched dirs.
- `go test -count=1 ./tools/uxqual/render/docs/` PASS; Playwright `docs-editor` + `docs-picker` specs 2/2 each in real Chromium.
- Fuzz `FuzzTodo_HUB_039` 60s / 280k execs PASS; benchmarks for HUB-027/028 run.
- Prettier/eslint clean on new specs. Staged coverage gate could not pass: unrelated `tools/gen/*` failures belong to other sessions; hub package coverage measured directly instead.
- Left partial: `git checkout` revert of regen-churned `ssr.html`/`gwc.html` fixtures is blocked on another session's index lock; the two files' one-line token-CSS diff is not mine. Commit hook + CI remain for a session that can commit (this session must leave work uncommitted).

## Blocked hub todos (15, all verified open in planning/todos.md)

Every blocker below is itself unticked. Hub-side prerequisites named here are done unless noted.

- HUB-001 isolated document DB: needs CHAT-001 (unsigned chat scope exchange). HUB-042's independent backup/restore covers the backup half; provisioning + signature stay open.
- HUB-002 core document ID routes: needs HUB-001 + CHAT-002 (core conversation routing).
- HUB-013 team/channel eligibility: needs CHAT-010 (HUB-011 done).
- HUB-014 official Docs tabs: needs HUB-013 (HUB-008 done).
- HUB-015 cross-company grants: needs HUB-013 + CHAT-012.
- HUB-030 search filters/cards: needs HUB-013 (HUB-027 done).
- HUB-031 agent retrieval: needs HUB-030 + CHAT-043 (agent identities).
- HUB-032 docs navigation UI: needs HUB-014 (HUB-012 done).
- HUB-034 reviewer/publisher controls: needs HUB-032 (HUB-008/009 done).
- HUB-036 search UI: needs HUB-030 + HUB-032.
- HUB-040 ownership transfer: needs HUB-014 (HUB-011 done).
- HUB-041 RPC/HTTP parity: needs INTAPI-007 (HUB-009/011 done).
- HUB-043 1000-person load test: needs CHAT-052 (HUB-027 + HUB-042 done).
- HUB-044 pilot: needs HUB-032 + HUB-034 + HUB-043.
- HUB-045 adversarial conformance: needs HUB-015 + HUB-041 (HUB-021/028 done).
