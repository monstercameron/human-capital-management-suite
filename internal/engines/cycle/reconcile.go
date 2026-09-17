package cycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// CompletionDimension is the closed set of close-readiness dimensions a
// runtime close must reconcile. A dimension is never inferred: the caller
// supplies evidence for exactly these five.
type CompletionDimension string

const (
	DimensionMembers      CompletionDimension = "MEMBERS"
	DimensionCalculations CompletionDimension = "CALCULATIONS"
	DimensionApprovals    CompletionDimension = "APPROVALS"
	DimensionEffects      CompletionDimension = "EFFECTS"
	DimensionObservations CompletionDimension = "OBSERVATIONS"
)

// RepairObligation is the closed repair vocabulary. Every incomplete
// dimension maps to exactly one obligation, so an incomplete close can
// never silently drop a dimension.
type RepairObligation string

const (
	ObligationReconcileMembership RepairObligation = "RECONCILE_MEMBERSHIP"
	ObligationRecalculate         RepairObligation = "RECALCULATE"
	ObligationReapprove           RepairObligation = "REAPPROVE"
	ObligationReplayEffects       RepairObligation = "REPLAY_EFFECTS"
	ObligationCollectObservations RepairObligation = "COLLECT_OBSERVATIONS"
)

// CompletionVerdict is COMPLETE only when every dimension is complete.
type CompletionVerdict string

const (
	VerdictComplete   CompletionVerdict = "COMPLETE"
	VerdictIncomplete CompletionVerdict = "INCOMPLETE"
)

// DimensionEvidence is one dimension's close-readiness claim. It carries
// references and digests, never mutable result contents.
type DimensionEvidence struct {
	Dimension      CompletionDimension
	Complete       bool
	EvidenceRef    string
	ManifestDigest string
	Detail         string
}

// RepairItem is one outstanding obligation on an incomplete close.
type RepairItem struct {
	Dimension   CompletionDimension
	Obligation  RepairObligation
	EvidenceRef string
	Detail      string
}

// ReconciliationRequest gates one runtime close. Cycle is the lifecycle
// view being closed; At is the close instant.
type ReconciliationRequest struct {
	Cycle      GovernedCycle
	At         time.Time
	Dimensions []DimensionEvidence
}

// CompletionResult is the sealed reconciliation record: every dimension
// and, when incomplete, the exact repair obligations.
type CompletionResult struct {
	CycleDigest    string
	RevisionDigest string
	State          LifecycleState
	At             time.Time
	Dimensions     []DimensionEvidence
	Verdict        CompletionVerdict
	Repairs        []RepairItem
	Digest         string
}

var (
	ErrCompletionInvalid   = errors.New("cycle: completion reconciliation request is invalid")
	ErrCompletionState     = errors.New("cycle: completion reconciles an open or closed cycle only")
	ErrCompletionEvidence  = errors.New("cycle: completion dimension evidence is invalid")
	ErrCompletionDimension = errors.New("cycle: completion requires exactly the five close dimensions")
	// ErrCompletionIncomplete refuses the close: the returned result still
	// reports each dimension and its repair obligations.
	ErrCompletionIncomplete = errors.New("cycle: close refused with incomplete dimensions")
)

func completionObligation(dimension CompletionDimension) (RepairObligation, bool) {
	switch dimension {
	case DimensionMembers:
		return ObligationReconcileMembership, true
	case DimensionCalculations:
		return ObligationRecalculate, true
	case DimensionApprovals:
		return ObligationReapprove, true
	case DimensionEffects:
		return ObligationReplayEffects, true
	case DimensionObservations:
		return ObligationCollectObservations, true
	default:
		return "", false
	}
}

func (d DimensionEvidence) validate() error {
	if _, ok := completionObligation(d.Dimension); !ok {
		return ErrCompletionDimension
	}
	if strings.TrimSpace(d.EvidenceRef) == "" || !validSHA256Digest(d.ManifestDigest) {
		return ErrCompletionEvidence
	}
	return nil
}

