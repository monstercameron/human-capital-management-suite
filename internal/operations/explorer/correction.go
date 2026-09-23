package explorer

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/hashchain"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/lineage"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/internal/viewdigest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// EventRef names one recorded assertion by its exact stream and sequence. It
// is the same type ledger.AppendRequest.Corrects already uses, so a lineage
// walk starts from the identical value a caller already holds from an
// AppendReceipt or a prior query - and so transport ports can name it
// without importing the data plane themselves.
type EventRef = lineage.EventRef

// CorrectionRequest is everything an operator supplies to record one governed
// business correction: the stream and expected head it lands on, the exact
// recorded assertion it supersedes, and the superseding assertion's own
// envelope. Tenant travels separately (as in lineage.Append) so the tenant
// scope is stated once at the call site, and the assertion class is fixed to
// CORRECTION: this path records nothing else.
type CorrectionRequest struct {
	StreamKey      string
	ExpectedHead   int64
	Corrects       EventRef
	Authority      string
	SourceRef      string
	SchemaRef      string
	Payload        []byte
	ArtifactRef    string
	OccurredAt     time.Time
	EffectiveAt    time.Time
	CorrelationID  uuid.UUID
	CausationID    uuid.UUID
	IdempotencyKey string
}

// RecordCorrectionView is what recording a correction wrote and where, plus
// a content digest.
type RecordCorrectionView struct {
	Ref      EventRef
	EventID  uuid.UUID
	Sequence int64
	Digest   string
}

// ErrCorrectionForbidden reports that a correction write was refused because
// the caller's decision does not disclose the correction's subject: an
// operator who may not learn that a subject exists may not author
// corrections about it either.
type ErrCorrectionForbidden struct {
	Reason string
}

func (ErrCorrectionForbidden) Code() string { return "EXPLORER_CORRECTION_FORBIDDEN" }

func (e ErrCorrectionForbidden) Error() string {
	return fmt.Sprintf("%s: %s", e.Code(), e.Reason)
}

// RecordCorrection records one governed business correction. It appends the
// CORRECTION through internal/data/ledger/lineage.Append - which refuses a
// dangling or cross-tenant target before anything is written - and records
// the correction's hash-chain link in the same transaction, the well-formed
// calling pattern, so the stream still verifies afterwards. It writes
// nothing else and exposes no update or delete.
//
// A nil dec is unrestricted. A non-nil dec whose subject is not disclosable
// refuses the write with [ErrCorrectionForbidden] before any statement runs.
func RecordCorrection(ctx context.Context, tx dbport.Tx, chain *hashchain.Appender, tenant uuid.UUID, req CorrectionRequest, dec *authz.Decision) (RecordCorrectionView, error) {
	if dec != nil && !dec.SubjectDisclosable {
		return RecordCorrectionView{}, ErrCorrectionForbidden{Reason: "correction subject is not disclosable to the caller"}
	}
	receipt, err := lineage.Append(ctx, tx, tenant, ledger.AppendRequest{
		Tenant:         tenant,
		StreamKey:      req.StreamKey,
		ExpectedHead:   req.ExpectedHead,
		AssertionClass: ledger.Correction,
		Authority:      req.Authority,
		SourceRef:      req.SourceRef,
		SchemaRef:      req.SchemaRef,
		Payload:        req.Payload,
		ArtifactRef:    req.ArtifactRef,
		OccurredAt:     req.OccurredAt,
		EffectiveAt:    req.EffectiveAt,
		CorrelationID:  req.CorrelationID,
		CausationID:    req.CausationID,
		IdempotencyKey: req.IdempotencyKey,
		Corrects:       &req.Corrects,
	})
	if err != nil {
		return RecordCorrectionView{}, fmt.Errorf("explorer: record correction on %s: %w", req.StreamKey, err)
	}
	if _, err := chain.Append(ctx, tx, receipt); err != nil {
		return RecordCorrectionView{}, fmt.Errorf("explorer: record correction chain link on %s@%d: %w", receipt.StreamKey, receipt.Sequence, err)
	}
	view := RecordCorrectionView{
		Ref:      EventRef{StreamKey: receipt.StreamKey, Sequence: receipt.Sequence},
		EventID:  receipt.EventID,
		Sequence: receipt.Sequence,
	}
	view.Digest = digestRecordCorrection(view)
	return view, nil
}

func digestRecordCorrection(v RecordCorrectionView) string {
	return viewdigest.New().
		String("ref.stream_key", v.Ref.StreamKey).
		Int("ref.sequence", v.Ref.Sequence).
		String("event_id", v.EventID.String()).
		Int("sequence", v.Sequence).
		Digest()
}

// EffectiveCurrentView is ref's current-effective truth: the terminal node of
// its supersession chain together with the path from ref to it (excluding
// ref), plus a content digest.
type EffectiveCurrentView struct {
	Ref     EventRef
	Current lineage.Node
	Path    []lineage.Node
	// Withheld is true when the decision's subject was not disclosable:
	// Current is then zero and Path nil rather than an empty resolution a
	// caller could mistake for "this event was never corrected."
	Withheld bool
	Digest   string
}

// EffectiveCurrent resolves the terminal node of ref's supersession chain
// through internal/data/ledger/lineage.EffectiveCurrent: the latest direct
// corrector, repeatedly, until a node with no corrector remains. A nil dec
// is unrestricted. A non-nil dec whose subject is not disclosable withholds
// (Withheld=true, zero Current, nil Path) rather than walking at all.
func EffectiveCurrent(ctx context.Context, q lineage.Querier, tenant uuid.UUID, ref EventRef, dec *authz.Decision) (EffectiveCurrentView, error) {
	if dec != nil && !dec.SubjectDisclosable {
		v := EffectiveCurrentView{Ref: ref, Withheld: true}
		v.Digest = digestEffectiveCurrent(v)
		return v, nil
	}
	current, path, err := lineage.EffectiveCurrent(ctx, q, tenant, ref)
	if err != nil {
		return EffectiveCurrentView{}, fmt.Errorf("explorer: effective current of %s@%d: %w", ref.StreamKey, ref.Sequence, err)
	}
	view := EffectiveCurrentView{Ref: ref, Current: current, Path: path}
	view.Digest = digestEffectiveCurrent(view)
	return view, nil
}

func digestEffectiveCurrent(v EffectiveCurrentView) string {
	b := viewdigest.New().
		String("ref.stream_key", v.Ref.StreamKey).
		Int("ref.sequence", v.Ref.Sequence).
		Bool("withheld", v.Withheld).
		String("current.event_id", v.Current.EventID.String()).
		String("current.assertion_class", string(v.Current.AssertionClass)).
		Int("path", int64(len(v.Path)))
	for i, n := range v.Path {
		digestLineageNode(b, fmt.Sprintf("path[%d]", i), n)
	}
	return b.Digest()
}
