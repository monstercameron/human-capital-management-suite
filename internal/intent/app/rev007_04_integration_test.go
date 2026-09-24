package app_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

var rev00704FixedNow = time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)

func rev00704At(delta time.Duration) *timestamppb.Timestamp {
	return timestamppb.New(rev00704FixedNow.Add(delta))
}

// rev00704RecommendRequest is a fully-formed, internally consistent
// recommendation input matching the served cell's tenant, organization
// scope and purpose. The capability it names is unpublished on the served
// cell, so the route must refuse it after validating the envelope.
func rev00704RecommendRequest(tenant, org, purpose string) *intentsv1.RecommendIntentActionRequest {
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
			},
			Uncertainty: &intentsv1.AnalysisUncertainty{Class: "SAMPLING", Confidence: "MEDIUM", Limitations: []string{"small-n"}},
			GeneratedAt: rev00704At(-time.Minute),
			ValidUntil:  rev00704At(time.Hour),
			Digest:      "sha256:analysis",
		},
		Action: &intentsv1.RecommendedActionSpec{
			DefinitionRef: "test.action", CapabilityRef: "test.unpublished_capability", InputDigest: "sha256:action-inputs",
		},
		SelectedEvidenceIds: []string{"ev-attrition"},
		Population: &intentsv1.RecommendationPopulation{
			TenantId: tenant, Ref: "population-2026-09", Version: "2026.09",
			Digest: "sha256:population", Authorized: true,
		},
		CausalLink: &intentsv1.RecommendationCausalLink{
			Kind: "ASSOCIATIONAL", Basis: "ev-attrition associates with the proposed review",
			EvidenceIds: []string{"ev-attrition"},
		},
		Governance: &intentsv1.RecommendationGovernance{
			DecisionRef: "gov-rev00704", DecisionDigest: "sha256:gov", State: "ALLOW",
			ScopeDigest: tenant + "/" + org, Purpose: purpose,
			EvaluatedAt: rev00704At(-time.Minute), ValidUntil: rev00704At(time.Hour),
		},
		Simulation: &intentsv1.RecommendationSimulation{
			SimulationRef: "sim-rev00704", SimulationDigest: "sha256:sim",
			ActionInputDigest: "sha256:action-inputs", Status: "PASS",
			EvaluatedAt: rev00704At(-time.Minute), ValidUntil: rev00704At(time.Hour),
		},
	}
}

// rev00704Cell composes the production cell over real PostgreSQL like the
// journey integration tests do, and returns the composed cell plus a
// principal factory.
func rev00704Cell(t *testing.T) (*app.Cell, func(subject string) context.Context) {
	t.Helper()
	db := pgtest.New(t)
	poolURL, err := url.Parse(db.URL)
	if err != nil {
		t.Fatalf("parse pool URL: %v", err)
	}
	poolParams := poolURL.Query()
	poolParams.Set("pool_max_conns", "4")
	poolURL.RawQuery = poolParams.Encode()
	pool, err := pgxadapter.NewPool(context.Background(), poolURL.String(), map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	store, err := pgstore.New(pool)
	if err != nil {
		t.Fatalf("pgstore.New: %v", err)
	}
	if err := store.Bootstrap(context.Background(), string(fixtures.Tenant)); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	cell, err := app.NewCell(app.CellConfig{
		Store:       store,
		Verifier:    trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return nil, nil }),
		Audience:    "hcm-next-api",
		ExecutionDB: pool,
		Executor:    rev00704NoExecutor{},
		TenantUUID:  func(tenant values.TenantId) uuid.UUID { return pgstore.TenantID(string(tenant)) },
		Now:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewCell: %v", err)
	}
	principal := func(subject string) context.Context {
		p, err := trust.NewPrincipal(trust.PrincipalSpec{
			Tenant: fixtures.Tenant, Subject: subject, SubjectKind: trust.SubjectKindHuman,
			OrganizationScopeID:  "org-north-america",
			Roles:                []string{"intent_author", string(authz.RoleCompAdmin)},
			AuthorityRefs:        []string{"authority:position:vp-people"},
			Purposes:             []string{authz.PurposeCompensationReview},
			AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
			SessionRef: "session-" + subject, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
			CredentialDigest: "credential-digest-" + subject,
		})
		if err != nil {
			t.Fatalf("NewPrincipal(%s): %v", subject, err)
		}
		return trust.WithPrincipal(context.Background(), p)
	}
	return cell, principal
}

