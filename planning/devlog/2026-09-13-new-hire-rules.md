# 2026-09-13 — New-hire requirements dataset

## What

A jurisdiction-keyed ruleset for `lifecycle.recruit_hire_onboard/v1`:
`planning/research/state-employment-law/new-hire-requirements.yaml`
(51 jurisdictions) plus `new-hire-requirements.md` documenting schema,
sources, and unresolved cells.

## Decisions

- Scope = the legal hooks the workflow spec names: posting legality
  (ban-the-box stage, pay-transparency posting, salary-history bans),
  pre-employment checks (E-Verify), tax/payroll forms (state withholding
  certificates vs federal W-4 vs none), required notices at hire, and
  post-hire new-hire reporting deadlines.
- E-Verify, notice, salary-history and pay-transparency fields were extracted
  from the checked-in generated RulePacks so the dataset cannot drift from
  the packs the governance plane already publishes. New-hire reporting and
  withholding-form values were researched against PRWORA plus state matrices;
  cross-source conflicts (AR deadline, CT/ME/MI/NV/NY ban-the-box coverage)
  are kept as `verify` rather than resolved by fiat.

## Verified

- YAML parses; all 51 jurisdictions present (50 states + DC).
- Extractor safety: `extract.Generate` iterates the fixed `States` list, so
  research-dir additions cannot disturb pack regeneration.

## Left partial

- `wage_notice_at_hire` content fields are pack-coarse; model from corpus
  section 2 before generating notice logic.
- Minor work-permit rules and sick-leave/PFML onboarding notices are not in
  the dataset (corpus sections exist; not yet merged in).
- No testable todo covers this; dataset consumption into the workflow engine
  is a follow-up the user named as the goal.
