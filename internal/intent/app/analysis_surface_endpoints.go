package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/analysis"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/surface"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// REV-007-04: governed analysis-to-action and intent-inspection reads.
//
// # What is served here, and what deliberately is not
//
// The pure `internal/intent/analysis` recommendation and the
// `internal/intent/surface` inspection reads had no transport caller:
// `RecommendAction` and the deep-link/inspector/export surface were
// exercised only by their own unit tests. This file routes exactly those
// through `transport.IntentHandler`, each entering through CAP-002's
// gateway on the single invocation path INTENT-013 proves for the other
// lifecycle methods (verified principal via [caller], tenant scope,
// purpose and capability/visibility checks before any domain call).
//
// The surface ACTIONS (cancel, supersede, correct, escalate), the timeline
// and the search stay on their existing served routes (`CancelIntent`,
// `SupersedeIntent`, `EditProposal`, `RequestJourneyIntervention`,
// `ListIntentTimeline`, `ListIntents`, `InspectJourney`/`GetIntent`):
// those are durable intent/app-backed transitions and reads, and wiring
// second writers/readers through the in-memory surface views would fork
// the lifecycle. Subscriptions stay out for the same reason in the other
// direction: a subscription id without a durable subscriber store is
// theater, and no such store exists in this release.
//
// # View projection (reviewed mapping, not a second model)
//
// The surface reads operate on server-loaded state, never on
// caller-supplied views: `loadInstance` plus `denyIntentRead` decide
// visibility exactly like `GetIntent`, and a denial hides (NOT_FOUND).
// The projection carries only instance scalars the read contract already
// discloses — definition, lifecycle request state, instance version,
// initiator, purpose, organization scope — with no field marked
// restricted: this plane's disclosure boundary is row-level
// (`denyIntentRead`), and inventing field-level restrictions stricter than
// `GetIntent` would be an inconsistent second policy. History and related
// intents stay empty on the view because `ListIntentTimeline` and the
// supersession lineage serve them durably. The authority the surface sees
// is the verified principal with the invocation purpose; export
// additionally binds the wire purpose to the invocation purpose, so an
// export labeled with a purpose the caller does not hold is refused.
//
// # Recommendation authorization (server-minted, never asserted)
//
// `RecommendAction` takes an `analysis.Authorization` snapshot the analysis
// package explicitly does not mint. The route builds it from the verified
// principal, the tenant and organization scope, the invocation purpose and
// the capability grant for the proposed action's own capability: a caller
// can only propose an action it could invoke. Requested fields echo the
// wire set and allowed evidence echoes the presented result's
// non-restricted evidence — the enforced properties are tenant, purpose,
// capability, freshness and digest shape, which the package's own checks
// verify; the snapshot binds rather than invents them. The result's own
// authorization digest must arrive empty (a client-sent citation is
// refused); the route binds the presented result to the minted snapshot
// itself, the wire analog of `SourceResult`'s stamp, so the package's
// binding check verifies the result's own scope against this decision.

// recommendationAuthTTL bounds a minted recommendation authorization
// snapshot. The snapshot is consumed by the immediate RecommendAction call,
// never stored, so the TTL only limits accidental reuse, not a grant.
const recommendationAuthTTL = 5 * time.Minute

// surfaceViewFields is the fixed field set the inspection projection
// carries. Scalar instance metadata only: no request payload parsing (that
// would be a second disclosure policy), no history (served by
// ListIntentTimeline), no relations (served by the supersession lineage).
func surfaceViewFields(inst intent.Instance, rec IntentRecord) map[string]surface.Field {
	plain := func(value string) surface.Field { return surface.Field{Value: value} }
	return map[string]surface.Field{
		"definition":        plain(string(inst.Definition.TypeID)),
		"lifecycle":         plain(inst.Lifecycle.Request.String()),
		"instance_version":  plain(fmt.Sprintf("%d", rec.InstanceVersion)),
		"initiator":         plain(inst.Initiator.PrincipalID),
		"purpose":           plain(inst.Purpose),
		"organizationScope": plain(inst.OrganizationScopeID),
	}
}

