package timeclockstore

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
)

// RetentionAdapter is the production composition seam for clock retention.
// The policy remains value-owned by the caller; evaluation is delegated to
// clockservice, which in turn uses the shared records disposition simulator.
type RetentionAdapter struct {
	Policy clockservice.TimeRecordRetentionPolicy
}

// NewRetentionAdapter validates the published schedule before it can reach a
// retention job or a worker-facing read path.
func NewRetentionAdapter(policy clockservice.TimeRecordRetentionPolicy) (RetentionAdapter, error) {
	if err := policy.Validate(); err != nil {
		return RetentionAdapter{}, err
	}
	return RetentionAdapter{Policy: policy}, nil
}

// Evaluate returns the payload-free retention verdict for one declared clock
// record. A legal hold remains a blocker even when the minimum has elapsed.
func (a RetentionAdapter) Evaluate(declaration clockservice.TimeRecordDeclaration, asOf time.Time) (clockservice.TimeRecordRetentionDecision, error) {
	return clockservice.EvaluateTimeRecordRetention(a.Policy, declaration, asOf)
}

// EvaluateRetention is the descriptive adapter alias used by disposition
// runners.
func (a RetentionAdapter) EvaluateRetention(declaration clockservice.TimeRecordDeclaration, asOf time.Time) (clockservice.TimeRecordRetentionDecision, error) {
	return a.Evaluate(declaration, asOf)
}

var _ interface {
	Evaluate(clockservice.TimeRecordDeclaration, time.Time) (clockservice.TimeRecordRetentionDecision, error)
} = RetentionAdapter{}
