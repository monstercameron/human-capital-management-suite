// Package timecard owns the WF-CAP-007 timecard aggregate and the FTIME-004
// review, FTIME-005 allocation, WTIME-009 duration-timesheet and WTIME-010
// exception-only concerns that sit on top of it. The package is
// kernel-pure: no database, no clock, no network, no package-level mutable
// state. Every clock or instant a function needs arrives as a parameter.
package timecard

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/attendance"
)

// ErrTimecardRejected is the WF-CAP-007 seeded-defect sentinel. Every
// refusal below is reachable through errors.Is against this sentinel.
var ErrTimecardRejected = errors.New("WF_CAP_007_REJECTED")

// Rejection is the stable WF-CAP-007 failure shape: which field, in which
// state, and why.
type Rejection struct {
	Field  string
	State  string
	Reason string
}

func (r *Rejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s: %s", ErrTimecardRejected, r.Field, r.State, r.Reason)
}

// Unwrap exposes the WF_CAP_007_REJECTED sentinel to errors.Is.
func (r *Rejection) Unwrap() error { return ErrTimecardRejected }

func reject(field, state, reason string) error {
	return &Rejection{Field: field, State: state, Reason: reason}
}

// State is the WF-CAP-007 timecard lifecycle. Reopening a locked or
// approved timecard is its own state, distinct from draft: a reopened
// timecard has a history a fresh draft does not.
type State string

const (
	Draft     State = "DRAFT"
	Submitted State = "SUBMITTED"
	Attested  State = "ATTESTED"
	Approved  State = "APPROVED"
	Locked    State = "LOCKED"
	Reopened  State = "REOPENED"
)

func (s State) Valid() bool {
	switch s {
	case Draft, Submitted, Attested, Approved, Locked, Reopened:
		return true
	default:
		return false
	}
}

// Party identifies which side of a timecard attestation speaks.
type Party string

const (
	PartyWorker     Party = "WORKER"
	PartySupervisor Party = "SUPERVISOR"
)

func (p Party) Valid() bool { return p == PartyWorker || p == PartySupervisor }

// LineKind is the closed vocabulary of a timecard line's evidence source.
type LineKind string

const (
	LineSession   LineKind = "SESSION"   // a paired IN/OUT punch interval
	LineDuration  LineKind = "DURATION"  // a reported hours-per-day-and-project entry
	LineException LineKind = "EXCEPTION" // an exception-only period deviation
)

func (k LineKind) Valid() bool {
	switch k {
	case LineSession, LineDuration, LineException:
		return true
	default:
		return false
	}
}

// Line is one worked-time entry contributing to the timecard total. Open is
// true when the line's supporting evidence is incomplete -- most commonly a
// missing OUT punch. An open line is excluded from the total by
// construction: it never becomes a reported zero-hour entry, and Validate
// refuses an open line that also carries minutes.
type Line struct {
	ID         string
	Kind       LineKind
	Minutes    int
	Open       bool
	SourceRefs []string
}

func (l Line) Validate() error {
	if strings.TrimSpace(l.ID) == "" {
		return reject("line.id", "MISSING", "line id is required")
	}
	if !l.Kind.Valid() {
		return reject("line.kind", "INVALID", "line kind is not declared")
	}
	if l.Minutes < 0 {
		return reject("line.minutes", "NEGATIVE", "line minutes cannot be negative")
	}
	if l.Open && l.Minutes != 0 {
		return reject("line.minutes", "OPEN_WITH_MINUTES", "an open line cannot report minutes")
	}
	if len(l.SourceRefs) == 0 {
		return reject("line.source_refs", "MISSING", "line requires source evidence")
	}
	return nil
}

// Attestation binds one party's sign-off to the exact content digest of the
// timecard lines at the moment of attestation. It is never deleted when the
// lines later change: bound reports whether it still matches the live
// digest, so an edit after attestation invalidates it without erasing the
// evidence of what was attested.
type Attestation struct {
	Party         Party
	By            string
	At            time.Time
	ContentDigest string
}

func (a Attestation) bound(digest string) bool {
	return a.Party.Valid() && strings.TrimSpace(a.By) != "" && !a.At.IsZero() && a.ContentDigest != "" && a.ContentDigest == digest
}

// Approval pins the reviewed revision, the applicable rules and the exact
// totals digest at approval time.
type Approval struct {
	ApproverRef      string
	RulesRef         attendance.VersionedRef
	ReviewedRevision uint64
	TotalsDigest     string
	At               time.Time
}

