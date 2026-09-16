package operator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ReviewWindow is how long a bypass may stay unreviewed before its authority
// family is suspended. It is deliberately one working day: long enough that a
// genuine emergency is not punished for happening overnight, short enough that
// "we will look at it later" is not a durable state.
const ReviewWindow = 24 * time.Hour

// What a receipt declares it bypassed. A break-glass action always bypasses
// the JIT authority path; whether it also bypassed dual control or simulation
// depends on what its kind's policy demanded.
const (
	BypassJITAuthority = "JIT_AUTHORITY"
	BypassDualControl  = "DUAL_CONTROL"
	BypassSimulation   = "SIMULATION"
)

// ObligationOutcome is the closed vocabulary of a post-use review.
type ObligationOutcome string

// Review outcomes.
const (
	ObligationJustified ObligationOutcome = "JUSTIFIED"
	ObligationViolation ObligationOutcome = "VIOLATION"
)

// ObligationStatus is where an obligation stands at one instant.
type ObligationStatus string

// Obligation statuses.
const (
	ObligationOpen       ObligationStatus = "OPEN"
	ObligationOverdue    ObligationStatus = "OVERDUE"
	ObligationDischarged ObligationStatus = "DISCHARGED"
)

// Additional refusal codes for obligations and repair separation.
const (
	// CodeObligationOverdue refuses a new action in a family that holds an
	// obligation past its due review.
	CodeObligationOverdue = "OPERATOR_OBLIGATION_OVERDUE"
	// CodeObligationRequired refuses a bypass the gateway cannot make
	// accountable because no obligation store is wired.
	CodeObligationRequired = "OPERATOR_OBLIGATION_STORE_REQUIRED"
	// CodeObligationFailed reports an obligation the store refused to record
	// or discharge.
	CodeObligationFailed = "OPERATOR_OBLIGATION_FAILED"
	// CodeObligationUnknown refuses a review of an obligation that is not
	// outstanding.
	CodeObligationUnknown = "OPERATOR_OBLIGATION_UNKNOWN"
	// CodeObligationReviewer refuses a review by the operator who incurred the
	// obligation or by the approver who authorized the bypass.
	CodeObligationReviewer = "OPERATOR_OBLIGATION_REVIEWER_INVALID"
	// CodeRepairSeparation refuses a repair-family action to an operator who
	// is an approver of record over the same scope.
	CodeRepairSeparation = "OPERATOR_REPAIR_SEPARATION"
)

// ObligationReview is the post-use review that discharges one obligation.
type ObligationReview struct {
	Reviewer string            `json:"reviewer"`
	Outcome  ObligationOutcome `json:"outcome"`
	Note     string            `json:"note"`
	At       time.Time         `json:"at"`
}

func (r ObligationReview) validate(o Obligation) error {
	reviewer := strings.TrimSpace(r.Reviewer)
	switch {
	case reviewer == "" || strings.TrimSpace(r.Note) == "" || r.At.IsZero():
		return refuse(CodeInvalidRequest, o.Kind, "a review needs a reviewer, a note and an instant")
	case r.Outcome != ObligationJustified && r.Outcome != ObligationViolation:
		return refuse(CodeInvalidRequest, o.Kind, "review outcome %q is not JUSTIFIED or VIOLATION", r.Outcome)
	case strings.EqualFold(reviewer, o.Operator):
		return refuse(CodeObligationReviewer, o.Kind, "the operator who incurred the bypass cannot review it")
	case strings.EqualFold(reviewer, o.Approver):
		return refuse(CodeObligationReviewer, o.Kind, "the approver who authorized the bypass cannot review it")
	}
	return nil
}

// Obligation is the accountable debt one bypass leaves behind: who acted,
// under whose approval, over exactly which scope, what was bypassed and why,
// and by when a distinct person must review it. It is recorded durably in the
// same journal as the receipt it belongs to, so a bypass taken before a
// restart is still outstanding after one.
type Obligation struct {
	ID             string          `json:"id"`
	Tenant         values.TenantId `json:"tenant"`
	Kind           Kind            `json:"kind"`
	Family         Family          `json:"family"`
	Scope          Scope           `json:"scope"`
	Operator       string          `json:"operator"`
	Approver       string          `json:"approver"`
	AuthorityKind  string          `json:"authority_kind"`
	AuthorityRef   string          `json:"authority_ref"`
	IdempotencyKey string          `json:"idempotency_key"`
	// RequestDigest pins the exact action the bypass was taken for. It is the
	// receipt's request digest, which is stable from the pending receipt to
	// the final one, so the obligation and its receipt stay joined whatever
	// outcome the effect reached.
	RequestDigest string            `json:"request_digest"`
	TicketRef     string            `json:"ticket_ref"`
	Bypassed      []string          `json:"bypassed"`
	BypassReason  string            `json:"bypass_reason"`
	RecordedAt    time.Time         `json:"recorded_at"`
	DueAt         time.Time         `json:"due_at"`
	Review        *ObligationReview `json:"review,omitempty"`
	Digest        string            `json:"digest"`
}

