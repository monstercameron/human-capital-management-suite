package main

// Rich seed documents: Mermaid diagrams and GFM tables mixed with prose,
// plus a few passage-anchored comment threads, so the document page has
// realistic diagrams, tables and quotes to render. Fences are written as
// ''' in the Go source (raw strings cannot hold backticks) and become ```.

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

type richComment struct {
	// Author is "viewer", "owner" or a worker-key prefix.
	Author, Body, Quote, Prefix, Suffix string
	// ReplyTo is the index of the comment this one answers, or -1.
	ReplyTo  int
	Resolved bool
}

type richDoc struct {
	Title, OwnerPrefix, Folder string
	AgeDays                    int
	// SharedWith lists worker-key prefixes besides the viewer; Private docs
	// are not shared with the viewer.
	SharedWith []string
	Private    bool
	Body       string
	Comments   []richComment
}

// seedRichDocs is the fixed rich corpus; titles are new so the main plan's
// idempotency (owner and first title) covers them too.
var seedRichDocs = []richDoc{
	{Title: "Hiring approval workflow", OwnerPrefix: "hc-052-", Folder: "Policies", AgeDays: 64, SharedWith: []string{"hc-051-", "hc-004-", "hc-054-"},
		Body: `# Hiring approval workflow

**Owner:** Talent Acquisition · **Applies to:** every new requisition

Every new role goes through the same approval path so budget, level and pay range are settled before a job is posted. Most requisitions are approved within four business days.

## Approval flow

'''mermaid
flowchart TD
    A[Hiring manager drafts requisition] --> B{Budgeted headcount?}
    B -- Yes --> C[People Partner reviews level and range]
    B -- No --> D[Finance review]
    D --> E{Approved by Finance?}
    E -- No --> F[Requisition closed]
    E -- Yes --> C
    subgraph Leadership sign-off
        C --> G[Department head approves]
        G --> H[Chief People Officer approves]
    end
    H --> I[Recruiter opens the role]
'''

## Approval thresholds

| Requisition type | Approvers | Target turnaround |
| --- | --- | --- |
| Backfill, same level | People Partner, department head | 2 business days |
| New budgeted role | People Partner, department head, CPO | 4 business days |
| Unbudgeted role | Finance, department head, CPO | 7 business days |
| Executive role | CEO and CPO | 10 business days |

## Notes

Backfills for clinical roles skip the department head step when the unit is below its staffing grid. Offers above the midpoint of the range need a second approval from the People Partner.
`,
		Comments: []richComment{
			{Author: "viewer", Body: "Can we make this explicit for travel nurse conversions too?", Quote: "Backfills for clinical roles skip the department head step when the unit is below its staffing grid.", ReplyTo: -1, Resolved: true},
			{Author: "owner", Body: "Added to the next revision; conversions follow the backfill path.", ReplyTo: 0},
			{Author: "hc-054-", Body: "Finance would like 5 business days here during budget season.", Quote: "7 business days", Prefix: "Unbudgeted role Finance, department head, CPO", ReplyTo: -1},
		}},
	{Title: "Leave request lifecycle", OwnerPrefix: "hc-053-", AgeDays: 41, SharedWith: []string{"hc-051-", "hc-011-"},
		Body: `# Leave request lifecycle

This page shows what happens between an employee asking for time off and the time appearing on their pay statement.

## Sequence

'''mermaid
sequenceDiagram
    participant E as Employee
    participant W as Workspace
    participant M as Manager
    participant P as Payroll
    E->>W: Submit leave request
    W->>M: Notify for approval
    M-->>W: Approve or decline
    W-->>E: Confirmation
    W->>P: Send approved hours at cutoff
    P-->>E: Hours shown on pay statement
'''

## Service levels

Managers respond within three business days. Requests submitted after the payroll cutoff are paid in the following cycle.

| Step | Owner | Service level |
| --- | --- | --- |
| Manager decision | Manager | 3 business days |
| Payroll sync | People Operations | Next cutoff |
| Correction of errors | Payroll | 2 business days |
`,
		Comments: []richComment{
			{Author: "viewer", Body: "Should we say what happens when the manager is on leave?", Quote: "Managers respond within three business days.", ReplyTo: -1},
		}},
	{Title: "Headcount by department: Q3 2026", OwnerPrefix: "hc-031-", AgeDays: 20, SharedWith: []string{"hc-001-", "hc-054-"},
		Body: `# Headcount by department: Q3 2026

Headcount as of September 1, counting active employees at 1.0 FTE or less as one person each.

'''mermaid
pie showData
    title Headcount by department
    "Clinical operations" : 214
    "Care coordination" : 96
    "Engineering and product" : 71
    "Customer success and sales" : 48
    "People, finance and legal" : 37
    "Workplace services" : 12
'''

| Department | Headcount | Change since Q2 |
| --- | --- | --- |
| Clinical operations | 214 | +9 |
| Care coordination | 96 | +4 |
| Engineering and product | 71 | +2 |
| Customer success and sales | 48 | -1 |
| People, finance and legal | 37 | 0 |
| Workplace services | 12 | +1 |

Clinical operations grew the most, mostly through the new graduate nurse cohort.
`},
	{Title: "Monthly attrition report 2026", OwnerPrefix: "hc-033-", AgeDays: 12, SharedWith: []string{"hc-051-", "hc-053-", "hc-004-"},
		Body: `# Monthly attrition report 2026

Voluntary exits by month with the rolling twelve-month attrition rate.

'''mermaid
xychart-beta
    title "Voluntary exits and 12-month attrition"
    x-axis [Jan, Feb, Mar, Apr, May, Jun, Jul, Aug]
    y-axis "Exits" 0 --> 20
    y-axis "12-month rate (%)" 0 --> 20
    bar "Voluntary exits" [9, 7, 11, 8, 6, 10, 12, 7]
    line "12-month attrition rate (%)" [14.2, 13.9, 14.1, 13.8, 13.5, 13.6, 13.9, 13.4]
'''

| Month | Voluntary exits | 12-month rate |
| --- | --- | --- |
| June | 10 | 13.6% |
| July | 12 | 13.9% |
| August | 7 | 13.4% |

July was the highest month, driven by night-shift nurses leaving for day roles elsewhere. Stay interviews started in August on the two units with the most exits.
`,
		Comments: []richComment{
			{Author: "viewer", Body: "Let's bring this to the next leadership meeting.", Quote: "Stay interviews started in August on the two units with the most exits.", ReplyTo: -1},
		}},
	{Title: "New hire onboarding plan: October cohort", OwnerPrefix: "hc-052-", Folder: "Onboarding", AgeDays: 9, SharedWith: []string{"hc-005-", "hc-051-"},
		Body: `# New hire onboarding plan: October cohort

The October cohort has 18 new hires, 11 of them registered nurses.

'''mermaid
gantt
    title October cohort onboarding
    dateFormat YYYY-MM-DD
    section Before day one
    Background checks        :done,    bg, 2026-09-14, 10d
    Accounts and equipment   :active,  eq, 2026-09-21, 7d
    section First weeks
    Orientation              :         or, 2026-10-05, 2d
    Unit shadowing           :         sh, after or, 10d
    EHR training             :         ehr, after or, 5d
    section Check-ins
    Day 30 check-in          :milestone, m1, 2026-11-04, 0d
'''

## Owners

- **Recruiting:** background checks and start dates
- **IT:** accounts and laptops by September 28
- **Clinical education:** orientation, shadowing and EHR training
`},
	{Title: "Compensation cycle process map", OwnerPrefix: "hc-050-", Folder: "Comp cycle 2026", AgeDays: 95, SharedWith: []string{"hc-051-", "hc-053-", "hc-054-", "hc-055-"},
		Body: `# Compensation cycle process map

The end-to-end flow of the annual cycle, from budget to letters.

'''mermaid
flowchart LR
    B[Budget set by Finance] --> R[Managers recommend]
    R --> C[Calibration sessions]
    C --> Q{Within budget?}
    Q -- No --> R
    Q -- Yes --> A[Leadership approval]
    A --> L[Letters delivered]
    L --> P[Payroll effective date]
'''

## Checkpoints

| Checkpoint | Date | Owner |
| --- | --- | --- |
| Budgets released | February 2 | Finance |
| Recommendations due | February 20 | Managers |
| Calibration complete | March 6 | People Partners |
| Letters delivered | March 27 | Managers |

Recommendations that exceed the division budget go back to the manager before calibration, not after.
`,
		Comments: []richComment{
			{Author: "hc-054-", Body: "Finance agrees; this saved a week last year.", Quote: "Recommendations that exceed the division budget go back to the manager before calibration, not after.", ReplyTo: -1},
		}},
	{Title: "Incident escalation path", OwnerPrefix: "hc-023-", AgeDays: 33, SharedWith: []string{"hc-020-", "hc-035-"},
		Body: `# Incident escalation path

Use this path for any incident that affects pay, scheduling or patient-facing systems.

'''mermaid
flowchart TD
    D[Alert or employee report] --> T{Severity}
    T -- SEV-1 --> P1[Page on-call and incident lead]
    T -- SEV-2 --> P2[Page on-call]
    T -- SEV-3 --> Q[Ticket in service desk queue]
    P1 --> X[Notify Director of People Operations]
    P1 --> Y[Status page update every 30 minutes]
    P2 --> Y
'''

| Severity | Example | Response time |
| --- | --- | --- |
| SEV-1 | Payroll run cannot complete | 15 minutes |
| SEV-2 | Scheduling system degraded | 30 minutes |
| SEV-3 | Single user cannot sign in | 1 business day |

A SEV-1 that affects pay is always escalated to the Director of People Operations, even outside business hours.
`},
	{Title: "Care coordination restructure timeline", OwnerPrefix: "hc-011-", AgeDays: 27, SharedWith: []string{"hc-005-", "hc-051-"},
		Body: `# Care coordination restructure timeline

Care coordination moves from one central team to three regional pods.

'''mermaid
timeline
    title Care coordination restructure
    July 2026 : Proposal approved
    August 2026 : Pod leads selected : Caseload mapping
    September 2026 : North pod live
    October 2026 : East and West pods live
    December 2026 : Review of caseload balance
'''

## What changes for coordinators

Each coordinator keeps their current caseload through October. New referrals route to the pod that covers the patient's home clinic from the pod's go-live date.
`,
		Comments: []richComment{
			{Author: "viewer", Body: "People Ops will need the final pod rosters for the org chart.", Quote: "Each coordinator keeps their current caseload through October.", ReplyTo: -1},
			{Author: "owner", Body: "Rosters go out on October 1.", ReplyTo: 0},
		}},
	{Title: "Holiday calendar 2027", OwnerPrefix: "hc-053-", AgeDays: 6, SharedWith: []string{"hc-051-"},
		Body: `# Holiday calendar 2027

| Holiday | Date observed | Day | Premium for hours worked |
| --- | --- | --- | --- |
| New Year's Day | January 1 | Friday | 1.5x |
| Martin Luther King Jr. Day | January 18 | Monday | 1.5x |
| Memorial Day | May 31 | Monday | 1.5x |
| Juneteenth | June 18 | Friday | 1.5x |
| Independence Day | July 5 | Monday | 1.5x |
| Labor Day | September 6 | Monday | 1.5x |
| Thanksgiving Day | November 25 | Thursday | 1.5x |
| Christmas Day | December 24 | Friday | 2x |

When a holiday falls on a weekend it is observed on the nearest weekday. Clinical units bid for holiday shifts in September.
`},
	{Title: "Merit matrix 2026: final", OwnerPrefix: "hc-050-", AgeDays: 180, SharedWith: []string{"hc-051-", "hc-053-", "hc-054-"},
		Body: `# Merit matrix 2026: final

Approved by the comp committee on January 28. Use it with the merit cycle guide.

| Rating | Below midpoint | Near midpoint | Above midpoint |
| --- | --- | --- | --- |
| Exceptional | 6.0% | 5.0% | 4.0% |
| Strong | 4.5% | 3.5% | 2.5% |
| Solid | 3.0% | 2.5% | 1.5% |
| Developing | 1.0% | 0% | 0% |

Recommendations more than one point above the matrix need a written rationale. The overall budget is 3.5 percent of eligible base pay.
`,
		Comments: []richComment{
			{Author: "hc-051-", Body: "Can we add an example rationale?", Quote: "Recommendations more than one point above the matrix need a written rationale.", ReplyTo: -1, Resolved: true},
		}},
	{Title: "Shift differential rates by unit", OwnerPrefix: "hc-005-", AgeDays: 58, SharedWith: []string{"hc-006-", "hc-053-"},
		Body: `# Shift differential rates by unit

Differentials stack with weekend premiums and are part of the regular rate for overtime.

| Unit | Evening | Night | Weekend |
| --- | --- | --- | --- |
| ICU | $3.00 | $5.00 | $2.00 |
| Emergency department | $3.00 | $5.00 | $2.00 |
| Telemetry | $2.50 | $4.25 | $1.75 |
| Med-surg | $2.50 | $4.25 | $1.75 |
| Care coordination | $1.50 | $2.50 | $1.00 |

ICU and emergency department rates were raised in July to help fill night shifts.
`,
		Comments: []richComment{
			{Author: "viewer", Body: "Payroll confirmed the July change is live.", Quote: "ICU and emergency department rates were raised in July to help fill night shifts.", ReplyTo: -1},
			{Author: "hc-006-", Body: "Thanks, the night team noticed.", ReplyTo: 0},
		}},
	{Title: "PTO accrual by tenure", OwnerPrefix: "hc-051-", AgeDays: 140, SharedWith: []string{"hc-053-"},
		Body: `# PTO accrual by tenure

Accrual per biweekly pay period for full-time staff; part-time staff accrue in proportion to their FTE.

| Years of service | Hours per pay period | Days per year | Carryover cap |
| --- | --- | --- | --- |
| 0 to 2 | 6.15 | 20 | 40 hours |
| 3 to 5 | 7.69 | 25 | 60 hours |
| 6 to 10 | 9.23 | 30 | 80 hours |
| More than 10 | 10.77 | 35 | 80 hours |

A new rate starts in the pay period after the service anniversary.
`},
	{Title: "Benefits plan comparison 2027", OwnerPrefix: "hc-053-", AgeDays: 15, SharedWith: []string{"hc-051-", "hc-054-"},
		Body: `# Benefits plan comparison 2027

Compare the three medical plans before open enrollment. Premiums are per paycheck for employee-only coverage.

| | Standard PPO | High-deductible with HSA | HMO |
| --- | --- | --- | --- |
| Premium per paycheck | $86 | $31 | $62 |
| Deductible | $750 | $1,800 | $0 |
| Out-of-pocket maximum | $3,500 | $4,000 | $3,000 |
| Primary care visit | $25 copay | 20% after deductible | $20 copay |
| HarborCare HSA contribution | None | $750 per year | None |

'''mermaid
pie title Enrollment in 2026
    "Standard PPO" : 58
    "High-deductible with HSA" : 27
    "HMO" : 15
'''

The high-deductible plan is the lowest total cost for most employees who expect fewer than six doctor visits a year.
`},
	{Title: "Platform on-call rotation: Q4 2026", OwnerPrefix: "hc-020-", AgeDays: 4, SharedWith: []string{"hc-023-", "hc-021-"},
		Body: `# Platform on-call rotation: Q4 2026

| Week of | Primary | Secondary |
| --- | --- | --- |
| October 5 | Imani Thompson | Maya Patel |
| October 12 | Samuel Rivera | Julian Miller |
| October 19 | Valentina Rossi | Leo Garcia |
| October 26 | Maya Patel | Imani Thompson |

'''mermaid
flowchart LR
    Alert --> Primary
    Primary -- no ack in 10 min --> Secondary
    Secondary -- no ack in 10 min --> Director[Director of Engineering]
'''

Swaps are fine as long as both people update the rotation before the week starts.
`},
	{Title: "People analytics metric definitions", OwnerPrefix: "hc-032-", AgeDays: 75, SharedWith: []string{"hc-031-", "hc-051-"},
		Body: `# People analytics metric definitions

| Metric | Definition | Refresh |
| --- | --- | --- |
| Headcount | Active employees on the last day of the period | Daily |
| Voluntary attrition | Voluntary exits divided by average headcount, rolling 12 months | Monthly |
| Time to fill | Days from requisition approval to offer acceptance | Weekly |
| Internal fill rate | Share of roles filled by current employees | Monthly |

'''mermaid
flowchart LR
    HRIS[(HRIS)] --> W[Warehouse]
    ATS[(Applicant tracking)] --> W
    W --> D[Dashboards]
'''

All metrics exclude contractors and per-diem staff unless a dashboard says otherwise.
`},
}

