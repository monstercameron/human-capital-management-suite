# Grouped commit round, 2026-09-17

Committed the working tree in eight area groups (data, domain engines, domains,
trust, operations/messaging/transport/workflow, policy/tools, UI, docs) and marked
81 working-tree todos plus the 4 HEAD-ticked todos (`CONF-004`, `ELIG-008`,
`LEAVE-013`, `LEAVE-015`) complete in the registry. Registry open count moves
270 to 185. Nothing was pushed; the branch stays local.

## What landed

- Eight commits, each through the full pre-commit hook with no `--no-verify`:
  data (`JOB-002`, `EVENT-005`), domain engines and intent (`CYCLE-007/008/010`,
  `ELIG-006`, `SCHED-004`, `ALIGN-030`), domains (19 todos incl. `BAL-006/007/008`,
  `PAYRUN-003/007`, `TENANT-005`, `RECRUIT-001`, `PRIV-006`), trust (`PRIV-008/009`,
  `PROCESS-001`), operations/messaging/transport/workflow (`FORENSIC-001`,
  `MAIL-001`, `ALIGN-056/060`, `FULFILL-001`, `ALIGN-018`, `NEXT-007`),
  policy/tools (`ALIGN-004`, `SUPPLY-004`, coverage-gate exceptions, archdoc
  regen), UI (`UIPOLISH-009/010`, `UXAUDIT-002/009/010/013` plus rebuilt WASM
  bundle), docs (`planning/todos.md`, regenerated registry, CHANGELOG, this file).
- Verification before staging: `go build ./...` clean, then targeted
  `go test -count=1` per affected package — 27 domain packages, 5 engines,
  governance/privacy, intent/app (106s), messaging, data/jobs (124s) and
  data/outbox (203s) on embedded PostgreSQL, crmstore, leavereturn, leave,
  payinput, operations, transport/productquery, workflow conformance, all tools
  packages, productui (153s), test/workspace (67s) and test/workflow (168s).
  Every staged package passed; the exact commands are appended to the 80
  evidence lines that lacked a backticked `go test` result.

## Defects found and decisions taken

- `LEGAL-016` was ticked with evidence claiming a passing suite, but
  `TestTodo_LEGAL_016_Golden` (vector byte-size pin) and
  `TestTodo_LEGAL_016_Conformance` (MI NON_COMPETE matrix-vs-pack conflict) fail
  against the updated packs. Unticked it and removed the false evidence line;
  it stays open with the red legal area.
- The `internal/governance/legal` package is red on 20 not-yet-migrated states
  (new tests vs un-updated packs: IA/ID/IL/IN/KS/KY/NJ/NM/NY/OH/TX/UT/VT/WA/WV/WY
  plus MA/MD/ME/MI/MN and the LA golden) and `extract` regresses done-todo
  goldens (`LEGAL_010_Golden`, `LEGAL_CFG_Conformance`, `PERFOPT_003_Golden`).
  The 26 ticked states plus `LEGAL-005/007/014` keep their ticks (their own
  primaries pass) but no legal Go files or packs were staged; they land with the
  golden re-pins in a later legal commit.
- `tools/planning/intentmanifests` fails two new tests (hidden-children mutant
  accepted) for unticked `FEATURE-CONF-001`; not staged, not ticked.
- Evidence hygiene fixed for all 85: appended backticked `go test` commands
  (GOV-017 `EVIDENCE_MISSING_GO_TEST_RESULT`) and corrected 18 wrong TEST names
  (`TestTodo_LEGAL-ST-*` dash/underscore conflation, `TestTodo_CASE_002`,
  `TestTodo_CBA_003`, `TestTodo_ER_002` vs their long primary names).
  `todogovernance` now reports zero findings on these 85 (75 pre-existing
  findings elsewhere remain, as at HEAD).
- The registry is generator-owned: hand-editing it tripped
  `TestTodoRegistryMatchesMarkdown`, so the final registry state comes from
  `go run ./tools/planning/cmd/todoregistry`. Also regenerated the archdoc
  inventory (driftgate was red on the new `recruiting` package).
- Mechanical normalization the hook forced on deferred/in-flight files
  (whitespace only, unstaged): `gofmt -w` on 13 new lane Go files and
  `prettier --write` on 10 lane-edited state packs. The pack reflow moved four
  ticked-state golden digests (`NC/ND/NH/NV`), which were re-pinned after the
  states' primaries and conformance tests confirmed unchanged semantics; those
  four states are fully green again.
- In-flight files for open todos were deliberately left unstaged (verified
  green where present, but unticked): abuse investigation (`ABUSE-006`),
  pseudonym rate-limit/reidentification (`ANON-006/008`), search propagation,
  benefits eligibility, clock observation, garnishment, intelligence metric,
  jobarch pathstore, knowledge rag/resolve, settlement submit, intentmanifests
  channel parity, productclient access preview (`UXSCAN-008`), the four
  `uipolish00X` test files plus `uipolish002_settings.go`, and
  `test/workspace/todo_uxscan_008_test.go`.

## Left partial

- The red legal area (packs for LA/MA/MD/ME/MI/MN and the remaining states,
  goldens, matrices, `LEGAL-016`) needs its owning lane to finish migration;
  landing it must re-pin `LEGAL_010`, `LEGAL_CFG` and `PERFOPT_003` goldens in
  the same commit since the pack changes regress those done todos' tests.
- `.claude/settings.json` is modified in the tree (it drops the `git push`
  denial) and was not staged, reset or otherwise touched here; it looks like
  another session's edit and it weakens a guardrail, so it needs owner review.
- `todogovernance` still fails on pre-existing findings unrelated to this
  round; `uxscan008`/`UXSCAN-008` and the UIPOLISH-002/003/005/006 P0 items
  remain open with green-but-unticked tests in the tree.
