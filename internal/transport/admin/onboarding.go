package admin

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/onboarding"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/onboardingruns"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// onboardingServer adapts Dependencies to the generated
// adminv1.OnboardingServiceServer interface (REV-036-01). Every method is a
// thin forward: requireOperator runs first, the caller tenant must match the
// request tenant, and the remaining lines call the application
// OnboardingOperator port and map its typed result onto the wire message. No
// method here reaches into internal/connectivity/onboarding directly and no
// method decides an HCM business rule.
type onboardingServer struct {
	adminv1.UnimplementedOnboardingServiceServer

	deps Dependencies
}

// RegisterOnboarding adds hcmnext.admin.v1.OnboardingService to srv under
// the same trusted-request interceptor chain as AdminService.
func RegisterOnboarding(srv *grpc.Server, deps Dependencies) {
	adminv1.RegisterOnboardingServiceServer(srv, &onboardingServer{deps: deps})
}

func (s *onboardingServer) ops(principal *trust.Principal, inv *transport.Invocation) (onboardingruns.OnboardingOperator, *envelope.Error) {
	if s.deps.Onboarding == nil {
		return nil, envelope.New(envelope.CodeUnavailable,
			"admin.onboarding_unconfigured",
			"the onboarding operator port is not configured").
			WithCorrelation(inv.RequestID()).
			WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
	}
	return s.deps.Onboarding, nil
}

func checkOnboardingTenant(principal *trust.Principal, inv *transport.Invocation, tenantID string) *envelope.Error {
	if tenantID == "" || tenantID != principal.Tenant().String() {
		return envelope.New(envelope.CodePermissionDenied,
			"admin.onboarding_tenant_denied",
			"the request tenant does not match the caller tenant").
			WithCorrelation(inv.RequestID()).
			WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
	}
	return nil
}

func onboardingError(principal *trust.Principal, inv *transport.Invocation, err error) error {
	base := envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"}
	switch {
	case errors.Is(err, onboardingruns.ErrOnboardingRunNotFound):
		return envelope.New(envelope.CodeNotFound, "admin.onboarding_run_not_found", "no onboarding run with that id for the caller tenant").
			WithCorrelation(inv.RequestID()).WithEvidence(base)
	case errors.Is(err, onboardingruns.ErrOnboardingStateConflict):
		return envelope.New(envelope.CodeFailedPrecondition, "admin.onboarding_state_conflict", err.Error()).
			WithCorrelation(inv.RequestID()).WithEvidence(base)
	case errors.Is(err, onboardingruns.ErrOnboardingTenantMismatch):
		return envelope.New(envelope.CodePermissionDenied, "admin.onboarding_tenant_denied", err.Error()).
			WithCorrelation(inv.RequestID()).WithEvidence(base)
	case errors.Is(err, onboardingruns.ErrOnboardingConflict):
		return envelope.New(envelope.CodeAlreadyExists, "admin.onboarding_run_conflict", err.Error()).
			WithCorrelation(inv.RequestID()).WithEvidence(base)
	case errors.Is(err, onboardingruns.ErrOnboardingUnconfigured):
		return envelope.New(envelope.CodeUnavailable, "admin.onboarding_unconfigured", err.Error()).
			WithCorrelation(inv.RequestID()).WithEvidence(base)
	default:
		return envelope.Coerce(err)
	}
}

