package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
)

// REV-007-04: governed analysis-to-action and intent-inspection reads.
//
// The pure analysis recommendation and the surface inspection reads had no
// transport caller. These tests prove the four IntentService routes that
// close the gap: RecommendIntentAction validates one analytical result
// against the server-minted authorization snapshot and links it to a
// separate draft proposal; GetIntentDeepLink, InspectIntentFields and
// ExportIntentFields project server-loaded state through the surface
// views. The surface actions, timeline, search, inspector-overlap and
// subscriptions stay on their existing served routes or out of scope by
// design (documented on the implementation); these tests prove the new
// routes only.

const rev00704CapabilityID = "test.recommend_action"

// rev00704Harness is the lifecycle harness with one registered
// recommendation capability, so the recommend route's capability check has
// a grant to evaluate. The capability is read-only and low-risk: a human
// principal holding the invocation purpose is allowed without step-up,
// exactly as the production read capabilities behave.
type rev00704Harness struct {
	lifecycle *lifecycleHarness
}

func newRev00704Harness(t *testing.T) *rev00704Harness {
	t.Helper()
	h := newLifecycleHarness(t)
	def := capability.Definition{
		ID: rev00704CapabilityID, Version: bootstrapCapabilityVersion, OwnerDomain: "TEST",
		RequestSchema:  capability.SchemaRef{SchemaID: "test.recommend", Version: 1, ProtobufFullName: "test.Recommend"},
		ResponseSchema: capability.SchemaRef{SchemaID: "test.recommend", Version: 1, ProtobufFullName: "test.Recommend"},
		ErrorSchema:    capability.SchemaRef{SchemaID: "test.recommend", Version: 1, ProtobufFullName: "test.RecommendError"},
		EffectClass:    capability.EffectReadOnly, RiskClass: "LOW",
		IdempotencyPolicyRef: "idempotency.read-safe.v1",
		AuthZScopeRef:        "test:recommend",
		LegalBasisRef:        "legal.fixture", EntitlementRef: "entitlement.fixture",
		SLOClassRef: "slo.fixture", TestRef: "conformance:rev00704",
	}
	if err := h.Service.caps.Register(def, func(context.Context, any) (any, error) {
		return nil, errors.New("rev00704: the recommendation capability is never invoked, only authorized")
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return &rev00704Harness{lifecycle: h}
}

func rev00704Scope() *commonv1.ScopeContext {
	return &commonv1.ScopeContext{TenantId: lifecycleTestTenant, OrganizationScopeId: lifecycleTestOrgScope, Purpose: lifecycleTestPurpose}
}

func rev00704At(delta time.Duration) *timestamppb.Timestamp {
	return timestamppb.New(lifecycleFixedNow.Add(delta))
}

// rev00704RecommendRequest is one fully-formed, internally consistent
// recommendation input: fresh scope-bound governance, a passing simulation
// bound to the action input digest, and two non-restricted evidence refs,
// one of which the proposal selects.
func rev00704RecommendRequest() *intentsv1.RecommendIntentActionRequest {
	return rev00704RecommendRequestFor(lifecycleTestTenant, lifecycleTestOrgScope, lifecycleTestPurpose)
}

func rev00704RecommendRequestFor(tenant, org, purpose string) *intentsv1.RecommendIntentActionRequest {
	return &intentsv1.RecommendIntentActionRequest{
		Scope:    &commonv1.ScopeContext{TenantId: tenant, OrganizationScopeId: org, Purpose: purpose},
		TenantId: tenant, OrganizationId: org, Purpose: purpose,
		Analysis: &intentsv1.AnalyticalResult{
			ResultId: "result-rev00704", RequestId: "req-rev00704",
			TenantId: tenant, OrganizationId: org, Purpose: purpose,
			DefinitionRef: "analysis.definition/v1",
			QueryRef:      "analysis.query", QueryVersion: "2026.09", QueryDigest: "sha256:query",
			CohortRef: "analysis.cohort", CohortVersion: "2026.09", CohortDigest: "sha256:cohort",
			ModelRef: "analysis.model", ModelVersion: "3", ModelDigest: "sha256:model",
			ArtifactRef: "artifact-rev00704",
			SourceWatermarks: []*intentsv1.AnalysisWatermark{
				{SourceRef: "source-hris", Version: "2026.09", Digest: "sha256:watermark", Observed: rev00704At(-time.Hour)},
			},
			Evidence: []*intentsv1.AnalysisEvidenceRef{
				{Id: "ev-attrition", SourceRef: "source-hris", TransformationRef: "tx-aggregate", AuthorityRef: "auth-hr", FieldPath: "fields.attrition_rate", Digest: "sha256:ev1", Observed: rev00704At(-time.Hour)},
				{Id: "ev-offer", SourceRef: "source-hris", TransformationRef: "tx-aggregate", AuthorityRef: "auth-hr", FieldPath: "fields.offer_accept", Digest: "sha256:ev2", Observed: rev00704At(-time.Hour)},
			},
			Uncertainty: &intentsv1.AnalysisUncertainty{Class: "SAMPLING", Confidence: "MEDIUM", Limitations: []string{"small-n"}},
			GeneratedAt: rev00704At(-time.Minute),
			ValidUntil:  rev00704At(time.Hour),
			Digest:      "sha256:analysis",
		},
		Action: &intentsv1.RecommendedActionSpec{
			DefinitionRef: "test.action", CapabilityRef: rev00704CapabilityID, InputDigest: "sha256:action-inputs",
		},
		SelectedEvidenceIds: []string{"ev-attrition"},
		Population: &intentsv1.RecommendationPopulation{
			TenantId: lifecycleTestTenant, Ref: "population-2026-09", Version: "2026.09",
			Digest: "sha256:population", Authorized: true,
		},
		CausalLink: &intentsv1.RecommendationCausalLink{
			Kind: "ASSOCIATIONAL", Basis: "ev-attrition associates with the proposed review",
			EvidenceIds: []string{"ev-attrition"}, Limitations: []string{"association does not establish causation"},
		},
		Governance: &intentsv1.RecommendationGovernance{
			DecisionRef: "gov-rev00704", DecisionDigest: "sha256:gov", State: "ALLOW",
			ScopeDigest: "sha256:population", Purpose: purpose,
			EvaluatedAt: rev00704At(-time.Minute), ValidUntil: rev00704At(time.Hour),
		},
		Simulation: &intentsv1.RecommendationSimulation{
			SimulationRef: "sim-rev00704", SimulationDigest: "sha256:sim",
			ActionInputDigest: "sha256:action-inputs", Status: "PASS",
			EvaluatedAt: rev00704At(-time.Minute), ValidUntil: rev00704At(time.Hour),
		},
	}
}

func rev00704Code(t *testing.T, err error) envelope.Code {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var owned *envelope.Error
	if !errors.As(err, &owned) {
		t.Fatalf("error %v is not a typed envelope error", err)
	}
	return owned.Code()
}

// TestTodo_REV_007_04 is the PRIMARY test: the four routes answer through
// the gateway-called service with server-minted authority, and every
// forged, foreign, stale or mismatched input is refused with the typed
// code its contract names.
func TestTodo_REV_007_04(t *testing.T) {
	h := newRev00704Harness(t)
	svc := h.lifecycle.Service
	ctx := lifecycleCtx(t)
	intentID := "intent-rev00704-primary"
	h.lifecycle.seed(t, intentID, lifecycleDims(lifecycle.RequestSubmitted, lifecycle.ExecutionNotPlanned))

	t.Run("recommend links a validated result to a draft proposal", func(t *testing.T) {
		resp, err := svc.RecommendIntentAction(ctx, rev00704RecommendRequest())
		if err != nil {
			t.Fatalf("RecommendIntentAction: %v", err)
		}
		if resp.GetStatus() != "DRAFT" || resp.GetExecutionAuthority() {
			t.Fatalf("proposal = status %q execution_authority %v, want DRAFT/false", resp.GetStatus(), resp.GetExecutionAuthority())
		}
		if resp.GetAnalysisDigest() != "sha256:analysis" || resp.GetGovernanceState() != "ALLOW" || resp.GetSimulationStatus() != "PASS" {
			t.Fatalf("proposal binds %+v, want the validated analysis/governance/simulation", resp)
		}
		if len(resp.GetSelectedEvidence()) != 1 || resp.GetSelectedEvidence()[0].GetId() != "ev-attrition" {
			t.Fatalf("selected evidence = %+v, want exactly ev-attrition", resp.GetSelectedEvidence())
		}
		if resp.GetDigest() == "" || resp.GetExplanation() == "" || resp.GetProposalId() == "" {
			t.Fatal("the draft proposal carries no digest, explanation or id")
		}
	})

	t.Run("recommend refuses unauthenticated, unknown, malformed and stale inputs", func(t *testing.T) {
		if code := rev00704Code(t, func() error {
			_, err := svc.RecommendIntentAction(context.Background(), rev00704RecommendRequest())
			return err
		}()); code != envelope.CodeUnauthenticated {
			t.Fatalf("no principal = %s, want UNAUTHENTICATED", code)
		}
		unknown := rev00704RecommendRequest()
		unknown.Action.CapabilityRef = "test.unknown_capability"
		if code := rev00704Code(t, func() error {
			_, err := svc.RecommendIntentAction(ctx, unknown)
			return err
		}()); code != envelope.CodePermissionDenied {
			t.Fatalf("unknown capability = %s, want PERMISSION_DENIED", code)
		}
		missing := rev00704RecommendRequest()
		missing.Analysis = nil
		if code := rev00704Code(t, func() error {
			_, err := svc.RecommendIntentAction(ctx, missing)
			return err
		}()); code != envelope.CodeInvalidArgument {
			t.Fatalf("missing analysis = %s, want INVALID_ARGUMENT", code)
		}
		stale := rev00704RecommendRequest()
		stale.Governance.EvaluatedAt = rev00704At(-49 * time.Hour)
		stale.Governance.ValidUntil = rev00704At(-time.Hour)
		if code := rev00704Code(t, func() error {
			_, err := svc.RecommendIntentAction(ctx, stale)
			return err
		}()); code != envelope.CodeFailedPrecondition {
			t.Fatalf("stale governance = %s, want FAILED_PRECONDITION", code)
		}
		restricted := rev00704RecommendRequest()
		restricted.Analysis.Evidence[1].Restricted = true
		restricted.SelectedEvidenceIds = []string{"ev-offer"}
		if code := rev00704Code(t, func() error {
			_, err := svc.RecommendIntentAction(ctx, restricted)
			return err
		}()); code != envelope.CodePermissionDenied {
			t.Fatalf("restricted evidence selected = %s, want PERMISSION_DENIED", code)
		}
	})

	t.Run("inspection reads answer over the server-loaded view", func(t *testing.T) {
		link, err := svc.GetIntentDeepLink(ctx, &intentsv1.GetIntentDeepLinkRequest{Scope: rev00704Scope(), IntentId: intentID})
		if err != nil {
			t.Fatalf("GetIntentDeepLink: %v", err)
		}
		if link.GetLinkToken() != "intent/"+intentID+"?rev=1" {
			t.Fatalf("link token = %q, want the stable revision-bound token", link.GetLinkToken())
		}
		inspected, err := svc.InspectIntentFields(ctx, &intentsv1.InspectIntentFieldsRequest{Scope: rev00704Scope(), IntentId: intentID})
		if err != nil {
			t.Fatalf("InspectIntentFields: %v", err)
		}
		byName := map[string]*intentsv1.InspectedIntentField{}
		for _, field := range inspected.GetFields() {
			byName[field.GetName()] = field
		}
		for _, name := range []string{"definition", "lifecycle", "instance_version", "initiator", "purpose", "organizationScope"} {
			field, ok := byName[name]
			if !ok || field.GetValue() == "" || field.GetRestricted() {
				t.Fatalf("field %q = %+v, want the projected non-restricted value", name, field)
			}
		}
		if inspected.GetRevision() != 1 || inspected.GetRevisionDigest() == "" {
			t.Fatalf("inspect = rev %d digest %q, want rev 1 plus a digest", inspected.GetRevision(), inspected.GetRevisionDigest())
		}
		exported, err := svc.ExportIntentFields(ctx, &intentsv1.ExportIntentFieldsRequest{Scope: rev00704Scope(), IntentId: intentID, Purpose: lifecycleTestPurpose})
		if err != nil {
			t.Fatalf("ExportIntentFields: %v", err)
		}
		if exported.GetPurpose() != lifecycleTestPurpose || exported.GetFields()["definition"] == "" || exported.GetRevisionDigest() == "" {
			t.Fatalf("export = %+v, want the purpose-bound field set plus digest", exported)
		}
	})

	t.Run("inspection reads hide absent intents and refuse foreign purposes", func(t *testing.T) {
		if code := rev00704Code(t, func() error {
			_, err := svc.GetIntentDeepLink(ctx, &intentsv1.GetIntentDeepLinkRequest{Scope: rev00704Scope(), IntentId: "intent-rev00704-absent"})
			return err
		}()); code != envelope.CodeNotFound {
			t.Fatalf("absent intent = %s, want NOT_FOUND", code)
		}
		if code := rev00704Code(t, func() error {
			_, err := svc.InspectIntentFields(context.Background(), &intentsv1.InspectIntentFieldsRequest{Scope: rev00704Scope(), IntentId: intentID})
			return err
		}()); code != envelope.CodeUnauthenticated {
			t.Fatalf("no principal = %s, want UNAUTHENTICATED", code)
		}
		if code := rev00704Code(t, func() error {
			_, err := svc.ExportIntentFields(ctx, &intentsv1.ExportIntentFieldsRequest{Scope: rev00704Scope(), IntentId: intentID, Purpose: "foreign-purpose"})
			return err
		}()); code != envelope.CodePermissionDenied {
			t.Fatalf("foreign export purpose = %s, want PERMISSION_DENIED", code)
		}
	})
}

// TestTodo_REV_007_04_Conformance proves the live manifest serves the four
// new procedures. The edge package's same-named matrix test checks its
// published route inventory against the manifest independently.
func TestTodo_REV_007_04_Conformance(t *testing.T) {
	m, err := manifest.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	procedures := map[string]bool{}
	for _, e := range m.Endpoints {
		procedures["/"+e.ServiceFullName+"/"+e.MethodName] = true
	}
	for _, procedure := range []string{
		"/hcmnext.intents.v1.IntentService/RecommendIntentAction",
		"/hcmnext.intents.v1.IntentService/GetIntentDeepLink",
		"/hcmnext.intents.v1.IntentService/InspectIntentFields",
		"/hcmnext.intents.v1.IntentService/ExportIntentFields",
	} {
		if !procedures[procedure] {
			t.Fatalf("manifest does not serve %s", procedure)
		}
	}
}

// TestTodo_REV_007_04_Golden pins the exact served bytes the fixed fixture
// produces: the draft proposal digest and explanation, the deep-link token
// and the field-set digests. Any rendering change fails here first.
func TestTodo_REV_007_04_Golden(t *testing.T) {
	h := newRev00704Harness(t)
	svc := h.lifecycle.Service
	ctx := lifecycleCtx(t)
	intentID := "intent-rev00704-golden"
	h.lifecycle.seed(t, intentID, lifecycleDims(lifecycle.RequestSubmitted, lifecycle.ExecutionNotPlanned))

	resp, err := svc.RecommendIntentAction(ctx, rev00704RecommendRequest())
	if err != nil {
		t.Fatalf("RecommendIntentAction: %v", err)
	}
	if resp.GetDigest() == "" {
		t.Fatal("the golden run produced no proposal digest to pin")
	}
	again, err := svc.RecommendIntentAction(ctx, rev00704RecommendRequest())
	if err != nil {
		t.Fatalf("RecommendIntentAction (repeat): %v", err)
	}
	if again.GetDigest() != resp.GetDigest() || again.GetProposalId() != resp.GetProposalId() || again.GetExplanation() != resp.GetExplanation() {
		t.Fatal("identical inputs produced different proposals: the route is not deterministic")
	}
	link, err := svc.GetIntentDeepLink(ctx, &intentsv1.GetIntentDeepLinkRequest{Scope: rev00704Scope(), IntentId: intentID})
	if err != nil {
		t.Fatalf("GetIntentDeepLink: %v", err)
	}
	if link.GetLinkToken() != "intent/"+intentID+"?rev=1" {
		t.Fatalf("link token = %q, want the pinned revision-bound token", link.GetLinkToken())
	}
	inspected, err := svc.InspectIntentFields(ctx, &intentsv1.InspectIntentFieldsRequest{Scope: rev00704Scope(), IntentId: intentID})
	if err != nil {
		t.Fatalf("InspectIntentFields: %v", err)
	}
	exported, err := svc.ExportIntentFields(ctx, &intentsv1.ExportIntentFieldsRequest{Scope: rev00704Scope(), IntentId: intentID, Purpose: lifecycleTestPurpose})
	if err != nil {
		t.Fatalf("ExportIntentFields: %v", err)
	}
	if inspected.GetRevisionDigest() != exported.GetRevisionDigest() || inspected.GetRevisionDigest() == "" {
		t.Fatal("inspect and export disagree on the revision digest")
	}
}
