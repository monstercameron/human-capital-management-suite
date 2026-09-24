package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrDataOpsUnavailable = errors.New("app: DataOps service is unavailable")
	ErrDataOpsArtifact    = errors.New("app: DataOps follow-up artifact is invalid")
)

// DataOpsArtifactStore is the durable tenant-scoped store used to resolve
// follow-up calls by the digest returned from an earlier call.
type DataOpsArtifactStore interface {
	Put(context.Context, values.TenantId, string, string, []byte) error
	Get(context.Context, values.TenantId, string, string) ([]byte, error)
}

// DataOpsService is the authorized application boundary for unary DataOps
// operations. It has no mutating method: repair output is a recommendation and
// an in-memory projection.
type DataOpsService struct {
	handlers   *domainHandlers
	artifacts  DataOpsArtifactStore
	connection string
	source     string
	now        func() time.Time
}

// DataOpsHistoryRequest contains only caller-selected coordinates. Tenant and
// authorization are resolved from the verified request context.
type DataOpsHistoryRequest struct {
	Subject       values.EntityRef
	Fields        []dataops.FieldID
	AsOfEffective values.LocalDate
	AsKnownAt     values.KnownAt
	IncludeClaims bool
}

// DataOpsDiffRequest selects one record and projection. Connection identity,
// tenant and authorization are checked against the composed cell.
type DataOpsDiffRequest struct {
	Subject         values.EntityRef
	Fields          []dataops.FieldID
	ConnectionID    string
	EvaluatedAt     values.Instant
	FreshnessPolicy string
}

// DataOpsRepairPlanRequest binds a non-executable recommendation to a prior
// stored diff. Policy, systems and approval roles are owned by this service.
type DataOpsRepairPlanRequest struct {
	PlanID     string
	DiffDigest string
}

// NewDataOpsService builds the authorized service from the same governed
// worker and incumbent observation sources used by the intent capability
// handlers. This factory is called by the serving composition after NewCell.
func (c *Cell) NewDataOpsService(artifacts DataOpsArtifactStore, now func() time.Time) (*DataOpsService, error) {
	if c == nil || artifacts == nil || c.Workers == nil || c.Incumbent == nil || c.Connection == nil || c.Observations == nil {
		return nil, ErrDataOpsUnavailable
	}
	observations, err := NewExternalObservations(c.Incumbent, c.Connection, c.Observations, ObservationFreshnessBudget)
	if err != nil {
		return nil, fmt.Errorf("app: compose DataOps observations: %w", err)
	}
	if now == nil {
		now = time.Now
	}
	return &DataOpsService{
		handlers:  &domainHandlers{history: &workerFieldHistory{workers: c.Workers}, observations: observations},
		artifacts: artifacts, connection: c.Connection.ID(), source: observations.SourceRef(), now: now,
	}, nil
}

// ExplainFieldHistory returns an authorized, redacted timeline.
func (s *DataOpsService) ExplainFieldHistory(ctx context.Context, req DataOpsHistoryRequest) (dataops.HistoryExplanation, error) {
	principal, tenant, purpose, at, err := s.trusted(ctx)
	if err != nil {
		return dataops.HistoryExplanation{}, err
	}
	subject, fields, decision, err := s.authorize(principal, tenant, purpose, values.NewInstant(at), req.Subject, req.Fields)
	if err != nil {
		return dataops.HistoryExplanation{}, err
	}
	return dataops.ExplainFieldHistory(ctx, s.handlers.history, dataops.ExplainFieldHistoryRequest{
		Tenant: tenant, Subject: subject, Fields: fields, AsOfEffective: req.AsOfEffective,
		AsKnownAt: req.AsKnownAt, IncludeClaims: req.IncludeClaims, Authorization: decision,
	})
}