// projectSurfaceView loads one intent through the read contract and
// projects the reviewed inspection view over it.
func (s *IntentService) projectSurfaceView(ctx context.Context, principal *trust.Principal, purpose, intentID string) (surface.IntentView, *envelope.Error) {
	tenant := principal.Tenant().String()
	inst, rec, ownedErr := s.loadInstance(ctx, tenant, intentID)
	if ownedErr != nil {
		return surface.IntentView{}, ownedErr
	}
	if denyErr := s.denyIntentRead(ctx, principal, purpose, inst); denyErr != nil {
		return surface.IntentView{}, readRefusal(denyErr)
	}
	return surface.IntentView{
		ID:       inst.IntentID,
		Revision: rec.InstanceVersion,
		Status:   inst.Lifecycle.Request.String(),
		Fields:   surfaceViewFields(inst, rec),
	}, nil
}

// surfaceAuthority is the verified principal as the surface package sees
// it: row visibility already decided, purposes bound to the invocation.
func surfaceAuthority(principal *trust.Principal, purpose string) surface.Authority {
	return surface.Authority{Principal: principal.Subject(), Purposes: []string{purpose}}
}

// GetIntentDeepLink returns the stable link token for one visible intent.
func (s *IntentService) GetIntentDeepLink(ctx context.Context, req *intentsv1.GetIntentDeepLinkRequest) (*intentsv1.GetIntentDeepLinkResponse, error) {
	principal, inv, ownedErr := caller(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}
	if strings.TrimSpace(req.GetIntentId()) == "" {
		return nil, requireField("intent_id")
	}
	view, readErr := s.projectSurfaceView(ctx, principal, purposeOf(principal, inv), req.GetIntentId())
	if readErr != nil {
		return nil, readErr
	}
	token, err := surface.DeepLink(&view, surfaceAuthority(principal, purposeOf(principal, inv)))
	if err != nil {
		return nil, readRefusal(err)
	}
	return &intentsv1.GetIntentDeepLinkResponse{IntentId: view.ID, LinkToken: token}, nil
}

// InspectIntentFields shows the authorized field set with restriction
// markers and the current revision digest.
func (s *IntentService) InspectIntentFields(ctx context.Context, req *intentsv1.InspectIntentFieldsRequest) (*intentsv1.InspectIntentFieldsResponse, error) {
	principal, inv, ownedErr := caller(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}
	if strings.TrimSpace(req.GetIntentId()) == "" {
		return nil, requireField("intent_id")
	}
	view, readErr := s.projectSurfaceView(ctx, principal, purposeOf(principal, inv), req.GetIntentId())
	if readErr != nil {
		return nil, readErr
	}
	fields, revisionDigest, err := surface.Inspector(&view, surfaceAuthority(principal, purposeOf(principal, inv)))
	if err != nil {
		return nil, readRefusal(err)
	}
	out := &intentsv1.InspectIntentFieldsResponse{IntentId: view.ID, Revision: view.Revision, RevisionDigest: revisionDigest}
	for _, field := range fields {
		out.Fields = append(out.Fields, &intentsv1.InspectedIntentField{Name: field.Name, Value: field.Value, Restricted: field.Restricted})
	}
	return out, nil
}

