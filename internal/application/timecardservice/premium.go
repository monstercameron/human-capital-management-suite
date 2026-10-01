// WTIME-013/014 (application-service half): when a timecard is submitted,
// classify its worked hours and price on-call, call-back, reporting-time,
// split-shift and differential premiums, and store the trace. The engine
// itself is internal/domains/timecalc; wave2_common.md notes it "may still
// be finishing", so this service depends on it only through the narrow
// PremiumCalculator port declared in ports.go rather than importing timecalc
// directly. Whoever wires the orchestrator supplies the adapter that maps a
// PremiumRequest onto timecalc's Request/RuleSet shapes.
package timecardservice

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ComputeTimecardPremiumsRequest carries one submitted timecard's worked
// intervals to classify and price.
type ComputeTimecardPremiumsRequest struct {
	WorkerRef      string
	TimecardID     string
	Profile        timeprofile.TimeProfile
	Input          PremiumRequest
	IdempotencyKey string
}

// ComputeTimecardPremiums authorizes, runs the calculator, and records the
// resulting trace in the ledger so the classification an approval later
// relies on can be reproduced and audited.
func (s Service) ComputeTimecardPremiums(ctx context.Context, p *trust.Principal, req ComputeTimecardPremiumsRequest) (PremiumTrace, error) {
	if err := validPrincipal(p); err != nil {
		return PremiumTrace{}, err
	}
	if s.Premiums == nil || s.Ledger == nil {
		return PremiumTrace{}, ErrUnavailable
	}
	if strings.TrimSpace(req.TimecardID) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		return PremiumTrace{}, ErrInvalidRequest
	}
	tenant := tenantOf(p)
	if err := s.authorize(ctx, p, tenant, req.WorkerRef, CapCorrectTimecard); err != nil {
		return PremiumTrace{}, err
	}
	trace, err := s.Premiums.Calculate(ctx, req.Profile, req.Input)
	if err != nil {
		return PremiumTrace{}, err
	}
	if err := s.Ledger.RecordPremiumTrace(ctx, tenant, req.TimecardID, trace, req.IdempotencyKey); err != nil {
		return PremiumTrace{}, err
	}
	return trace, nil
}
