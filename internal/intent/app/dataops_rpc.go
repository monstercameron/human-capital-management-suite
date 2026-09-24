package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	dataopsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1"
	evidencev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/fielddiff"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// DataOpsRPCAdapter is the thin protobuf boundary for the intent-owned
// authorized DataOps service.
type DataOpsRPCAdapter struct {
	transport.DataOpsHandler
	service *DataOpsService
}

var _ transport.DataOpsHandler = (*DataOpsRPCAdapter)(nil)

// NewDataOpsHandler composes the four unary DataOps RPCs over this cell's
// governed readers and the durable digest artifact store.
func (c *Cell) NewDataOpsHandler(artifacts DataOpsArtifactStore, now func() time.Time) (transport.DataOpsHandler, error) {
	service, err := c.NewDataOpsService(artifacts, now)
	if err != nil {
		return nil, err
	}
	return &DataOpsRPCAdapter{service: service}, nil
}

func (a *DataOpsRPCAdapter) ExplainFieldHistory(ctx context.Context, req *dataopsv1.ExplainFieldHistoryRequest) (*dataopsv1.ExplainFieldHistoryResponse, error) {
	if req == nil || req.GetSubject() == nil || req.GetAsOfEffective() == nil || req.GetAsKnownAt() == nil {
		return nil, fmt.Errorf("app: invalid ExplainFieldHistory request")
	}
	subject := entityFromProto(req.GetSubject())
	effective, err := values.NewLocalDate(int(req.GetAsOfEffective().GetYear()), time.Month(req.GetAsOfEffective().GetMonth()), int(req.GetAsOfEffective().GetDay()))
	if err != nil {
		return nil, err
	}
	known, err := values.NewKnownAt(values.NewInstant(req.GetAsKnownAt().AsTime()))
	if err != nil {
		return nil, err
	}
	result, err := a.service.ExplainFieldHistory(ctx, DataOpsHistoryRequest{Subject: subject, Fields: fieldIDs(req.GetFields()), AsOfEffective: effective, AsKnownAt: known, IncludeClaims: req.GetIncludeClaims()})
	if err != nil {
		return nil, err
	}
	return &dataopsv1.ExplainFieldHistoryResponse{Explanation: historyProto(result)}, nil
}

func (a *DataOpsRPCAdapter) DiffRecord(ctx context.Context, req *dataopsv1.DiffRecordRequest) (*dataopsv1.DiffRecordResponse, error) {
	if req == nil || req.GetSubject() == nil {
		return nil, fmt.Errorf("app: invalid DiffRecord request")
	}
	var evaluated values.Instant
	if req.EvaluatedAt != nil {
		evaluated = values.NewInstant(req.EvaluatedAt.AsTime())
	}
	result, err := a.service.DiffRecord(ctx, DataOpsDiffRequest{Subject: entityFromProto(req.GetSubject()), Fields: fieldIDs(req.GetFields()), ConnectionID: req.GetConnectionId(), EvaluatedAt: evaluated, FreshnessPolicy: req.GetFreshnessPolicyRef()})
	if err != nil {
		return nil, err
	}
	return &dataopsv1.DiffRecordResponse{Diff: diffProto(result)}, nil
}

func (a *DataOpsRPCAdapter) CreateRepairPlan(ctx context.Context, req *dataopsv1.CreateRepairPlanRequest) (*dataopsv1.CreateRepairPlanResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("app: invalid CreateRepairPlan request")
	}
	plan, err := a.service.CreateRepairPlan(ctx, DataOpsRepairPlanRequest{PlanID: req.GetPlanId(), DiffDigest: req.GetDiffDigest()})
	if err != nil {
		return nil, err
	}
	return &dataopsv1.CreateRepairPlanResponse{Plan: repairPlanProto(plan)}, nil
}

func (a *DataOpsRPCAdapter) SimulateRepair(ctx context.Context, req *dataopsv1.SimulateRepairRequest) (*dataopsv1.SimulateRepairResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("app: invalid SimulateRepair request")
	}
	simulation, err := a.service.SimulateRepair(ctx, req.GetPlanDigest())
	if err != nil {
		return nil, err
	}
	return &dataopsv1.SimulateRepairResponse{Simulation: repairSimulationProto(simulation)}, nil
}

