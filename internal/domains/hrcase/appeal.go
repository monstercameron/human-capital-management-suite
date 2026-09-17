// CASE-004: case reopen, appeal, relationship graph and evidence-package
// export.
//
// Reopen and appeal create successor CaseRevisions, never edits: the
// original closure and finding stay byte-identical and the successor carries
// the disposition forward with an explicit REOPEN_OF or APPEAL_OF edge.
// Direct links allow only DUPLICATE_OF and RELATED_TO between distinct
// cases, and an edge records case IDs alone — never compartment membership.
// ExportEvidence builds a scoped evidence package from the ledger inputs it
// is given: only authorized compartments are included, hold/redaction/
// retention/correction lineage must be complete, and the canonical digest is
// stable across input orderings and repeated exports. Clocks are injected
// by the caller; this file reads no wall clock and touches no database.
package hrcase

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	// ErrInvalidSuccessor marks a reopen/appeal from a state that cannot
	// originate a successor proceeding.
	ErrInvalidSuccessor = errors.New("hrcase: case cannot originate a successor proceeding from its state")
	// ErrInvalidEdge marks a malformed relationship edge.
	ErrInvalidEdge = errors.New("hrcase: invalid case relationship edge")
	// ErrExportInvalid marks an evidence package with incomplete identity,
	// chronology or lineage.
	ErrExportInvalid = errors.New("hrcase: invalid evidence package export")
)

// EdgeKind is the closed relationship vocabulary between cases.
type EdgeKind string

const (
	// EdgeReopenOf links a REOPENED successor to the closure it continues.
	EdgeReopenOf EdgeKind = "REOPEN_OF"
	// EdgeAppealOf links an APPEALED successor to the proceeding it reviews.
	EdgeAppealOf EdgeKind = "APPEAL_OF"
	// EdgeDuplicateOf links a case to the case it duplicates.
	EdgeDuplicateOf EdgeKind = "DUPLICATE_OF"
	// EdgeRelatedTo links two distinct but related cases.
	EdgeRelatedTo EdgeKind = "RELATED_TO"
)

// ValidEdgeKind reports whether kind is in the closed edge vocabulary.
func ValidEdgeKind(kind EdgeKind) bool {
	switch kind {
	case EdgeReopenOf, EdgeAppealOf, EdgeDuplicateOf, EdgeRelatedTo:
		return true
	}
	return false
}

// CaseEdge is one explicit relationship between cases. It records case IDs
// alone: no participants, roles, compartments or membership travel with it.
type CaseEdge struct {
	Kind       EdgeKind
	FromCaseID string
	ToCaseID   string
	Reason     string
	At         time.Time
}

// successor builds the next revision of current in state with its
// disposition carried forward. The input is never mutated.
func successor(current CaseRevision, state CaseState) (CaseRevision, error) {
	if err := current.Validate(); err != nil {
		return CaseRevision{}, err
	}
	if !current.Verify() {
		return CaseRevision{}, fmt.Errorf("%w: revision digest does not match its content", ErrInvalidSuccessor)
	}
	next := cloneRevision(current)
	next.Revision++
	next.Previous = current.Revision
	next.State = state
	return NewCaseRevision(next)
}

// ReopenCase continues a CLOSED case as a REOPENED successor. The closure
// disposition is carried forward, never erased, and the original revision
// is returned untouched alongside the REOPEN_OF edge.
func ReopenCase(closed CaseRevision, reason string, at time.Time) (CaseRevision, CaseEdge, error) {
	if closed.State != Closed {
		return CaseRevision{}, CaseEdge{}, fmt.Errorf("%w: only a CLOSED case reopens", ErrInvalidSuccessor)
	}
	if strings.TrimSpace(reason) == "" || at.IsZero() {
		return CaseRevision{}, CaseEdge{}, fmt.Errorf("%w: reason and time are required", ErrInvalidSuccessor)
	}
	next, err := successor(closed, Reopened)
	if err != nil {
		return CaseRevision{}, CaseEdge{}, err
	}
	return next, CaseEdge{Kind: EdgeReopenOf, FromCaseID: next.CaseID, ToCaseID: closed.CaseID, Reason: reason, At: at}, nil
}