func onboardingManifestFromProto(pb *adminv1.OnboardingManifest) (onboarding.OnboardingManifest, error) {
	if pb == nil {
		return onboarding.OnboardingManifest{}, errors.New("admin: onboarding manifest is required")
	}
	version, err := connectivity.ParseVersion(pb.GetConnectorVersion())
	if err != nil {
		return onboarding.OnboardingManifest{}, err
	}
	objects := make([]connectivity.ObjectKind, 0, len(pb.GetObjects()))
	for _, name := range pb.GetObjects() {
		kind := connectivity.ObjectKind(name)
		if !kind.Valid() {
			return onboarding.OnboardingManifest{}, errors.New("admin: unknown onboarding object " + name)
		}
		objects = append(objects, kind)
	}
	pins := make(map[connectivity.ObjectKind]string, len(pb.GetSchemaVersionPins()))
	for name, ver := range pb.GetSchemaVersionPins() {
		kind := connectivity.ObjectKind(name)
		if !kind.Valid() {
			return onboarding.OnboardingManifest{}, errors.New("admin: unknown schema-pin object " + name)
		}
		pins[kind] = ver
	}
	allow := make(map[connectivity.ObjectKind][]string, len(pb.GetFieldAllowList()))
	for _, entry := range pb.GetFieldAllowList() {
		kind := connectivity.ObjectKind(entry.GetObject())
		if !kind.Valid() {
			return onboarding.OnboardingManifest{}, errors.New("admin: unknown allow-list object " + entry.GetObject())
		}
		allow[kind] = append([]string(nil), entry.GetFields()...)
	}
	population := make(map[connectivity.ObjectKind]int64, len(pb.GetExpectedPopulation()))
	for name, count := range pb.GetExpectedPopulation() {
		kind := connectivity.ObjectKind(name)
		if !kind.Valid() {
			return onboarding.OnboardingManifest{}, errors.New("admin: unknown population object " + name)
		}
		population[kind] = count
	}
	refs := make([]onboarding.ReferencePin, 0, len(pb.GetReferencePins()))
	for _, pin := range pb.GetReferencePins() {
		refs = append(refs, onboarding.ReferencePin{Name: pin.GetName(), Version: pin.GetVersion(), Digest: pin.GetDigest()})
	}
	var createdAt time.Time
	if pb.GetCreatedAt() != nil {
		createdAt = pb.GetCreatedAt().AsTime()
	}
	var wallTime time.Duration
	if pb.GetBudget() != nil {
		wallTime = time.Duration(pb.GetBudget().GetMaxWallTimeSeconds()) * time.Second
	}
	return onboarding.OnboardingManifest{
		ManifestID:         pb.GetManifestId(),
		TenantID:           pb.GetTenantId(),
		OwnerRef:           pb.GetOwnerRef(),
		SourceAuthorityRef: pb.GetSourceAuthorityRef(),
		Classification:     pb.GetClassification(),
		Residency:          pb.GetResidency(),
		RetentionPolicy:    pb.GetRetentionPolicy(),
		RollbackPolicy:     pb.GetRollbackPolicy(),
		IdempotencyKey:     pb.GetIdempotencyKey(),
		ConnectorID:        pb.GetConnectorId(),
		ConnectorVersion:   version,
		ConnectionID:       pb.GetConnectionId(),
		Objects:            objects,
		FieldAllowList:     allow,
		SchemaVersionPins:  pins,
		ExpectedPopulation: population,
		ReferencePins:      refs,
		Budget: onboarding.Budget{
			MaxPages:    pb.GetBudget().GetMaxPages(),
			MaxRecords:  pb.GetBudget().GetMaxRecords(),
			MaxBytes:    pb.GetBudget().GetMaxBytes(),
			MaxWallTime: wallTime,
		},
		CreatedAt: createdAt,
		CreatedBy: pb.GetCreatedBy(),
	}, nil
}

