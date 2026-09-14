# New-hire requirements by jurisdiction (research index)

Status: DRAFTED (2026-09-13)
Purpose: rule inputs for `lifecycle.recruit_hire_onboard/v1`
(`planning/workflows/lifecycle/recruit-hire-onboard.md`). The workflow engine
consumes `new-hire-requirements.yaml` in this directory; this file documents the
schema, the sources behind each field, and everything left unresolved.

## Workflow hooks this feeds

| Workflow step                                   | Rule family that drives it                                                          |
| ----------------------------------------------- | ----------------------------------------------------------------------------------- |
| 3 PublishJobPosting (jurisdiction-safe posting) | `ban_the_box`, `screening.pay_transparency_posting`, `screening.salary_history_ban` |
| 11 RunBackgroundCheck / EvaluateBackgroundCheck | `ban_the_box` (stage at which criminal inquiry is permitted), `e_verify`            |
| 14 CollectWageTaxForms                          | `withholding_form`, federal W-4/I-9 baselines                                       |
| 18 DeliverRequiredNotices                       | `wage_notice_at_hire` (statute, timing, content fields)                             |
| Post-hire (off-workflow obligation)             | `new_hire_reporting` (deadline_days, contractor coverage)                           |

## Schema

Each `jurisdictions.<CC>` entry carries six blocks:

- `new_hire_reporting` — `deadline_days` (from hire date), `contractor_reporting`
  (`REQUIRED` | `NONE` | `PARTIAL`), `contractor_threshold` where applicable,
  `note`. Federal baseline under PRWORA (42 U.S.C. § 653a): 20 days; seven
  states shorten it (see below). Report = employee name, address, SSN, DOB if
  known, date of hire; employer name, address, EIN. Typical non-compliance
  penalty ~$25/report (~$500 where conspiracy is found) — verify per state.
- `withholding_form` — `form`: the state wage-withholding certificate name,
  `FEDERAL_W4` where the state accepts the federal form, or `NONE` where no
  wage income tax exists (AK FL NV NH SD TN TX WA WY) or no form is required
  (PA flat-rate; PA instead requires the Local Earned Income Tax Residency
  Certification Form). Federal Form W-4 is required in every jurisdiction.
- `e_verify` — `mandate` scope, `statute`, `new_hires_only` flag, `verify`
  where scope is unconfirmed.
- `wage_notice_at_hire` — `required`, `statute`, `timing_days`,
  `content_fields`, `verify`, `note`. Sourced from the generated RulePacks
  (`NOTICE` obligations); the pack content model is coarse — see "Known gaps".
- `ban_the_box` — `scope` (`PRIVATE` | `PUBLIC_ONLY` | `LOCAL_ONLY` | `NONE` |
  `PARTIAL` | `VERIFY`), `min_employees`, `stage` (when criminal-history
  inquiry first becomes permissible), `statute`, `verify`.
- `screening` — `salary_history_ban`, `pay_transparency_posting` (posting-stage
  pay-range disclosure jurisdiction exists — per-rule scope in the packs).

## Key findings

- **New-hire reporting deadlines shorter than the federal 20 days:** ME 7,
  GA 10, VT 10, MA 14, RI 14, IA 15, MS 15. All other jurisdictions use 20.
- **Contractor reporting required:** CA ($600+/yr), CT (>$5,000/yr, verify),
  IA ($600+), KY (partial: job refusals + certain contractors, verify),
  ME ($2,500+), MA ($600+), MN, MS (recurring, verify), NE (verify),
  NH ($2,500+), NJ (all contractors).
- **E-Verify mandated:** AL, AZ, MS, SC all employers; GA public + private 11+;
  NC private 25+; TN private 35+; FL private 25+ / all public; UT private 150+
  (verify); PA public-works contractors only (verify).
- **Private-sector ban-the-box confirmed in corpus:** CA (5+, post-offer),
  CO, CT (verify), DC (11+), IL, MD (15+), MA, MN, NJ (15+), NM, OR, RI (4+),
  VT, WA (post-offer). Corpus conflicts flagged `VERIFY`: HI, ME, MI, NV, NY.
- **Wage notice at hire required in** ~23 jurisdictions (per generated packs);
  timing and content fields are coarse in the pack model.

## Known gaps / do not rely on

- `wage_notice_at_hire` is derived from pack `NOTICE` obligations whose
  `content_fields` are extraction-coarse (e.g. CA § 2810.5 lists only
  "payday"). The underlying corpus section 2 carries the real enumerations;
  model the full field lists from the corpus before generating notice logic.
- Minor work-permit / age-certificate requirements (~40 jurisdictions) are not
  yet in this dataset — add before hiring minors is in scope.
- Paid-sick-leave and PFML onboarding notices (e.g. WA, CA, NY, MA) live in
  corpus section 5, not in this dataset.
- `ban_the_box` corpus conflicts (CT, HI, ME, MI, NV, NY) and contractor
  thresholds marked `verify` must be counsel-verified before the packs for
  those states can leave `REQUIRES_CUSTOMER_COUNSEL_CONFIGURATION`.

## Sources

- Generated RulePacks: `definitions/legal/packs/states/us-*.json`
  (E_VERIFY / NOTICE / FIELD_RESTRICTION / PAY_TRANSPARENCY obligations).
- Corpus: `planning/research/state-employment-law/*.md` — primarily section 2
  (notices at hire), section 4 (pay transparency), section 8 (ban-the-box,
  background checks, drug testing, E-Verify). Retrieval dates per file.
- New-hire reporting deadlines/contractor coverage: PRWORA (42 U.S.C. § 653a);
  state matrices from OpenAccountants, Symmetry Software, FirstHR —
  cross-checked where they disagreed; conflicts noted with `verify`.
- State withholding forms: state revenue-department form inventories
  (Patriot Software state W-4 chart).