// AppealCase reviews a RESOLVED or CLOSED case as an APPEALED successor.
// The original finding and disposition are carried forward and the input
// revision is left byte-identical.
func AppealCase(original CaseRevision, reason string, at time.Time) (CaseRevision, CaseEdge, error) {
	if original.State != Resolved && original.State != Closed {
		return CaseRevision{}, CaseEdge{}, fmt.Errorf("%w: only a RESOLVED or CLOSED case is appealable", ErrInvalidSuccessor)
	}
	if strings.TrimSpace(reason) == "" || at.IsZero() {
		return CaseRevision{}, CaseEdge{}, fmt.Errorf("%w: reason and time are required", ErrInvalidSuccessor)
	}
	next, err := successor(original, Appealed)
	if err != nil {
		return CaseRevision{}, CaseEdge{}, err
	}
	return next, CaseEdge{Kind: EdgeAppealOf, FromCaseID: next.CaseID, ToCaseID: original.CaseID, Reason: reason, At: at}, nil
}

// LinkCases records a DUPLICATE_OF or RELATED_TO edge between two distinct
// cases. REOPEN_OF and APPEAL_OF are refused here: those edges are minted
// only by their successor constructors so chronology cannot be forged.
func LinkCases(fromCaseID, toCaseID string, kind EdgeKind, reason string, at time.Time) (CaseEdge, error) {
	if kind != EdgeDuplicateOf && kind != EdgeRelatedTo {
		return CaseEdge{}, fmt.Errorf("%w: %s links only through its successor proceeding", ErrInvalidEdge, kind)
	}
	if strings.TrimSpace(fromCaseID) == "" || strings.TrimSpace(toCaseID) == "" || fromCaseID == toCaseID {
		return CaseEdge{}, fmt.Errorf("%w: two distinct case ids are required", ErrInvalidEdge)
	}
	if strings.TrimSpace(reason) == "" || at.IsZero() {
		return CaseEdge{}, fmt.Errorf("%w: reason and time are required", ErrInvalidEdge)
	}
	return CaseEdge{Kind: kind, FromCaseID: fromCaseID, ToCaseID: toCaseID, Reason: reason, At: at}, nil
}

// PackageEvent is one ordered chronology entry in an evidence package.
type PackageEvent struct {
	Seq  uint64
	Kind string
	Ref  string
	At   time.Time
}

// ExportArtifact is one scoped artifact reference. Artifact bytes live
// elsewhere; LegalHold, CorrectionOf and RetentionDecision carry the
// hold/correction/retention lineage the package must preserve.
type ExportArtifact struct {
	EvidenceID        string
	ArtifactDigest    string
	CompartmentID     string
	RedactionNote     string
	LegalHold         bool
	HoldRef           string
	RetentionDecision string
	CorrectionOf      string
}

// EvidencePackage is the verifiable scoped export of one case: the complete
// ordered chronology, the authorized artifacts, the withheld IDs, the
// redaction/hold/retention/decision lineage and the canonical digest.
type EvidencePackage struct {
	CaseID     string
	Chronology []PackageEvent
	Artifacts  []ExportArtifact
	Withheld   []string
	Decisions  []string
	Digest     string
}

