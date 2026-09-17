# District of Columbia Employment Law (side task, researched 2026-09-17)

Research notes on the employment laws of the District of Columbia that affect
how Human Capital Management Suite operates: what it must record, notify,
retain, disclose, time, or refuse when it governs a workforce change for a
D.C.-based worker. Written from primary and official sources plus reputable
secondary summaries. This is a research input for the Legal plane's rule packs;
it is not legal advice and is not a normative contract; a rule enters the
product only through a reviewed rule pack.

The District is not a state: D.C. Council acts are subject to congressional
review, the D.C. Code is organized differently from state codes, and several
programs (Universal Paid Leave, the near-total non-compete ban) have no
fifty-state analogue. Figures below are stated as found; anything uncertain is
marked "verify".

## 1. Summary for Human Capital Management Suite

- **Minimum wage $17.95/hour (July 1, 2025), rising to $18.40/hour on July 1, 2026** (D.C. Code § 32-1003, CPI-indexed annually); tipped minimum is a percentage of the standard rate under a 2025 law ($10.30 from July 1, 2026).
- **Wage Theft Prevention notice at hire and on every change**: written notice in English and the employee's primary language stating employer identity, rate of pay and basis, allowances, overtime rate or exemption, and payday; a new notice is due whenever any item changes, so every promotion or base-pay change re-triggers the duty.
- **Pay transparency since June 30, 2024** (Wage Transparency Omnibus Amendment Act of 2023): min/max projected pay in all job listings, healthcare-benefits disclosure, salary-history ban, conspicuous rights posting; applies to employers with at least one D.C. employee.
- **Universal Paid Leave is employer-funded only** (0.75% payroll tax, no employee contribution): up to 12 weeks parental, family, and medical leave plus 2 weeks prenatal leave per 52-week period (benefit reductions scheduled October 1, 2026 — verify before modeling post-September windows).
- **Accrued Sick and Safe Leave tiers by employer size**: 1 hour per 37 hours (100+ employees, 7 days), per 43 hours (25–99, 5 days), per 87 hours (under 25, 3 days).
- **Near-total non-compete ban** (effective October 1, 2022): void except for highly compensated employees ($150,000+, $250,000+ for medical specialists, CPI-adjusted from 2024), capped at 365 days (730 for medical specialists).
- **Final pay**: discharge by the next working day; resignation by the next regular payday or within 7 days, whichever is earlier (D.C. Code § 32-1303).
- **Records 3 years minimum** (D.C. Code § 32-1008); **no private-sector personnel-file access statute** (the access right in § 1-631.05 covers District government employees only).
- **Breach notification** "in the most expedient time possible and without unreasonable delay" plus Attorney General notice at 50+ residents (D.C. Code § 28-3852).
- **No standalone equal-pay statute confirmed**; no District mini-WARN, drug-testing, or e-verify mandate confirmed (federal baselines apply; verify each before modeling).

## 2. Employment relationship: at-will status and exceptions; required written notices