// DiffRecord computes a comparison from the cell's own canonical and external
// readers, then saves the exact inputs behind its digest for a later plan call.
func (s *DataOpsService) DiffRecord(ctx context.Context, req DataOpsDiffRequest) (dataops.RecordDiff, error) {
	principal, tenant, purpose, received, err := s.trusted(ctx)
	if err != nil {
		return dataops.RecordDiff{}, err
	}
	if req.ConnectionID != s.connection {
		return dataops.RecordDiff{}, fmt.Errorf("app: requested connector connection does not match the composed connection")
	}
	if req.FreshnessPolicy != "" && req.FreshnessPolicy != FreshnessPolicyVersion {
		return dataops.RecordDiff{}, fmt.Errorf("app: freshness policy does not match the published policy")
	}
	evaluated := req.EvaluatedAt
	if !evaluated.IsSet() {
		evaluated = values.NewInstant(received)
	}
	dateTime := evaluated.Time()
	effective, err := values.NewLocalDate(dateTime.Year(), dateTime.Month(), dateTime.Day())
	if err != nil {
		return dataops.RecordDiff{}, err
	}
	known, err := values.NewKnownAt(evaluated)
	if err != nil {
		return dataops.RecordDiff{}, err
	}
	subject, fields, authorization, err := s.authorize(principal, tenant, purpose, evaluated, req.Subject, req.Fields)
	if err != nil {
		return dataops.RecordDiff{}, err
	}
	in := RepairInputs{
		Tenant: tenant, Subject: subject, Source: s.source, Fields: fields,
		AsOfEffective: effective, AsKnownAt: known, EvaluatedAt: evaluated, Authorization: authorization,
		Freshness: freshnessPolicy(), Approval: repairApprovalPolicy(), AuthorityPolicyVersion: FieldAuthorityPolicyVersion,
		LocalSystem: LocalSystem, ExternalSystem: s.source, PageLimit: ObservationPageLimit,
	}
	canonical, _, err := s.handlers.canonicalSide(ctx, in)
	if err != nil {
		return dataops.RecordDiff{}, err
	}
	observed, present, watermark, err := s.handlers.observedSide(ctx, in)
	if err != nil {
		return dataops.RecordDiff{}, err
	}
	diff, err := dataops.DiffRecord(dataops.DiffRecordRequest{
		Canonical: canonical, Observed: observed, ObservedPresent: present, Observation: watermark,
		Fields: fields, Authorization: authorization, Freshness: in.Freshness, EvaluatedAt: evaluated,
	})
	if err != nil {
		return dataops.RecordDiff{}, err
	}
	artifact := dataOpsDiffArtifact{Subject: subject, Fields: fields, Effective: effective.String(), KnownAt: known.String(), EvaluatedAt: evaluated.String(), Source: in.Source}
	if err := s.save(ctx, tenant, "diff", diff.Digest, artifact); err != nil {
		return dataops.RecordDiff{}, err
	}
	return diff, nil
}

// CreateRepairPlan re-resolves the stored comparison coordinates and refuses
// a digest whose evidence has moved. The returned plan is non-executable.
func (s *DataOpsService) CreateRepairPlan(ctx context.Context, req DataOpsRepairPlanRequest) (repair.RepairPlan, error) {
	principal, tenant, purpose, _, err := s.trusted(ctx)
	if err != nil {
		return repair.RepairPlan{}, err
	}
	var prior dataOpsDiffArtifact
	if err := s.load(ctx, tenant, "diff", req.DiffDigest, &prior); err != nil {
		return repair.RepairPlan{}, err
	}
	effective, known, evaluated, err := prior.coordinates()
	if err != nil {
		return repair.RepairPlan{}, err
	}
	inputs, err := s.rebuildInputs(ctx, principal, tenant, purpose, prior, evaluated)
	if err != nil {
		return repair.RepairPlan{}, err
	}
	answer, err := s.handlers.createRepairPlan(ctx, RepairInputs{
		PlanID: req.PlanID, Tenant: inputs.Tenant, Subject: inputs.Subject, Source: inputs.Source,
		Fields: inputs.Fields, AsOfEffective: effective, AsKnownAt: known,
		EvaluatedAt: inputs.EvaluatedAt, Authorization: inputs.Authorization, Freshness: inputs.Freshness,
		Approval: inputs.Approval, AuthorityPolicyVersion: inputs.AuthorityPolicyVersion,
		LocalSystem: inputs.LocalSystem, ExternalSystem: inputs.ExternalSystem, PageLimit: inputs.PageLimit,
	})
	if err != nil {
		return repair.RepairPlan{}, err
	}
	planAnswer := answer.(repairPlanAnswer)
	if planAnswer.Diff.Digest != req.DiffDigest {
		return repair.RepairPlan{}, ErrDataOpsArtifact
	}
	if err := s.savePlan(ctx, tenant, planAnswer.Plan.Digest, prior, planAnswer.Plan); err != nil {
		return repair.RepairPlan{}, err
	}
	return planAnswer.Plan, nil
}

