// RECRUIT-004: ATS withdrawal, rejection, rescission, correction and
// external reconciliation semantics.
//
// The ATS candidacy ledger is append-only: withdrawal, rejection and
// rescission are terminal transitions that preserve history, and a
// correction opens a successor candidacy instead of rewriting the
// terminal record. Late provider events never reopen a candidacy;
// duplicate external IDs quarantine instead of merging; observation
// mismatches create a scoped RepairPlan instead of a blind overwrite.
// Rejection reasons are purpose-limited: ordinary renders omit them.
// The ledger is kernel-pure and persists nothing.
package recruiting

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// ATSVersion is the rejection version for RECRUIT-004.
const ATSVersion = "recruiting-ats/v1"

var (
	// ErrATSRejected is the RECRUIT-004 sentinel. History deletion,
	// terminal rewrites, blind overwrites and candidate merges fail with
	// this error carrying the offending field, state and version.
	ErrATSRejected = errors.New("RECRUIT_004_REJECTED")
)

// ATSRejection is the stable RECRUIT-004 failure shape.
type ATSRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *ATSRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrATSRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the RECRUIT_004_REJECTED sentinel to errors.Is.
func (r *ATSRejection) Unwrap() error { return ErrATSRejected }

func atsReject(field, state, reason string) error {
	return &ATSRejection{Field: field, State: state, Version: ATSVersion, Reason: reason}
}

// CandidacyState is the closed RECRUIT-004 state vocabulary.
type CandidacyState string

const (
	CandidacyApplied   CandidacyState = "APPLIED"
	CandidacyScreening CandidacyState = "SCREENING"
	CandidacyInterview CandidacyState = "INTERVIEW"
	CandidacyOffered   CandidacyState = "OFFERED"
	CandidacyAccepted  CandidacyState = "ACCEPTED"
	CandidacyWithdrawn CandidacyState = "WITHDRAWN"
	CandidacyRejected  CandidacyState = "REJECTED"
	CandidacyRescinded CandidacyState = "RESCINDED"
)

// Valid reports whether the state is declared.
func (s CandidacyState) Valid() bool {
	switch s {
	case CandidacyApplied, CandidacyScreening, CandidacyInterview, CandidacyOffered,
		CandidacyAccepted, CandidacyWithdrawn, CandidacyRejected, CandidacyRescinded:
		return true
	default:
		return false
	}
}

// Terminal reports whether the state ends the candidacy.
func (s CandidacyState) Terminal() bool {
	return s == CandidacyWithdrawn || s == CandidacyRejected || s == CandidacyRescinded
}

// Transition is one immutable history entry. RestrictedReason carries a
// purpose-limited rejection rationale, never rendered ordinarily.
type Transition struct {
	Seq              uint64
	From             CandidacyState
	To               CandidacyState
	Reason           string
	RestrictedReason string
	AuthorityRef     string
	At               time.Time
}

// CandidacyRecord is the append-only history for one candidacy.
type CandidacyRecord struct {
	Tenant      string
	CandidacyID string
	CandidateID string
	ExternalID  string
	History     []Transition
	SuccessorOf string
}

// Current reports the latest state.
func (r CandidacyRecord) Current() CandidacyState {
	if len(r.History) == 0 {
		return ""
	}
	return r.History[len(r.History)-1].To
}