// ExportEvidence builds the scoped package for the compartments in allowed.
// Artifacts outside allowed are withheld by ID, never included. Every legal
// hold and correction must resolve to a lineage decision ("hold:<ref>",
// "correction:<ref>") or the export is refused with zero effect. A viewer
// with no authorized compartment is denied without disclosure.
func ExportEvidence(caseID string, allowed []string, events []PackageEvent, artifacts []ExportArtifact, decisions []string) (EvidencePackage, error) {
	if strings.TrimSpace(caseID) == "" {
		return EvidencePackage{}, fmt.Errorf("%w: case id is required", ErrExportInvalid)
	}
	scope := map[string]bool{}
	for _, c := range allowed {
		if strings.TrimSpace(c) != "" {
			scope[c] = true
		}
	}
	if len(scope) == 0 {
		return EvidencePackage{}, ErrCaseDenied
	}
	if len(events) == 0 {
		return EvidencePackage{}, fmt.Errorf("%w: chronology is required", ErrExportInvalid)
	}
	chronology := append([]PackageEvent(nil), events...)
	for i, e := range chronology {
		if e.Seq == 0 || strings.TrimSpace(e.Kind) == "" || strings.TrimSpace(e.Ref) == "" || e.At.IsZero() {
			return EvidencePackage{}, fmt.Errorf("%w: chronology[%d] is incomplete", ErrExportInvalid, i)
		}
	}
	sort.Slice(chronology, func(i, j int) bool {
		if chronology[i].Seq != chronology[j].Seq {
			return chronology[i].Seq < chronology[j].Seq
		}
		return chronology[i].Ref < chronology[j].Ref
	})

	have := map[string]bool{}
	for _, d := range decisions {
		have[d] = true
	}
	var included []ExportArtifact
	var withheld []string
	for i, a := range artifacts {
		if strings.TrimSpace(a.EvidenceID) == "" || strings.TrimSpace(a.ArtifactDigest) == "" || strings.TrimSpace(a.CompartmentID) == "" {
			return EvidencePackage{}, fmt.Errorf("%w: artifacts[%d] is incomplete", ErrExportInvalid, i)
		}
		if strings.TrimSpace(a.RetentionDecision) == "" {
			return EvidencePackage{}, fmt.Errorf("%w: artifact %s lacks a retention decision", ErrExportInvalid, a.EvidenceID)
		}
		if a.LegalHold && !have["hold:"+a.HoldRef] {
			return EvidencePackage{}, fmt.Errorf("%w: legal hold %s lacks lineage", ErrExportInvalid, a.HoldRef)
		}
		if a.CorrectionOf != "" && !have["correction:"+a.CorrectionOf] {
			return EvidencePackage{}, fmt.Errorf("%w: correction of %s lacks lineage", ErrExportInvalid, a.CorrectionOf)
		}
		if !scope[a.CompartmentID] {
			withheld = append(withheld, a.EvidenceID)
			continue
		}
		included = append(included, a)
	}
	sort.Slice(included, func(i, j int) bool { return included[i].EvidenceID < included[j].EvidenceID })
	sort.Strings(withheld)
	lineage := append([]string(nil), decisions...)
	for _, id := range withheld {
		lineage = append(lineage, "withhold:"+id+":unauthorized-compartment")
	}
	sort.Strings(lineage)

	var b strings.Builder
	b.WriteString(caseID + "\x1f")
	for _, e := range chronology {
		fmt.Fprintf(&b, "event:%d:%s:%s:%s\x1f", e.Seq, e.Kind, e.Ref, e.At.UTC().Format(time.RFC3339))
	}
	for _, a := range included {
		fmt.Fprintf(&b, "artifact:%s:%s:%s:%s:%t:%s:%s:%s\x1f", a.EvidenceID, a.ArtifactDigest, a.CompartmentID, a.RedactionNote, a.LegalHold, a.HoldRef, a.RetentionDecision, a.CorrectionOf)
	}
	for _, id := range withheld {
		fmt.Fprintf(&b, "withheld:%s\x1f", id)
	}
	for _, d := range lineage {
		fmt.Fprintf(&b, "decision:%s\x1f", d)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return EvidencePackage{CaseID: caseID, Chronology: chronology, Artifacts: included, Withheld: withheld, Decisions: lineage, Digest: fmt.Sprintf("sha256:%x", sum)}, nil
}
