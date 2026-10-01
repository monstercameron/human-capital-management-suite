package clockservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
)

// WorkingTimeWindowReader is the OBSERVE capability boundary. Implementations
// read the ledger and return the profile window plus the exact revision used
// for that read; they do not accept a client-provided revision.
type WorkingTimeWindowReader interface {
	Observe(context.Context, timeprofile.WorkingTimeWindowRequest) (timeprofile.WorkingTimeWindows, error)
}

// WorkingTimeDecisionRecorder atomically records a workflow DECISION against
// the revision it observed. A stale revision must be rejected by the store.
type WorkingTimeDecisionRecorder interface {
	Record(context.Context, string, string, string, string) error
}

// WorkingTimeRunReevaluator is notified after a committed correction so open
// workflow runs can reread the affected window. The service never evaluates
// policy itself.
type WorkingTimeRunReevaluator interface {
	Reevaluate(context.Context, string, string, string) error
}

type WorkingTimeWindowObservation struct {
	TenantID       string
	AggregationKey string
	DecisionRef    string
	Windows        timeprofile.WorkingTimeWindows
}

type WorkingTimeWindowReadRequest struct {
	TenantID        string
	AggregationKey  string
	AsOf            time.Time
	ReferencePeriod time.Duration
	HirerID         string
	BreakTolerance  time.Duration
}

func (r WorkingTimeWindowReadRequest) profileRequest() timeprofile.WorkingTimeWindowRequest {
	return timeprofile.WorkingTimeWindowRequest{TenantID: r.TenantID, WorkerID: r.AggregationKey, AsOf: r.AsOf, ReferencePeriod: r.ReferencePeriod, HirerID: r.HirerID, BreakTolerance: r.BreakTolerance}
}

// WorkingTimeWindowService is the application boundary used by workflow
// decisions. It pins the observed revision in the returned value and makes a
// correction's reevaluation an explicit downstream effect.
type WorkingTimeWindowService struct {
	Reader     WorkingTimeWindowReader
	Decisions  WorkingTimeDecisionRecorder
	Reevaluate WorkingTimeRunReevaluator
}

func (s WorkingTimeWindowService) Observe(ctx context.Context, req WorkingTimeWindowReadRequest) (WorkingTimeWindowObservation, error) {
	if s.Reader == nil || strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.AggregationKey) == "" || req.AsOf.IsZero() {
		return WorkingTimeWindowObservation{}, ErrInvalidRequest
	}
	windows, err := s.Reader.Observe(ctx, req.profileRequest())
	if err != nil {
		return WorkingTimeWindowObservation{}, err
	}
	if windows.Revision == "" {
		return WorkingTimeWindowObservation{}, fmt.Errorf("%w: working-time read returned no revision", ErrInvalidRequest)
	}
	return WorkingTimeWindowObservation{TenantID: req.TenantID, AggregationKey: req.AggregationKey, Windows: windows}, nil
}

func (s WorkingTimeWindowService) RecordDecision(ctx context.Context, observation WorkingTimeWindowObservation, decisionRef string) error {
	if s.Decisions == nil || observation.TenantID == "" || observation.AggregationKey == "" || observation.Windows.Revision == "" || strings.TrimSpace(decisionRef) == "" {
		return ErrInvalidRequest
	}
	return s.Decisions.Record(ctx, observation.TenantID, observation.AggregationKey, observation.Windows.Revision, decisionRef)
}

// NotifyCorrection forwards a committed ledger correction to the open-run
// reevaluator. The correction revision is required so reevaluation can reject
// an older notification arriving after a newer one.
func (s WorkingTimeWindowService) NotifyCorrection(ctx context.Context, tenant, aggregationKey, revision string) error {
	if s.Reevaluate == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(aggregationKey) == "" || strings.TrimSpace(revision) == "" {
		return ErrInvalidRequest
	}
	return s.Reevaluate.Reevaluate(ctx, tenant, aggregationKey, revision)
}