// ReopenReason is the typed, closed vocabulary for reopening an approved or
// locked timecard.
type ReopenReason string

const (
	ReopenWorkerDispute        ReopenReason = "WORKER_DISPUTE"
	ReopenSupervisorCorrection ReopenReason = "SUPERVISOR_CORRECTION"
	ReopenPayrollRecall        ReopenReason = "PAYROLL_RECALL"
)

func (r ReopenReason) Valid() bool {
	switch r {
	case ReopenWorkerDispute, ReopenSupervisorCorrection, ReopenPayrollRecall:
		return true
	default:
		return false
	}
}

// RejectionReason is the typed, closed vocabulary for rejecting a submitted
// or attested timecard back to draft.
type RejectionReason string

const (
	RejectMissingEvidence     RejectionReason = "MISSING_EVIDENCE"
	RejectPolicyViolation     RejectionReason = "POLICY_VIOLATION"
	RejectDuplicateSubmission RejectionReason = "DUPLICATE_SUBMISSION"
)

func (r RejectionReason) Valid() bool {
	switch r {
	case RejectMissingEvidence, RejectPolicyViolation, RejectDuplicateSubmission:
		return true
	default:
		return false
	}
}

// HistoryEntry records one lifecycle transition. The prior state is
// retained, never overwritten: correction and lifecycle history is
// append-only.
type HistoryEntry struct {
	From, To State
	At       time.Time
	By       string
	Reason   string
	Detail   string
}

// Timecard is the WF-CAP-007 aggregate: one worker, one assignment, one
// period, one revision at a time. Every command below returns a new value;
// none mutates its receiver's slices in place.
type Timecard struct {
	Tenant                 string
	WorkerID               string
	AssignmentID           string
	PeriodStart, PeriodEnd time.Time
	Revision               uint64
	State                  State
	Lines                  []Line
	WorkerAttestation      *Attestation
	SupervisorAttestation  *Attestation
	Approval               *Approval
	History                []HistoryEntry
}

// NewTimecard builds a fresh draft timecard for one worker's period.
func NewTimecard(tenant, workerID, assignmentID string, periodStart, periodEnd time.Time) (Timecard, error) {
	if strings.TrimSpace(tenant) == "" {
		return Timecard{}, reject("tenant", "MISSING", "tenant is required")
	}
	if strings.TrimSpace(workerID) == "" {
		return Timecard{}, reject("worker_id", "MISSING", "worker id is required")
	}
	if strings.TrimSpace(assignmentID) == "" {
		return Timecard{}, reject("assignment_id", "MISSING", "assignment id is required")
	}
	if periodStart.IsZero() || periodEnd.IsZero() || !periodEnd.After(periodStart) {
		return Timecard{}, reject("period", "INVALID", "period must be a non-empty interval")
	}
	return Timecard{
		Tenant: tenant, WorkerID: workerID, AssignmentID: assignmentID,
		PeriodStart: periodStart, PeriodEnd: periodEnd, Revision: 1, State: Draft,
	}, nil
}