- **At-will employment** is the default in the District; exceptions include terminations for exercising statutory rights (wage claims, workers' compensation, jury duty, military service), whistleblowing, refusal to commit an illegal act, and discrimination prohibited by the D.C. Human Rights Act (verify the full exception list before modeling an at-will override).
- **D.C. Human Rights Act (D.C. Code §§ 2-1402.01 et seq. — verify title/section before citing in product)**: bars employment discrimination across a broad set of protected traits (verify the current trait list; it is among the broadest in the country). A promotion decision is a covered employment action — screen promotion workflows for trait-based differentials.
- **Retaliation for wage activity**: the wage-payment titles protect workers who assert wage rights, file complaints, or assist investigations (verify the precise protected-activity list and remedy — treble/liquidated exposure has attached to wage violations — before modeling retaliation flags).
- **Wage Theft Prevention Amendment Act of 2014 (D.C. Law 20-157, effective February 26, 2015)**: at hire, the employer must furnish each employee a written, jointly-signed notice in English and the employee's primary language disclosing the employer's name, physical address, telephone number and trade names; the employee's rate of pay and basis (hour, shift, day); allowances claimed as part of the minimum wage; the overtime rate or exemption; and the regular payday.
- **A new notice is due whenever any noticed item changes**: a promotion, base-pay change, exemption-status change, or payday change each require an updated notice given to the employee when the change goes into effect.
- The employer must **retain copies of all furnished notices for at least three years** and post a copy or summary of the Minimum Wage Act in a prescribed form.
- The notice must be **jointly signed and dated** by employer and employee; for workers hired before the Act's effective date, notices were due within 90 days of effectiveness, and new hires receive the notice at the time of hiring.
- Penalties for notice failures are substantial (per-employee fines; verify current penalty schedule before modeling exposure).
- The Act broadened the Wage Payment and Collection Law's definition of employee so exemptions no longer exclude workers from coverage (verify the current exemption list before modeling who is out of scope).
- Record-keeping was tightened alongside notice: employers must record precise time worked except for employees exempt from minimum-wage and overtime requirements (verify the current record-content list in § 32-1008 before modeling time-capture fields).

## 3. Wages: minimum wage and overtime; pay frequency; final pay; deductions; pay statements

**Minimum wage (D.C. Code § 32-1003)**:

- July 1, 2025: $17.95/hour for all workers regardless of employer size (per the District's Office of Wage-Hour notice).
- July 1, 2026: $18.40/hour (same series of notices). The rate adjusts annually with the Consumer Price Index.
- Tipped minimum: set as a percentage of the standard rate under a 2025 law; $10.30/hour from July 1, 2026 (verify the tipped schedule before modeling tipped workers, given the delayed phase-out history).
- Applies to all employers; no size threshold.

**Overtime**: 1.5x the regular rate for hours over 40 per workweek (D.C. Code § 32-1003(c) as quoted in federal case law; verify exemptions track the FLSA before modeling exempt workers). There is no daily overtime threshold confirmed in this pass.

**Tipped workers**: the tipped minimum moves as a percentage of the standard rate under the 2025 revision ($10.30/hour from July 1, 2026, i.e. 56% of the standard rate). The phase-out history is tangled — an earlier schedule would have raised the tipped rate to $12.00 (delayed by Act 26-94) before the percentage peg replaced it — so verify the tipped schedule against the current DOES poster before modeling tipped workers, and never count tips or service charges toward the standard minimum-wage obligation (verify).

**Living wage and prevailing wage**: District contractors and subsidy recipients may owe higher living-wage or prevailing-wage rates (verify thresholds before modeling government-adjacent workers); the hire notice must state the applicable living or prevailing wage where one applies.

**Pay frequency**: District law designates regular paydays in advance; amendments reference payment at least once per month (verify whether semimonthly designation is required before modeling a frequency floor — carry no frequency constraint until confirmed).

**Final pay (D.C. Code § 32-1303)**:

- **Discharge**: all earned wages due not later than the working day following discharge (the next business day).
- **Resignation** (no written contract over 30 days): wages due on the next regular payday or within 7 days of quitting, whichever is earlier.
- **Labor-dispute suspension**: wages earned at suspension due by the next regular payday.
- Failure to pay triggers employer liability under the same section (verify liquidated-damages multipliers before modeling exposure).

**Deductions**: no District deduction schedule confirmed in this pass (verify before modeling permissible deductions; federal rules apply).

**Pay statement (D.C. Code § 32-1008(b))**: at the time of wage payment, the employer must furnish an itemized statement showing the date of payment, gross wages paid, each deduction taken, net wages paid, hours worked during the pay period, and the employee's tip declarations where applicable. The statement must reflect the promoted rate from its effective date — a mid-period promotion must not be paid out at the old rate on the next stub.

**Third-party payroll**: since January 1, 2020, employers with tipped workers paid under the tip-credit schedule (except hotel employers) must use a third-party payroll business to prepare payroll (verify current scope before modeling payroll-vendor constraints).

## 4. Pay transparency and equity

**Pay transparency (Wage Transparency Omnibus Amendment Act of 2023, signed January 12, 2024, effective June 30, 2024)**:

- Employers with at least one employee in the District must include the **minimum and maximum projected salary or hourly pay** (the good-faith range at the time of posting) in all advertised job listings and position descriptions; fixed pay may be stated as a single figure.
- Employers must **inform applicants of healthcare benefits** associated with the position.
- Employers may not **seek or rely on wage history**: no screening on prior compensation, no requiring history to satisfy min/max criteria, no contacting prior employers for it.
- Employers must **post a conspicuous notice** informing employees of their rights under the Act.
- The Act amends the Wage Transparency Act of 2014; enforcement runs through the Office of Human Rights and the courts (verify remedy specifics before modeling exposure).
- **Internal mobility counts**: promotions, transfers, and internal postings are within the disclosure duty — an internal-only promotion slate without a range violates the Act the same way an external posting does.
- **Good-faith range**: the min/max must reflect what the employer genuinely believes it would pay at posting time; placeholder or impossibly wide ranges defeat the duty (verify enforcement posture on range breadth before flagging ranges as compliant).
- **Healthcare-benefits notice**: the posting-time benefits statement must describe the healthcare coverage associated with the role (verify the required content items before generating posting templates).

**Equal pay**: no standalone District equal-pay statute was confirmed in this pass. Compensation discrimination claims proceed under federal law (Equal Pay Act, Title VII) and, verify, the D.C. Human Rights Act's employment provisions. Carry the pay-equity review as a ?-emission until the DCHRA compensation coverage is confirmed.

## 5. Leave and time

**Universal Paid Leave (Universal Paid Leave Amendment Act of 2016, administered by the Office of Paid Family Leave)**:

- **Funded solely by an employer payroll tax** (0.75% of covered wages; no employee contribution — the deduction logic every state program needs does not apply here). District government and federal employees are excluded; other part-time and full-time D.C. workers qualify regardless of residence.
- **Benefit durations in the 2026 window**: up to 12 weeks parental leave, 12 weeks family leave, and 12 weeks medical leave, plus 2 weeks prenatal leave, per 52-week period; maximum weekly benefit $1,190.
- **Scheduled reductions October 1, 2026** (FY2027 Budget Support Act, verify before modeling post-September windows): maximum weekly benefit $1,190 to $1,100, medical leave 12 to 10 weeks, family leave 12 to 6 weeks; parental bonding at 12 weeks and prenatal at 2 weeks unchanged; employer tax stays 0.75%.
- On promotion during active leave, the worker returns to the promoted role and updated rate; the benefit itself is state-paid, so the employer interaction is role/rate restoration plus contribution continuity.
- **Qualifying events**: parental leave covers bonding with a new child (birth, adoption, foster placement); family leave covers caring for a family member with a serious health condition; medical leave covers the worker's own serious health condition; prenatal leave covers prenatal care appointments (verify precise statutory definitions before modeling eligibility gates).
- **Eligibility**: part-time and full-time employees performing work in the District qualify regardless of residence, subject to covered-wage requirements (verify the base-period wage test before modeling eligibility); District government and federal employees are excluded, while self-employed workers may opt in by paying the tax.
- **Job protection**: verify whether UPL carries its own job-protection mandate or relies on FMLA overlap before modeling restoration guarantees for sub-50 employers.
- **Coordination**: employer-provided paid leave may run concurrently under program rules (verify coordination treatment before offsetting employer top-ups).

**Accrued Sick and Safe Leave Act of 2008 (as amended)**:

- 100+ employees: at least 1 hour per 37 hours worked, up to 7 days per year.
- 25–99 employees: at least 1 hour per 43 hours worked, up to 5 days per year.
- Under 25 employees: at least 1 hour per 87 hours worked, up to 3 days per year.
- Accrued unused leave carries over year to year subject to annual use caps; no payout of unused leave at separation is required.
- Permitted uses include the worker's or family member's health needs and domestic-violence/stalking services (verify the full use list before modeling eligibility).
- **Accrual mechanics**: leave accrues from the start of employment under the size-tiered rate; use caps track the annual day caps above even where carryover inflates the banked balance (verify the carryover/use-cap interplay before modeling balances).
- **No front-loading requirement confirmed**: whether an employer may front-load the annual cap instead of accruing per-hours-worked was not confirmed in this pass (verify before modeling accrual-method choice).
- **Promotion interaction**: a promotion changes neither the accrual tier (tier keys off employer headcount, not the worker's role) nor the banked balance; the post-promotion rate applies to paid sick hours taken after the effective date (verify rate-application timing before modeling payroll).

**Other leave**: no District paid family-leave program beyond UPL; federal FMLA applies at 50+ employees. No District vote, jury-pay, or bereavement mandate was confirmed in this pass (verify before modeling).

## 6. Records and access: retention, employee access, format

**Payroll records (D.C. Code § 32-1008(a))**: every covered employer must make, keep, and preserve for **not less than 3 years** (or the prevailing federal standard, whichever is greater) a record of each employee's name, address, occupation, rate of pay, hours worked each day and week, wages paid each pay period, and deductions. Records must be open to the Mayor's inspection.

**Notice copies**: copies of all Wage Theft Prevention notices furnished to employees must be retained for at least three years (verify whether the three-year clock runs from furnishing or separation before modeling purges).

**Personnel files**: **no District statute grants private-sector employees access to personnel files**. D.C. Code § 1-631.05 grants District government employees access to their official personnel records; that merit-personnel-system right does not extend to private employment. Carry the personnel-file kind as a ?-emission granting nothing.

**Record format**: no District electronic-format mandate confirmed (verify before modeling format constraints).

## 7. Privacy and data

**Breach notification (D.C. Code § 28-3852, as amended by the Security Breach Protection Amendment Act of 2020)**:

- Any person or entity conducting business in the District that owns or licenses computerized data including personal information must notify affected D.C. residents **in the most expedient time possible and without unreasonable delay** (no fixed day count; law-enforcement delay and scope-determination allowances apply).
- Notice must include credit-reporting-agency contacts and security-freeze instructions.
- **Attorney General notice** is required when the breach affects 50 or more D.C. residents; consumer-reporting-agency notice at 1,000+ residents.
- Personal information covers name linked to SSN, financial account, driver's license, passport, or biometric identifiers, unless publicly available or encrypted/redacted.
- **Good-faith exception**: acquisition by an employee or agent for a legitimate business purpose is not a breach unless the information is further disclosed or used for an unrelated purpose (verify the exception text before modeling incident triage).
- **Content**: resident notice must describe the breach, the types of information involved, contact information for the major credit-reporting agencies, and instructions for requesting a security freeze (verify the full content list before generating notices).
- **Enforcement**: the Attorney General enforces the breach title with civil penalties (verify the penalty schedule before modeling exposure).

**Monitoring, biometrics, data residency**: no District employee-monitoring, biometric-privacy, or data-residency statute was confirmed in this pass (verify before modeling; federal law applies).

## 8. Hiring and background

- **Ban-the-box**: the Fair Criminal Record Screening Amendment Act of 2014 restricts criminal-history inquiries — inquiry is deferred until after a conditional offer for covered employers (verify the employer-size threshold, covered offenses, and applicant-notice duties before modeling — carried as verify, not in the pack). A promotion posting for an open role is a fresh hiring decision for screening purposes (verify).
- **Background checks**: no District credit-history or social-media-password statute was confirmed in this pass (verify; federal FCRA rules apply).
- **Drug testing**: no District statute regulating private-sector drug testing was confirmed in this pass (verify; federal rules apply). Medical and adult-use cannabis are legal in the District, but no employee-protection rule for off-duty use was confirmed (verify before modeling test-result actions).
- **E-Verify**: no District mandate confirmed in this pass (verify; federal contractor rules apply). Federal Form I-9 completion and retention follows federal schedules.

## 9. Separation: mini-WARN, severance, non-competes

- **No District mini-WARN act was confirmed** in this pass; the federal WARN Act applies (verify before modeling a District notice rule).
- **No District severance mandate**; final-pay timing follows § 32-1303 (section 3).
- **Non-competes (Ban on Non-Compete Agreements Amendment Act of 2020, as narrowed by the Non-Compete Clarification Amendment Act of 2022, effective October 1, 2022)**:
  - Post-employment non-competes are **void** except for **highly compensated employees** earning at least $150,000/year ($250,000/year for medical specialists: licensed physicians who completed residency); broadcast employees never qualify.
  - Thresholds adjust with the Consumer Price Index beginning in 2024 (verify the current-year figure before enforcing a threshold).
  - Duration caps: 365 days from separation (730 days for medical specialists); substantive and procedural requirements attach (verify the full requirement list before modeling enforceability).
  - Agreements covering sub-threshold workers signed on or after October 1, 2022 are void; earlier agreements remain under prior law (verify retroactivity treatment before flagging legacy agreements).
  - **Procedural requirements** attach to surviving highly compensated covenants — including advance written notice with prescribed language and a minimum consideration period (verify the exact notice text and day count before generating covenant workflows).
  - **Concurrent-employment and moonlighting rules**: the original 2020 Act also restricted barring outside employment; the 2022 amendment re-scoped those provisions (verify current outside-work limits before modeling moonlighting policies).
  - **Non-solicitation**: customer and employee non-solicitation covenants are analyzed separately from non-competes under District law (verify before treating a non-solicit as a non-compete).

## 10. Classification and multi-state

- A District-specific independent-contractor test was not confirmed in this pass (verify before modeling classification; federal tests apply).
- UPL eligibility turns on District work, not residence: part-time and full-time D.C. workers qualify regardless of where they live; District government and federal employees are excluded.
- Remote workers performing D.C. work from outside the District create multi-jurisdiction exposure (verify the work-location rule before attributing).
- **Commuter belt**: workers living in Maryland or Virginia but performing work in the District are D.C.-covered for wage, leave, and transparency duties while D.C. income-tax withholding follows reciprocity rules (verify the current reciprocity posture before modeling withholding — Maryland and Virginia have historically held D.C. reciprocity).
- **Multi-site promotions**: a promotion moving a worker into or out of the District changes the applicable pack mid-stream; the D.C. hire/change notice, UPL contribution status, and non-compete enforceability must all be re-evaluated at the effective date.

## 11. Implications for P1A/P1B

1. **Wage floor (§ 32-1003)**: verify the new rate is at least the in-effect minimum ($17.95 through June 30, 2026; $18.40 from July 1, 2026); re-pin the floor each July 1.
2. **Hire/change notice (WTPA 2014)**: every promotion or base-pay change must regenerate the bilingual written notice with the new rate, basis, and effective date; store the signed copy for the 3-year retention rule.
3. **Posting transparency (2023 Omnibus, eff. 2024-06-30)**: block promotion postings missing a good-faith min/max range; capture healthcare-benefits text; never store applicant wage history.
4. **UPL interaction**: on promotion during active UPL leave, restore the promoted role and rate; keep the 0.75% employer contribution whole (no employee deduction exists to adjust); track the October 2026 benefit cuts for post-September windows (verify).
5. **Sick-leave tiers (ASSLA)**: carry forward accrued balances at the size-tiered accrual rate; never cash out at separation.
6. **Non-compete recheck**: on promotion, void sub-threshold covenants (post-October-2022 agreements); only highly compensated/medical-specialist covenants within duration caps survive (verify current CPI-adjusted thresholds).
7. **Final pay (§ 32-1303)**: discharge by the next working day; resignation by the next payday or within 7 days, whichever is earlier, all at the promoted rate from the effective date.
8. **Pay-equity review**: ?-emission — no standalone equal-pay statute confirmed; verify DCHRA compensation coverage before promoting this to a Y rule.
9. **Personnel file**: ?-emission granting nothing — no private-sector access statute; § 1-631.05 is District-government-only.
10. **Open questions**: pay-frequency floor; deduction schedule; WTPA penalty amounts; ban-the-box timing; contractor test; DCHRA pay-coverage confirmation; post-September-2026 UPL benefit figures.

## Pack evidence map (what the us-dc.json draft carries and why)

- WAGE_FLOOR ← §3 (§ 32-1003; $17.95 at window start, $18.40 from 2026-07-01, CPI-indexed).
- NOTICE ← §2 (WTPA 2014 hire/change bilingual written notice with new notice on every promotion/pay change).
- PAY_TRANSPARENCY ← §4 (2023 Omnibus: min/max range in all postings, healthcare-benefits disclosure, salary-history ban, conspicuous rights posting; eff. 2024-06-30).
- LEAVE_INTERACTION (UPL) ← §5 (12 weeks parental/family/medical + 2 weeks prenatal; 0.75% employer-only tax; role/rate restoration on return).
- LEAVE_INTERACTION (ASSLA) ← §5 (size-tiered accrual 1/37, 1/43, 1/87 with 7/5/3-day caps; carryover; no separation payout).
- NON_COMPETE ← §9 (2020 ban as narrowed 2022; $150k/$250k thresholds CPI-adjusted from 2024; 365/730-day caps; eff. 2022-10-01).
- FINAL_PAY_DEADLINE ← §3 (§ 32-1303: discharge next working day; resignation next payday or within 7 days, whichever earlier).
- RETENTION ← §6 (§ 32-1008(a): payroll records 3+ years; WTPA notice copies 3 years).
- PAY_STATEMENT ← §3 (§ 32-1008(b) itemized stub each payday).
- BREACH_NOTIFICATION ← §7 (§ 28-3852: most-expedient/without-unreasonable-delay resident notice; AG notice at 50+ residents).
- PAY_EQUITY_REVIEW ← §4 ?-emission (no standalone equal-pay statute confirmed; verify DCHRA compensation coverage).
- PERSONNEL_FILE ← §6 ?-emission granting nothing (no private-sector access statute; § 1-631.05 is District-government-only).
- F (correctly absent): pay-frequency floor, deduction schedule, mini-WARN, severance mandate, biometric/monitoring duties, drug-testing limits, e-verify, contractor test — none confirmed in this pass; federal baselines apply.

### Enforcement map (which agency owns what)

- **Office of Wage-Hour (Department of Employment Services)**: minimum wage, overtime, final pay, WTPA notices, sick-leave accrual, UPL contributions and benefits. The DOES wage-hour rules and posters are the operative compliance surface for payroll-impacting duties.
- **Office of Human Rights**: Wage Transparency Act and D.C. Human Rights Act employment claims.
- **Office of the Attorney General**: breach-notification intake (50+ residents), workers'-rights enforcement actions, civil penalties under the wage titles.
- **Private rights of action** attach to several titles (transparency, wage payment); statutes of limitations vary by title (verify each limitations period before modeling claim-exposure windows).
- **Posting surface**: the DOES Universal Wage Law poster consolidates minimum-wage, sick-leave, pay-stub, and retaliation rights; the transparency Act adds a separate conspicuous posting. A promotion workflow that changes worksite posting coverage must confirm both posters remain displayed.

### Promotion-day checklist (minimum compliant execution for a D.C. worker)

- Confirm the new rate clears the in-effect minimum ($17.95 through June 30, 2026; $18.40 from July 1, 2026).
- Regenerate and collect the signed bilingual WTPA notice with the new rate, basis, and effective date; file the copy for the 3-year retention rule.
- If the promotion is posted (internally or externally), attach the good-faith min/max range and healthcare-benefits text; never pull applicant wage history.
- Carry forward ASSLA balances; apply the post-promotion rate to sick hours taken after the effective date.
- If the worker holds a non-compete, test it against the $150k/$250k thresholds and 365/730-day caps; void sub-threshold post-October-2022 covenants.
- If the worker is on UPL leave, restore the promoted role and rate on return; adjust the 0.75% contribution base going forward.
- Reflect the promoted rate on the next § 32-1008(b) itemized stub; on separation, pay discharge wages by the next working day.

### UPL claims mechanics (for leave-workflow modeling)

- Workers file UPL claims with the Office of Paid Family Leave; benefits are paid by the District, not the employer, so the employer's payroll role is limited to accurate wage reporting and contribution payment.
- Contribution underpayment creates employer liability independent of any worker's leave event — a promotion with a raise increases the contribution base from the effective date.
- Medical-certification and claims-appeal procedures follow OPFL regulations (verify documentation requirements before requesting worker medical information — federal ADA confidentiality constraints also apply).
- Self-employed opt-in coverage and multi-employer wage aggregation follow program rules (verify before modeling nonstandard work arrangements).

## 12. Sources

1. [D.C. DOES: Minimum Wage Increase Notice (July 1, 2026: $17.95 to $18.40)](https://does.dc.gov/sites/default/files/dc/sites/does/publication/attachments/2026%20Minimum%20Wage%20Increase%20Noticevf%20-%20Clean%20updated.pdf) — retrieved 2026-09-17
2. [Bloomberg Law: D.C. Minimum Wage Rising to $18.40 on July 1](https://news.bloomberglaw.com/payroll/district-of-columbia-minimum-wage-rising-to-18-40-on-july-1) — retrieved 2026-09-17
3. [Bloomberg Law: D.C. Minimum Wage Rising to $17.95, Not $18](https://news.bloomberglaw.com/payroll/district-of-columbia-minimum-wage-rising-to-17-95-not-18) — retrieved 2026-09-17
4. [D.C. OCFO: Universal Paid Leave Fund chapter (2026)](https://cfo.dc.gov/sites/default/files/dc/sites/ocfo/publication/attachments/ul_uplf_chapter_2026m.pdf) — retrieved 2026-09-17
5. [Insurance Business: DC Paid Leave Cuts (FY2027 Budget Support Act benefit reductions, 0.75% tax)](https://www.insurancebusinessmag.com/us/news/benefits/dc-paid-leave-cuts-expose-a-risk-brokers-cant-ignore-588709.aspx) — retrieved 2026-09-17
6. [Bloomberg Law: D.C. to Expand Paid Leave, Cut Tax Rate After Strong Audit (12-week expansion)](https://news.bloomberglaw.com/daily-labor-report/d-c-to-expand-paid-leave-cut-tax-rate-after-strong-audit) — retrieved 2026-09-17
7. [Lexology: D.C. Council Approves New Paid Parental, Family, and Medical Leave Act (0.62% origination tax)](https://www.lexology.com/library/detail.aspx?g=705ba8b8-ea30-4d9a-a43e-6ab8e9e7ea9c) — retrieved 2026-09-17
8. [Kelley Drye: D.C.'s Wage Theft Amendment Takes Effect (hire/change notice duties)](https://www.kelleydrye.com/viewpoints/blogs/labor-days/d-c-s-wage-theft-amendment-takes-effect-february-26-imposes-new-notice-and-hours-recording-obligations-on-employers) — retrieved 2026-09-17
9. [DC Bar: Don't Forget the Wage Theft Prevention Amendment Act (new notice on every change)](https://www.dcbar.org/pro-bono/about-the-center/pro-bono-center-nonprofit-newsletter/nonprofit-newsletter-fall-2016/don%E2%80%99t-forget-the-wage-theft-prevention-amendment-a) — retrieved 2026-09-17
10. [D.C. DOES: Universal Wage Law Poster 2025 ($17.95 minimum, sick-leave and pay-stub rights)](https://does.dc.gov/sites/default/files/dc/sites/does/publication/attachments/2025%20Universal%20Wage%20Law%20Poster_0.pdf) — retrieved 2026-09-17
11. [Lexology: New Pay Transparency Requirements for D.C. Employers in 2024 (Omnibus Act duties)](https://www.lexology.com/library/detail.aspx?g=f8480085-a58e-4db8-aa50-6cef47bb3ab2) — retrieved 2026-09-17
12. [SHRM: District of Columbia Amends Wage Transparency Law (6/30/24)](https://www.shrm.org/topics-tools/tools/express-requests/district-of-columbia-amends-wage-transparency) — retrieved 2026-09-17
13. [Shawe Rosenthal: New Pay Transparency Requirements Coming to D.C.](https://shawe.com/eupdate/new-pay-transparency-requirements-coming-to-d-c/) — retrieved 2026-09-17
14. [Mondaq: New Washington DC Pay Transparency Law Effective June 30, 2024](https://www.mondaq.com/unitedstates/employee-rights-labour-relations/1433146/new-washington-dc-pay-transparency-law-scheduled-to-go-into-effect-on-june-30-2024) — retrieved 2026-09-17
15. [Lexology: D.C.'s New Non-Compete Restrictions Take Effect October 1, 2022 ($150k/$250k thresholds)](https://www.lexology.com/library/detail.aspx?g=6ad1b6a7-6de7-4b22-82b2-4a26223ebfa2) — retrieved 2026-09-17
16. [Crowell: Amendment to D.C.'s Ban on Non-Compete Agreements (365/730-day caps)](https://www.crowell.com/en/insights/client-alerts/long-awaited-amendment-to-d-c-s-ban-on-non-compete-agreements-addresses-employer-concerns) — retrieved 2026-09-17
17. [Hogan Lovells: DC's New Non-Compete Restrictions Take Effect October 1, 2022](https://www.hoganlovells.com/en/publications/dcs-new-non-compete-restrictions-take-effect-october-1-2022) — retrieved 2026-09-17
18. [Kollman: D.C. Scales Back Non-Compete Ban (CPI adjustment from 2024)](https://www.kollmanlaw.com/restrictive-covenants/d-c-scales-back-non-compete-ban/) — retrieved 2026-09-17
19. [Justia: D.C. Code § 32-1303, Payment of Wages Upon Discharge or Resignation](https://law.justia.com/codes/district-of-columbia/2021/title-32/chapter-13/subchapter-i/section-32-1303/) — retrieved 2026-09-17
20. [Nolo: Final Paycheck State Laws (D.C. next-business-day / 7-day limbs)](https://nolo.com/legal-encyclopedia/final-paycheck-employee-rights-chart-29882.html) — retrieved 2026-09-17
21. [EY: District of Columbia Employees May Use Accrued Paid Sick Leave (ASSLA tiers)](https://taxnews.ey.com/news/2020-0758-district-of-columbia-employees-may-use-accrued-paid-sick-leave-for-covid-19) — retrieved 2026-09-17
22. [Lexology: D.C. Expands Paid Sick Leave Law (1/37, 1/43 tier rates)](https://www.lexology.com/library/detail.aspx?g=f58bef9f-dc37-492b-baf4-32e032d8c66f) — retrieved 2026-09-17
23. [Casetext: D.C. Code § 32-1008, Duties of Employers; Open Records (3-year retention)](https://casetext.com/statute/district-of-columbia-official-code/division-v-local-business-affairs/title-32-labor/chapter-10-minimum-wages/subchapter-i-general/section-32-1008-duties-of-employers-open-records) — retrieved 2026-09-17
24. [Justia: D.C. Code § 32-1331.12, Employer Record-Keeping Requirements (3 years)](https://law.justia.com/codes/district-of-columbia/2016/title-32/chapter-13/subchapter-ii/section-32-1331.12/) — retrieved 2026-09-17
25. [Pillsbury: The Legal Landscape Rapidly Changes for D.C. Employers (§ 32-1008 three-year records)](https://www.pillsburylaw.com/a/web/1001/AlertFeb2015EmploymentDCEmployers.pdf) — retrieved 2026-09-17
26. [FindLaw: D.C. Code § 28-3852, Notification of Security Breach](https://codes.findlaw.com/dc/division-v-local-business-affairs/dc-code-sect-28-3852.html) — retrieved 2026-09-17
27. [Casetext: Washington D.C. Adds Security Requirements in New Data Breach Notification Law (§ 28-3852 timing, 50-resident AG notice)](https://casetext.com/analysis/washington-dc-adds-security-requirements-in-new-data-breach-notification-law) — retrieved 2026-09-17
28. [Casetext: D.C. Code § 1-631.05, Employee Access to Official Personnel Record (District-government-only)](https://casetext.com/statute/district-of-columbia-official-code/division-i-government-of-district/title-1-government-organization/chapter-6-merit-personnel-system/subchapter-xxxi-records-management-and-privacy-of-records/section-1-63105-employee-access-to-official-personnel-record) — retrieved 2026-09-17
29. [DC Wage Law: D.C. Wage Payment and Collection Law text (§ 32-1303 discharge/resignation limbs)](http://dcwagelaw.com/wp-content/uploads/02-dcwpcl-pdf-version.pdf) — retrieved 2026-09-17