// ExportIntentFields renders the purpose-bound authorized field set. The
// wire purpose must equal the invocation purpose: an export labeled with
// a purpose the caller does not hold is refused rather than relabeled.
func (s *IntentService) ExportIntentFields(ctx context.Context, req *intentsv1.ExportIntentFieldsRequest) (*intentsv1.ExportIntentFieldsResponse, error) {
	principal, inv, ownedErr := caller(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}
	if strings.TrimSpace(req.GetIntentId()) == "" {
		return nil, requireField("intent_id")
	}
	if strings.TrimSpace(req.GetPurpose()) == "" {
		return nil, requireField("purpose")
	}
	purpose := purposeOf(principal, inv)
	if req.GetPurpose() != purpose {
		return nil, authorizationRefusal(fmt.Errorf("export purpose %q is not the invocation purpose %q", req.GetPurpose(), purpose))
	}
	view, readErr := s.projectSurfaceView(ctx, principal, purpose, req.GetIntentId())
	if readErr != nil {
		return nil, readErr
	}
	exported, err := surface.Export(&view, surfaceAuthority(principal, purpose), req.GetPurpose())
	if err != nil {
		if errors.Is(err, surface.ErrNotAuthorized) {
			return nil, authorizationRefusal(err)
		}
		return nil, readRefusal(err)
	}
	_, revisionDigest, err := surface.Inspector(&view, surfaceAuthority(principal, purpose))
	if err != nil {
		return nil, readRefusal(err)
	}
	return &intentsv1.ExportIntentFieldsResponse{
		IntentId: view.ID, Purpose: req.GetPurpose(), Revision: view.Revision,
		Fields: exported, RevisionDigest: revisionDigest,
	}, nil
}

// analysisTime converts a wire timestamp, refusing the missing or
// unrepresentable instant rather than defaulting it.
func analysisTime(ts *timestamppb.Timestamp, field string) (time.Time, *envelope.Error) {
	if ts == nil {
		return time.Time{}, requireField(field)
	}
	if err := ts.CheckValid(); err != nil {
		return time.Time{}, requireField(field)
	}
	return ts.AsTime(), nil
}

// analysisResultFromProto maps the wire lineage envelope onto the package
// type. It carries lineage only; raw values have no wire field to arrive
// in, which is what makes smuggling one a shape error rather than a
// policy decision.
func analysisResultFromProto(msg *intentsv1.AnalyticalResult) (analysis.AnalyticalResult, *envelope.Error) {
	if msg == nil {
		return analysis.AnalyticalResult{}, requireField("analysis")
	}
	generatedAt, err := analysisTime(msg.GetGeneratedAt(), "analysis.generated_at")
	if err != nil {
		return analysis.AnalyticalResult{}, err
	}
	validUntil, err := analysisTime(msg.GetValidUntil(), "analysis.valid_until")
	if err != nil {
		return analysis.AnalyticalResult{}, err
	}
	out := analysis.AnalyticalResult{
		ResultID: msg.GetResultId(), RequestID: msg.GetRequestId(),
		TenantID: msg.GetTenantId(), OrganizationID: msg.GetOrganizationId(), Purpose: msg.GetPurpose(),
		DefinitionRef: msg.GetDefinitionRef(),
		QueryRef:      msg.GetQueryRef(), QueryVersion: msg.GetQueryVersion(), QueryDigest: msg.GetQueryDigest(),
		CohortRef: msg.GetCohortRef(), CohortVersion: msg.GetCohortVersion(), CohortDigest: msg.GetCohortDigest(),
		ModelRef: msg.GetModelRef(), ModelVersion: msg.GetModelVersion(), ModelDigest: msg.GetModelDigest(),
		ArtifactRef: msg.GetArtifactRef(), Digest: msg.GetDigest(),
		AuthorizationDigest: msg.GetAuthorizationDigest(),
		GeneratedAt:         generatedAt.UTC(), ValidUntil: validUntil.UTC(),
	}
	for _, watermark := range msg.GetSourceWatermarks() {
		observed, err := analysisTime(watermark.GetObserved(), "analysis.source_watermarks.observed")
		if err != nil {
			return analysis.AnalyticalResult{}, err
		}
		out.SourceWatermarks = append(out.SourceWatermarks, analysis.Watermark{
			SourceRef: watermark.GetSourceRef(), Version: watermark.GetVersion(),
			Digest: watermark.GetDigest(), Observed: observed.UTC(),
		})
	}
	for _, evidence := range msg.GetEvidence() {
		observed, err := analysisTime(evidence.GetObserved(), "analysis.evidence.observed")
		if err != nil {
			return analysis.AnalyticalResult{}, err
		}
		out.Evidence = append(out.Evidence, analysis.EvidenceRef{
			ID: evidence.GetId(), SourceRef: evidence.GetSourceRef(),
			TransformationRef: evidence.GetTransformationRef(), AuthorityRef: evidence.GetAuthorityRef(),
			FieldPath: evidence.GetFieldPath(), Digest: evidence.GetDigest(), Observed: observed.UTC(),
			RequiredAuthorityRef: evidence.GetRequiredAuthorityRef(), Restricted: evidence.GetRestricted(),
		})
	}
	if uncertainty := msg.GetUncertainty(); uncertainty != nil {
		out.Uncertainty = analysis.Uncertainty{
			Class: uncertainty.GetClass(), Confidence: uncertainty.GetConfidence(),
			Limitations: append([]string(nil), uncertainty.GetLimitations()...),
		}
	}
	return out, nil
}

