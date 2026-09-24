package app

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// maxObservationPages bounds how many pages one comparison will read while
// looking for its subject. It mirrors the bound the drift detector applies to
// itself: a source that never terminates its cursor stops the run rather than
// hanging it.
const maxObservationPages = 64

// repairPlanAnswer is what the create_repair_plan capability returns.
//
// It carries the two sides and the comparison alongside the plan because
// simulate_repair has to be about the same evidence: handing the simulation a
// freshly re-read world would let a plan be validated against a comparison it
// was never drawn from, which is exactly the drift the plan's own
// preconditions exist to catch.
type repairPlanAnswer struct {
	Plan            repair.RepairPlan
	Diagnosis       repair.Diagnosis
	Diff            dataops.RecordDiff
	Canonical       dataops.CanonicalRecord
	Observed        dataops.ObservedRecord
	ObservedPresent bool
	Observation     dataops.ObservationWatermark
}

// detectDrift answers hcmnext.operations.detect_drift/v1.
func (h *domainHandlers) detectDrift(ctx context.Context, payload any) (any, error) {
	req, ok := payload.(dataops.DetectDriftRequest)
	if !ok {
		return nil, fmt.Errorf("app: detect_drift expects a dataops.DetectDriftRequest, got %T", payload)
	}
	if h.history == nil || h.observations == nil {
		return nil, fmt.Errorf("%w: %s", ErrCapabilityUnbound, dataops.DetectDriftIntentType)
	}
	return dataops.DetectDrift(ctx, h.history, h.observations, req)
}

// explainFieldHistory answers hcmnext.dataops.explain_field_history/v1.
// Authorization is part of the typed request and is passed through unchanged
// so the domain library remains the authority for field redaction.
func (h *domainHandlers) explainFieldHistory(ctx context.Context, payload any) (any, error) {
	req, ok := payload.(dataops.ExplainFieldHistoryRequest)
	if !ok {
		return nil, fmt.Errorf("app: explain_field_history expects a dataops.ExplainFieldHistoryRequest, got %T", payload)
	}
	if h.history == nil {
		return nil, fmt.Errorf("%w: %s", ErrCapabilityUnbound, dataops.ExplainFieldHistoryOperation)
	}
	return dataops.ExplainFieldHistory(ctx, h.history, req)
}

// createRepairPlan answers hcmnext.operations.create_repair_plan/v1.
//
// It reads both sides, compares them, diagnoses the disagreement and plans the
// correction, in that order, and it returns a plan that is marked
// non-executable by the repair package itself. There is no branch here that
// could execute one: this capability produces a recommendation, and the only
// thing downstream of it in P1A is a simulation.
func (h *domainHandlers) createRepairPlan(ctx context.Context, payload any) (any, error) {
	in, ok := payload.(RepairInputs)
	if !ok {
		return nil, fmt.Errorf("app: create_repair_plan expects a RepairInputs, got %T", payload)
	}
	if h.history == nil || h.observations == nil {
		return nil, fmt.Errorf("%w: %s", ErrCapabilityUnbound, repair.CreateRepairPlanIntentType)
	}

	canonical, explanation, err := h.canonicalSide(ctx, in)
	if err != nil {
		return nil, err
	}
	observed, present, watermark, err := h.observedSide(ctx, in)
	if err != nil {
		return nil, err
	}

	diff, err := dataops.DiffRecord(dataops.DiffRecordRequest{
		Canonical:       canonical,
		Observed:        observed,
		ObservedPresent: present,
		Observation:     watermark,
		Fields:          in.Fields,
		Authorization:   in.Authorization,
		Freshness:       in.Freshness,
		EvaluatedAt:     in.EvaluatedAt,
	})
	if err != nil {
		return nil, err
	}

	diagnosis, err := repair.Diagnose(in.PlanID+":diagnosis", diff, []string{
		explanation.ResultDigest,
		watermark.Digest,
	})
	if err != nil {
		return nil, err
	}

	plan, err := repair.CreateRepairPlan(repair.CreateRepairPlanRequest{
		PlanID:                 in.PlanID,
		Tenant:                 in.Tenant,
		Diagnosis:              diagnosis,
		Diff:                   diff,
		Canonical:              canonical,
		Observed:               observed,
		ObservedPresent:        present,
		Observation:            watermark,
		LocalSystem:            in.LocalSystem,
		ExternalSystem:         in.ExternalSystem,
		Approval:               in.Approval,
		AuthorityPolicyVersion: in.AuthorityPolicyVersion,
	})
	if err != nil {
		return nil, err
	}
	return repairPlanAnswer{
		Plan:            plan,
		Diagnosis:       diagnosis,
		Diff:            diff,
		Canonical:       canonical,
		Observed:        observed,
		ObservedPresent: present,
		Observation:     watermark,
	}, nil
}