func entityFromProto(in *commonv1.EntityRef) values.EntityRef {
	return values.EntityRef{Tenant: values.TenantId(in.GetTenantId()), Kind: values.Kind(strings.ToLower(in.GetKind())), Id: in.GetId()}
}

func entityProto(in values.EntityRef) *commonv1.EntityRef {
	return &commonv1.EntityRef{TenantId: string(in.Tenant), Kind: string(in.Kind), Id: in.Id}
}
func fieldIDs(in []string) []dataops.FieldID {
	out := make([]dataops.FieldID, len(in))
	for i, f := range in {
		out[i] = dataops.FieldID(f)
	}
	return out
}
func timestamp(in values.Instant) *timestamppb.Timestamp {
	if !in.IsSet() {
		return nil
	}
	return timestamppb.New(in.Time())
}
func presenceProto(in values.Presence[string]) *dataopsv1.PresenceValue {
	out := &dataopsv1.PresenceValue{Presence: commonv1.Presence(in.State())}
	if value, ok := in.Get(); ok {
		out.Value = value
	}
	return out
}
func valueKindProto(in fielddiff.ValueKind) dataopsv1.ValueKind {
	switch in.String() {
	case "STRING", "ENUM":
		return dataopsv1.ValueKind_VALUE_KIND_STRING
	case "NUMBER", "DECIMAL", "MONEY":
		return dataopsv1.ValueKind_VALUE_KIND_NUMBER
	case "BOOLEAN", "BOOL":
		return dataopsv1.ValueKind_VALUE_KIND_BOOLEAN
	case "DATE":
		return dataopsv1.ValueKind_VALUE_KIND_DATE
	case "INSTANT":
		return dataopsv1.ValueKind_VALUE_KIND_INSTANT
	case "ENTITY_REF":
		return dataopsv1.ValueKind_VALUE_KIND_ENTITY_REF
	default:
		return dataopsv1.ValueKind_VALUE_KIND_UNSPECIFIED
	}
}

func knownAtProto(in values.KnownAt) *timestamppb.Timestamp {
	if in.Canonical() == nil {
		return nil
	}
	return timestamppb.New(in.Instant().Time())
}
func authorityProto(in evidence.SourceAuthority) *evidencev1.SourceAuthority {
	if in.Validate() != nil {
		return nil
	}
	return &evidencev1.SourceAuthority{Kind: evidencev1.AuthorityKind(in.Kind), System: in.System, PolicyRef: in.PolicyRef}
}
func provenanceProto(in evidence.Provenance) *evidencev1.Provenance {
	if in.Validate() != nil {
		return nil
	}
	return &evidencev1.Provenance{Source: in.Source, EvidenceRef: in.EvidenceRef, RecordedAt: timestamppb.New(in.RecordedAt.Instant().Time())}
}
func revisionProto(in values.RevisionToken) *commonv1.RevisionToken {
	if !in.IsSpecified() {
		return nil
	}
	seq, hasSeq := in.Sequence()
	out := &commonv1.RevisionToken{SourceAuthorityId: in.Stream()}
	if hasSeq {
		out.Revision = seq
	} else if b, ok := in.Opaque(); ok {
		out.OpaqueCasBytes = b
	}
	return out
}
func localDateProto(in values.LocalDate) *commonv1.LocalDate {
	if in.Validate() != nil {
		return nil
	}
	return &commonv1.LocalDate{Year: in.Year(), Month: int32(in.Month()), Day: int32(in.Day())}
}
func intervalProto(in values.EffectiveInterval) *commonv1.EffectiveTimeRange {
	if in.Validate() != nil {
		return nil
	}
	out := &commonv1.EffectiveTimeRange{}
	if in.Kind() == values.IntervalKindLocalDate {
		out.Kind = commonv1.EffectiveTimeRangeKind_EFFECTIVE_TIME_RANGE_KIND_LOCAL_DATE
		start, _ := in.StartDate()
		out.StartInclusive = &commonv1.TimePoint{Point: &commonv1.TimePoint_LocalDate{LocalDate: localDateProto(start)}}
		if end, ok := in.EndDate(); ok {
			out.EndExclusive = &commonv1.TimePoint{Point: &commonv1.TimePoint_LocalDate{LocalDate: localDateProto(end)}}
		}
	} else {
		out.Kind = commonv1.EffectiveTimeRangeKind_EFFECTIVE_TIME_RANGE_KIND_INSTANT
		start, _ := in.StartInstant()
		out.StartInclusive = &commonv1.TimePoint{Point: &commonv1.TimePoint_Instant{Instant: timestamp(start)}}
		if end, ok := in.EndInstant(); ok {
			out.EndExclusive = &commonv1.TimePoint{Point: &commonv1.TimePoint_Instant{Instant: timestamp(end)}}
		}
	}
	out.CalendarRef = in.Calendar().Ref
	out.Timezone = in.Zone().ID
	out.TzdbVersion = in.Zone().TzdbVersion
	out.Disambiguation = commonv1.DisambiguationRule(in.Disambiguation())
	return out
}