// seedRichDocuments creates the rich corpus (reusing documents already
// present), shares it, files three documents in the viewer's folders and
// adds anchored comment threads to newly created documents only.
func seedRichDocuments(ctx context.Context, store *documenthubstore.Store, plan documentSeedPlan, now time.Time, tenant string, existing map[string]string, out io.Writer) error {
	byPrefix := func(prefix string) (seedPerson, bool) {
		for _, p := range plan.People {
			if strings.HasPrefix(p.Key, prefix) {
				return p, true
			}
		}
		return seedPerson{}, false
	}
	var created, reused, shared, comments, replies, resolved, filed int
	folders := map[string][]string{}
	for i, d := range seedRichDocs {
		owner, ok := byPrefix(d.OwnerPrefix)
		if !ok {
			owner = plan.People[i%len(plan.People)]
		}
		body := strings.ReplaceAll(d.Body, "'''", "```")
		if id, ok := existing[owner.Key+"\x00"+d.Title]; ok {
			reused++
			if d.Folder != "" {
				folders[d.Folder] = append(folders[d.Folder], id)
			}
			continue
		}
		id, first, err := store.CreatePersonalDocument(ctx, tenant, owner.Key, d.Title, body)
		if err != nil {
			return fmt.Errorf("create %q: %w", d.Title, err)
		}
		created++
		recipients := []string{}
		if !d.Private && owner.Key != plan.Viewer.Key {
			recipients = append(recipients, plan.Viewer.Key)
		}
		for _, prefix := range d.SharedWith {
			if p, ok := byPrefix(prefix); ok && p.Key != owner.Key {
				recipients = append(recipients, p.Key)
			}
		}
		for _, r := range recipients {
			if err := store.SharePersonalDocumentRole(ctx, tenant, id, owner.Key, r, documenthubstore.RoleCommenter); err != nil {
				return fmt.Errorf("share %q: %w", d.Title, err)
			}
			shared++
		}
		writtenAt := now.AddDate(0, 0, -d.AgeDays).Truncate(time.Second)
		var ids, commentIDs []string
		var resolvedIDs []string
		for j, c := range d.Comments {
			author := owner.Key
			switch {
			case c.Author == "viewer":
				author = plan.Viewer.Key
			case c.Author != "owner":
				if p, ok := byPrefix(c.Author); ok {
					author = p.Key
				}
			}
			in := documenthubstore.CommentInput{DocumentID: id, VersionID: first.ID, AuthorID: author, Body: c.Body, Quote: c.Quote, Prefix: c.Prefix, Suffix: c.Suffix}
			if c.ReplyTo >= 0 && c.ReplyTo < j {
				in.ParentID = ids[c.ReplyTo]
				replies++
			}
			comment, err := store.AddComment(ctx, tenant, in)
			if err != nil {
				return fmt.Errorf("comment on %q: %w", d.Title, err)
			}
			ids = append(ids, comment.ID)
			commentIDs = append(commentIDs, comment.ID)
			comments++
			if c.Resolved {
				if err := store.ResolveComment(ctx, tenant, id, comment.ID, owner.Key, true); err != nil {
					return fmt.Errorf("resolve on %q: %w", d.Title, err)
				}
				resolvedIDs = append(resolvedIDs, comment.ID)
				resolved++
			}
		}
		dated := seedDocPlan{Created: writtenAt}
		for j := range commentIDs {
			dated.Comments = append(dated.Comments, seedComment{At: writtenAt.Add(time.Duration(3+5*j) * time.Hour)})
		}
		if err := backdateSeedDocument(ctx, store, tenant, id, first.ID, "", dated, commentIDs); err != nil {
			return fmt.Errorf("backdate %q: %w", d.Title, err)
		}
		if err := backdateResolutions(ctx, store, tenant, resolvedIDs, writtenAt.Add(48*time.Hour)); err != nil {
			return err
		}
		if d.Folder != "" {
			folders[d.Folder] = append(folders[d.Folder], id)
		}
	}
	lib, err := store.GetLibrary(ctx, tenant, plan.Viewer.Key)
	if err != nil {
		return err
	}
	for _, f := range lib.Folders {
		if ids := folders[f.Name]; len(ids) > 0 {
			if err := store.MoveDocuments(ctx, tenant, plan.Viewer.Key, ids, f.ID); err != nil {
				return fmt.Errorf("file rich documents into %q: %w", f.Name, err)
			}
			filed += len(ids)
		}
	}
	fmt.Fprintf(out, "document seed %s rich documents: %d (%d created, %d already present); %d shares, %d comments (%d replies, %d resolved), %d filed\n",
		tenant, len(seedRichDocs), created, reused, shared, comments, replies, resolved, filed)
	return nil
}

// backdateResolutions moves seeded resolution events next to their
// backdated comments.
func backdateResolutions(ctx context.Context, store *documenthubstore.Store, tenant string, commentIDs []string, at time.Time) error {
	if len(commentIDs) == 0 {
		return nil
	}
	return store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE document_comment_resolution SET created_at=$3 WHERE tenant_id=$1 AND comment_id=ANY($2::text[])`, tenant, commentIDs, at)
		return err
	})
}
