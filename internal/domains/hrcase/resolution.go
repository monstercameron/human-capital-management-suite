// CASE-003: case assignment, SLA, evidence review, finding and disposition.
//
// Work owns claims, leases and tasks; this package owns accountable
// assignment history, findings and disposition. Assignment binds an explicit
// eligibility roster with recusal and delegation. The SLA records its
// calendar, version, timezone and pause reasons — never ambient local time.
// A finding binds the exact reviewed evidence digests and becomes immutable
// once approved. Close is a compare-and-swap over the disposition sequence
// and succeeds once only, when every mandatory obligation is satisfied.
// Clocks are injected by the caller; this file reads no wall clock.
package hrcase

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// ErrResolutionInvalid marks a malformed resolution input.
	ErrResolutionInvalid = errors.New("hrcase: invalid case resolution input")
	// ErrAssignmentIneligible is the single non-disclosing refusal for
	// unknown, recused and wrong-role assignees alike.
	ErrAssignmentIneligible = errors.New("hrcase: assignee is not eligible for this case")
	// ErrSLAInvalid marks an SLA without explicit calendar, version,
	// timezone or interval.
	ErrSLAInvalid = errors.New("hrcase: invalid case SLA")
	// ErrEvidenceUnreviewed marks a finding that cites evidence with no
	// approved review.
	ErrEvidenceUnreviewed = errors.New("hrcase: finding cites unreviewed evidence")
	// ErrFindingImmutable marks a finding change after approval.
	ErrFindingImmutable = errors.New("hrcase: approved finding cannot be changed")
	// ErrObligationOpen marks a close attempt with mandatory obligations
	// still unsatisfied.
	ErrObligationOpen = errors.New("hrcase: mandatory obligations remain open")
	// ErrStaleClose marks a close with a stale sequence or a repeated close
	// after the single disposition.
	ErrStaleClose = errors.New("hrcase: stale case close")
)

// EligibleAssignee is one principal the case may be assigned to.
type EligibleAssignee struct {
	Principal string
	Role      string
}

// Assignment is one accountable assignment event. Reviews live on the
// resolution, so reassignment never drops evidence review history.
type Assignment struct {
	CaseID        string
	Assignee      string
	Role          string
	DelegatedFrom string
	At            time.Time
	Seq           uint64
}

// SLAPause records one SLA pause interval with its reason.
type SLAPause struct {
	Reason string
	From   time.Time
	To     time.Time
}

// CaseSLA binds the deadline to an explicit calendar version and timezone.
type CaseSLA struct {
	CalendarID      string
	CalendarVersion string
	Timezone        string
	OpenedAt        time.Time
	DueAt           time.Time
	Pauses          []SLAPause
}

// EvidenceReview binds one evidence artifact to its reviewer decision.
type EvidenceReview struct {
	EvidenceID     string
	ArtifactDigest string
	Reviewer       string
	Approved       bool
	At             time.Time
}

// CaseFinding binds the exact reviewed evidence behind a rationale.
// ApprovalRef is empty until ApproveFinding seals it.
type CaseFinding struct {
	ID              string
	EvidenceDigests []string
	Rationale       string
	ApprovalRef     string
}

// Obligation is one required task gating close when Mandatory.
type Obligation struct {
	ID        string
	Kind      string
	Mandatory bool
	Satisfied bool
}

// CaseDisposition is the single close outcome. Seq is the close-CAS
// sequence: the first close carries Seq 1.
type CaseDisposition struct {
	Outcome     string
	FindingID   string
	ApprovalRef string
	ClosedAt    time.Time
	Seq         uint64
}

// Resolution is the accountable progress state of one case. It is safe for
// concurrent use; Close is a compare-and-swap over the disposition sequence.
type Resolution struct {
	mu          sync.Mutex
	caseID      string
	eligible    map[string]string
	recused     map[string]bool
	assignments []Assignment
	reviews     map[string]EvidenceReview
	finding     CaseFinding
	findingSet  bool
	obligations map[string]Obligation
	sla         CaseSLA
	slaSet      bool
	closeSeq    uint64
	disposition CaseDisposition
	closed      bool
}