func (r CandidacyRecord) computedDigest() string {
	// History seals in program order: sequence position is semantic for
	// an append-only ledger, so reordered entries never digest alike.
	w := canonicalbytes.New("hcmnext.domains.recruiting.Candidacy", 1).
		String("tenant", r.Tenant).
		String("candidacy", r.CandidacyID).
		String("candidate", r.CandidateID).
		String("external", r.ExternalID).
		String("successor_of", r.SuccessorOf).
		Int("entries", int64(len(r.History)))
	for i, h := range r.History {
		w.String(fmt.Sprintf("history_%d", i), strings.Join([]string{
			fmt.Sprintf("%d", h.Seq), string(h.From), string(h.To),
			h.Reason, h.RestrictedReason, h.AuthorityRef,
			h.At.UTC().Format(time.RFC3339),
		}, "\x00"))
	}
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// Digest seals the record.
func (r CandidacyRecord) Digest() string { return r.computedDigest() }

// TerminalCommand is one terminal or corrective command.
type TerminalCommand struct {
	Kind             CandidacyState
	Reason           string
	RestrictedReason string
	AuthorityRef     string
	At               time.Time
}

// ApplyTerminal appends one withdrawal, rejection or rescission. The
// terminal transition preserves every prior entry; a terminal candidacy
// accepts no further transition.
func ApplyTerminal(record CandidacyRecord, cmd TerminalCommand) (CandidacyRecord, error) {
	if !cmd.Kind.Terminal() {
		return CandidacyRecord{}, atsReject("ats.command", "NOT_TERMINAL", fmt.Sprintf("kind %s is not a terminal command", cmd.Kind))
	}
	if strings.TrimSpace(record.CandidacyID) == "" || strings.TrimSpace(record.Tenant) == "" {
		return CandidacyRecord{}, atsReject("ats.candidacy", "MISSING", "tenant and candidacy id are required")
	}
	if len(record.History) == 0 {
		return CandidacyRecord{}, atsReject("ats.history", "MISSING", "terminal commands append to a live history")
	}
	if current := record.Current(); current.Terminal() {
		return CandidacyRecord{}, atsReject("ats.history", "TERMINAL", fmt.Sprintf("candidacy already %s: history is preserved, not rewritten", current))
	}
	if strings.TrimSpace(cmd.AuthorityRef) == "" {
		return CandidacyRecord{}, atsReject("ats.authority_ref", "MISSING", "authority ref is required")
	}
	if cmd.At.IsZero() {
		return CandidacyRecord{}, atsReject("ats.at", "MISSING", "transition instant is required")
	}
	if cmd.Kind == CandidacyRejected && strings.TrimSpace(cmd.RestrictedReason) == "" && strings.TrimSpace(cmd.Reason) == "" {
		return CandidacyRecord{}, atsReject("ats.reason", "MISSING", "rejection carries a rationale")
	}
	next := CandidacyRecord{
		Tenant: record.Tenant, CandidacyID: record.CandidacyID,
		CandidateID: record.CandidateID, ExternalID: record.ExternalID,
		SuccessorOf: record.SuccessorOf,
		History: append(append([]Transition(nil), record.History...), Transition{
			Seq: uint64(len(record.History) + 1), From: record.Current(), To: cmd.Kind,
			Reason: cmd.Reason, RestrictedReason: cmd.RestrictedReason,
			AuthorityRef: cmd.AuthorityRef, At: cmd.At.UTC(),
		}),
	}
	return next, nil
}

// CorrectTerminal opens a successor candidacy that references the
// terminal record. The original history is preserved verbatim.
func CorrectTerminal(record CandidacyRecord, successorID, reason, authorityRef string, at time.Time) (CandidacyRecord, error) {
	if !record.Current().Terminal() {
		return CandidacyRecord{}, atsReject("ats.history", "NOT_TERMINAL", "corrections open successors only for terminal candidacies")
	}
	if strings.TrimSpace(successorID) == "" || strings.TrimSpace(reason) == "" || strings.TrimSpace(authorityRef) == "" {
		return CandidacyRecord{}, atsReject("ats.correction", "MISSING", "successor id, reason and authority are required")
	}
	if at.IsZero() {
		return CandidacyRecord{}, atsReject("ats.at", "MISSING", "transition instant is required")
	}
	return CandidacyRecord{
		Tenant: record.Tenant, CandidacyID: successorID,
		CandidateID: record.CandidateID, ExternalID: record.ExternalID,
		SuccessorOf: record.CandidacyID,
		History: []Transition{{
			Seq: 1, From: record.Current(), To: CandidacyApplied,
			Reason: reason, AuthorityRef: authorityRef, At: at.UTC(),
		}},
	}, nil
}

// RenderPurpose is the closed reason-visibility vocabulary.
type RenderPurpose string

const (
	RenderOrdinary   RenderPurpose = "ORDINARY"
	RenderRestricted RenderPurpose = "RESTRICTED_REVIEW"
)

// RenderedCandidacy is the purpose-limited view of a record.
type RenderedCandidacy struct {
	CandidacyID string
	CandidateID string
	Current     CandidacyState
	Reasons     []string
}

// Render omits restricted rejection reasons from ordinary views.
func Render(record CandidacyRecord, purpose RenderPurpose) RenderedCandidacy {
	out := RenderedCandidacy{CandidacyID: record.CandidacyID, CandidateID: record.CandidateID, Current: record.Current()}
	for _, h := range record.History {
		if h.Reason != "" {
			out.Reasons = append(out.Reasons, h.Reason)
		}
		if purpose == RenderRestricted && h.RestrictedReason != "" {
			out.Reasons = append(out.Reasons, "restricted:"+h.RestrictedReason)
		}
	}
	return out
}

// ExternalObservation is one provider-side event for reconciliation.
type ExternalObservation struct {
	ExternalID string
	State      CandidacyState
	At         time.Time
}

// RepairPlan is the scoped repair for drift. Quarantine holds late or
// ambiguous identity; Reconcile lists the fields a human must align.
type RepairPlan struct {
	CandidacyID string
	Kind        string
	Fields      []string
	CreatedAt   time.Time
}

// ReconcileExternal compares provider observations against the
// authoritative record. Late events never reopen a terminal candidacy;
// duplicate external IDs quarantine instead of merging; mismatches
// create a repair instead of a blind overwrite.
func ReconcileExternal(record CandidacyRecord, knownExternalIDs map[string]string, obs ExternalObservation, now time.Time) (*RepairPlan, error) {
	if strings.TrimSpace(record.CandidacyID) == "" {
		return nil, atsReject("ats.candidacy", "MISSING", "candidacy id is required")
	}
	if strings.TrimSpace(obs.ExternalID) == "" {
		return nil, atsReject("ats.external_id", "MISSING", "external id is required")
	}
	if !obs.State.Valid() {
		return nil, atsReject("ats.external_state", "UNDECLARED", fmt.Sprintf("state %q is not declared", obs.State))
	}
	if obs.At.IsZero() || now.IsZero() {
		return nil, atsReject("ats.at", "MISSING", "observation and reconciliation instants are required")
	}
	if owner, dup := knownExternalIDs[obs.ExternalID]; dup && owner != record.CandidacyID {
		return &RepairPlan{
			CandidacyID: record.CandidacyID, Kind: "QUARANTINE_IDENTITY",
			Fields: []string{"external_id"}, CreatedAt: now.UTC(),
		}, nil
	}
	current := record.Current()
	if current.Terminal() {
		if obs.State != current {
			return &RepairPlan{
				CandidacyID: record.CandidacyID, Kind: "QUARANTINE_LATE_EVENT",
				Fields: []string{"external_state"}, CreatedAt: now.UTC(),
			}, nil
		}
		return nil, nil
	}
	if obs.State != current {
		return &RepairPlan{
			CandidacyID: record.CandidacyID, Kind: "RECONCILE_DRIFT",
			Fields: []string{"external_state"}, CreatedAt: now.UTC(),
		}, nil
	}
	return nil, nil
}