// ReconcileCompletion seals one close-readiness record. A close with any
// incomplete dimension is refused with ErrCompletionIncomplete, and the
// returned result names the exact repair obligations, so runtime close
// can never mask incomplete members, calculations, approvals, effects
// or observations. Nothing is mutated: the cycle view and the request
// slices are copied, never aliased.
func ReconcileCompletion(request ReconciliationRequest) (CompletionResult, error) {
	if request.Cycle.Revision.Digest == "" || request.At.IsZero() {
		return CompletionResult{}, ErrCompletionInvalid
	}
	if request.Cycle.State != StateOpen && request.Cycle.State != StateClosed {
		return CompletionResult{}, ErrCompletionState
	}
	if len(request.Dimensions) != 5 {
		return CompletionResult{}, ErrCompletionDimension
	}
	seen := map[CompletionDimension]struct{}{}
	dimensions := make([]DimensionEvidence, 0, len(request.Dimensions))
	for _, dimension := range request.Dimensions {
		if err := dimension.validate(); err != nil {
			return CompletionResult{}, err
		}
		if _, ok := seen[dimension.Dimension]; ok {
			return CompletionResult{}, ErrCompletionDimension
		}
		seen[dimension.Dimension] = struct{}{}
		dimensions = append(dimensions, dimension)
	}
	for _, dimension := range []CompletionDimension{DimensionMembers, DimensionCalculations, DimensionApprovals, DimensionEffects, DimensionObservations} {
		if _, ok := seen[dimension]; !ok {
			return CompletionResult{}, ErrCompletionDimension
		}
	}
	var repairs []RepairItem
	for _, dimension := range dimensions {
		if dimension.Complete {
			continue
		}
		obligation, _ := completionObligation(dimension.Dimension)
		repairs = append(repairs, RepairItem{Dimension: dimension.Dimension, Obligation: obligation, EvidenceRef: dimension.EvidenceRef, Detail: dimension.Detail})
	}
	verdict := VerdictComplete
	if len(repairs) != 0 {
		verdict = VerdictIncomplete
	}
	out := CompletionResult{CycleDigest: request.Cycle.CanonicalDigest(), RevisionDigest: request.Cycle.Revision.Digest, State: request.Cycle.State, At: request.At.UTC(), Dimensions: dimensions, Verdict: verdict, Repairs: repairs}
	body, err := out.canonicalBody()
	if err != nil {
		return CompletionResult{}, err
	}
	sum := sha256.Sum256(body)
	out.Digest = "sha256:" + hex.EncodeToString(sum[:])
	if verdict == VerdictIncomplete {
		return out, ErrCompletionIncomplete
	}
	return out, nil
}

func (r CompletionResult) canonicalBody() ([]byte, error) {
	return json.Marshal(struct {
		CycleDigest    string
		RevisionDigest string
		State          LifecycleState
		At             time.Time
		Dimensions     []DimensionEvidence
		Verdict        CompletionVerdict
		Repairs        []RepairItem
	}{r.CycleDigest, r.RevisionDigest, r.State, r.At, r.Dimensions, r.Verdict, r.Repairs})
}

// CanonicalDigest returns the sealed result digest.
func (r CompletionResult) CanonicalDigest() string { return r.Digest }

// Verify checks both the semantic contract and the seal of a previously
// constructed result. Callers loading durable records must verify them
// before treating the verdict or obligations as trustworthy.
func (r CompletionResult) Verify() error {
	if r.CycleDigest == "" || r.RevisionDigest == "" || r.At.IsZero() || len(r.Dimensions) != 5 {
		return ErrCompletionInvalid
	}
	seen := map[CompletionDimension]struct{}{}
	incomplete := 0
	for _, dimension := range r.Dimensions {
		if err := dimension.validate(); err != nil {
			return err
		}
		if _, ok := seen[dimension.Dimension]; ok {
			return ErrCompletionDimension
		}
		seen[dimension.Dimension] = struct{}{}
		if !dimension.Complete {
			incomplete++
		}
	}
	for _, dimension := range []CompletionDimension{DimensionMembers, DimensionCalculations, DimensionApprovals, DimensionEffects, DimensionObservations} {
		if _, ok := seen[dimension]; !ok {
			return ErrCompletionDimension
		}
	}
	switch r.Verdict {
	case VerdictComplete:
		if incomplete != 0 || len(r.Repairs) != 0 {
			return ErrCompletionInvalid
		}
	case VerdictIncomplete:
		if incomplete == 0 || len(r.Repairs) != incomplete {
			return ErrCompletionInvalid
		}
	default:
		return ErrCompletionInvalid
	}
	body, err := r.canonicalBody()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	if r.Digest != "sha256:"+hex.EncodeToString(sum[:]) {
		return ErrCompletionInvalid
	}
	return nil
}