func onboardingExtractionMode(mode string) (connectivity.ReadMode, error) {
	switch mode {
	case "", "FULL":
		return connectivity.ReadFull, nil
	case "INCREMENTAL":
		return connectivity.ReadIncremental, nil
	case "DELTA":
		return connectivity.ReadDelta, nil
	default:
		return "", errors.New("admin: unknown extraction mode " + mode)
	}
}
func (s *onboardingServer) PublishOnboardingManifest(ctx context.Context, req *adminv1.PublishOnboardingManifestRequest) (*adminv1.PublishOnboardingManifestResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	manifest, err := onboardingManifestFromProto(req.GetManifest())
	if err != nil {
		return nil, onboardingError(principal, inv, err)
	}
	if tenantErr := checkOnboardingTenant(principal, inv, manifest.TenantID); tenantErr != nil {
		return nil, tenantErr
	}
	ops, cfgErr := s.ops(principal, inv)
	if cfgErr != nil {
		return nil, cfgErr
	}
	runID, err := ops.Publish(ctx, manifest.TenantID, onboarding.SignedManifest{
		Manifest:    manifest,
		Digest:      req.GetDigest(),
		SignerKeyID: req.GetSignerKeyId(),
		Signature:   req.GetSignature(),
	})
	if err != nil {
		return nil, onboardingError(principal, inv, err)
	}
	view, err := ops.Get(ctx, manifest.TenantID, runID)
	if err != nil {
		return nil, onboardingError(principal, inv, err)
	}
	return &adminv1.PublishOnboardingManifestResponse{RunId: runID, ManifestDigest: view.ManifestDigest}, nil
}

func (s *onboardingServer) RunOnboardingPreflight(ctx context.Context, req *adminv1.RunOnboardingPreflightRequest) (*adminv1.RunOnboardingPreflightResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	if tenantErr := checkOnboardingTenant(principal, inv, req.GetTenantId()); tenantErr != nil {
		return nil, tenantErr
	}
	ops, cfgErr := s.ops(principal, inv)
	if cfgErr != nil {
		return nil, cfgErr
	}
	res, err := ops.RunPreflight(ctx, req.GetTenantId(), req.GetRunId())
	if err != nil && res.Reason == nil {
		return nil, onboardingError(principal, inv, err)
	}
	out := &adminv1.RunOnboardingPreflightResponse{Passed: res.Passed, CheckedAt: timestamppb.New(res.CheckedAt)}
	if res.Reason != nil {
		out.Reason = res.Reason.Error()
	}
	return out, nil
}

func (s *onboardingServer) StartOnboardingExtraction(ctx context.Context, req *adminv1.StartOnboardingExtractionRequest) (*adminv1.StartOnboardingExtractionResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	if tenantErr := checkOnboardingTenant(principal, inv, req.GetTenantId()); tenantErr != nil {
		return nil, tenantErr
	}
	ops, cfgErr := s.ops(principal, inv)
	if cfgErr != nil {
		return nil, cfgErr
	}
	mode, err := onboardingExtractionMode(req.GetMode())
	if err != nil {
		return nil, onboardingError(principal, inv, err)
	}
	results, err := ops.StartExtraction(ctx, req.GetTenantId(), req.GetRunId(), onboarding.ExtractRequest{
		Mode:             mode,
		MaxPagesPerRun:   int(req.GetMaxPagesPerRun()),
		MaxRunsPerObject: int(req.GetMaxRunsPerObject()),
		PageSize:         int(req.GetPageSize()),
	})
	if err != nil {
		return nil, onboardingError(principal, inv, err)
	}
	view, err := ops.Get(ctx, req.GetTenantId(), req.GetRunId())
	if err != nil {
		return nil, onboardingError(principal, inv, err)
	}
	out := &adminv1.StartOnboardingExtractionResponse{RunId: req.GetRunId(), State: view.State}
	for _, res := range results {
		out.Outcomes = append(out.Outcomes, &adminv1.OnboardingObjectOutcome{
			Object:          string(res.Object),
			Status:          string(res.Run.Status),
			SnapshotChanged: res.SnapshotChanged,
			Pages:           int64(res.Run.Pages),
			Records:         int64(res.Run.Records),
		})
	}
	return out, nil
}