// ObligationID derives the obligation identity of one tenant-scoped
// idempotency key, so re-recording the same bypass is the same obligation.
func ObligationID(tenant values.TenantId, key string) string {
	sum := sha256.Sum256([]byte(string(tenant) + "\x1f" + key))
	return "obligation:operator:" + hex.EncodeToString(sum[:16])
}

func (o Obligation) sealed() Obligation {
	o.Digest = ""
	body, _ := json.Marshal(o)
	sum := sha256.Sum256(body)
	o.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return o
}

// Sealed returns o with its content digest recomputed.
func (o Obligation) Sealed() Obligation { return o.sealed() }

// Verify reports whether the obligation's digest matches its content.
func (o Obligation) Verify() error {
	if o.Digest == "" || o.sealed().Digest != o.Digest {
		return refuse(CodeObligationFailed, o.Kind, "obligation digest does not match its content")
	}
	return nil
}

// Status reports where the obligation stands at now.
func (o Obligation) Status(now time.Time) ObligationStatus {
	switch {
	case o.Review != nil:
		return ObligationDischarged
	case o.DueAt.IsZero() || now.UTC().Before(o.DueAt):
		return ObligationOpen
	default:
		return ObligationOverdue
	}
}

// Overdue reports whether the obligation is past its due review and still
// undischarged.
func (o Obligation) Overdue(now time.Time) bool { return o.Status(now) == ObligationOverdue }

// Discharged returns o with review recorded and its digest resealed, or the
// typed refusal when the review is malformed or the reviewer is the operator
// who incurred the obligation or the approver who authorized the bypass. The
// reviewer rules live here so every store applies exactly the same ones.
func (o Obligation) Discharged(review ObligationReview) (Obligation, error) {
	if o.Review != nil {
		return Obligation{}, refuse(CodeObligationUnknown, o.Kind, "obligation %s is already discharged", o.ID)
	}
	if err := review.validate(o); err != nil {
		return Obligation{}, err
	}
	review.Reviewer = strings.TrimSpace(review.Reviewer)
	review.At = review.At.UTC()
	o.Review = &review
	return o.sealed(), nil
}

func (o Obligation) validate() error {
	switch {
	case strings.TrimSpace(o.ID) == "":
		return refuse(CodeInvalidRequest, o.Kind, "obligation id is required")
	case o.Tenant.Validate() != nil:
		return refuse(CodeInvalidRequest, o.Kind, "obligation tenant is required")
	case strings.TrimSpace(o.IdempotencyKey) == "":
		return refuse(CodeInvalidRequest, o.Kind, "obligation idempotency key is required")
	case strings.TrimSpace(o.Operator) == "" || strings.TrimSpace(o.Approver) == "":
		return refuse(CodeInvalidRequest, o.Kind, "an obligation names both the operator and the approver of record")
	case len(o.Bypassed) == 0 || strings.TrimSpace(o.BypassReason) == "":
		return refuse(CodeInvalidRequest, o.Kind, "an obligation names what was bypassed and why")
	case o.RecordedAt.IsZero() || !o.DueAt.After(o.RecordedAt):
		return refuse(CodeInvalidRequest, o.Kind, "an obligation is due strictly after it is recorded")
	}
	return o.Verify()
}

// ObligationStore durably records bypass obligations and their reviews. The
// operator journal implements it (REFACTOR: obligations reuse the operator
// gateway journal), so a composition wires one store, not two.
type ObligationStore interface {
	// RecordObligation records o. Recording the same obligation id twice is
	// the same obligation and must not fail.
	RecordObligation(ctx context.Context, o Obligation) error
	// OutstandingObligations returns every undischarged obligation of tenant.
	OutstandingObligations(ctx context.Context, tenant values.TenantId) ([]Obligation, error)
	// DischargeObligation records review against the outstanding obligation
	// id and returns the discharged obligation.
	DischargeObligation(ctx context.Context, tenant values.TenantId, id string, review ObligationReview) (Obligation, error)
}

