# Legal Rule Packs and State Configuration Contract

This contract defines how jurisdiction-specific employment law becomes executable
content in Human Capital Management Suite: the jurisdiction model, the versioned rule-pack definition
family, the typed obligation kinds, the per-state configuration matrix, the
evaluation semantics, the authoring and review pipeline, and the tests that hold
all of it in place.

It builds on what `internal/governance/legal` already implements for `LEGAL-001`
(a signed `LegalContext`, an effective-dated `RulePack` with ten typed obligation
kinds, a `Registry` that fails closed with `RULE_COVERAGE_UNKNOWN`, and the
California and New York seed packs). Nothing here replaces those types. Every
change below is stated as an extension of a named existing type, with the
compatibility rule that governs it.

The normative inputs are the fifty state research files in
[`planning/research/state-employment-law/`](../research/state-employment-law/README.md),
read through their "Summary for Human Capital Management Suite" and "Implications for P1A/P1B"
sections. Those files are drafted research, not legal advice, and not a
contract; a rule reaches the product only through a reviewed, signed release as
described in [Authoring and Review Pipeline](#7-authoring-and-review-pipeline).

Governing authorities for scope: [next-steps.md](../next-steps.md) fixes P1A and
P1B contents, the [Legal and Compliance Plane](platform-architecture-catalog.md#912-legal-and-compliance-plane)
fixes the plane's responsibilities, and
[Governance Decision and Obligation Composition](governance-decision-and-obligation-composition.md)
owns composition across planes. This contract owns only the Legal plane's own
content model and its internal composition.

## 1. Scope and Phase Placement

```text
in scope
  jurisdiction model and attribution rules
  rule-pack definition family: schema, digest, signature, versioning
  typed obligation kinds and their lifecycle bindings
  per-state configuration matrix for the promotion / base-pay flow
  evaluation semantics: fail-closed, composition, preemption, receipts
  authoring, review and release pipeline
  test strategy

out of scope (owned elsewhere)
  cross-plane composition with AuthZ / DLP / Entitlement  -> governance-decision-and-obligation-composition.md
  record series, cutoff and disposition execution         -> records-management-and-disposition.md
  notice rendering and delivery evidence                  -> messaging-and-notification-plane.md
  wage, tax and filing computation                        -> WAGE-001, TAX-001, FILING-001
  processing inventory, DSR, residency                    -> PRIV-001..PRIV-007, RESIDENCY-001
```

Phase placement is unchanged by this contract. `LEGAL-001` remains delivered at
the reduced P1B depth recorded in the 2026-09-02 disposition. The matrix in
[section 5](#5-per-state-configuration-matrix) is a planning artifact for P1A; a
signed release for any state other than the two seeded fixtures is P1B or later
and requires the pipeline in [section 7](#7-authoring-and-review-pipeline).

## 2. Jurisdiction Model

### 2.1 JurisdictionRef

`Jurisdiction` (country, state, locality) is extended to carry an ordered
locality path, because the research shows nested sub-state rules that a single
locality string cannot express: Cook County and Chicago both regulate paid
leave for the same Illinois worker, and Montgomery County sets both a minimum
wage and a leave accrual cap distinct from Maryland's.

```text
JurisdictionRef
  country          ISO 3166-1 alpha-2, uppercase, required
  subdivision      ISO 3166-2 principal subdivision without country prefix
  locality_path[]  ordered, coarse to fine, e.g. ["Cook County", "Chicago"]
  level            COUNTRY | SUBDIVISION | LOCALITY
```

Compatibility: the existing `Jurisdiction.Locality` string is the last element
of `locality_path`. A zero-length path is a subdivision-level jurisdiction. The
canonical byte encoding appends the path length as a `uint32` field followed by
each element as a length-prefixed field, so an empty path and a one-element
empty-string path never encode identically.

`JurisdictionRef` is a fact carrier. It never infers anything, and locale never
contributes to it; that prohibition is already enforced by
`LegalContextInput.Locale` and stays enforced.

### 2.2 Attribution rules

`Resolve` today returns exactly one `Jurisdiction`. It must return an ordered
`JurisdictionSet`: one `PRIMARY` subdivision-level jurisdiction plus zero or
more `OVERLAY` jurisdictions, because for a worker in Chicago the applicable law
is Illinois **and** Cook County **and** Chicago, not a choice among them.

```text
JurisdictionSet
  primary          JurisdictionRef, level = SUBDIVISION, exactly one
  overlays[]       JurisdictionRef, level = LOCALITY or COUNTRY, deduplicated
  confidence       VERIFIED | ASSERTED
  provenance       which rule fired, from which fact, from which source system
```

Attribution rules, applied in order. The first rule that resolves wins; each
records its own provenance entry.

| Rule | Condition                                                                                                      | Result                                                                                                                                  |
| ---- | -------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| A1   | Not remote work and the worksite subdivision equals the asserted employment jurisdiction                       | `primary` = that subdivision; `confidence` = `VERIFIED`                                                                                 |
| A2   | Not remote work and they disagree                                                                              | `LEGAL_CONTEXT_UNKNOWN` (unchanged from today)                                                                                          |
| A3   | Remote work, one physical work subdivision over the whole attribution window                                   | `primary` = physical work subdivision; `confidence` = `VERIFIED` if the assertion agrees, else `ASSERTED`                               |
| A4   | Remote work, several physical work subdivisions, one holds at least `primary_work_threshold` of scheduled time | `primary` = that subdivision; every other subdivision with a nonzero share is recorded as a `multi_state_exposure` note, not an overlay |
| A5   | Remote work, several physical work subdivisions, none at or above the threshold                                | `LEGAL_CONTEXT_UNKNOWN` with reason `MULTI_STATE_UNRESOLVED`                                                                            |
| A6   | Any rule resolved a `primary`; the worker's work address falls inside a registered locality                    | that locality and each of its registered ancestors join `overlays`                                                                      |

`primary_work_threshold` is tenant configuration with no platform default; the
absence of a configured threshold makes A4 and A5 both resolve to
`LEGAL_CONTEXT_UNKNOWN`. Human Capital Management Suite does not choose a work-location tiebreak on the
customer's behalf.

The physical-work-location-controls policy in A3 is a stated policy, not a legal
conclusion. Nineteen research files record a multi-state or remote-work open
question and none of the fifty states supplies a general answer, so the policy is
declared per tenant, recorded in provenance, and shown in the receipt.

A locality joins `overlays` only when a locality-level release is registered for
it. An unregistered locality is not silently dropped: it is recorded as
`unregistered_locality` in the receipt, and a tenant may configure that condition
to fail closed.

### 2.3 Effective dating and legal time

Three independent time axes, none substitutable for another:

```text
statute time    EffectiveWindow [start, end) on a rule-pack release
business time   the transaction's effective date
knowledge time  known_at / recorded_at on the LegalContext
```

Rules:

1. Release selection uses **business time**, never execution time. A promotion
   effective 2027-01-01 is evaluated under the release whose window contains
   2027-01-01, even when drafted in 2026.
2. A future-dated transaction is revalidated whenever a release whose window
   covers its effective date is published or superseded after the proposal was
   simulated. Revalidation follows the materiality rule in
   [Business Intent and Change Request](business-intent-and-change-request.md#materiality-rule-for-control-snapshots):
   an unchanged obligation set leaves approvals standing; a changed obligation
   set creates a new revision; a new mandatory prohibition blocks execution.
3. A retroactive transaction resolves **two** releases: the release in force at
   the historical business date, and the release in force at `recorded_at`. Both
   are pinned in the receipt. Obligations from the historical release describe
   what should have happened; obligations from the current release describe
   present-day correction, notice and reporting duties. The two sets are never
   merged into one undifferentiated list.
4. A statutory amendment closes the prior release's window at the amendment date
   and opens a new release. Published releases are immutable; an amendment is
   never an edit.

Effective-dating is not theoretical for this corpus. Nine of the fifty files
carry a rule whose effective date falls inside the twelve months around
2026-09-03 — Virginia's wage-transparency and salary-history statute (Va. Code
§ 40.1-28.7:12) takes effect on 2026-09-03 itself; Ohio's mini-WARN (R.C.
4113.31) on 2025-09-29; Connecticut's restrictive-covenant amendments on
2026-10-01; Nebraska's paid sick leave on 2025-10-01; Michigan's small-employer
ESTA tier on 2025-10-01; Maryland's FAMLI contributions on 2027-01-01;
Massachusetts' PFML contribution shift and Connecticut's paid-sick-leave
expansion on 2027-01-01; Oklahoma's HB 3127 safety-sensitive rule on 2026-11-01.
A promotion effective a week either side of one of those dates gets a different
obligation set, and the golden vectors in
[section 8](#8-test-strategy) pin both sides.

## 3. Rule-Pack Definition Family

### 3.1 The three artifacts

```text
PackDefinition     authoring artifact; mutable; not evaluable
      |  author + cite + type
      v
PackCandidate      complete, validated, unsigned; evaluable only in review mode
      |  review + sign
      v
PackRelease        immutable, digested, signed; the only thing Evaluate reads
```

`RulePack` in `internal/governance/legal/rulepack.go` is the `PackRelease` shape.
The additions below are fields on that struct and a parallel definition file the
loader produces it from; `Registry.Register` keeps its current immutability
guarantee that a `(jurisdiction, pack id, version)` slot is written once.

```text
PackRelease
  pack_id                 stable, e.g. "us-il-promotion-base-pay-change"
  version                 major.minor, monotonically increasing per pack_id
  vocabulary_version      the ObligationKind vocabulary this release was typed against
  jurisdiction            JurisdictionRef
  window                  EffectiveWindow [start, end)
  source_type             STATUTE | REGULATION | AGENCY_GUIDANCE | CBA | CONTRACT | CUSTOMER_POLICY
  review_status           see 7.2
  obligations[]           ordered; each carries kind, typed body, Citation, ConfidenceMarker
  preemption_assertions[] see 6.4
  supersedes              PackReleaseRef?
  superseded_by           PackReleaseRef?
  digest                  lowercase hex sha256 over the canonical encoding
  signatures[]            detached ed25519 over the digest, one per signing role
```

### 3.2 What the digest covers

Covered, in this order, using the existing length-prefixed framing in
`internal/governance/legal/canonical.go`:

```text
"RP1" magic
pack_id, version.major, version.minor, vocabulary_version
jurisdiction (country, subdivision, locality_path[])
window.start, window.end, window.has_end
source_type, review_status
obligation_count
  for each obligation, in declared order:
    kind wire token
    obligation id
    every typed body field, in the field order declared in section 4
    citation.source_file, citation.section, citation.status, citation.confidence_marker
preemption_assertion_count, then each assertion's kind and citation
supersedes ref (pack_id, version, jurisdiction) or the empty triple
```

Deliberately not covered: the citation `Note` paraphrase, any display or
localization string, authoring metadata (author identity, draft timestamps,
research-file line numbers), and registry indexes. Rewrapping a research file or
correcting a paraphrase must not invalidate a signed release; changing any typed
field must. `Note` therefore may never carry a value that evaluation reads.

`vocabulary_version` is inside the digest because adding an obligation kind
changes what an empty obligation list means. A release typed against vocabulary
`v1` and evaluated by a `v2` engine is evaluated as "these kinds were not
considered", not as "these kinds do not apply", and the receipt says so.

### 3.3 Versioning

| Change                                                                         | Version bump | New release required  |
| ------------------------------------------------------------------------------ | ------------ | --------------------- |
| Statute amended, repealed, or newly effective                                  | major        | yes                   |
| Obligation added, removed, or moved to a different kind                        | major        | yes                   |
| A typed body field value changes (threshold, day count, record class, trigger) | major        | yes                   |
| Citation section corrected to a different operative provision                  | major        | yes                   |
| `ConfidenceMarker` cleared from `VERIFY` to `CONFIRMED`                        | minor        | yes                   |
| `review_status` raised                                                         | minor        | yes                   |
| Citation `Note` paraphrase reworded                                            | none         | no (digest unchanged) |

A `major` bump for a law change always pairs with an `EffectiveWindow` change:
the prior release gains an `End` equal to the new release's `Start`, and the two
link through `supersedes` / `superseded_by`. A `LegalContext` that pinned the
prior release keeps resolving to it forever, which is why `Registry.GetExact`
exists and why `Evaluate` re-fetches by exact release rather than by "latest".

## 4. Obligation Kinds

The ten kinds in `internal/governance/legal/obligation.go` keep their names,
wire tokens, ordinal values and typed shapes. Twelve kinds are added. Each new
kind is justified by the number of the fifty research files that raise it as
something the promotion or base-pay flow must do, counted from
[section 5](#5-per-state-configuration-matrix).

### 4.1 Existing kinds, unchanged

| Wire token           | Typed shape              | Trigger predicate                                 | Lifecycle step consumed         | Binding kind |
| -------------------- | ------------------------ | ------------------------------------------------- | ------------------------------- | ------------ |
| `NOTICE`             | `NoticeObligation`       | base pay rate changed                             | SIMULATE, EXECUTE, POST-COMMIT  | NODE, TIMER  |
| `FIELD_RESTRICTION`  | `FieldRestriction`       | compensation-setting consulted a restricted field | DRAFT                           | FIELD_MASK   |
| `RETENTION`          | `RetentionRule`          | unconditional                                     | POST-COMMIT                     | NODE         |
| `LEAVE_INTERACTION`  | `LeaveInteraction`       | worker holds a mandated leave balance             | PREFLIGHT, EXECUTE              | GUARD        |
| `PAY_FREQUENCY`      | `PayFrequencyConstraint` | unconditional                                     | PREFLIGHT                       | GUARD        |
| `FINAL_PAY_DEADLINE` | `FinalPayDeadline`       | concurrent separation                             | EXECUTE                         | TIMER        |
| `PAY_TRANSPARENCY`   | `PayTransparencyDuty`    | internal promotion                                | DRAFT, PREFLIGHT                | NODE         |
| `NON_COMPETE`        | `NonCompeteThreshold`    | worker has an existing covenant                   | SIMULATE, APPROVAL, POST-COMMIT | HUMAN_TASK   |
| `E_VERIFY`           | `EVerifyStatusCheck`     | new hire                                          | DRAFT                           | GUARD        |
| `MINI_WARN`          | `MiniWARNTrigger`        | concurrent reduction at or above threshold        | EXECUTE                         | TIMER        |

One correction to the existing trigger set: `LEAVE_INTERACTION` currently fires
only when `OnProtectedLeave` is true. Twenty-eight states impose an accrual-carry
rule that binds on **every** promotion, whether or not the worker is on leave —
Minnesota ESST, Illinois PLAWA, Arizona PST, Nevada NRS 608.0197 and the rest all
prohibit reset or forfeiture at a role change. The trigger becomes "the worker
holds a balance in a leave program the pack names", and `OnProtectedLeave`
becomes an additional fact that selects the job-restoration sub-rule.

### 4.2 Added kinds

| Wire token            | Typed shape and required fields                                                                                                                            | Trigger predicate                                       | Lifecycle step      | Binding kind      | States |
| --------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- | ------------------- | ----------------- | ------ |
| `WAGE_FLOOR`          | `id, floor_amount Money, worker_class, basis (HOURLY\|WEEKLY\|ANNUAL), indexation (NONE\|CPI\|SCHEDULE), next_adjustment_date?, citation`                  | unconditional                                           | PREFLIGHT, EXECUTE  | GUARD             | 30     |
| `PAY_EQUITY_REVIEW`   | `id, protected_bases[], comparator_standard, employer_size_floor?, permitted_differentials[], documentation_required bool, citation`                       | base pay rate changed                                   | SIMULATE, APPROVAL  | HUMAN_TASK        | 36     |
| `PAY_STATEMENT`       | `id, required_fields[], delivery (PAPER\|ELECTRONIC\|EITHER), consent_required bool, citation`                                                             | base pay rate changed                                   | POST-COMMIT         | NODE              | 18     |
| `CLASSIFICATION`      | `id, dimension (EXEMPTION\|OVERTIME_THRESHOLD\|CONTRACTOR), test_description, salary_threshold Money?, overtime_trigger?, citation`                        | role, hours, pay basis or pay rate changed              | PREFLIGHT, APPROVAL | GUARD, HUMAN_TASK | 10     |
| `PERSONNEL_FILE`      | `id, response_days int, day_basis (CALENDAR\|BUSINESS), frequency_cap_per_year?, copy_fee_permitted bool, citation`                                        | unconditional                                           | POST-COMMIT         | NODE              | 18     |
| `ANTI_RETALIATION`    | `id, protected_activities[], lookback_days int, disposition (FLAG\|BLOCK), citation`                                                                       | a protected activity is recorded inside the lookback    | PREFLIGHT, APPROVAL | GUARD, HUMAN_TASK | 50     |
| `JOB_SECURITY`        | `id, standard (AT_WILL\|GOOD_CAUSE_AFTER_PROBATION\|HANDBOOK_DISCLAIMER\|IMPLIED_CONTRACT_REVIEW), probation_days?, justification_required bool, citation` | adverse change (pay decrease, demotion, separation)     | APPROVAL            | HUMAN_TASK        | 4 + 30 |
| `SEPARATION_FILING`   | `id, form_name, recipient_authority, deadline_days int, day_basis, content_fields[], citation`                                                             | concurrent separation                                   | POST-COMMIT         | NODE              | 9      |
| `DRUG_TESTING`        | `id, permitted_bases[], written_policy_required bool, advance_notice_days?, protected_status[], citation`                                                  | role becomes safety-sensitive, or a test is ordered     | PREFLIGHT           | GUARD             | 13     |
| `BREACH_NOTIFICATION` | `id, subject_deadline_days int, day_basis, authority_threshold_count?, authority_deadline_days?, credit_monitoring_required bool, citation`                | a personal-data breach incident is opened               | POST-COMMIT         | TIMER             | 49     |
| `AUTOMATED_DECISION`  | `id, covered_uses[], bias_audit_required bool, audit_period_months?, candidate_notice_days?, disclosure_required bool, citation`                           | a model scored, ranked or recommended the subject       | DRAFT, APPROVAL     | GUARD, HUMAN_TASK | 3      |
| `MONITORING_CONSENT`  | `id, data_categories[], consent_form (WRITTEN\|NOTICE_ONLY), retention_limit_months?, deletion_deadline_days?, citation`                                   | the transaction reads or writes a covered data category | DRAFT               | FIELD_MASK        | 5      |

The `States` column counts research files whose Summary or §11 section asserts a
state-level rule of that kind. `ANTI_RETALIATION` is 50 and
`BREACH_NOTIFICATION` is 49 — every file records a protected-activity exception,
and every file but Massachusetts states a breach-notification duty. They are kept
as typed kinds rather than platform constants precisely because their parameters
(lookback windows, notification day counts, authority thresholds) differ per
state and must be cited, not assumed.

`JOB_SECURITY` reads `4 + 30`: four states impose a statutory standard that
changes the shape of the flow — Montana's WDEA good-cause requirement after
probation (MCA §§ 39-2-904, 39-2-912), South Carolina's handbook-disclaimer
statute (§ 41-1-110), Wyoming's handbook implied-contract doctrine, and Arizona's
constructive-discharge notice procedure (A.R.S. § 23-1502) — and roughly thirty
more files instruct the platform to route a handbook job-security promise to
review. The kind carries both, distinguished by `standard`.

### 4.3 Rejected as kinds

- **Minimum-wage indexation schedules** are `WAGE_FLOOR` fields, not a kind. The
  research gives twelve states an annual CPI adjustment; that is a field value.
- **Local ordinances** are a jurisdiction level plus a `PreemptionAssertion`
  (section 6.4), not an obligation kind. A Chicago paid-leave rule is a
  `LEAVE_INTERACTION` published against a locality-level `JurisdictionRef`.
- **Ban-the-box** appears in twenty-one files but never binds a promotion or
  base-pay change; it belongs to a hiring pack. It is not added here, and its
  absence is recorded so a later hiring pack does not rediscover it.
- **Equal-pay data reporting** (California Gov. Code § 12999, Massachusetts,
  Illinois EPRC) is a periodic filing, not a transaction obligation. It belongs
  to `FILING-001` and is explicitly out of scope per section 9.

### 4.4 Every kind is answered, including the inapplicable ones

An obligation whose trigger predicate evaluates false is recorded in the
evaluation receipt as `CONSIDERED_NOT_APPLICABLE` with the fact that made it
false. Silence never means "considered and dismissed". This mirrors the backlog's
`UNIT_ONLY` rule and is what makes the receipt an audit artifact rather than a
list of hits.

## 5. Per-State Configuration Matrix

Every cell is derived from that state's research file. The matrix is a planning
artifact and a completeness oracle, not a legal conclusion.

```text
Y   the research asserts a state-level rule of this kind
L   the research asserts only a local (sub-state) rule; no state rule
P   the state preempts local rules of this kind
F   the research asserts no state rule; the federal baseline applies
?   the research is uncertain, self-contradictory, or marked "verify"
```

A `?` is a blocking finding, not a footnote: a pack may not be released for a
state while any kind the flow consumes is `?`. Section 10 lists what has to be
re-researched.

### 5.1 Table A — kinds the promotion and base-pay flow consumes

| State | NOTICE | PAY_TRANSP | FIELD_RESTR | WAGE_FLOOR | PAY_FREQ | PAY_STMT | LEAVE | NON_COMPETE | CLASSIFN | PAY_EQUITY | RETENTION | PERSONNEL_FILE |
| ----- | ------ | ---------- | ----------- | ---------- | -------- | -------- | ----- | ----------- | -------- | ---------- | --------- | -------------- |
| AL    | F      | F          | F           | F          | F        | F        | F     | Y           | F        | Y          | Y         | F              |
| AK    | Y      | F          | F           | Y          | Y        | Y        | Y     | F           | Y        | F          | ?         | Y              |
| AZ    | F      | F          | F           | Y          | Y        | ?        | Y     | F           | F        | Y          | ?         | F              |
| AR    | F      | F          | F           | Y          | Y        | F        | F     | Y           | F        | Y          | ?         | F              |
| CA    | Y      | Y          | Y           | Y          | Y        | Y        | Y     | Y           | Y        | Y          | Y         | Y              |
| CO    | ?      | Y          | Y           | Y          | ?        | Y        | Y     | Y           | Y        | Y          | Y         | Y              |
| CT    | ?      | Y          | Y           | Y          | ?        | ?        | Y     | Y           | ?        | Y          | ?         | Y              |
| DC    | Y      | Y          | Y           | Y          | Y        | Y        | Y     | Y           | L        | Y          | Y         | F              |
| DE    | F      | F          | Y           | Y          | Y        | ?        | Y     | F           | F        | ?          | Y         | Y              |
| FL    | F      | F          | F           | Y          | F        | F        | F     | Y           | F        | Y          | F         | F              |
| GA    | F      | F          | F           | F          | Y        | F        | Y     | Y           | F        | Y          | Y         | F              |
| HI    | ?      | Y          | Y           | Y          | Y        | Y        | Y     | Y           | F        | Y          | Y         | ?              |
| ID    | Y      | F          | F           | F          | ?        | Y        | F     | Y           | F        | F          | Y         | F              |
| IL    | Y      | Y          | Y           | Y          | Y        | Y        | Y     | Y           | F        | Y          | Y         | Y              |
| IN    | F      | F          | F           | F          | Y        | F        | F     | Y           | F        | F          | Y         | F              |
| IA    | Y      | F          | F           | F          | ?        | ?        | F     | F           | F        | Y          | Y         | Y              |
| KS    | Y      | F          | F           | F          | Y        | F        | F     | ?           | Y        | F          | F         | F              |
| KY    | F      | F          | F           | F          | Y        | ?        | F     | F           | Y        | Y          | Y         | F              |
| LA    | F      | F          | F           | F          | F        | F        | F     | Y           | F        | F          | F         | F              |
| ME    | ?      | F          | Y           | Y          | Y        | Y        | Y     | Y           | Y        | Y          | Y         | Y              |
| MD    | Y      | Y          | Y           | Y          | ?        | ?        | Y     | Y           | F        | Y          | ?         | ?              |
| MA    | F      | Y          | Y           | Y          | ?        | ?        | Y     | Y           | F        | Y          | Y         | Y              |
| MI    | Y      | F          | F           | Y          | ?        | ?        | Y     | Y           | F        | F          | ?         | Y              |
| MN    | Y      | Y          | Y           | Y          | Y        | Y        | Y     | Y           | Y        | Y          | Y         | Y              |
| MS    | F      | F          | F           | F          | Y        | F        | F     | F           | F        | Y          | F         | F              |
| MO    | Y      | F          | F           | Y          | Y        | Y        | F     | Y           | F        | Y          | F         | F              |
| MT    | ?      | F          | F           | Y          | Y        | ?        | F     | Y           | F        | F          | ?         | F              |
| NE    | Y      | F          | F           | Y          | ?        | ?        | Y     | Y           | F        | Y          | F         | F              |
| NV    | Y      | Y          | Y           | Y          | Y        | Y        | Y     | Y           | Y        | Y          | Y         | Y              |
| NH    | Y      | F          | F           | F          | ?        | ?        | F     | Y           | F        | Y          | Y         | Y              |
| NJ    | Y      | Y          | Y           | Y          | ?        | ?        | Y     | F           | F        | Y          | Y         | ?              |
| NM    | ?      | F          | F           | Y          | Y        | ?        | Y     | Y           | F        | Y          | Y         | F              |
| NY    | Y      | Y          | Y           | Y          | ?        | Y        | Y     | F           | F        | Y          | Y         | ?              |
| NC    | Y      | F          | ?           | F          | Y        | ?        | Y     | Y           | F        | F          | ?         | F              |
| ND    | F      | F          | F           | F          | Y        | ?        | F     | Y           | F        | Y          | Y         | F              |
| OH    | F      | L          | L           | Y          | Y        | Y        | Y     | F           | F        | Y          | Y         | F              |
| OK    | F      | F          | F           | F          | Y        | ?        | P     | Y           | F        | Y          | Y         | F              |
| OR    | F      | F          | Y           | Y          | ?        | Y        | Y     | Y           | F        | Y          | Y         | Y              |
| PA    | Y      | F          | L           | F          | ?        | ?        | L     | Y           | F        | Y          | ?         | Y              |
| RI    | Y      | Y          | Y           | Y          | Y        | Y        | Y     | Y           | Y        | Y          | Y         | Y              |
| SC    | Y      | F          | F           | F          | ?        | Y        | F     | Y           | F        | F          | Y         | F              |
| SD    | F      | F          | F           | Y          | Y        | ?        | F     | Y           | F        | Y          | Y         | F              |
| TN    | F      | F          | F           | F          | Y        | ?        | Y     | Y           | F        | F          | ?         | F              |
| TX    | F      | F          | F           | F          | Y        | ?        | P     | Y           | F        | F          | F         | F              |
| UT    | F      | F          | F           | F          | Y        | ?        | F     | Y           | F        | Y          | Y         | F              |
| VT    | Y      | Y          | Y           | Y          | Y        | ?        | Y     | F           | F        | Y          | ?         | F              |
| VA    | Y      | Y          | Y           | Y          | Y        | ?        | L     | Y           | Y        | Y          | Y         | ?              |
| WA    | F      | Y          | Y           | Y          | ?        | Y        | Y     | Y           | F        | Y          | Y         | Y              |
| WV    | Y      | F          | F           | Y          | Y        | ?        | F     | Y           | F        | Y          | Y         | ?              |
| WI    | F      | P          | P           | P          | ?        | Y        | P     | Y           | F        | Y          | ?         | Y              |
| WY    | F      | F          | F           | F          | Y        | Y        | F     | Y           | F        | Y          | Y         | F              |

Column totals (`Y` + `L`, excluding `P`, `F` and `?`):

```text
NOTICE          23      PAY_TRANSPARENCY 17      FIELD_RESTRICTION 21
WAGE_FLOOR      31      PAY_FREQUENCY    32      PAY_STATEMENT     19
LEAVE           29      NON_COMPETE      40      CLASSIFICATION    11
PAY_EQUITY      39      RETENTION        32      PERSONNEL_FILE    18
```

### 5.2 Table B — kinds the flow declares but usually does not trigger

| State | FINAL_PAY | MINI_WARN | SEP_FILING | E_VERIFY | DRUG_TEST | ANTI_RETAL | JOB_SEC | BREACH | AUTO_DEC | LOCAL |
| ----- | --------- | --------- | ---------- | -------- | --------- | ---------- | ------- | ------ | -------- | ----- |
| AL    | F         | F         | ?          | Y all    | Y         | Y          | F       | Y 45d  | F        | L     |
| AK    | Y         | F         | ?          | F        | ?         | Y          | F       | Y      | F        | L     |
| AZ    | Y         | F         | ?          | Y all    | Y         | Y          | Y       | Y      | F        | Y     |
| AR    | Y         | F         | ?          | L pub    | ?         | Y          | F       | Y      | F        | L     |
| CA    | Y         | Y         | ?          | F        | Y         | Y          | F       | Y      | ?        | Y     |
| CO    | Y         | ?         | Y          | F        | ?         | Y          | F       | Y      | Y        | Y     |
| CT    | Y         | Y         | ?          | F        | ?         | Y          | F       | Y      | F        | F     |
| DC    | Y         | F         | ?          | F        | ?         | Y          | F       | Y      | F        | F     |
| DE    | Y         | Y         | ?          | F        | ?         | Y          | F       | Y 60d  | F        | F     |
| FL    | F         | F         | ?          | Y 25+    | Y         | Y          | F       | Y 30d  | F        | ?     |
| GA    | F         | Y 48h     | Y          | Y 11+    | ?         | Y          | F       | Y      | F        | F     |
| HI    | Y         | Y         | ?          | F        | ?         | Y          | F       | Y      | F        | F     |
| ID    | Y         | F         | ?          | F        | ?         | Y          | F       | Y      | F        | F     |
| IL    | Y         | Y         | ?          | F        | ?         | Y          | F       | Y      | Y        | Y     |
| IN    | Y         | F         | Y          | L pub    | ?         | Y          | F       | Y      | F        | F     |
| IA    | Y         | Y         | ?          | F        | ?         | Y          | F       | Y      | F        | F     |
| KS    | Y         | F         | ?          | F        | ?         | Y          | F       | Y      | F        | F     |
| KY    | ?         | F         | Y          | F        | ?         | Y          | F       | Y      | F        | F     |
| LA    | Y         | F         | Y          | L pub    | Y         | Y          | F       | Y      | F        | P     |
| ME    | Y         | Y         | ?          | F        | ?         | Y          | F       | Y      | F        | ?     |
| MD    | Y         | Y         | ?          | F        | ?         | Y          | F       | Y      | F        | Y     |
| MA    | Y         | ?         | ?          | F        | ?         | Y          | F       | ?      | F        | F     |
| MI    | Y         | F         | ?          | F        | ?         | Y          | F       | Y      | F        | L     |
| MN    | Y         | ?         | ?          | F        | Y         | Y          | F       | Y      | F        | Y     |
| MS    | F         | F         | ?          | Y all    | Y         | Y          | F       | Y      | F        | F     |
| MO    | Y         | F         | Y          | L pub    | ?         | Y          | F       | Y      | F        | F     |
| MT    | Y         | F         | Y          | F        | Y         | Y          | Y       | Y      | F        | F     |
| NE    | Y         | Y 90d     | ?          | L pub    | ?         | Y          | F       | Y      | F        | F     |
| NV    | Y         | F         | ?          | F        | Y         | Y          | F       | Y      | F        | F     |
| NH    | Y         | Y         | ?          | F        | ?         | Y          | F       | Y      | F        | F     |
| NJ    | Y         | Y 90d     | ?          | F        | ?         | Y          | F       | Y      | F        | F     |
| NM    | Y         | F         | ?          | F        | ?         | Y          | F       | Y      | F        | Y     |
| NY    | ?         | Y 90d     | ?          | F        | ?         | Y          | F       | Y      | L NYC    | Y     |
| NC    | Y         | F         | ?          | Y 25+    | ?         | Y          | F       | Y      | F        | ?     |
| ND    | Y         | F         | ?          | F        | ?         | Y          | F       | Y      | F        | F     |
| OH    | Y         | Y         | ?          | L constr | ?         | Y          | F       | Y      | F        | Y     |
| OK    | Y         | F         | ?          | L pub    | Y         | Y          | F       | Y      | F        | P     |
| OR    | Y         | ?         | ?          | F        | ?         | Y          | F       | Y      | F        | Y     |
| PA    | Y         | F         | ?          | F        | ?         | Y          | F       | Y      | F        | Y     |
| RI    | Y         | ?         | ?          | F        | ?         | Y          | F       | Y      | F        | F     |
| SC    | Y         | F         | ?          | Y all    | ?         | Y          | Y       | Y      | F        | F     |
| SD    | Y         | F         | ?          | F        | F         | Y          | F       | Y      | F        | L     |
| TN    | Y         | Y notify  | Y          | Y 35+    | ?         | Y          | F       | Y      | F        | P     |
| TX    | Y         | F         | ?          | F        | ?         | Y          | F       | Y 60d  | F        | P     |
| UT    | Y         | F         | ?          | Y 150+   | ?         | Y          | F       | Y      | F        | F     |
| VT    | Y         | ?         | ?          | F        | Y         | Y          | F       | Y 45d  | F        | F     |
| VA    | Y         | F         | ?          | F        | ?         | Y          | F       | Y      | F        | F     |
| WA    | Y         | Y         | ?          | F        | ?         | Y          | F       | Y      | F        | ?     |
| WV    | Y         | F         | Y          | F        | Y         | Y          | F       | Y      | F        | F     |
| WI    | Y         | Y         | ?          | F        | ?         | Y          | F       | Y      | F        | P     |
| WY    | Y         | F         | ?          | L pub    | Y         | Y          | Y       | Y      | F        | F     |

Column totals (`Y` + `L`, excluding `P`, `F` and `?`):

```text
FINAL_PAY_DEADLINE 45      MINI_WARN 17      SEPARATION_FILING  9
E_VERIFY           17      DRUG_TEST 13      ANTI_RETALIATION  51
JOB_SECURITY        4      BREACH    50      AUTOMATED_DECISION 3
LOCAL overlay      16      LOCAL preempted 5
```

`MONITORING_CONSENT` is not a column: only Illinois (BIPA, 740 ILCS 14),
Colorado (HB 24-1130), New York (Civil Rights Law § 201-i), California
(CCPA/CPRA applied to employee data) and Texas record a state rule, and none of
the five binds a promotion. It is declared so a later timekeeping or monitoring
capability does not invent it.

## 6. Evaluation Semantics

### 6.1 Statuses

`LegalEvaluationStatus` gains two values. The three existing ones keep their
ordinals and wire tokens.

```text
RESOLVED_ALLOW                 no applicable obligation
ALLOW_WITH_OBLIGATIONS         at least one applicable obligation, all satisfiable
RULE_COVERAGE_UNKNOWN          a pinned release could not be re-fetched, or no release covers a jurisdiction in the set
CONTRADICTORY_REQUIREMENTS     two obligations are mutually impossible; see 6.3
REVIEW_STATUS_INSUFFICIENT     a pinned release sits below the tenant's required review floor
```

Every one of the last three is fail-closed. `RESOLVED_ALLOW` is only reachable
when every jurisdiction in the set resolved to a pinned release and every
obligation in every release evaluated its trigger to false. An empty registry
never produces `RESOLVED_ALLOW`.

### 6.2 Evaluation order

```text
1  resolve JurisdictionSet                      -> LEGAL_CONTEXT_UNKNOWN on failure
2  pin one PackRelease per jurisdiction         -> RULE_COVERAGE_UNKNOWN on failure
3  check review floor per release               -> REVIEW_STATUS_INSUFFICIENT on failure
4  apply preemption assertions                  -> removes locality obligations only
5  evaluate each obligation's trigger predicate -> applicable / CONSIDERED_NOT_APPLICABLE
6  compose per kind                             -> per-kind comparator, section 6.3
7  detect contradictions                        -> CONTRADICTORY_REQUIREMENTS
8  bind, sort deterministically, emit receipt
```

Step 6 must be order-independent. Composing `{state, county, city}` in any
permutation yields byte-identical output; that is a property test, not a
convention.

### 6.3 Per-kind composition

There is no single "most protective wins" rule, because "protective" points in
opposite directions for different kinds: a longer non-compete protects the
employer, and voiding it protects the worker. Each kind declares its comparator.

| Kind                                  | Composition rule                                                                                                 | Contradiction condition                                                            |
| ------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `WAGE_FLOOR`                          | highest floor after normalizing basis to hourly                                                                  | two floors in different currencies                                                 |
| `NOTICE`                              | earliest required delivery date, i.e. the longest lead time; content fields union                                | one rule requires notice before and another forbids notice before the same date    |
| `PAY_FREQUENCY`                       | most frequent floor                                                                                              | none                                                                               |
| `RETENTION`                           | longest minimum per record class                                                                                 | a mandatory maximum shorter than another jurisdiction's minimum for the same class |
| `FIELD_RESTRICTION`                   | union of restricted fields                                                                                       | a restriction and a disclosure duty naming the same field                          |
| `PAY_TRANSPARENCY`                    | union of disclosure duties; strictest trigger point (posting < offer < on-request)                               | none                                                                               |
| `LEAVE_INTERACTION`                   | union; highest accrual rate and cap per named program                                                            | two rules assigning the same program incompatible carryover semantics              |
| `NON_COMPETE`                         | any release marking the covenant void wins; otherwise the shortest permitted duration and narrowest scope        | none                                                                               |
| `PAY_EQUITY_REVIEW`                   | union of protected bases; broadest comparator standard; union of permitted differentials                         | none                                                                               |
| `FINAL_PAY_DEADLINE`                  | earliest deadline per trigger                                                                                    | none                                                                               |
| `MINI_WARN`                           | lowest employee threshold, longest notice period                                                                 | none                                                                               |
| `PERSONNEL_FILE`, `SEPARATION_FILING` | shortest response deadline; union of content fields                                                              | none                                                                               |
| `CLASSIFICATION`                      | highest salary threshold, lowest overtime trigger                                                                | two incompatible exemption tests for the same role                                 |
| `JOB_SECURITY`                        | strictest standard: `GOOD_CAUSE_AFTER_PROBATION` > `HANDBOOK_DISCLAIMER` > `IMPLIED_CONTRACT_REVIEW` > `AT_WILL` | none                                                                               |
| all others                            | union by `(kind, id)`; deduplicate by identical digest of the typed body                                         | as declared on the kind                                                            |

A contradiction is reported with the full contradiction set: both obligations,
both citations, both releases. No evaluation order resolves it silently, and no
override flag exists in the Legal plane; resolution happens in the source
authority or by changing the business plan, matching
[Governance Decision and Obligation Composition](governance-decision-and-obligation-composition.md).

### 6.4 Preemption

```text
PreemptionAssertion
  kind          the ObligationKind that is preempted
  scope         LOCALITY_ONLY
  citation      Citation
```

A subdivision-level release may assert that it preempts locality-level rules of a
named kind. The assertion removes locality obligations of that kind from the set
before composition. It never removes a subdivision or country obligation, and it
never applies across kinds.

Five states in the corpus assert preemption that the promotion flow can hit:

| State | Kinds preempted                                                            | Citation                                      |
| ----- | -------------------------------------------------------------------------- | --------------------------------------------- |
| WI    | `WAGE_FLOOR`, `LEAVE_INTERACTION`, `FIELD_RESTRICTION`, `PAY_TRANSPARENCY` | Wis. Stat. § 104.001(2); § 103.10(1m); AB 748 |
| LA    | `LEAVE_INTERACTION`                                                        | La. R.S. 23:642                               |
| TX    | `LEAVE_INTERACTION`, `WAGE_FLOOR`                                          | HB 2127                                       |
| OK    | `LEAVE_INTERACTION`                                                        | 40 O.S. § 160                                 |
| TN    | `LEAVE_INTERACTION`, `WAGE_FLOOR`                                          | Tenn. Code § 50-2-112                         |

A preemption row and a Table A `F` cell for the same state and kind are not a
contradiction. `F` says the state itself imposes nothing beyond the federal rule
for that kind; the preemption assertion says localities may not add to it either.
Texas and Tennessee `WAGE_FLOOR` and Louisiana `LEAVE_INTERACTION` are exactly
that shape: the state pack carries no obligation of the kind and does carry the
`LOCALITY_ONLY` assertion. Extraction (2026-09-03) reads obligations from
section 5 and assertions from this table, and both stay authoritative for their
own half.

Without preemption modelled as a first-class assertion, a naive
"most-protective-wins" composition would attach a preempted Milwaukee paid-leave
obligation to a Wisconsin promotion. That is the concrete failure the assertion
exists to prevent, and it is a golden vector.

### 6.5 Receipt

```text
LegalEvaluationReceipt
  legal_context_digest
  jurisdiction_set            primary, overlays, unregistered localities
  pinned_releases[]           pack_id, version, jurisdiction, digest, review_status
  attribution_rule_fired      A1..A6
  remote_work_policy_applied
  obligations_applied[]       kind, id, typed body digest, citation, binding
  obligations_not_applicable[] kind, id, the fact that made the trigger false
  preemptions_applied[]       kind, asserting jurisdiction, removed obligation ids
  composition_trace[]         per kind: inputs, comparator, winner
  contradictions[]            pairs with citations
  status
  evaluated_at, effective_date, known_at
  digest, signature
```

The receipt is the evidence artifact for `ObligationState`. It is signed by the
same mechanism as `LegalContext`, verifies offline, and is what a later
reconciliation or repair replays against. A receipt whose `pinned_releases` no
longer resolve in a registry is still verifiable, because the release digest
travels inside it.

## 7. Authoring and Review Pipeline

### 7.1 Stages and roles

```text
research file (agent-drafted, unreviewed)
   |  mechanical extraction; no invention; every rule cites file + section
   v
PackDefinition        author: Rule Author (vendor)
   |  schema validation, citation completeness, VERIFY-marker inventory
   v
PackCandidate         reviewer: Vendor Legal Reviewer  -> review_status VENDOR_BASELINE
   |  tenant review
   v
PackCandidate         reviewer: Customer Counsel       -> review_status COUNSEL_APPROVED
   |  publish
   v
PackRelease           signer: Release Publisher (key holder)
```

Separation of duties: the Rule Author may not be a reviewer for the same pack;
the Release Publisher's signing key is not held by the author or by either
reviewer. A release carries one signature per role that approved it, all over the
same digest. `VENDOR_BASELINE` needs the publisher signature; `COUNSEL_APPROVED`
needs the publisher signature plus the customer-counsel signature.

Extraction is mechanical and adversarial in one direction only: an extractor may
narrow a research claim or drop it, never broaden it. A rule the research states
as a recommendation ("best practice: 1+ pay period") is extracted with
`ConfidenceMarker = VERIFY` and a `standard = RECOMMENDED` qualifier, never as a
statutory requirement. The research corpus contains many such recommendations and
they are the single largest source of over-claiming risk.

### 7.2 Review statuses

`ReviewStatus` extends from three values to six. Existing ordinals and wire
tokens do not move.

```text
REVIEW_STATUS_UNSPECIFIED                    zero value; never legal on a registered citation
UNREVIEWED                                   agent-drafted research, no review; fixture only
COUNSEL_APPROVED                             customer's authorized legal team approved this interpretation
VENDOR_BASELINE                              vendor legal review only; the customer has not approved it
CUSTOMER_DEFINED                             the customer replaced the vendor interpretation
REQUIRES_CUSTOMER_COUNSEL_CONFIGURATION      published deliberately unresolved; blocks evaluation until configured
```

Each tenant declares a **review floor** per jurisdiction. Evaluating against a
release below the floor returns `REVIEW_STATUS_INSUFFICIENT`. The two seed packs
in `internal/governance/legal` stay at `UNREVIEWED` and are therefore unusable
under any nonzero floor, which is the intended behavior for a fixture.

### 7.3 Confidence markers

```text
ConfidenceMarker
  CONFIRMED   the citation was checked against the primary source
  VERIFY      the research marked it uncertain, or a reviewer could not confirm it
  DISPUTED    two sources in the corpus disagree; both are recorded
```

A rule carrying `VERIFY` or `DISPUTED` may not reach `COUNSEL_APPROVED`. A pack
containing such a rule may still be published at `VENDOR_BASELINE` with the rule
present, so the uncertainty is visible rather than deleted. `DISPUTED` is the
status for the internal contradictions catalogued in section 10.

### 7.4 Change management

A law change enters as a `LegalChange` record: detection source, effective date,
affected `pack_id` set, and the impact query defined by `LEGAL-003`. The pipeline
above then runs unchanged. Publication is future-effective; activation is the
window boundary, not a deploy. No release is ever edited in place, and no
activation is implicit in a code deployment.

## 8. Test Strategy

### 8.1 Golden vectors per state

One directory per registered pack:

```text
testdata/legal/us-<subdivision>/
  release.json            the PackRelease as published, including digest and signatures
  proposal.json           the canonical promotion + base-pay-change proposal
  receipt.golden.json     the exact LegalEvaluationReceipt, including the
                          CONSIDERED_NOT_APPLICABLE set and the composition trace
```

The canonical proposal is one fixed scenario reused for every state so that
differences in the receipt are attributable to the pack and nothing else: an
internal promotion of an existing non-exempt worker to an exempt role, base pay
rising across every non-compete threshold in the corpus, effective on a fixed
date, with an existing covenant, a mandated leave balance, and no concurrent
separation or reduction.

Boundary vectors are additional golden files, not variants of the canonical one.
Required at minimum: Virginia at 2026-09-02 and 2026-09-04 (the § 40.1-28.7:12
boundary), Ohio at 2025-09-28 and 2025-09-30 (the mini-WARN boundary), and
Wisconsin with and without a Milwaukee locality overlay registered (the
preemption case).

### 8.2 Property tests

| Property                | Statement                                                                                                                                         |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| Commutativity           | composing a jurisdiction set in any permutation produces a byte-identical receipt body                                                            |
| Idempotence             | composing a set with a duplicated jurisdiction equals composing it once                                                                           |
| Preemption monotonicity | applying a preemption assertion never adds an obligation and never removes a non-locality obligation                                              |
| Comparator monotonicity | for each kind, adding a jurisdiction never weakens the composed constraint in that kind's declared direction                                      |
| Fail-closed             | for any generated input, an unresolved jurisdiction or an unfetchable release never yields `RESOLVED_ALLOW` or `ALLOW_WITH_OBLIGATIONS`           |
| Trigger totality        | for every obligation in every registered release, the receipt contains it in exactly one of `obligations_applied` or `obligations_not_applicable` |
| Digest stability        | rewording a citation `Note` leaves the release digest unchanged; changing any typed field changes it                                              |

### 8.3 Conformance fixture

`TestTodo_LEGAL_CFG_Conformance` loads every registered pack, validates it,
evaluates the canonical promotion proposal against it, and asserts:

- every pack validates and every obligation carries a complete `Citation`;
- every pack's `citation.source_file` resolves to an existing file under
  `planning/research/state-employment-law/`;
- the set of `PackRelease` jurisdictions equals the set of matrix rows marked
  releasable, and no releasable row contains a `?` in a kind the flow consumes;
- for every matrix cell marked `Y`, the corresponding pack carries at least one
  obligation of that kind, and for every cell marked `F`, it carries none —
  this is the completeness oracle that keeps section 5 and the code from drifting;
- every receipt verifies against its embedded signature after a round trip
  through the canonical encoding.

### 8.4 Other test classes

```text
FUZZ        FuzzPackDefinitionLoader        malformed definitions never panic and never
                                            produce a release that Validate accepts
MUTATION    seeded mutants on every trigger predicate and every comparator; a
            surviving mutant blocks completion
RACE        concurrent Register / Lookup / GetExact against one Registry
SECURITY    a release with a valid digest and a forged signature is rejected; a
            release signed by an untrusted key is rejected by VerifyWithKey; a
            tenant cannot read another tenant's CUSTOMER_DEFINED release
GOLDEN      the canonical encoding of PackRelease and LegalEvaluationReceipt
```

## 9. APIs, Failure, Security and Evidence

```text
legal_context.resolve|explain
rule_packs.define|validate|review|sign|publish|supersede
rule_packs.list|get_exact|lookup
legal.evaluate|explain|revalidate
legal.matrix.report
preemption.explain
```

Typed failures: `LEGAL_CONTEXT_UNKNOWN` (with reason
`MISSING_FACT | JURISDICTION_DISAGREEMENT | MULTI_STATE_UNRESOLVED | UNREGISTERED_LOCALITY`),
`RULE_COVERAGE_UNKNOWN`, `REVIEW_STATUS_INSUFFICIENT`,
`CONTRADICTORY_REQUIREMENTS`, `PACK_VALIDATION_FAILED`, `PACK_DUPLICATE_RELEASE`,
`SIGNATURE_INVALID`, `DIGEST_MISMATCH`, `VOCABULARY_VERSION_UNSUPPORTED`.

Define, review, sign, publish and supersede are five distinct capabilities with
separation-of-duties rules; read and evaluate are separate from all of them. A
tenant's `CUSTOMER_DEFINED` releases are tenant-scoped and never visible to
another tenant. Signing keys are held by the Release Publisher role only and are
out of scope for `LEGAL-001`'s fixture-grade `Signer`.

Evidence for every evaluation: the `LegalContext` digest and signature, every
pinned release digest and review status, the attribution rule that fired, the
composition trace, the preemptions applied, the contradiction set if any, and the
signed receipt. Evidence for every publication: the definition digest, the
extraction diff against the research file, the reviewer identities and their
signatures, the supersession link, and the effective window.

Gate A registers releases and evaluates them in simulation only, produces
receipts, and mutates nothing. Gate B adds execution-time revalidation against
the pinned releases, binds obligations to workflow nodes, guards, field masks,
timers and human tasks, and tests the boundary, preemption and contradiction
vectors above.

## 10. Non-Goals

1. **Not legal advice.** No release, receipt or matrix cell is a compliance
   claim. The platform proves which configured interpretation governed a
   transaction and whether its obligations were discharged. Nothing more.
2. **No automatic filings.** `SEPARATION_FILING`, `MINI_WARN` and pay-data
   reporting produce obligations, deadlines, owners and content — never a
   transmission. Human Capital Management Suite does not file with any state agency, and no rule pack
   may declare a transmitting effect.
3. **No legal-conclusion inference.** The platform never decides whether a
   non-compete is enforceable, whether an exemption test is met, whether a
   contractor is misclassified, or whether a termination has good cause. It
   surfaces the typed test and routes the conclusion to a human.
4. **No wage, tax or benefit calculation.** Minimum-wage floors are compared, not
   computed; overtime, tax and leave-benefit arithmetic belong to `WAGE-001`,
   `TAX-001` and `LEGAL-006`.
5. **No hiring, separation or leave packs in this scope.** Ban-the-box,
   background-check sequencing, mini-WARN execution and leave entitlement
   composition are named here only so a later pack does not rediscover them.
6. **No federal baseline pack.** The corpus states the federal FLSA and WARN
   baselines inconsistently (see below). A country-level `US` pack is required
   before any state pack that says "federal applies" can be evaluated, and it is
   not written here.

## 11. Open Questions

Blocking for a release, per section 5's `?` marks:

1. **Federal baseline is stated three ways.** Kentucky and West Virginia describe
   federal WARN as applying at 50+ employees; Alabama, Mississippi, Missouri and
   others state 100+; Iowa states "60 days for 50+ employee separations". The
   federal baseline must be researched once, as its own country-level pack, and
   the state files must stop restating it.
2. **Kentucky final pay is internally contradictory.** The summary reads "within
   14 days of termination or on the next regular payday", `§11` computes
   `Max(termination + 14 days, next payday)` and labels KRS 337.055 "whichever
   last occurs". Sooner and later are opposite rules. `DISPUTED` until resolved.
3. **New Jersey personnel-file access** is cited to "N.J.S.A. 34:8B-1, Personnel
   Files Act" with a 7-business-day window. No such act is confirmed; the cited
   chapter is not a personnel-records chapter. `VERIFY`.
4. **North Carolina salary-history ban (2024)** is asserted in the summary and
   already flagged in the research review log as unverified. `VERIFY`.
5. **Kansas non-competes** are cited to K.S.A. 50-163 with an unverified 2025
   SB 241 amendment; the review log already records that citation as likely
   wrong. `VERIFY`.
6. **Massachusetts pay transparency** carries three effective dates in one file:
   2025-07-31 for the mandate, 2025-10-29 for the posting requirement, and
   2025-02-01 for pay-data reporting. Effective dating cannot be typed until one
   is authoritative.
7. **Minnesota pay-range posting** is cited as § 181.173 in the summary and
   § 181.9414 in `§11`. One is wrong.
8. **Georgia payroll retention** is cited as O.C.G.A. § 34-7-2 in one place and
   § 34-4-5 in another for the same four-year rule.
9. **Illinois is the only file still `DRAFTED`** in the research queue and lists
   seven statutes its author could not access, including the E-Verify and
   credit-privacy provisions. Illinois cannot pass `VENDOR_BASELINE`.
10. **Alaska pay-change notice timing** reads "before any change" in the summary
    and "before the next pay period" in `§11`. Those produce different lawful
    effective dates.
11. **Michigan small-employer ESTA entitlement** is stated in the file itself as
    either 40 paid hours or 40 paid plus 32 unpaid.
12. **Personnel-file access is unresolved for ten states** (AZ, HI, MD, NJ, NY,
    NC, VA, VT, WV and the retention basis in AK). Several files distinguish a
    statutory right from a common-law practice without saying which governs.
13. **Pay-statement content is unresolved for eighteen states** marked `?` in
    Table A; most files describe deduction rules without stating whether an
    itemized statement is mandatory.
14. **State UI separation reporting** is named with a form and a deadline in only
    nine files, but is near-universal in practice. Either the other forty-one
    files are incomplete or the obligation is narrower than assumed.

Non-blocking, but they change the shape of the model:

15. **`primary_work_threshold` has no legal source.** Nineteen files raise a
    multi-state or remote-work question and none answers it. The threshold stays
    tenant configuration with no default until a source exists.
16. **Locality registration completeness.** The corpus names roughly forty
    localities with their own rules but no file enumerates them exhaustively.
    Florida and North Carolina do not address local ordinances or preemption at
    all, so both are `?` in Table B rather than `P`. An unregistered locality
    currently produces a receipt note; whether that should fail closed by
    default is a tenant policy question with no answer yet.
17. **CBA and works-council content** uses the same versioned rule interface per
    the architecture catalog, but no research file covers a collective agreement.
    The `source_type` enum reserves `CBA`; nothing populates it.

Extraction findings (2026-09-03), recorded when the fifty draft packs were first
generated from the corpus and not yet blocking because every affected cell is
already `?` or already flagged above:

- Eleven `Y` cells have no locatable statutory section in their research file
  (Arkansas and Connecticut `ANTI_RETALIATION`; Illinois `AUTOMATED_DECISION`;
  Maryland `FINAL_PAY_DEADLINE`; Montana and Tennessee `SEPARATION_FILING`; New
  York, Oregon, Washington and Wisconsin `BREACH_NOTIFICATION`; Wyoming
  `DRUG_TESTING`). Each emits an obligation marked `VERIFY` with the missing
  citation stated as missing; dropping them would assert the duty does not exist.
- `DRUG_TESTING` is `?` in thirty-six files and `SEPARATION_FILING` in forty-one.
  Both kinds need the same dedicated research pass item 14 already demands for
  separation reporting before any pack that declares them can leave
  `UNREVIEWED`.
- Locality (`L`) cells are excluded from state packs by construction, so the
  section 5 `Y+L` totals will never equal a state-pack inventory. Locality packs
  are a separate release family and are out of scope until a tenant selects a
  locality.