// NewResolution opens resolution tracking for caseID over an explicit
// eligibility roster.
func NewResolution(caseID string, eligible []EligibleAssignee) (*Resolution, error) {
	if strings.TrimSpace(caseID) == "" {
		return nil, fmt.Errorf("%w: case id is required", ErrResolutionInvalid)
	}
	if len(eligible) == 0 {
		return nil, fmt.Errorf("%w: eligibility roster is required", ErrResolutionInvalid)
	}
	roster := make(map[string]string, len(eligible))
	for i, e := range eligible {
		if strings.TrimSpace(e.Principal) == "" || strings.TrimSpace(e.Role) == "" {
			return nil, fmt.Errorf("%w: eligible[%d] is incomplete", ErrResolutionInvalid, i)
		}
		if _, dup := roster[e.Principal]; dup {
			return nil, fmt.Errorf("%w: duplicate eligible principal", ErrResolutionInvalid)
		}
		roster[e.Principal] = e.Role
	}
	return &Resolution{caseID: caseID, eligible: roster, recused: map[string]bool{}, reviews: map[string]EvidenceReview{}, obligations: map[string]Obligation{}}, nil
}

// Recuse declares a conflict: the principal can no longer be assigned.
func (r *Resolution) Recuse(principal string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recused[principal] = true
}

func (r *Resolution) assignLocked(principal string, delegatedFrom string, at time.Time) (Assignment, error) {
	if strings.TrimSpace(principal) == "" || at.IsZero() {
		return Assignment{}, fmt.Errorf("%w: assignee and time are required", ErrResolutionInvalid)
	}
	role, ok := r.eligible[principal]
	if !ok || r.recused[principal] {
		return Assignment{}, ErrAssignmentIneligible
	}
	if delegatedFrom != "" {
		latest := ""
		if len(r.assignments) > 0 {
			latest = r.assignments[len(r.assignments)-1].Assignee
		}
		if delegatedFrom != latest {
			return Assignment{}, ErrAssignmentIneligible
		}
		if _, ok := r.eligible[delegatedFrom]; !ok || r.recused[delegatedFrom] {
			return Assignment{}, ErrAssignmentIneligible
		}
	}
	a := Assignment{CaseID: r.caseID, Assignee: principal, Role: role, DelegatedFrom: delegatedFrom, At: at, Seq: uint64(len(r.assignments) + 1)}
	r.assignments = append(r.assignments, a)
	return a, nil
}