func historyProto(in dataops.HistoryExplanation) *dataopsv1.HistoryExplanation {
	out := &dataopsv1.HistoryExplanation{Operation: in.Operation, OperationVersion: in.OperationVersion, Subject: entityProto(in.Subject), Exists: in.Exists,
		Disclosure: dataopsv1.Disclosure(in.Disclosure), WithheldReason: in.WithheldReason, AsOfEffective: localDateProto(in.AsOfEffective),
		AsKnownAt: knownAtProto(in.AsKnownAt), IncludeClaims: in.IncludeClaims, Watermark: revisionSequence(in.Watermark),
		Narrative: append([]string(nil), in.Narrative...), PolicyVersion: in.PolicyVersion, RulePackVersion: in.RulePackVersion,
		InputsDigest: in.InputsDigest, ResultDigest: in.ResultDigest, Effects: effectsProto(in.Effects), Receipt: dataOpsReceiptProto(in.Receipt)}
	for _, f := range in.Fields {
		field := &dataopsv1.FieldTimeline{Field: string(f.Field), Access: dataopsv1.Access(f.Access), DenialReason: f.DenialReason, ValueKind: valueKindProto(f.ValueKind),
			InForceId: optionalString(f.InForceID), Asserted: f.Asserted, CorrectionCount: int32(f.CorrectionCount)}
		for _, v := range f.Versions {
			a := v.Assertion
			field.Versions = append(field.Versions, &dataopsv1.ExplainedAssertion{Assertion: &dataopsv1.Assertion{Id: a.ID, Field: string(a.Field), Kind: valueKindProto(a.Kind),
				Value: presenceProto(a.Value), Effective: intervalProto(a.Effective), KnownAt: timestamppb.New(a.KnownAt.Instant().Time()), Revision: revisionSequence(a.Revision),
				Class: dataopsv1.AssertionClass(a.Class), Change: dataopsv1.ChangeKind(a.Change), Corrects: optionalString(a.Corrects), Authority: authorityProto(a.Authority), Provenance: provenanceProto(a.Provenance)},
				Superseded: v.Superseded, SupersededBy: optionalString(v.SupersededBy), EffectiveAtAsOf: v.EffectiveAtAsOf, KnownAtAsOf: v.KnownAtAsOf, InForce: v.InForce})
		}
		out.Fields = append(out.Fields, field)
	}
	return out
}

func diffProto(in dataops.RecordDiff) *dataopsv1.RecordDiff {
	out := &dataopsv1.RecordDiff{Subject: entityProto(in.Subject), CanonicalExists: in.CanonicalExists, ObservedExists: in.ObservedExists, Digest: in.Digest,
		Verdicts: verdictCountsProto(in.Verdicts), Safety: &dataopsv1.SafetyCounts{NotRequired: int32(in.Safety.NotRequired), Safe: int32(in.Safety.Safe), Unsafe: int32(in.Safety.Unsafe), Undecidable: int32(in.Safety.Undecidable)}}
	for _, f := range in.Findings {
		out.Findings = append(out.Findings, findingProto(f))
	}
	return out
}

func findingProto(f dataops.FieldFinding) *dataopsv1.FieldFinding {
	return &dataopsv1.FieldFinding{Subject: entityProto(f.Subject), Field: string(f.Field), Access: dataopsv1.Access(f.Access), Verdict: dataopsv1.Verdict(f.Verdict), Reason: f.Reason,
		Safety: dataopsv1.RepairSafety(f.Safety), SafetyReason: f.SafetyReason, CanonicalAuthority: authorityProto(f.CanonicalAuthority), CanonicalUpdatedAt: timestamp(f.CanonicalUpdatedAt),
		ExternalUpdatedAt: timestamp(f.ExternalUpdatedAt), CanonicalRevision: revisionSequence(f.CanonicalRevision), Observation: watermarkProto(f.Observation), AllowedNext: append([]string(nil), f.AllowedNext...)}
}