// simulateRepair answers hcmnext.operations.simulate_repair/v1.
func (h *domainHandlers) simulateRepair(_ context.Context, payload any) (any, error) {
	req, ok := payload.(repair.SimulateRepairRequest)
	if !ok {
		return nil, fmt.Errorf("app: simulate_repair expects a repair.SimulateRepairRequest, got %T", payload)
	}
	result, err := repair.SimulateRepair(req)
	if err != nil {
		return nil, err
	}
	preflight, err := repairPreflightPlan(req, result)
	if err != nil {
		return nil, fmt.Errorf("app: compile repair preflight plan: %w", err)
	}
	return repairSimulationAnswer{Simulation: result, PreflightPlan: preflight}, nil
}

// explainTransaction answers hcmnext.intelligence.explain_transaction/v1.
func (h *domainHandlers) explainTransaction(ctx context.Context, payload any) (any, error) {
	req, ok := payload.(intelligence.ExplainTransactionRequest)
	if !ok {
		return nil, fmt.Errorf("app: explain_transaction expects an intelligence.ExplainTransactionRequest, got %T", payload)
	}
	if h.transactions == nil {
		return nil, fmt.Errorf("%w: %s", ErrCapabilityUnbound, intelligence.ExplainTransactionIntentType)
	}
	return intelligence.ExplainTransaction(ctx, h.transactions, req)
}

// canonicalSide projects the governed field history onto the comparison's
// canonical record, through the effective-date debugger rather than around it.
func (h *domainHandlers) canonicalSide(ctx context.Context, in RepairInputs) (dataops.CanonicalRecord, dataops.HistoryExplanation, error) {
	explanation, err := dataops.ExplainFieldHistory(ctx, h.history, dataops.ExplainFieldHistoryRequest{
		Tenant:        in.Tenant,
		Subject:       in.Subject,
		Fields:        in.Fields,
		AsOfEffective: in.AsOfEffective,
		AsKnownAt:     in.AsKnownAt,
		IncludeClaims: in.IncludeClaims,
		Authorization: in.Authorization,
	})
	if err != nil {
		return dataops.CanonicalRecord{}, dataops.HistoryExplanation{}, err
	}
	record, err := dataops.ProjectCanonicalRecord(explanation)
	if err != nil {
		return dataops.CanonicalRecord{}, dataops.HistoryExplanation{}, err
	}
	return record, explanation, nil
}

// observedSide pages the external system until it finds the subject or the
// source exhausts its cursor.
//
// A subject the source returned nothing for is not an error and not an empty
// record: it comes back with ObservedPresent false, which the comparison reads
// as "the source said nothing" rather than as "the source said absent".
func (h *domainHandlers) observedSide(ctx context.Context, in RepairInputs) (dataops.ObservedRecord, bool, dataops.ObservationWatermark, error) {
	cursor := ""
	seen := map[string]struct{}{}
	var last dataops.ObservationWatermark
	for range maxObservationPages {
		if _, repeat := seen[cursor]; repeat {
			return dataops.ObservedRecord{}, false, dataops.ObservationWatermark{},
				fmt.Errorf("app: observation source repeated cursor %q", cursor)
		}
		seen[cursor] = struct{}{}

		got, err := h.observations.ObservationsAt(ctx, dataops.ObservationQuery{
			Tenant:   in.Tenant,
			Source:   in.Source,
			Subjects: []values.EntityRef{in.Subject},
			Fields:   in.Fields,
			Cursor:   cursor,
			Limit:    in.PageLimit,
		})
		if err != nil {
			return dataops.ObservedRecord{}, false, dataops.ObservationWatermark{}, err
		}
		if err := got.Validate(); err != nil {
			return dataops.ObservedRecord{}, false, dataops.ObservationWatermark{}, err
		}
		last = got.Watermark()
		if record, found := got.Lookup(in.Subject); found {
			return record, true, last, nil
		}
		if got.NextCursor == "" {
			return dataops.ObservedRecord{}, false, last, nil
		}
		cursor = got.NextCursor
	}
	return dataops.ObservedRecord{}, false, last,
		fmt.Errorf("app: observation source did not terminate its cursor within %d pages", maxObservationPages)
}