func (s *onboardingServer) GetOnboardingRun(ctx context.Context, req *adminv1.GetOnboardingRunRequest) (*adminv1.GetOnboardingRunResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	if tenantErr := checkOnboardingTenant(principal, inv, req.GetTenantId()); tenantErr != nil {
		return nil, tenantErr
	}
	ops, cfgErr := s.ops(principal, inv)
	if cfgErr != nil {
		return nil, cfgErr
	}
	view, err := ops.Get(ctx, req.GetTenantId(), req.GetRunId())
	if err != nil {
		return nil, onboardingError(principal, inv, err)
	}
	out := &adminv1.GetOnboardingRunResponse{
		RunId:            req.GetRunId(),
		TenantId:         view.TenantID,
		State:            view.State,
		ManifestDigest:   view.ManifestDigest,
		ObjectsExtracted: int32(len(view.Extraction)),
		AbortedReason:    view.AbortedReason,
	}
	if view.Preflight != nil {
		out.PreflightPassed = view.Preflight.Passed
	}
	for _, adj := range view.Adjudications {
		out.Adjudications = append(out.Adjudications, onboardingAdjudicationView(adj))
	}
	if view.Cutover != nil {
		out.CutoverOutcome = view.Cutover.Outcome
		out.CutoverFailedGate = view.Cutover.FailedGate
	}
	if view.Reconciliation != nil {
		out.ReconciliationDigest = view.Reconciliation.Digest
	}
	return out, nil
}

func onboardingAdjudicationView(adj onboarding.Adjudication) *adminv1.OnboardingAdjudicationView {
	return &adminv1.OnboardingAdjudicationView{
		Object:      string(adj.Object),
		ExternalId:  adj.ExternalID,
		Outcome:     string(adj.Outcome),
		CanonicalId: adj.CanonicalID,
		Candidates:  append([]string(nil), adj.Candidates...),
	}
}
func onboardingScopesFromProto(scopes []*adminv1.OnboardingAdjudicationScope) ([]onboardingruns.OnboardingAdjudicationScope, error) {
	out := make([]onboardingruns.OnboardingAdjudicationScope, 0, len(scopes))
	for _, scope := range scopes {
		kind := connectivity.ObjectKind(scope.GetObject())
		if !kind.Valid() {
			return nil, errors.New("admin: unknown adjudication object " + scope.GetObject())
		}
		if scope.GetPin() == nil || scope.GetSnapshot() == nil {
			return nil, errors.New("admin: adjudication scope needs a pin and a snapshot")
		}
		entries := make([]onboarding.CrosswalkEntry, 0, len(scope.GetSnapshot().GetEntries()))
		for _, entry := range scope.GetSnapshot().GetEntries() {
			entryKind := connectivity.ObjectKind(entry.GetObject())
			if !entryKind.Valid() {
				return nil, errors.New("admin: unknown crosswalk entry object " + entry.GetObject())
			}
			entries = append(entries, onboarding.CrosswalkEntry{
				Object:      entryKind,
				ExternalID:  entry.GetExternalId(),
				CanonicalID: entry.GetCanonicalId(),
				Exact:       entry.GetExact(),
			})
		}
		out = append(out, onboardingruns.OnboardingAdjudicationScope{
			Object: kind,
			Pin: onboarding.ReferencePin{
				Name:    scope.GetPin().GetName(),
				Version: scope.GetPin().GetVersion(),
				Digest:  scope.GetPin().GetDigest(),
			},
			Snapshot: onboarding.CrosswalkSnapshot{
				Name:    scope.GetSnapshot().GetName(),
				Version: scope.GetSnapshot().GetVersion(),
				Digest:  scope.GetSnapshot().GetDigest(),
				Entries: entries,
			},
			ExternalIDs: append([]string(nil), scope.GetExternalIds()...),
		})
	}
	return out, nil
}

func (s *onboardingServer) ReviewOnboardingAdjudication(ctx context.Context, req *adminv1.ReviewOnboardingAdjudicationRequest) (*adminv1.ReviewOnboardingAdjudicationResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	if tenantErr := checkOnboardingTenant(principal, inv, req.GetTenantId()); tenantErr != nil {
		return nil, tenantErr
	}
	ops, cfgErr := s.ops(principal, inv)
	if cfgErr != nil {
		return nil, cfgErr
	}
	scopes, err := onboardingScopesFromProto(req.GetScopes())
	if err != nil {
		return nil, onboardingError(principal, inv, err)
	}
	adjudications, err := ops.ReviewAdjudication(ctx, req.GetTenantId(), req.GetRunId(), scopes)
	if err != nil {
		return nil, onboardingError(principal, inv, err)
	}
	out := &adminv1.ReviewOnboardingAdjudicationResponse{}
	for _, adj := range adjudications {
		out.Adjudications = append(out.Adjudications, onboardingAdjudicationView(adj))
	}
	return out, nil
}