func watermarkProto(in dataops.ObservationWatermark) *dataopsv1.ObservationWatermark {
	return &dataopsv1.ObservationWatermark{Source: in.Source, SchemaVersion: in.SchemaVersion, RetrievedAt: timestamppb.New(in.RetrievedAt.Instant().Time()), Cursor: in.Cursor, Digest: in.Digest, Records: int32(in.Records)}
}

func repairPlanProto(in repair.RepairPlan) *dataopsv1.RepairPlan {
	diagnosis := &dataopsv1.RepairDiagnosis{Id: in.Diagnosis.ID, Subject: entityProto(in.Diagnosis.Subject), Problem: dataopsv1.RepairProblemClass(in.Diagnosis.Problem), Confidence: dataopsv1.RepairConfidence(in.Diagnosis.Confidence),
		EvidenceRefs: append([]string(nil), in.Diagnosis.EvidenceRefs...), Unknowns: append([]string(nil), in.Diagnosis.Unknowns...), DiffDigest: in.Diagnosis.DiffDigest}
	for _, f := range in.Diagnosis.Actionable {
		diagnosis.ActionableFields = append(diagnosis.ActionableFields, string(f))
	}
	for _, f := range in.Diagnosis.Blocked {
		diagnosis.BlockedFields = append(diagnosis.BlockedFields, string(f))
	}
	out := &dataopsv1.RepairPlan{IntentType: in.IntentType, IntentVersion: in.IntentVersion, Id: in.ID, TenantId: string(in.Tenant), Subject: entityProto(in.Subject), Diagnosis: diagnosis,
		DiffDigest: in.DiffDigest, Risk: dataopsv1.RepairRiskClass(in.Risk), RequiresApproval: in.RequiresApproval, ApprovalRoles: append([]string(nil), in.ApprovalRoles...), SodExcludedRoles: append([]string(nil), in.SoDExcludedRoles...),
		Executable: in.Executable, NotExecutableReason: in.NotExecutableReason, ExecutionState: in.ExecutionState, PolicyVersion: in.PolicyVersion, RulePackVersion: in.RulePackVersion,
		InputsDigest: in.InputsDigest, Digest: in.Digest, Effects: effectsProto(in.Effects), Receipt: dataOpsReceiptProto(in.Receipt)}
	for _, s := range in.Steps {
		sp := &dataopsv1.RepairStep{Ordinal: int32(s.Ordinal), Action: dataopsv1.RepairAction(s.Action), Target: &dataopsv1.RepairTarget{System: s.Target.System, Subject: entityProto(s.Target.Subject), Field: string(s.Target.Field)},
			ExpectedCurrent: presenceProto(s.ExpectedCurrent), ExpectedPost: presenceProto(s.ExpectedPost), ValueKind: valueKindProto(s.ValueKind), Treatment: dataopsv1.RepairHistoryTreatment(s.Treatment), Risk: dataopsv1.RepairRiskClass(s.Risk),
			IdempotencyKey: s.IdempotencyKey, MaxAttempts: int32(s.MaxAttempts), WriteSet: append([]string(nil), s.WriteSet...), DownstreamEffects: append([]string(nil), s.DownstreamEffects...), RequiresApproval: s.RequiresApproval,
			ApprovalRoles: append([]string(nil), s.ApprovalRoles...), SodExcludedRoles: append([]string(nil), s.SoDExcludedRoles...), Observation: s.Observation, Success: s.Success, Rollback: s.Rollback, Reason: s.Reason}
		for _, p := range s.Preconditions {
			sp.Preconditions = append(sp.Preconditions, &dataopsv1.RepairPrecondition{Kind: dataopsv1.RepairPreconditionKind(p.Kind), Ref: p.Ref, Expected: p.Expected})
		}
		out.Steps = append(out.Steps, sp)
	}
	for _, f := range in.SkippedFields {
		out.SkippedFields = append(out.SkippedFields, &dataopsv1.SkippedField{Field: string(f.Field), Reason: f.Reason})
	}
	return out
}