// linesDigest is the content digest an attestation binds to: line identity,
// kind, minutes and openness, in a stable order.
func linesDigest(lines []Line) string {
	ordered := append([]Line(nil), lines...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	var b strings.Builder
	for _, l := range ordered {
		fmt.Fprintf(&b, "%s|%s|%d|%t;", l.ID, l.Kind, l.Minutes, l.Open)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// totalsDigest is what an approval pins: the exact total plus the content
// digest and revision that produced it.
func totalsDigest(tc Timecard) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%s", tc.Revision, tc.TotalMinutes(), linesDigest(tc.Lines))))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// TotalMinutes sums every closed line's minutes. An open line contributes
// nothing: its absence from the total is the whole point of Line.Open.
func (tc Timecard) TotalMinutes() int {
	total := 0
	for _, l := range tc.Lines {
		if l.Open {
			continue
		}
		total += l.Minutes
	}
	return total
}

// SetLines replaces the timecard's lines and advances its revision. It is
// legal any time before the timecard is approved or locked: a draft,
// submitted, attested or reopened timecard may still be edited. Once
// locked, lines change only by reopening first. Attestations are never
// explicitly cleared here -- they are bound to the content digest, so an
// edit after either party attested makes that party's attestation stale
// automatically, the next time anything checks Attestation.bound, without
// erasing the record of what was attested.
func SetLines(tc Timecard, lines []Line, now time.Time, by string) (Timecard, error) {
	switch tc.State {
	case Draft, Submitted, Attested, Reopened:
	default:
		return Timecard{}, reject("state", string(tc.State), "lines cannot be edited once approved or locked; reopen first")
	}
	if len(lines) == 0 {
		return Timecard{}, reject("lines", "MISSING", "at least one line is required")
	}
	seen := make(map[string]struct{}, len(lines))
	clean := make([]Line, 0, len(lines))
	for i, l := range lines {
		if err := l.Validate(); err != nil {
			return Timecard{}, fmt.Errorf("line %d: %w", i, err)
		}
		if _, dup := seen[l.ID]; dup {
			return Timecard{}, reject("lines.id", "DUPLICATE", "duplicate line id "+l.ID)
		}
		seen[l.ID] = struct{}{}
		clean = append(clean, l)
	}
	if strings.TrimSpace(by) == "" || now.IsZero() {
		return Timecard{}, reject("edit", "MISSING", "editor and edit time are required")
	}
	out := tc
	out.Lines = clean
	out.Revision = tc.Revision + 1
	out.History = append(append([]HistoryEntry(nil), tc.History...), HistoryEntry{From: tc.State, To: tc.State, At: now, By: by, Reason: "LINES_EDITED"})
	return out, nil
}

// Submit moves a draft or reopened timecard into review.
func Submit(tc Timecard, now time.Time, by string) (Timecard, error) {
	if tc.State != Draft && tc.State != Reopened {
		return Timecard{}, reject("state", string(tc.State), "only a draft or reopened timecard can be submitted")
	}
	if len(tc.Lines) == 0 {
		return Timecard{}, reject("lines", "MISSING", "a timecard needs at least one line to submit")
	}
	if strings.TrimSpace(by) == "" || now.IsZero() {
		return Timecard{}, reject("submit", "MISSING", "submitter and submission time are required")
	}
	out := tc
	out.State = Submitted
	out.History = append(append([]HistoryEntry(nil), tc.History...), HistoryEntry{From: tc.State, To: Submitted, At: now, By: by})
	return out, nil
}

// Attest records one party's sign-off bound to the current content digest.
// The timecard becomes ATTESTED only once both parties are bound to the
// same digest; a re-attestation after an edit simply rebinds that party.
func Attest(tc Timecard, party Party, by string, now time.Time) (Timecard, error) {
	if tc.State != Submitted && tc.State != Attested {
		return Timecard{}, reject("state", string(tc.State), "attestation requires a submitted or attested timecard")
	}
	if !party.Valid() {
		return Timecard{}, reject("party", "INVALID", "attesting party is not declared")
	}
	if strings.TrimSpace(by) == "" || now.IsZero() {
		return Timecard{}, reject("attest", "MISSING", "attesting party and attestation time are required")
	}
	digest := linesDigest(tc.Lines)
	att := &Attestation{Party: party, By: by, At: now, ContentDigest: digest}
	out := tc
	switch party {
	case PartyWorker:
		out.WorkerAttestation = att
	case PartySupervisor:
		out.SupervisorAttestation = att
	}
	from := tc.State
	if out.WorkerAttestation != nil && out.WorkerAttestation.bound(digest) &&
		out.SupervisorAttestation != nil && out.SupervisorAttestation.bound(digest) {
		out.State = Attested
	} else {
		out.State = Submitted
	}
	out.History = append(append([]HistoryEntry(nil), tc.History...), HistoryEntry{From: from, To: out.State, At: now, By: by, Reason: "ATTESTED_" + string(party)})
	return out, nil
}

// Approve pins the reviewed revision, the applicable rules and the exact
// totals digest. It requires both attestations still bound to the live
// content, the caller's declared revision to match, and no unresolved
// exception or open line -- an approval can never wave away an unresolved
// break or overlap exception.
func Approve(tc Timecard, approverRef string, rulesRef attendance.VersionedRef, reviewedRevision uint64, openExceptions []attendance.Finding, now time.Time) (Timecard, error) {
	if tc.State != Attested {
		return Timecard{}, reject("state", string(tc.State), "approval requires an attested timecard")
	}
	digest := linesDigest(tc.Lines)
	if tc.WorkerAttestation == nil || !tc.WorkerAttestation.bound(digest) {
		return Timecard{}, reject("worker_attestation", string(tc.State), "worker attestation is stale or missing")
	}
	if tc.SupervisorAttestation == nil || !tc.SupervisorAttestation.bound(digest) {
		return Timecard{}, reject("supervisor_attestation", string(tc.State), "supervisor attestation is stale or missing")
	}
	if strings.TrimSpace(approverRef) == "" || now.IsZero() {
		return Timecard{}, reject("approve", "MISSING", "approver and approval time are required")
	}
	if err := rulesRef.Validate("rules"); err != nil {
		return Timecard{}, fmt.Errorf("%w: %v", ErrTimecardRejected, err)
	}
	if reviewedRevision != tc.Revision {
		return Timecard{}, reject("revision", "MISMATCH", "approval revision does not match the current timecard revision")
	}
	for _, l := range tc.Lines {
		if l.Open {
			return Timecard{}, reject("lines", "OPEN", "open line "+l.ID+" blocks approval")
		}
	}
	if len(openExceptions) > 0 {
		kinds := make([]string, 0, len(openExceptions))
		for _, e := range openExceptions {
			kinds = append(kinds, string(e.Kind))
		}
		return Timecard{}, reject("exceptions", "UNRESOLVED", "unresolved exceptions block approval: "+strings.Join(kinds, ","))
	}
	out := tc
	out.Approval = &Approval{ApproverRef: approverRef, RulesRef: rulesRef, ReviewedRevision: reviewedRevision, TotalsDigest: totalsDigest(tc), At: now}
	out.State = Approved
	out.History = append(append([]HistoryEntry(nil), tc.History...), HistoryEntry{From: tc.State, To: Approved, At: now, By: approverRef})
	return out, nil
}

// Lock finalizes an approved timecard. It re-verifies the pinned totals
// digest against the live lines, so a lock can never finalize a timecard
// that changed since it was approved.
func Lock(tc Timecard, now time.Time, by string) (Timecard, error) {
	if tc.State != Approved {
		return Timecard{}, reject("state", string(tc.State), "lock requires an approved timecard")
	}
	if tc.Approval == nil || tc.Approval.TotalsDigest != totalsDigest(tc) {
		return Timecard{}, reject("approval", "STALE", "approved totals no longer match the live lines")
	}
	if strings.TrimSpace(by) == "" || now.IsZero() {
		return Timecard{}, reject("lock", "MISSING", "locking party and lock time are required")
	}
	out := tc
	out.State = Locked
	out.History = append(append([]HistoryEntry(nil), tc.History...), HistoryEntry{From: tc.State, To: Locked, At: now, By: by})
	return out, nil
}

// Reopen returns an approved or locked timecard to REOPENED for a typed
// reason. The prior approval is preserved in history, not deleted.
func Reopen(tc Timecard, reason ReopenReason, by string, now time.Time) (Timecard, error) {
	if tc.State != Approved && tc.State != Locked {
		return Timecard{}, reject("state", string(tc.State), "only an approved or locked timecard can be reopened")
	}
	if !reason.Valid() {
		return Timecard{}, reject("reason", "INVALID", "reopen reason is not declared")
	}
	if strings.TrimSpace(by) == "" || now.IsZero() {
		return Timecard{}, reject("reopen", "MISSING", "reopening party and time are required")
	}
	detail := ""
	if tc.Approval != nil {
		detail = "prior_approval_digest=" + tc.Approval.TotalsDigest
	}
	out := tc
	out.State = Reopened
	out.History = append(append([]HistoryEntry(nil), tc.History...), HistoryEntry{From: tc.State, To: Reopened, At: now, By: by, Reason: string(reason), Detail: detail})
	return out, nil
}

// Reject returns a submitted or attested timecard to draft for a typed
// reason, so the worker can revise and resubmit it.
func Reject(tc Timecard, reason RejectionReason, by string, now time.Time) (Timecard, error) {
	if tc.State != Submitted && tc.State != Attested {
		return Timecard{}, reject("state", string(tc.State), "only a submitted or attested timecard can be rejected")
	}
	if !reason.Valid() {
		return Timecard{}, reject("reason", "INVALID", "rejection reason is not declared")
	}
	if strings.TrimSpace(by) == "" || now.IsZero() {
		return Timecard{}, reject("reject", "MISSING", "rejecting party and time are required")
	}
	out := tc
	out.State = Draft
	out.History = append(append([]HistoryEntry(nil), tc.History...), HistoryEntry{From: tc.State, To: Draft, At: now, By: by, Reason: string(reason)})
	return out, nil
}