// SimulateRepair resolves the prior plan by digest and compares its pinned
// evidence with a fresh read under this caller's current authorization.
func (s *DataOpsService) SimulateRepair(ctx context.Context, planDigest string) (repair.RepairSimulation, error) {
	principal, tenant, purpose, _, err := s.trusted(ctx)
	if err != nil {
		return repair.RepairSimulation{}, err
	}
	saved, err := s.loadPlan(ctx, tenant, planDigest)
	if err != nil {
		return repair.RepairSimulation{}, err
	}
	if saved.Plan.Digest != planDigest {
		return repair.RepairSimulation{}, ErrDataOpsArtifact
	}
	evaluated := values.NewInstant(s.now())
	currentDateTime := evaluated.Time()
	saved.Inputs.Effective = currentDateTime.Format("2006-01-02")
	saved.Inputs.KnownAt = evaluated.String()
	inputs, err := s.rebuildInputs(ctx, principal, tenant, purpose, saved.Inputs, evaluated)
	if err != nil {
		return repair.RepairSimulation{}, err
	}
	canonical, _, err := s.handlers.canonicalSide(ctx, inputs)
	if err != nil {
		return repair.RepairSimulation{}, err
	}
	observed, present, watermark, err := s.handlers.observedSide(ctx, inputs)
	if err != nil {
		return repair.RepairSimulation{}, err
	}
	current, err := dataops.DiffRecord(dataops.DiffRecordRequest{
		Canonical: canonical, Observed: observed, ObservedPresent: present, Observation: watermark,
		Fields: inputs.Fields, Authorization: inputs.Authorization, Freshness: inputs.Freshness, EvaluatedAt: evaluated,
	})
	if err != nil {
		return repair.RepairSimulation{}, err
	}
	answer, err := s.handlers.simulateRepair(ctx, repair.SimulateRepairRequest{
		Tenant: tenant, Plan: saved.Plan, CurrentDiff: current, Canonical: canonical,
		Observed: observed, ObservedPresent: present, Observation: watermark, Fields: inputs.Fields,
		Authorization: inputs.Authorization, Freshness: inputs.Freshness, EvaluatedAt: evaluated,
		AuthorityPolicyVersion: inputs.AuthorityPolicyVersion, LocalSystem: inputs.LocalSystem, ExternalSystem: inputs.ExternalSystem,
	})
	if err != nil {
		return repair.RepairSimulation{}, err
	}
	return answer.(repairSimulationAnswer).Simulation, nil
}

func (s *DataOpsService) trusted(ctx context.Context) (*trust.Principal, values.TenantId, string, time.Time, error) {
	inv, ok := transport.InvocationFromContext(ctx)
	principal, trusted := trust.FromContext(ctx)
	if !ok || !trusted || principal == nil || inv.Principal() != principal || principal.Tenant().String() != inv.TenantID() {
		return nil, "", "", time.Time{}, ErrAuthorizationDenied
	}
	return principal, principal.Tenant(), inv.Purpose(), inv.ReceivedAt(), nil
}

func (s *DataOpsService) authorize(principal *trust.Principal, tenant values.TenantId, purpose string, at values.Instant, subject values.EntityRef, fields []dataops.FieldID) (values.EntityRef, []dataops.FieldID, dataops.Authorization, error) {
	if subject.Tenant != tenant {
		return values.EntityRef{}, nil, dataops.Authorization{}, ErrAuthorizationDenied
	}
	for _, field := range fields {
		if err := field.Validate(); err != nil {
			return values.EntityRef{}, nil, dataops.Authorization{}, err
		}
	}
	peopleRead := make([]people.FieldID, 0, len(fields))
	for _, field := range fields {
		peopleRead = append(peopleRead, people.FieldID(field))
	}
	result, err := authorizeRead(principal, purpose, authorizationRequest{Subject: subject, EvaluatedAt: at, Read: peopleFields(peopleRead)})
	if err != nil {
		return values.EntityRef{}, nil, dataops.Authorization{}, err
	}
	return subject, fields, dataopsDecision(result, fields), nil
}

func (s *DataOpsService) rebuildInputs(ctx context.Context, principal *trust.Principal, tenant values.TenantId, purpose string, prior dataOpsDiffArtifact, evaluated values.Instant) (RepairInputs, error) {
	subject, fields, authorization, err := s.authorize(principal, tenant, purpose, evaluated, prior.Subject, prior.Fields)
	if err != nil {
		return RepairInputs{}, err
	}
	effective, known, _, err := prior.coordinates()
	if err != nil {
		return RepairInputs{}, err
	}
	return RepairInputs{Tenant: tenant, Subject: subject, Source: prior.Source, Fields: fields, AsOfEffective: effective,
		AsKnownAt: known, EvaluatedAt: evaluated, Authorization: authorization, Freshness: freshnessPolicy(),
		Approval: repairApprovalPolicy(), AuthorityPolicyVersion: FieldAuthorityPolicyVersion,
		LocalSystem: LocalSystem, ExternalSystem: prior.Source, PageLimit: ObservationPageLimit}, nil
}

type dataOpsDiffArtifact struct {
	Subject     values.EntityRef  `json:"subject"`
	Fields      []dataops.FieldID `json:"fields"`
	Effective   string            `json:"effective"`
	KnownAt     string            `json:"known_at"`
	EvaluatedAt string            `json:"evaluated_at"`
	Source      string            `json:"source"`
}

type dataOpsPlanArtifact struct {
	Inputs       dataOpsDiffArtifact   `json:"inputs"`
	Plan         repair.RepairPlan     `json:"plan"`
	StepPresence []dataOpsStepPresence `json:"step_presence"`
}