func onboardingEvidenceFromProto(pb *adminv1.OnboardingCutoverEvidence) (onboarding.CutoverEvidence, error) {
	if pb == nil {
		return onboarding.CutoverEvidence{}, errors.New("admin: cutover evidence is required")
	}
	var frozenAt time.Time
	if pb.GetFreeze() != nil && pb.GetFreeze().GetFrozenAt() != nil {
		frozenAt = pb.GetFreeze().GetFrozenAt().AsTime()
	}
	var decidedAt time.Time
	if pb.GetDecidedAt() != nil {
		decidedAt = pb.GetDecidedAt().AsTime()
	}
	var freeze onboarding.FreezeRecord
	var delta onboarding.DeltaRecord
	if pb.GetFreeze() != nil {
		freeze = onboarding.FreezeRecord{Token: pb.GetFreeze().GetToken(), SourceVersion: pb.GetFreeze().GetSourceVersion(), FrozenAt: frozenAt}
	}
	if pb.GetDelta() != nil {
		delta = onboarding.DeltaRecord{
			BaseVersion:    pb.GetDelta().GetBaseVersion(),
			ThroughVersion: pb.GetDelta().GetThroughVersion(),
			CommitDigest:   pb.GetDelta().GetCommitDigest(),
			Complete:       pb.GetDelta().GetComplete(),
			Failed:         int(pb.GetDelta().GetFailed()),
			Pending:        int(pb.GetDelta().GetPending()),
		}
	}
	return onboarding.CutoverEvidence{
		Tenant:                   pb.GetTenant(),
		Domain:                   pb.GetDomain(),
		SourceAuthority:          pb.GetSourceAuthority(),
		TargetAuthority:          pb.GetTargetAuthority(),
		Freeze:                   freeze,
		CurrentSourceVersion:     pb.GetCurrentSourceVersion(),
		SourceWritesSinceFreeze:  int(pb.GetSourceWritesSinceFreeze()),
		TargetWriters:            append([]string(nil), pb.GetTargetWriters()...),
		CutoverWriter:            pb.GetCutoverWriter(),
		PriorEpoch:               pb.GetPriorEpoch(),
		CurrentEpoch:             pb.GetCurrentEpoch(),
		Delta:                    delta,
		ValidationErrors:         int(pb.GetValidationErrors()),
		SimulationDigest:         pb.GetSimulationDigest(),
		SimulationZeroEffect:     pb.GetSimulationZeroEffect(),
		ApprovedSimulationDigest: pb.GetApprovedSimulationDigest(),
		ApprovedBy:               pb.GetApprovedBy(),
		RequestedBy:              pb.GetRequestedBy(),
		ReplicationLag:           time.Duration(pb.GetReplicationLagSeconds()) * time.Second,
		MaxReplicationLag:        time.Duration(pb.GetMaxReplicationLagSeconds()) * time.Second,
		ReconciliationMismatches: int(pb.GetReconciliationMismatches()),
		DecidedAt:                decidedAt,
	}, nil
}