// recommendAuthorization mints the snapshot RecommendAction validates
// against, from the verified principal and the capability grant for the
// proposed action's own capability. Nothing in it is caller-asserted:
// tenant and principal come from the credential, organization scope from
// the credential's scope, purpose from the invocation, and the decision
// reference and digest from the registry record the lookup returned.
func recommendAuthorization(principal *trust.Principal, purpose, organizationID string, rec capability.Record, evidenceIDs []string, now time.Time) analysis.Authorization {
	allowedEvidence := make([]string, 0, len(evidenceIDs))
	for _, id := range evidenceIDs {
		if id != "" {
			allowedEvidence = append(allowedEvidence, id)
		}
	}
	sort.Strings(allowedEvidence)
	return analysis.Authorization{
		Allowed:         true,
		TenantID:        principal.Tenant().String(),
		OrganizationID:  organizationID,
		PrincipalRef:    principal.Subject(),
		DecisionRef:     rec.Definition.ID,
		DecisionDigest:  rec.Digest,
		Purpose:         purpose,
		AllowedFields:   []string{"*"},
		AllowedEvidence: allowedEvidence,
		AuthorityRefs:   []string{rec.Definition.ID},
		ValidUntil:      now.Add(recommendationAuthTTL).UTC(),
	}
}

// RecommendIntentAction validates one analytical result against the
// server-minted authorization snapshot and links it to a separate draft
// proposal for the requested action. It writes nothing: the proposal is
// an in-memory DRAFT with execution authority always false, exactly as
// the analysis package guarantees.
func (s *IntentService) RecommendIntentAction(ctx context.Context, req *intentsv1.RecommendIntentActionRequest) (*intentsv1.RecommendIntentActionResponse, error) {
	principal, inv, ownedErr := caller(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}
	tenant := principal.Tenant().String()
	purpose := purposeOf(principal, inv)
	switch {
	case strings.TrimSpace(req.GetTenantId()) == "" || req.GetTenantId() != tenant:
		return nil, requireField("tenant_id")
	case strings.TrimSpace(req.GetOrganizationId()) == "" || req.GetOrganizationId() != principal.OrganizationScopeID():
		return nil, requireField("organization_id")
	case strings.TrimSpace(req.GetPurpose()) == "" || req.GetPurpose() != purpose:
		return nil, requireField("purpose")
	case req.GetAction() == nil || strings.TrimSpace(req.GetAction().GetCapabilityRef()) == "":
		return nil, requireField("action.capability_ref")
	case req.GetPopulation() == nil:
		return nil, requireField("population")
	case req.GetGovernance() == nil:
		return nil, requireField("governance")
	case req.GetSimulation() == nil:
		return nil, requireField("simulation")
	}
	result, mapErr := analysisResultFromProto(req.GetAnalysis())
	if mapErr != nil {
		return nil, mapErr
	}
	// The caller proposes under a capability it must itself hold: the
	// lookup and the allow decision below are the server's, never the
	// caller's assertion.
	rec, found := s.caps.Lookup(capability.Key{ID: req.GetAction().GetCapabilityRef(), Version: bootstrapCapabilityVersion})
	if !found {
		return nil, envelope.New(envelope.CodePermissionDenied, reasonAuthorizationDenied,
			"the caller is not authorized to act under this capability").
			WithViolation("action.capability_ref", "no capability is published at this identifier", "capability.published")
	}
	if decision := authorize(principal, purpose, rec.Definition); decision.Decision != capability.Allow {
		reason := decision.Reason
		if reason == "" {
			reason = "the principal is not authorized for this capability under the resolved purpose"
		}
		return nil, envelope.New(envelope.CodePermissionDenied, reasonAuthorizationDenied,
			"the caller is not authorized to act under this capability").
			WithViolation("action.capability_ref", reason, "capability_gateway."+string(decision.Decision))
	}
	now := s.clock().Time()
	// The result must not cite an authorization: the route is the sole
	// minter of the snapshot, exactly as SourceResult stamps what it
	// produces. A client-sent citation is either stale or forged, and the
	// server has no served route that would let a caller legitimately learn
	// the registry digest in advance.
	if result.AuthorizationDigest != "" {
		return nil, envelope.New(envelope.CodeInvalidArgument, reasonRequestRejected,
			"the result must not carry an authorization digest; the route mints the authorization snapshot").
			WithViolation("analysis.authorization_digest", "a wire result carries no authority of its own", rulePhaseCeiling)
	}
	allowedEvidence := make([]string, 0, len(result.Evidence))
	for _, evidence := range result.Evidence {
		if !evidence.Restricted {
			allowedEvidence = append(allowedEvidence, evidence.ID)
		}
	}
	governanceEvaluatedAt, mapErr := analysisTime(req.GetGovernance().GetEvaluatedAt(), "governance.evaluated_at")
	if mapErr != nil {
		return nil, mapErr
	}
	governanceValidUntil, mapErr := analysisTime(req.GetGovernance().GetValidUntil(), "governance.valid_until")
	if mapErr != nil {
		return nil, mapErr
	}
	simulationEvaluatedAt, mapErr := analysisTime(req.GetSimulation().GetEvaluatedAt(), "simulation.evaluated_at")
	if mapErr != nil {
		return nil, mapErr
	}
	simulationValidUntil, mapErr := analysisTime(req.GetSimulation().GetValidUntil(), "simulation.valid_until")
	if mapErr != nil {
		return nil, mapErr
	}
	auth := recommendAuthorization(principal, purpose, req.GetOrganizationId(), rec, allowedEvidence, now)
	// Bind the presented result to this decision before validating, the
	// wire analog of SourceResult's stamp: the package's binding check then
	// verifies the result's own tenant, organization and purpose against
	// the minted snapshot instead of comparing two caller assertions.
	result.AuthorizationDigest = auth.DecisionDigest
	recommendation := analysis.RecommendationRequest{
		Analysis:      result,
		Authorization: auth,
		Action: analysis.ActionSpec{
			DefinitionRef: req.GetAction().GetDefinitionRef(),
			CapabilityRef: req.GetAction().GetCapabilityRef(),
			InputDigest:   req.GetAction().GetInputDigest(),
		},
		SelectedEvidence: append([]string(nil), req.GetSelectedEvidenceIds()...),
		Population: analysis.Population{
			TenantID: req.GetPopulation().GetTenantId(), Ref: req.GetPopulation().GetRef(),
			Version: req.GetPopulation().GetVersion(), Digest: req.GetPopulation().GetDigest(),
			Authorized: req.GetPopulation().GetAuthorized(),
		},
		CausalLink: analysis.CausalLink{
			Kind: req.GetCausalLink().GetKind(), Basis: req.GetCausalLink().GetBasis(),
			EvidenceIDs: append([]string(nil), req.GetCausalLink().GetEvidenceIds()...),
			Confounders: append([]string(nil), req.GetCausalLink().GetConfounders()...),
			Limitations: append([]string(nil), req.GetCausalLink().GetLimitations()...),
		},
		Governance: analysis.GovernanceEvidence{
			DecisionRef: req.GetGovernance().GetDecisionRef(), DecisionDigest: req.GetGovernance().GetDecisionDigest(),
			State: req.GetGovernance().GetState(), ScopeDigest: req.GetGovernance().GetScopeDigest(),
			Purpose:     req.GetGovernance().GetPurpose(),
			EvaluatedAt: governanceEvaluatedAt.UTC(), ValidUntil: governanceValidUntil.UTC(),
		},
		Simulation: analysis.SimulationEvidence{
			SimulationRef: req.GetSimulation().GetSimulationRef(), SimulationDigest: req.GetSimulation().GetSimulationDigest(),
			ActionInputDigest: req.GetSimulation().GetActionInputDigest(), Status: req.GetSimulation().GetStatus(),
			EvaluatedAt: simulationEvaluatedAt.UTC(), ValidUntil: simulationValidUntil.UTC(),
		},
		Now: now,
	}
	proposal, err := analysis.RecommendAction(recommendation)
	if err != nil {
		return nil, analysisRefusal(err)
	}
	out := &intentsv1.RecommendIntentActionResponse{
		ProposalId: proposal.ProposalID, Status: proposal.Status, Family: proposal.Family,
		TenantId: proposal.TenantID, OrganizationId: proposal.OrganizationID,
		Action: &intentsv1.RecommendedActionSpec{
			DefinitionRef: proposal.Action.DefinitionRef,
			CapabilityRef: proposal.Action.CapabilityRef,
			InputDigest:   proposal.Action.InputDigest,
		},
		AnalysisId: proposal.AnalysisID, AnalysisDigest: proposal.AnalysisDigest,
		GovernanceState: proposal.Governance.State, SimulationStatus: proposal.Simulation.Status,
		ExecutionAuthority: proposal.ExecutionAuthority, Digest: proposal.Digest,
		Explanation: analysis.Explain(proposal),
	}
	for _, evidence := range proposal.SelectedEvidence {
		out.SelectedEvidence = append(out.SelectedEvidence, &intentsv1.RecommendedActionEvidence{
			Id: evidence.ID, SourceRef: evidence.SourceRef, TransformationRef: evidence.TransformationRef,
			AuthorityRef: evidence.AuthorityRef, FieldPath: evidence.FieldPath,
			Digest: evidence.Digest, Observed: timestamppb.New(evidence.Observed),
		})
	}
	return out, nil
}