// ObligationFor derives the obligation a bypassing receipt owes. approver is
// the approver of record on the authority that admitted the bypass.
func ObligationFor(r Receipt, approver string, now time.Time) Obligation {
	o := Obligation{
		ID: ObligationID(r.Tenant, r.IdempotencyKey), Tenant: r.Tenant, Kind: r.Kind,
		Family: r.Kind.Family(), Scope: r.Scope.normalized(), Operator: r.Operator,
		Approver: strings.TrimSpace(approver), AuthorityKind: r.AuthorityKind, AuthorityRef: r.AuthorityRef,
		IdempotencyKey: r.IdempotencyKey, RequestDigest: r.RequestDigest, TicketRef: r.TicketRef,
		Bypassed: slices.Clone(r.Bypassed), BypassReason: r.BypassReason,
		RecordedAt: now.UTC(), DueAt: now.UTC().Add(ReviewWindow),
	}
	return o.sealed()
}

// SuspendedFamilies returns the families the outstanding obligations suspend
// at now, and the obligation that suspends each.
func SuspendedFamilies(outstanding []Obligation, now time.Time) map[Family]Obligation {
	suspended := map[Family]Obligation{}
	for _, o := range outstanding {
		if !o.Overdue(now) {
			continue
		}
		if prior, ok := suspended[o.Family]; ok && !o.DueAt.Before(prior.DueAt) {
			continue
		}
		suspended[o.Family] = o
	}
	return suspended
}

// ApproverOverScope reports whether principal is an approver of record on an
// outstanding obligation whose scope overlaps scope. This is the durable half
// of the repair separation rule: the in-request half (a second approver
// distinct from the operator) only sees one submission, so without this an
// approver could simply come back later as the repair operator.
func ApproverOverScope(outstanding []Obligation, principal string, scope Scope) (Obligation, bool) {
	principal = strings.TrimSpace(principal)
	if principal == "" {
		return Obligation{}, false
	}
	want := scope.normalized()
	for _, o := range outstanding {
		if o.Review != nil || !strings.EqualFold(o.Approver, principal) {
			continue
		}
		if o.Scope.Resource != want.Resource {
			continue
		}
		for _, id := range o.Scope.normalized().IDs {
			if slices.Contains(want.IDs, id) {
				return o, true
			}
		}
	}
	return Obligation{}, false
}

// memoryObligations is the in-process obligation storage [MemoryJournal]
// embeds. A journal recomposed over the same value sees every obligation the
// previous one recorded, which is what a restart over durable storage looks
// like.
type memoryObligations struct {
	mu          sync.Mutex
	obligations map[string]Obligation
}

func (m *memoryObligations) record(o Obligation) error {
	if err := o.validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.obligations == nil {
		m.obligations = map[string]Obligation{}
	}
	k := journalKey(o.Tenant, o.ID)
	if _, ok := m.obligations[k]; ok {
		return nil
	}
	m.obligations[k] = o
	return nil
}

func (m *memoryObligations) outstanding(tenant values.TenantId) []Obligation {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Obligation, 0, len(m.obligations))
	for _, o := range m.obligations {
		if o.Tenant == tenant && o.Review == nil {
			out = append(out, o)
		}
	}
	slices.SortFunc(out, func(a, b Obligation) int { return a.DueAt.Compare(b.DueAt) })
	return out
}

func (m *memoryObligations) discharge(tenant values.TenantId, id string, review ObligationReview) (Obligation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.obligations[journalKey(tenant, id)]
	if !ok {
		return Obligation{}, refuse(CodeObligationUnknown, "", "no outstanding obligation %s", id)
	}
	discharged, err := o.Discharged(review)
	if err != nil {
		return Obligation{}, err
	}
	m.obligations[journalKey(tenant, id)] = discharged
	return discharged, nil
}

// RecordObligation implements ObligationStore.
func (j *MemoryJournal) RecordObligation(_ context.Context, o Obligation) error {
	return j.obligations.record(o)
}

// OutstandingObligations implements ObligationStore.
func (j *MemoryJournal) OutstandingObligations(_ context.Context, tenant values.TenantId) ([]Obligation, error) {
	return j.obligations.outstanding(tenant), nil
}

// DischargeObligation implements ObligationStore.
func (j *MemoryJournal) DischargeObligation(_ context.Context, tenant values.TenantId, id string, review ObligationReview) (Obligation, error) {
	return j.obligations.discharge(tenant, id, review)
}
