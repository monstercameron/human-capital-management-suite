# Devlog — 2026-09-13 — LEGAL-ST series: all fifty-one jurisdiction packs

## What landed

Every `LEGAL-ST-*-001` todo in section 20 is closed: the fifty states plus the
District of Columbia each carry a generated RulePack that is authored, reviewed
and published through the real pipeline, with byte-pinned release artifacts.

- One shared harness, `internal/governance/legal/legal_st_test.go`, plus one
  `legal_st_<cc>_test.go` per jurisdiction. Each PRIMARY test audits the
  generated `definitions/legal/packs/states/us-<cc>.json` against the todo's
  GREEN contract (kinds present with cited statute tokens, or asserted absent),
  then drives `pipeline.Author` → sign → `Review` → `Publish` and reads the
  release back out of the registry. GOLDEN pins `release.json`,
  `proposal.json` and `receipt.golden.json` under `testdata/legal/us-<cc>/`;
  CONFORMANCE walks the published release end to end; MUTATION (all states
  except AL and DC, matching the matrix each todo declares) corrupts each
  emitted obligation and proves the release fails closed.
- `planning/research/state-employment-law/district-of-columbia.md` — a new
  corpus file in the twelve-section format, marked DRAFTED, with verified
  D.C. Code citations (wage floor $17.95/hr § 32-1003; WTPAA hire/change
  notice § 32-1008; wage-transparency postings and the wage-history ban §§
  32-1452/32-1453.01; ASSLA § 32-531.02 and UPLA § 32-541.01; non-compete ban
  under $150,000 § 32-581.01 et seq.; next-working-day final pay § 32-1303;
  semimonthly paydays § 32-1302; ban-the-box § 32-1341; breach § 28-3852) and
  explicit "verify" markers wherever a figure was not confirmed.
- The section 5 matrix grew to fifty-one rows (Tables A and B), and the
  coverage arithmetic followed: `extract` state list and matrix completeness
  checks, the carveouts separation-filing and locality registries (DC is a
  state row — not a locality of MD or VA), the pack-tree file count (53), and
  every "fifty" in the surrounding doc comments.
- Extractor hardening in `body.go`, `evidence.go` and `research.go`:
  continuation-line binding and legal-abbreviation handling so cited sections
  like "Wis. Stat. § 109.03" bind their full note instead of truncating.

## Decisions taken

- Every pack publishes at `REQUIRES_CUSTOMER_COUNSEL_CONFIGURATION`, not
  `VENDOR_BASELINE`. Contract section 5 refuses release while any
  flow-consumed cell is `?`, and all fifty-one corpora have unresolved cells —
  so each review carries BLOCKING findings enumerating exactly the gaps and a
  CONCERN per "verify"-marked obligation. Publishing a baseline on partial
  research would be a lie; publishing blocked-with-enumerated-gaps is the
  reviewed artifact the contract asks for.
- Six matrix cells were corrected `F`→`Y` where the research file carried a
  real statute the cell denied: AZ and FL `PAY_EQUITY` (A.R.S. § 23-341; Fla.
  Stat. § 448.07), and MI, NE, NC, SC `NON_COMPETE` (MCL § 445.774a; Neb. Rev.
  Stat. § 48-623 plus common-law unenforceability; N.C. Gen. Stat. § 75-4; SC
  common-law reasonableness). Each flip was verified against the corpus row
  before the cell changed — the extractor's narrow-or-drop rule forbids
  inventing law, so nothing was written into a pack that a research file does
  not state.
- Minnesota `PERSONNEL_FILE` binds on the corpus's "REQUIRED" marker rather
  than a section number, because the corpus ties the duty to Minn. Stat. §
  177.25 while naming the file right in prose — the test asserts the obligation
  exists and is bound to the stated marker, not a wrong section.
- Virginia `LEAVE_INTERACTION` is asserted absent: the matrix cell is `L`
  (locality-scoped), which by design emits no state-level rule.
- The `LEGAL-010` PackRelease digest and the PERFOPT-003 extraction golden were
  repinned to the post-fix extractor output; the Wisconsin diff was reviewed
  line by line and is the intended fuller citations, not drift.

## Verified

- `go test -count=1 ./internal/governance/legal/...` — all ten packages PASS,
  including every `TestTodo_LEGAL_ST_*` matrix entry, the fifty-one pinned
  release/proposal/receipt goldens, the matrix-totals contract, and the
  carveouts registry coverage (51 state rows).
- `.artifacts/regen` (`extract.Generate`/`WriteAll`) reproduces all fifty-one
  pack files byte-for-byte — the checked-in packs are exactly what the
  extractor produces from the corpus.
- `go vet` and `gofmt` clean on every touched package.

## Left partial

- Pack _content_ review remains open by design: the BLOCKING findings on each
  release name the `?` cells customer counsel must resolve before baseline.
- DC's research file is DRAFTED; its "verify" markers (and any corrections
  counsel lands) flow through the same regen pipeline on the next research
  pass.
- The stateparams/indexation/payrules/attribution registries still key their
  fixtures to fifty state rows; widening those is each tool's own todo, not
  this series.