// analysisRefusal projects an analysis or recommendation failure onto the
// typed envelope vocabulary. Malformed inputs are INVALID_ARGUMENT;
// expired results, missing governance and missing simulation are
// FAILED_PRECONDITION (the facts are right but the preconditions are not
// met); authorization failures stay PERMISSION_DENIED; anything else is
// the cell's fault.
func analysisRefusal(err error) *envelope.Error {
	switch {
	case errors.Is(err, analysis.ErrInvalidRequest) || errors.Is(err, analysis.ErrInvalidResult) ||
		errors.Is(err, analysis.ErrInvalidRecommendation):
		return envelope.New(envelope.CodeInvalidArgument, reasonRequestRejected,
			"the request is malformed or structurally invalid").WithDiagnostic(err)
	case errors.Is(err, analysis.ErrUnauthorized):
		return authorizationRefusal(err)
	case errors.Is(err, analysis.ErrStale) || errors.Is(err, analysis.ErrGovernanceRequired) ||
		errors.Is(err, analysis.ErrSimulationRequired) || errors.Is(err, analysis.ErrRestrictedEvidence):
		return envelope.New(envelope.CodeFailedPrecondition, reasonGovernanceRequired,
			"a precondition for the operation is not met").WithDiagnostic(err)
	default:
		return envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(err)
	}
}