func (s *onboardingServer) ExecuteOnboardingCutover(ctx context.Context, req *adminv1.ExecuteOnboardingCutoverRequest) (*adminv1.ExecuteOnboardingCutoverResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	if tenantErr := checkOnboardingTenant(principal, inv, req.GetTenantId()); tenantErr != nil {
		return nil, tenantErr
	}
	ops, cfgErr := s.ops(principal, inv)
	if cfgErr != nil {
		return nil, cfgErr
	}
	if s.deps.OnboardingCutoverSigner == nil {
		return nil, envelope.New(envelope.CodeUnavailable,
			"admin.onboarding_cutover_signer_unconfigured",
			"no cutover signing authority is configured").
			WithCorrelation(inv.RequestID()).
			WithEvidence(envelope.Evidence{ID: principal.EvidenceID(), Kind: "authentication"})
	}
	evidence, err := onboardingEvidenceFromProto(req.GetEvidence())
	if err != nil {
		return nil, onboardingError(principal, inv, err)
	}
	dec, err := ops.ExecuteCutover(ctx, req.GetTenantId(), req.GetRunId(), evidence, s.deps.OnboardingCutoverSigner)
	if err != nil {
		return nil, onboardingError(principal, inv, err)
	}
	out := &adminv1.ExecuteOnboardingCutoverResponse{
		Outcome:    dec.Outcome,
		FailedGate: dec.FailedGate,
		Reason:     dec.Reason,
		Authority:  dec.Authority,
	}
	if dec.Epoch != nil {
		out.EpochDigest = dec.Epoch.Digest
	}
	return out, nil
}

func (s *onboardingServer) AbortOnboardingRun(ctx context.Context, req *adminv1.AbortOnboardingRunRequest) (*adminv1.AbortOnboardingRunResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	if tenantErr := checkOnboardingTenant(principal, inv, req.GetTenantId()); tenantErr != nil {
		return nil, tenantErr
	}
	ops, cfgErr := s.ops(principal, inv)
	if cfgErr != nil {
		return nil, cfgErr
	}
	if err := ops.Abort(ctx, req.GetTenantId(), req.GetRunId(), req.GetReason()); err != nil {
		return nil, onboardingError(principal, inv, err)
	}
	view, err := ops.Get(ctx, req.GetTenantId(), req.GetRunId())
	if err != nil {
		return nil, onboardingError(principal, inv, err)
	}
	return &adminv1.AbortOnboardingRunResponse{State: view.State}, nil
}

func (s *onboardingServer) ReportOnboardingReconciliation(ctx context.Context, req *adminv1.ReportOnboardingReconciliationRequest) (*adminv1.ReportOnboardingReconciliationResponse, error) {
	principal, inv, opErr := requireOperator(ctx)
	if opErr != nil {
		return nil, opErr
	}
	if tenantErr := checkOnboardingTenant(principal, inv, req.GetTenantId()); tenantErr != nil {
		return nil, tenantErr
	}
	ops, cfgErr := s.ops(principal, inv)
	if cfgErr != nil {
		return nil, cfgErr
	}
	source := make([]onboarding.SourceRow, 0, len(req.GetSource()))
	for _, row := range req.GetSource() {
		source = append(source, onboarding.SourceRow{
			RowID:         row.GetRowId(),
			SubjectID:     row.GetSubjectId(),
			EffectiveDate: row.GetEffectiveDate(),
			Fields:        row.GetFields(),
		})
	}
	target := make([]onboarding.TargetRecord, 0, len(req.GetTarget()))
	for _, record := range req.GetTarget() {
		target = append(target, onboarding.TargetRecord{
			SubjectID:           record.GetSubjectId(),
			EffectiveDate:       record.GetEffectiveDate(),
			Fields:              record.GetFields(),
			LedgerEventRefs:     append([]string(nil), record.GetLedgerEventRefs()...),
			Observed:            record.GetObserved(),
			IrreversibleEffects: append([]string(nil), record.GetIrreversibleEffects()...),
		})
	}
	rep, err := ops.ReportReconciliation(ctx, req.GetTenantId(), req.GetRunId(), req.GetImportId(), source, target)
	if err != nil {
		return nil, onboardingError(principal, inv, err)
	}
	out := &adminv1.ReportOnboardingReconciliationResponse{Digest: rep.Digest, Counts: make(map[string]int32, len(rep.Counts))}
	for name, count := range rep.Counts {
		out.Counts[name] = int32(count)
	}
	return out, nil
}