func repairSimulationProto(in repair.RepairSimulation) *dataopsv1.RepairSimulation {
	out := &dataopsv1.RepairSimulation{IntentType: in.IntentType, IntentVersion: in.IntentVersion, PlanId: in.PlanID, PlanDigest: in.PlanDigest, DiffDigest: in.DiffDigest, CurrentDiffDigest: in.CurrentDiffDigest,
		TenantId: string(in.Tenant), Subject: entityProto(in.Subject), Status: dataopsv1.RepairSimulationStatus(in.Status), StatusReason: in.StatusReason, ProjectedDiffDigest: in.ProjectedDiffDigest,
		RequiresApproval: in.RequiresApproval, ApprovalRoles: append([]string(nil), in.ApprovalRoles...), SodExcludedRoles: append([]string(nil), in.SoDExcludedRoles...), ObservationExpectations: append([]string(nil), in.ObservationExpectations...),
		SuccessCriteria: append([]string(nil), in.SuccessCriteria...), RollbackExpectations: append([]string(nil), in.RollbackExpectations...), Caveats: append([]string(nil), in.Caveats...), PolicyVersion: in.PolicyVersion, RulePackVersion: in.RulePackVersion,
		InputsDigest: in.InputsDigest, ResultDigest: in.ResultDigest, Effects: effectsProto(in.Effects), Receipt: dataOpsReceiptProto(in.Receipt), ResidualCounts: verdictCountsProto(in.ResidualCounts),
		Cost: &dataopsv1.RepairCost{Steps: int32(in.Cost.Steps), LocalWrites: int32(in.Cost.LocalWrites), ExternalWrites: int32(in.Cost.ExternalWrites), HumanReviews: int32(in.Cost.HumanReviews), MappingReviews: int32(in.Cost.MappingReviews), Approvals: int32(in.Cost.Approvals), MaxAttempts: int32(in.Cost.MaxAttempts)}}
	for _, p := range in.Projected {
		out.Projected = append(out.Projected, &dataopsv1.ProjectedField{Field: string(p.Field), System: p.System, Action: dataopsv1.RepairAction(p.Action), Before: presenceProto(p.Before), After: presenceProto(p.After), Changed: p.Changed, Reason: p.Reason})
	}
	for _, f := range in.ResidualFindings {
		out.ResidualFindings = append(out.ResidualFindings, findingProto(f))
	}
	for _, r := range in.Risks {
		out.Risks = append(out.Risks, &dataopsv1.RepairRiskNote{Field: string(r.Field), RiskClass: dataopsv1.RepairRiskClass(r.Class), Token: r.Token})
	}
	return out
}

func verdictCountsProto(in dataops.VerdictCounts) *dataopsv1.VerdictCounts {
	return &dataopsv1.VerdictCounts{Match: int32(in.Match), Mismatch: int32(in.Mismatch), Stale: int32(in.Stale), Unknown: int32(in.Unknown), Redacted: int32(in.Redacted), NotApplicable: int32(in.NotApplicable)}
}
func revisionSequence(in values.RevisionToken) uint64 { n, _ := in.Sequence(); return n }
func effectsProto(in evidence.EffectCounters) *evidencev1.EffectCounters {
	return &evidencev1.EffectCounters{DomainWrites: int32(in.DomainWrites), Reservations: int32(in.Reservations), WorkItems: int32(in.WorkItems), Timers: int32(in.Timers), Messages: int32(in.Messages), OutboxEntries: int32(in.OutboxEntries), ProviderCalls: int32(in.ProviderCalls), ApprovalBindings: int32(in.ApprovalBindings)}
}
func dataOpsReceiptProto(in evidence.ZeroEffectReceipt) *evidencev1.ZeroEffectReceipt {
	out := &evidencev1.ZeroEffectReceipt{IntentType: in.IntentType, IntentVersion: in.IntentVersion, RequestState: in.RequestState, ExecutionState: in.ExecutionState, InputsDigest: in.InputsDigest, ResultDigest: in.ResultDigest, Counters: effectsProto(in.Counters)}
	if in.Mode == evidence.ModePreflight {
		out.Mode = evidencev1.ReceiptMode_RECEIPT_MODE_PREFLIGHT
	} else if in.Mode == evidence.ModeSimulate {
		out.Mode = evidencev1.ReceiptMode_RECEIPT_MODE_SIMULATE
	}
	for _, c := range in.Controls {
		out.Controls = append(out.Controls, &evidencev1.ControlVersion{Name: c.Name, Version: c.Version})
	}
	return out
}
func optionalString(in string) *string {
	if in == "" {
		return nil
	}
	return &in
}