// rev00704NoExecutor satisfies ProposalExecutor so NewCell composes its
// journey engine. Nothing in this file executes a journey; every method
// refuses, so a test that reached one would fail loudly.
type rev00704NoExecutor struct{}

var errRev00704NoExecution = errors.New("rev00704: execution is not part of this fixture")

func (rev00704NoExecutor) Execute(context.Context, runtime.StartRequest) (app.ExecutionResult, error) {
	return app.ExecutionResult{}, errRev00704NoExecution
}

func (rev00704NoExecutor) Resume(context.Context, app.ExecutionResumeRequest) (app.ExecutionResult, error) {
	return app.ExecutionResult{}, errRev00704NoExecution
}

func (rev00704NoExecutor) ResumeTimer(context.Context, app.ExecutionTimerResumeRequest) (app.ExecutionResult, error) {
	return app.ExecutionResult{}, errRev00704NoExecution
}

// TestTodo_REV_007_04_Integration proves the inspection reads resolve from
// durable state on a production-composed cell: a journey-proposed intent
// answers the deep-link, inspect and export routes with a stable token,
// the projected fields and a digest, while the recommend route refuses an
// unpublished capability through the served refusal path.
func TestTodo_REV_007_04_Integration(t *testing.T) {
	cell, principal := rev00704Cell(t)
	author := principal("principal:rev00704-author")
	proposed, err := cell.Journey.Propose(author, workspace.ProposalInput{
		WorkerRef: "omar-reyes", TargetJobCode: "OPS-HRBP3", TargetGrade: "P3",
		ProposedBase: "98000.00", EffectiveDate: "2026-06-01", BusinessReason: "rev00704 fixture",
	})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	scope := &commonv1.ScopeContext{TenantId: string(fixtures.Tenant)}
	link, err := cell.Service.GetIntentDeepLink(author, &intentsv1.GetIntentDeepLinkRequest{Scope: scope, IntentId: proposed.IntentID})
	if err != nil {
		t.Fatalf("GetIntentDeepLink: %v", err)
	}
	if !strings.HasPrefix(link.GetLinkToken(), "intent/"+proposed.IntentID+"?rev=") {
		t.Fatalf("link token = %q, want the stable revision-bound token", link.GetLinkToken())
	}
	inspected, err := cell.Service.InspectIntentFields(author, &intentsv1.InspectIntentFieldsRequest{Scope: scope, IntentId: proposed.IntentID})
	if err != nil {
		t.Fatalf("InspectIntentFields: %v", err)
	}
	found := false
	for _, field := range inspected.GetFields() {
		if field.GetName() == "definition" && field.GetValue() != "" && !field.GetRestricted() {
			found = true
		}
	}
	if !found || inspected.GetRevisionDigest() == "" {
		t.Fatalf("inspect = %+v, want the projected definition field plus a digest", inspected)
	}
	exported, err := cell.Service.ExportIntentFields(author, &intentsv1.ExportIntentFieldsRequest{Scope: scope, IntentId: proposed.IntentID, Purpose: authz.PurposeCompensationReview})
	if err != nil {
		t.Fatalf("ExportIntentFields: %v", err)
	}
	if exported.GetPurpose() != authz.PurposeCompensationReview || len(exported.GetFields()) == 0 {
		t.Fatalf("export = %+v, want the purpose-bound field set", exported)
	}
	_, err = cell.Service.RecommendIntentAction(author, rev00704RecommendRequest(string(fixtures.Tenant), "org-north-america", authz.PurposeCompensationReview))
	if err == nil {
		t.Fatal("an unpublished capability was recommended")
	}
	var owned *envelope.Error
	if !errors.As(err, &owned) || owned.Code() != envelope.CodePermissionDenied {
		t.Fatalf("unpublished capability err = %v, want PERMISSION_DENIED", err)
	}
}