// Assign binds the next accountable assignee. Reviews stay on the
// resolution, so reassignment preserves them.
func (r *Resolution) Assign(principal string, at time.Time) (Assignment, error) {
	if r == nil {
		return Assignment{}, fmt.Errorf("%w: resolution is nil", ErrResolutionInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.assignLocked(principal, "", at)
}

// Delegate transfers accountability from the current assignee to an
// eligible, unrecused principal, recording the chain.
func (r *Resolution) Delegate(from, to string, at time.Time) (Assignment, error) {
	if r == nil {
		return Assignment{}, fmt.Errorf("%w: resolution is nil", ErrResolutionInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.assignLocked(to, from, at)
}

// Assignments returns the accountable history in order.
func (r *Resolution) Assignments() []Assignment {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Assignment(nil), r.assignments...)
}

// SetSLA binds the deadline to an explicit calendar version and timezone.
// An ambient SLA without calendar or timezone is refused.
func (r *Resolution) SetSLA(sla CaseSLA) error {
	if r == nil {
		return fmt.Errorf("%w: resolution is nil", ErrResolutionInvalid)
	}
	if strings.TrimSpace(sla.CalendarID) == "" || strings.TrimSpace(sla.CalendarVersion) == "" || strings.TrimSpace(sla.Timezone) == "" {
		return fmt.Errorf("%w: calendar, version and timezone are required", ErrSLAInvalid)
	}
	if sla.OpenedAt.IsZero() || sla.DueAt.IsZero() || !sla.DueAt.After(sla.OpenedAt) {
		return fmt.Errorf("%w: SLA requires an ordered open/due interval", ErrSLAInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	sla.Pauses = append([]SLAPause(nil), sla.Pauses...)
	r.sla = sla
	r.slaSet = true
	return nil
}

// SLA returns the bound SLA, if any.
func (r *Resolution) SLA() (CaseSLA, bool) {
	if r == nil {
		return CaseSLA{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.slaSet {
		return CaseSLA{}, false
	}
	out := r.sla
	out.Pauses = append([]SLAPause(nil), r.sla.Pauses...)
	return out, true
}

// PauseSLA records a pause reason and extends the due date by the paused
// interval. Times are caller-supplied; nothing reads a wall clock.
func (r *Resolution) PauseSLA(reason string, from, to time.Time) error {
	if r == nil {
		return fmt.Errorf("%w: resolution is nil", ErrResolutionInvalid)
	}
	if strings.TrimSpace(reason) == "" || from.IsZero() || to.IsZero() || !to.After(from) {
		return fmt.Errorf("%w: pause requires a reason and an ordered interval", ErrSLAInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.slaSet {
		return fmt.Errorf("%w: no SLA is bound", ErrSLAInvalid)
	}
	r.sla.Pauses = append(r.sla.Pauses, SLAPause{Reason: reason, From: from, To: to})
	r.sla.DueAt = r.sla.DueAt.Add(to.Sub(from))
	return nil
}

// RecordReview binds one evidence review. Unapproved reviews are retained
// so a finding cannot silently cite evidence that was never approved.
func (r *Resolution) RecordReview(rev EvidenceReview) error {
	if r == nil {
		return fmt.Errorf("%w: resolution is nil", ErrResolutionInvalid)
	}
	if strings.TrimSpace(rev.EvidenceID) == "" || strings.TrimSpace(rev.ArtifactDigest) == "" || strings.TrimSpace(rev.Reviewer) == "" || rev.At.IsZero() {
		return fmt.Errorf("%w: evidence, digest, reviewer and time are required", ErrResolutionInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reviews[rev.EvidenceID] = rev
	return nil
}

// Reviews returns recorded reviews ordered by evidence ID. Reassignment
// never alters this set.
func (r *Resolution) Reviews() []EvidenceReview {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.reviews))
	for id := range r.reviews {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]EvidenceReview, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.reviews[id])
	}
	return out
}

// SetFinding binds a finding to exact evidence digests. Every cited digest
// must carry an approved review, and an approved finding is immutable.
func (r *Resolution) SetFinding(f CaseFinding) error {
	if r == nil {
		return fmt.Errorf("%w: resolution is nil", ErrResolutionInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.findingSet && r.finding.ApprovalRef != "" {
		return ErrFindingImmutable
	}
	if strings.TrimSpace(f.ID) == "" || strings.TrimSpace(f.Rationale) == "" || len(f.EvidenceDigests) == 0 {
		return fmt.Errorf("%w: finding id, rationale and evidence are required", ErrResolutionInvalid)
	}
	approved := map[string]bool{}
	for _, rev := range r.reviews {
		if rev.Approved {
			approved[rev.ArtifactDigest] = true
		}
	}
	for _, d := range f.EvidenceDigests {
		if !approved[d] {
			return fmt.Errorf("%w: %s", ErrEvidenceUnreviewed, d)
		}
	}
	f.EvidenceDigests = append([]string(nil), f.EvidenceDigests...)
	if r.findingSet {
		f.ApprovalRef = r.finding.ApprovalRef
	}
	r.finding = f
	r.findingSet = true
	return nil
}

// ApproveFinding seals the finding with its approval reference.
func (r *Resolution) ApproveFinding(approvalRef string) error {
	if r == nil {
		return fmt.Errorf("%w: resolution is nil", ErrResolutionInvalid)
	}
	if strings.TrimSpace(approvalRef) == "" {
		return fmt.Errorf("%w: approval reference is required", ErrResolutionInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.findingSet {
		return fmt.Errorf("%w: no finding is bound", ErrResolutionInvalid)
	}
	if r.finding.ApprovalRef != "" {
		return ErrFindingImmutable
	}
	r.finding.ApprovalRef = approvalRef
	return nil
}

// AddObligation registers one task gating close when mandatory.
func (r *Resolution) AddObligation(o Obligation) error {
	if r == nil {
		return fmt.Errorf("%w: resolution is nil", ErrResolutionInvalid)
	}
	if strings.TrimSpace(o.ID) == "" || strings.TrimSpace(o.Kind) == "" {
		return fmt.Errorf("%w: obligation id and kind are required", ErrResolutionInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.obligations[o.ID]; dup {
		return fmt.Errorf("%w: duplicate obligation", ErrResolutionInvalid)
	}
	r.obligations[o.ID] = o
	return nil
}

// SatisfyObligation marks one obligation complete.
func (r *Resolution) SatisfyObligation(id string) error {
	if r == nil {
		return fmt.Errorf("%w: resolution is nil", ErrResolutionInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.obligations[id]
	if !ok {
		return fmt.Errorf("%w: unknown obligation", ErrResolutionInvalid)
	}
	o.Satisfied = true
	r.obligations[id] = o
	return nil
}

// Close seals the case with its disposition. It is a compare-and-swap over
// the disposition sequence: expectedSeq must equal the current sequence,
// and after the single disposition every further close is stale. Close
// requires a sealed finding and zero open mandatory obligations.
func (r *Resolution) Close(outcome, approvalRef string, expectedSeq uint64, at time.Time) (CaseDisposition, error) {
	if r == nil {
		return CaseDisposition{}, fmt.Errorf("%w: resolution is nil", ErrResolutionInvalid)
	}
	if strings.TrimSpace(outcome) == "" || strings.TrimSpace(approvalRef) == "" || at.IsZero() {
		return CaseDisposition{}, fmt.Errorf("%w: outcome, approval and time are required", ErrResolutionInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || expectedSeq != r.closeSeq {
		return CaseDisposition{}, ErrStaleClose
	}
	if !r.findingSet || r.finding.ApprovalRef == "" {
		return CaseDisposition{}, fmt.Errorf("%w: no approved finding is bound", ErrResolutionInvalid)
	}
	for _, o := range r.obligations {
		if o.Mandatory && !o.Satisfied {
			return CaseDisposition{}, fmt.Errorf("%w: %s", ErrObligationOpen, o.ID)
		}
	}
	r.closeSeq++
	r.disposition = CaseDisposition{Outcome: outcome, FindingID: r.finding.ID, ApprovalRef: approvalRef, ClosedAt: at, Seq: r.closeSeq}
	r.closed = true
	return r.disposition, nil
}

// CanonicalDigest binds the full accountable state: roster, recusals,
// assignment history, SLA, reviews, finding, obligations and disposition.
func (r *Resolution) CanonicalDigest() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	b.WriteString(r.caseID + "\x1f")
	principals := make([]string, 0, len(r.eligible))
	for p := range r.eligible {
		principals = append(principals, p)
	}
	sort.Strings(principals)
	for _, p := range principals {
		fmt.Fprintf(&b, "eligible:%s:%s\x1f", p, r.eligible[p])
	}
	recused := make([]string, 0)
	for p, v := range r.recused {
		if v {
			recused = append(recused, p)
		}
	}
	sort.Strings(recused)
	for _, p := range recused {
		fmt.Fprintf(&b, "recused:%s\x1f", p)
	}
	for _, a := range r.assignments {
		fmt.Fprintf(&b, "assign:%d:%s:%s:%s\x1f", a.Seq, a.Assignee, a.Role, a.DelegatedFrom)
	}
	if r.slaSet {
		fmt.Fprintf(&b, "sla:%s:%s:%s:%s:%s\x1f", r.sla.CalendarID, r.sla.CalendarVersion, r.sla.Timezone, r.sla.OpenedAt.UTC().Format(time.RFC3339), r.sla.DueAt.UTC().Format(time.RFC3339))
		for _, p := range r.sla.Pauses {
			fmt.Fprintf(&b, "pause:%s:%s:%s\x1f", p.Reason, p.From.UTC().Format(time.RFC3339), p.To.UTC().Format(time.RFC3339))
		}
	}
	ids := make([]string, 0, len(r.reviews))
	for id := range r.reviews {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		rev := r.reviews[id]
		fmt.Fprintf(&b, "review:%s:%s:%s:%t\x1f", rev.EvidenceID, rev.ArtifactDigest, rev.Reviewer, rev.Approved)
	}
	if r.findingSet {
		fmt.Fprintf(&b, "finding:%s:%s:%s:%s\x1f", r.finding.ID, strings.Join(r.finding.EvidenceDigests, ","), r.finding.Rationale, r.finding.ApprovalRef)
	}
	obligations := make([]string, 0, len(r.obligations))
	for id := range r.obligations {
		obligations = append(obligations, id)
	}
	sort.Strings(obligations)
	for _, id := range obligations {
		o := r.obligations[id]
		fmt.Fprintf(&b, "obligation:%s:%s:%t:%t\x1f", o.ID, o.Kind, o.Mandatory, o.Satisfied)
	}
	if r.closed {
		fmt.Fprintf(&b, "disposition:%s:%s:%s:%d\x1f", r.disposition.Outcome, r.disposition.FindingID, r.disposition.ApprovalRef, r.disposition.Seq)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("sha256:%x", sum)
}