type dataOpsStepPresence struct{ Current, Post dataOpsPresence }
type dataOpsPresence struct {
	State  string
	Value  string
	Reason string
}

func presenceDTO(p values.Presence[string]) dataOpsPresence {
	dto := dataOpsPresence{State: p.State().String(), Reason: p.Reason()}
	if value, ok := p.Get(); ok {
		dto.Value = value
	}
	return dto
}

func (p dataOpsPresence) domain() (values.Presence[string], error) {
	state, err := values.ParsePresenceState(p.State)
	if err != nil {
		return values.Presence[string]{}, err
	}
	switch state {
	case values.PresenceValue:
		return values.Value(p.Value), nil
	case values.PresenceAbsent:
		return values.Absent[string](), nil
	case values.PresenceNull:
		return values.Null[string](), nil
	case values.PresenceUnknown:
		return values.Unknown[string](p.Reason), nil
	case values.PresenceRedacted:
		return values.Redacted[string](p.Reason), nil
	case values.PresenceUnavailable:
		return values.Unavailable[string](p.Reason), nil
	case values.PresenceNotApplicable:
		return values.NotApplicable[string](p.Reason), nil
	default:
		return values.Presence[string]{}, ErrDataOpsArtifact
	}
}

func (a dataOpsDiffArtifact) coordinates() (values.LocalDate, values.KnownAt, values.Instant, error) {
	date, err := values.ParseLocalDate(a.Effective)
	if err != nil {
		return values.LocalDate{}, values.KnownAt{}, values.Instant{}, err
	}
	knownTime, err := time.Parse(time.RFC3339Nano, a.KnownAt)
	if err != nil {
		return values.LocalDate{}, values.KnownAt{}, values.Instant{}, err
	}
	known, err := values.NewKnownAt(values.NewInstant(knownTime))
	if err != nil {
		return values.LocalDate{}, values.KnownAt{}, values.Instant{}, err
	}
	evaluatedTime, err := time.Parse(time.RFC3339Nano, a.EvaluatedAt)
	if err != nil {
		return values.LocalDate{}, values.KnownAt{}, values.Instant{}, err
	}
	return date, known, values.NewInstant(evaluatedTime), nil
}

func (s *DataOpsService) savePlan(ctx context.Context, tenant values.TenantId, digest string, inputs dataOpsDiffArtifact, plan repair.RepairPlan) error {
	artifact := dataOpsPlanArtifact{Inputs: inputs, Plan: plan, StepPresence: make([]dataOpsStepPresence, len(plan.Steps))}
	for i, step := range plan.Steps {
		artifact.StepPresence[i] = dataOpsStepPresence{Current: presenceDTO(step.ExpectedCurrent), Post: presenceDTO(step.ExpectedPost)}
	}
	return s.save(ctx, tenant, "repair_plan", digest, artifact)
}

func (s *DataOpsService) loadPlan(ctx context.Context, tenant values.TenantId, digest string) (dataOpsPlanArtifact, error) {
	var artifact dataOpsPlanArtifact
	if err := s.load(ctx, tenant, "repair_plan", digest, &artifact); err != nil {
		return dataOpsPlanArtifact{}, err
	}
	if len(artifact.Plan.Steps) != len(artifact.StepPresence) {
		return dataOpsPlanArtifact{}, ErrDataOpsArtifact
	}
	for i := range artifact.Plan.Steps {
		current, err := artifact.StepPresence[i].Current.domain()
		if err != nil {
			return dataOpsPlanArtifact{}, err
		}
		post, err := artifact.StepPresence[i].Post.domain()
		if err != nil {
			return dataOpsPlanArtifact{}, err
		}
		artifact.Plan.Steps[i].ExpectedCurrent, artifact.Plan.Steps[i].ExpectedPost = current, post
	}
	if err := artifact.Plan.Validate(); err != nil {
		return dataOpsPlanArtifact{}, fmt.Errorf("%w: invalid plan payload: %v", ErrDataOpsArtifact, err)
	}
	return artifact, nil
}

func (s *DataOpsService) save(ctx context.Context, tenant values.TenantId, kind, digest string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("app: encode DataOps artifact: %w", err)
	}
	return s.artifacts.Put(ctx, tenant, kind, digest, payload)
}

func (s *DataOpsService) load(ctx context.Context, tenant values.TenantId, kind, digest string, target any) error {
	payload, err := s.artifacts.Get(ctx, tenant, kind, digest)
	if err != nil {
		return err
	}
	if len(payload) == 0 || len(payload) > 16<<20 {
		return ErrDataOpsArtifact
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("%w: decode stored %s: %v", ErrDataOpsArtifact, kind, err)
	}
	return nil
}
