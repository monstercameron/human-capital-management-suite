package journey_test

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Fixture identities. fakeVerifier recognizes the manager and organization
// appearance administrator tokens; the unknown token remains a negative
// fixture. Every accepted token exercises the real admission pipeline, so
// every test here exercises the real transport.Admit admission pipeline
// (deadline capping, reserved-metadata screening, authentication, strict
// validation, trusted-field derivation) without standing up an identity
// provider.
const (
	fixtureTenant               = "harborcare-demo"
	fixtureManagerToken         = "test-manager-token"
	fixtureAppearanceAdminToken = "test-appearance-admin-token"
	fixtureOtherTenantToken     = "test-other-tenant-token"
	fixtureUnknownToken         = "test-unrecognized-token"
	fixtureSubject              = "manager-jane"
	fixtureOrganization         = "org:harborcare-demo:people-ops"

	fixtureIntentID = "intent-9f2a"
	fixtureWorkerID = "11111111-1111-4111-8111-111111111111"
)

// fakeVerifier is a hermetic trust.Verifier: one recognized bearer token
// standing for an ordinary authenticated user, because JourneyService
// applies no role predicate of its own.
type fakeVerifier struct{}

func (fakeVerifier) Verify(_ context.Context, cred trust.Credential) (*trust.Principal, error) {
	if cred.Token != fixtureManagerToken && cred.Token != fixtureAppearanceAdminToken && cred.Token != fixtureOtherTenantToken {
		return nil, trust.ErrInvalidCredential
	}
	subject := fixtureSubject
	roles := []string{"intent_author"}
	tenant := fixtureTenant
	if cred.Token == fixtureAppearanceAdminToken {
		subject = "appearance-admin"
		roles = append(roles, "comp_admin")
	} else if cred.Token == fixtureOtherTenantToken {
		tenant = "other-tenant"
		subject = "other-tenant-user"
	}
	now := time.Now()
	return trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               values.TenantId(tenant),
		OrganizationScopeID:  fixtureOrganization,
		Subject:              subject,
		SubjectKind:          trust.SubjectKindHuman,
		Roles:                roles,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "session-fixture",
		Purposes:             []string{"hcm_operations"},
		IssuedAt:             now.Add(-time.Minute),
		ExpiresAt:            now.Add(time.Hour),
		CredentialDigest:     "digest:" + cred.Token,
	})
}

// startTestServer builds a *grpc.Server carrying the exact shared
// trusted-request interceptor chain
// (internal/transport/grpcserver.UnaryInterceptor and
// grpcserver.StreamInterceptor, chained the way
// internal/transport/grpcserver.NewServer chains them) plus JourneyService
// registered via [journey.Register], listening on a loopback port chosen by
// the OS. It returns a ready client connection; cleanup is registered on t.
//
// Both cardinalities are installed because WatchJourney is a stream: a
// harness that chained only the unary interceptor would let the watch tests
// pass against a stream nobody admitted, which is the exact failure this
// service is not allowed to have.
func startTestServer(t *testing.T, deps journey.Dependencies) *grpc.ClientConn {
	t.Helper()
	if deps.RoleAccess == nil {
		deps.RoleAccess = defaultFixtureRoleAccess()
	}

	cfg := transport.Config{Verifier: fakeVerifier{}}
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(cfg)),
		grpc.ChainStreamInterceptor(grpcserver.StreamInterceptor(cfg)),
	)
	journey.Register(srv, deps)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		srv.Stop()
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		srv.Stop()
		_ = lis.Close()
	})
	return conn
}

// defaultFixtureRoleAccess gives legacy transport fixtures the same explicit
// page and feature grants a composed server receives from role-access
// bootstrap. Production remains fail closed when the store is absent; the
// RBAC tests use startRT2Server and continue to exercise nil, empty, and
// partially seeded stores directly.
func defaultFixtureRoleAccess() *roleAccessSpy {
	// The shared transport fixture exercises forwarding, projection, and
	// error mapping rather than the default role matrix. Give both admitted
	// fixture roles every page operation, then let the feature catalogue cap
	// each feature at the operations it actually supports. Authorization
	// behavior has dedicated fail-closed fixtures below startRT2Server.
	roles := []string{"intent_author", "comp_admin"}
	pageIDs := make(map[string]struct{})
	for _, permission := range roleaccess.DefaultPagePermissions() {
		pageIDs[permission.PageID] = struct{}{}
	}
	pages := make([]roleaccess.PagePermission, 0, len(pageIDs)*len(roles))
	for pageID := range pageIDs {
		for _, roleID := range roles {
			if pageID == roleaccess.PageJourneyDiagnostics && roleID != "comp_admin" {
				continue
			}
			pages = append(pages, roleaccess.PagePermission{
				RoleID: roleID, PageID: pageID,
				View: true, Create: true, Update: true, Delete: true,
			})
		}
	}
	registrations := productui.FlattenFeatureDefinitions()
	features := make([]roleaccess.FeatureDefinition, 0, len(registrations))
	for _, registration := range registrations {
		features = append(features, roleaccess.FeatureDefinition{
			PageID:    string(registration.Page),
			FeatureID: string(registration.Feature.ID),
			View:      registration.Feature.View,
			Create:    registration.Feature.Create,
			Update:    registration.Feature.Update,
			Delete:    registration.Feature.Delete,
		})
	}
	return &roleAccessSpy{snapshot: roleaccess.Snapshot{
		PagePermissions:    pages,
		FeaturePermissions: roleaccess.DefaultFeaturePermissions(features, pages),
	}}
}

// dialJourneyClient returns the generated Go journey client bound to conn.
func dialJourneyClient(conn *grpc.ClientConn) journeyv1.JourneyServiceClient {
	return journeyv1.NewJourneyServiceClient(conn)
}

// withToken attaches a bearer token as outgoing gRPC metadata for one client
// call, using the same metadata key transport.PreAdmit reads.
func withToken(ctx context.Context, token string) context.Context {
	if token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, "Bearer "+token)
}

// assertOwnedCode decodes err as the repository's owned error model and
// asserts its condition, which is what proves the journey surface's wire
// shape matches every other service's rather than merely producing a similar
// gRPC status code.
func assertOwnedCode(t *testing.T, err error, want envelope.Code) *envelope.Error {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	owned, ok := envelope.FromGRPC(err)
	if !ok {
		t.Fatalf("error %v did not decode as an owned envelope.Error", err)
	}
	if owned.Code() != want {
		t.Fatalf("Code() = %s, want %s (reason=%s message=%s)", owned.Code(), want, owned.ReasonRef(), owned.Message())
	}
	return owned
}

// fakeEngine is a deterministic workspace.JourneyEngine. Every method can be
// pointed at a canned failure, and the detail it answers with can be
// swapped while a WatchJourney call is in flight, which is how the change
// path is observed without a database.
type fakeEngine struct {
	mu sync.Mutex

	summaries []workspace.JourneySummary
	detail    workspace.JourneyDetail

	workers []workspace.WorkerSummary
	options workspace.WorkforceOptions

	// inspectHonorsContext makes Inspect fail once its context is done,
	// the way the real engine's transaction does.
	inspectHonorsContext bool

	listErr         error
	proposeErr      error
	inspectErr      error
	executeErr      error
	decideErr       error
	acknowledgeErr  error
	listWorkersErr  error
	createWorkerErr error
	editErr         error
	previewErr      error
	interveneErr    error

	listCalls         int
	proposeCalls      int
	inspectCalls      int
	executeCalls      int
	decideCalls       int
	listWorkersCalls  int
	createWorkerCalls int
	editCalls         int
	previewCalls      int
	interveneCalls    int

	lastProposal        workspace.ProposalInput
	lastIntentID        string
	lastDecision        workspace.Decision
	lastAcknowledgement workspace.Acknowledgement
	lastWorkerInput     workspace.WorkerInput
	lastEditInput       workspace.EditProposalInput
	lastEditExpected    uint64
	lastEditIdempotency string
	lastEditReason      string
	lastPreviewKind     workspace.JourneyInterventionKind
	lastInterventionReq workspace.JourneyInterventionRequest

	editSuccessor   workspace.JourneySummary
	editSuperseded  string
	preview         workspace.JourneyInterventionPreview
	interveneResult workspace.JourneyInterventionResult
}

var _ workspace.JourneyEngine = (*fakeEngine)(nil)

func newFakeEngine() *fakeEngine {
	return &fakeEngine{
		summaries: []workspace.JourneySummary{fixtureSummary()},
		detail:    fixtureDetail(),
		workers:   []workspace.WorkerSummary{fixtureCreatedWorker(), fixtureCorpusWorker()},
		options:   fixtureWorkforceOptions(),
	}
}

// setDetail swaps the detail every subsequent Inspect answers with.
func (f *fakeEngine) setDetail(d workspace.JourneyDetail) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detail = d
}

// inspectCount reports how many times Inspect has been called, which is how
// the watch test proves polling actually happened.
func (f *fakeEngine) inspectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inspectCalls
}

func (f *fakeEngine) ListJourneys(context.Context) ([]workspace.JourneySummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.summaries, nil
}

func (f *fakeEngine) Propose(_ context.Context, in workspace.ProposalInput) (workspace.JourneySummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.proposeCalls++
	f.lastProposal = in
	if f.proposeErr != nil {
		return workspace.JourneySummary{}, f.proposeErr
	}
	return f.summaries[0], nil
}

func (f *fakeEngine) Inspect(ctx context.Context, intentID string) (workspace.JourneyDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inspectCalls++
	f.lastIntentID = intentID
	if f.inspectErr != nil {
		return workspace.JourneyDetail{}, f.inspectErr
	}
	// The real engine opens a transaction on ctx and fails as soon as the
	// caller is gone; the fake does the same when asked to, so the watch
	// handler's own handling of that race is testable.
	if f.inspectHonorsContext && ctx.Err() != nil {
		return workspace.JourneyDetail{}, fmt.Errorf("app: journey: begin: %w", ctx.Err())
	}
	return f.detail, nil
}

func (f *fakeEngine) Execute(_ context.Context, intentID string) (workspace.JourneyDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executeCalls++
	f.lastIntentID = intentID
	if f.executeErr != nil {
		return workspace.JourneyDetail{}, f.executeErr
	}
	return f.detail, nil
}

func (f *fakeEngine) Decide(_ context.Context, intentID string, d workspace.Decision) (workspace.JourneyDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decideCalls++
	f.lastIntentID = intentID
	f.lastDecision = d
	if f.decideErr != nil {
		return workspace.JourneyDetail{}, f.decideErr
	}
	return f.detail, nil
}

func (f *fakeEngine) Acknowledge(_ context.Context, intentID string, ack workspace.Acknowledgement) (workspace.JourneyDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastIntentID = intentID
	f.lastAcknowledgement = ack
	if f.acknowledgeErr != nil {
		return workspace.JourneyDetail{}, f.acknowledgeErr
	}
	return f.detail, nil
}

func (f *fakeEngine) EditProposal(_ context.Context, intentID string, expected uint64, idempotencyKey, reason string, in workspace.EditProposalInput) (workspace.JourneySummary, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.editCalls++
	f.lastIntentID = intentID
	f.lastEditInput = in
	f.lastEditExpected = expected
	f.lastEditIdempotency = idempotencyKey
	f.lastEditReason = reason
	if f.editErr != nil {
		return workspace.JourneySummary{}, "", f.editErr
	}
	return f.editSuccessor, f.editSuperseded, nil
}

func (f *fakeEngine) PreviewIntervention(_ context.Context, intentID string, kind workspace.JourneyInterventionKind) (workspace.JourneyInterventionPreview, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.previewCalls++
	f.lastIntentID = intentID
	f.lastPreviewKind = kind
	if f.previewErr != nil {
		return workspace.JourneyInterventionPreview{}, f.previewErr
	}
	return f.preview, nil
}

func (f *fakeEngine) RequestIntervention(_ context.Context, intentID string, req workspace.JourneyInterventionRequest) (workspace.JourneyInterventionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.interveneCalls++
	f.lastIntentID = intentID
	f.lastInterventionReq = req
	if f.interveneErr != nil {
		return workspace.JourneyInterventionResult{}, f.interveneErr
	}
	return f.interveneResult, nil
}

func (f *fakeEngine) ListWorkers(context.Context) ([]workspace.WorkerSummary, workspace.WorkforceOptions, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listWorkersCalls++
	if f.listWorkersErr != nil {
		return nil, workspace.WorkforceOptions{}, f.listWorkersErr
	}
	return f.workers, f.options, nil
}

func (f *fakeEngine) CreateWorker(_ context.Context, in workspace.WorkerInput) (workspace.WorkerSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createWorkerCalls++
	f.lastWorkerInput = in
	if f.createWorkerErr != nil {
		return workspace.WorkerSummary{}, f.createWorkerErr
	}
	return f.workers[0], nil
}

// fixtureCreatedWorker is a fully populated created worker: every field set,
// including the compensation baseline only a created worker carries, so a
// conversion that dropped one is visible.
func fixtureCreatedWorker() workspace.WorkerSummary {
	return workspace.WorkerSummary{
		WorkerRef: "ada-lovelace-1a2b3c4d", WorkerID: fixtureWorkerID,
		LegalName: "Ada Lovelace", PreferredName: "Ada", WorkerNumber: "W-J1A2B3C4D",
		JobCode: "OPS-HRBP2", JobTitle: "Senior People Partner", Grade: "P2", OrgUnit: "people-ops",
		PositionID: "POS-HRBP-204", Location: "Boston, MA", PayZone: "US-EAST",
		BasePay: "90000.00", Currency: "USD", BonusTarget: "0.0500",
		HireDate: "2021-04-05", Source: workspace.WorkerSourceCreated,
		CreatedAt: fixtureTime(), ManagerRef: "manager-ada", ProfilePhotoURL: "/workspace/assets/person-ada-small.jpg",
	}
}

// fixtureCorpusWorker is a corpus worker: no compensation baseline and no
// creation instant, which is exactly what makes the two sources tell apart.
func fixtureCorpusWorker() workspace.WorkerSummary {
	return workspace.WorkerSummary{
		WorkerRef: "omar-reyes", WorkerID: fixtureWorkerID,
		LegalName: "Omar Reyes", PreferredName: "Omar", WorkerNumber: "W-1002",
		JobCode: "OPS-HRBP2", Grade: "P2", OrgUnit: "people-ops",
		PositionID: "POS-HRBP-204", Location: "Boston, MA", PayZone: "US-EAST",
		HireDate: "2019-07-15", Source: workspace.WorkerSourceCorpus,
	}
}

func fixtureWorkforceOptions() workspace.WorkforceOptions {
	return workspace.WorkforceOptions{
		JobCodes:  []string{"CLN-NURSE4", "ENG-MGR1", "ENG-SWE3", "OPS-HRBP2", "OPS-HRBP3"},
		Grades:    []string{"M1", "N4", "P2", "P3"},
		OrgUnits:  []string{"eng-platform", "people-ops"},
		PayZones:  []string{"US-EAST", "US-WEST"},
		Positions: []string{"POS-HRBP-101", "POS-HRBP-204", "POS-SWE-118"},
		Currency:  "USD",
		Placements: []workspace.WorkforcePlacementOption{
			{JobCode: "OPS-HRBP2", Grade: "P2", PayZone: "US-EAST", Currency: "USD"},
			{JobCode: "OPS-HRBP3", Grade: "P3", PayZone: "US-EAST", Currency: "USD"},
		},
		PromotionPaths: []workspace.PromotionPathOption{
			{PathRef: "path-ops-hrbp2-hrbp3", Revision: "2026.1", SourceProfileRef: "profile-ops-hrbp2", SourceJobCode: "OPS-HRBP2", SourceGrade: "P2", TargetProfileRef: "profile-ops-hrbp3", TargetJobCode: "OPS-HRBP3", TargetGrade: "P3", TargetTitle: "Senior HR Business Partner", Kind: "UPWARD", MinimumBaseIncrease: "0.0500", MaximumBaseIncrease: "0.1500", CompensationPolicyRef: "promotion-rules@2026.1", BenefitRuleRefs: []string{"professional-benefit-eligibility@2026.1"}},
		},
	}
}

// fixtureWorkerInput is a complete create form.
func fixtureWorkerInput() workspace.WorkerInput {
	return workspace.WorkerInput{
		LegalName: "Ada Lovelace", PreferredName: "Ada",
		JobCode: "OPS-HRBP2", Grade: "P2", OrgUnit: "people-ops",
		PositionID: "POS-HRBP-204", Location: "Boston, MA", PayZone: "US-EAST",
		BasePay: "90000.00", Currency: "USD", BonusTarget: "0.0500",
		HireDate: "2021-04-05", ManagerRef: "rel_mgr_1a2b3c4d",
	}
}

// fixtureTime is the base instant every fixture timestamp is derived from.
// It is UTC and whole-second so that a timestamppb round trip is exact.
func fixtureTime() time.Time { return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) }

// fixtureWorkerRef is the fixture's tenant-scoped worker reference. It must
// validate, because values.EntityRef.String returns "" for one that does not.
func fixtureWorkerRef() values.EntityRef {
	return values.EntityRef{Tenant: fixtureTenant, Kind: "worker", Id: fixtureWorkerID}
}

// fixtureSummary is a fully populated journey summary: every field set, so a
// conversion that drops one is visible in a round trip rather than hidden
// behind a zero value that happened to match.
func fixtureSummary() workspace.JourneySummary {
	return workspace.JourneySummary{
		IntentID:      fixtureIntentID,
		CorrelationID: "corr-9f2a",
		Worker:        fixtureWorkerRef(),
		WorkerName:    "Jordan Vega",
		Current: workspace.JourneyPlacement{
			JobCode: "ENG-3", Grade: "G7", PositionID: "pos-101",
			OrgUnit: "org-eng", PayZone: "zone-us-east",
		},
		Target: workspace.JourneyPlacement{
			JobCode: "ENG-4", Grade: "G8", PositionID: "pos-202",
			OrgUnit: "org-eng-platform", PayZone: "zone-us-east",
		},
		CurrentBase:        "142000.00",
		ProposedBase:       "168500.00",
		Currency:           "USD",
		EffectiveDate:      "2026-10-01",
		BusinessReason:     "sustained scope increase over three quarters",
		Stage:              workspace.JourneyStageAwaitingApproval,
		ProposalRevisionID: "revision-3",
		MaterialDigest:     "sha256:fixture-material-digest",
		InstanceID:         "instance-77",
		InstanceVersion:    4,
		CreatedAt:          fixtureTime(),
		UpdatedAt:          fixtureTime().Add(time.Hour),
	}
}

// fixtureDetail is a fully populated detail, including the sections that are
// empty until a journey has been executed, so the conversion of every one of
// them is exercised.
func fixtureDetail() workspace.JourneyDetail {
	base := fixtureTime()
	return workspace.JourneyDetail{
		Summary: fixtureSummary(),
		Findings: []workspace.JourneyFinding{
			{Severity: "INFO", Code: "budget.within_envelope", Message: "the proposed base is inside the approved envelope"},
			{Severity: "WARN", Code: "compa_ratio.high", Message: "the resulting compa-ratio is above the band midpoint"},
		},
		PlannedWrites: []string{"people.assignment", "compensation.base_pay"},
		Instance: &workspace.JourneyInstance{
			InstanceID:      "instance-77",
			InstanceVersion: 4,
			WorkflowID:      "promotion.prototype",
			WorkflowVersion: 2,
			PlanDigest:      "sha256:fixture-plan-digest",
			Status:          "PARKED",
			CurrentNodeIDs:  []string{"approve_promotion"},
			CorrelationID:   "corr-9f2a",
			CreatedAt:       base,
			StartedAt:       new(base.Add(time.Minute)),
			CompletedAt:     nil,
		},
		Nodes: []workspace.JourneyNode{{
			NodeID:      "approve_promotion",
			Attempt:     1,
			StepType:    "HUMAN_APPROVAL",
			Status:      "PARKED",
			TraceID:     "trace-1",
			StartedAt:   new(base.Add(time.Minute)),
			CompletedAt: nil,
			RecordedAt:  base.Add(2 * time.Minute),
		}},
		WorkItems: []workitem.WorkItem{fixtureWorkItem()},
		Transitions: []workspace.JourneyTransition{{
			WorkItemID: "9d3f0f2c-0000-4000-8000-000000000001",
			From:       "CREATED",
			To:         "ROUTED",
			Actor:      "system",
			Reason:     "policy route resolved",
			At:         base.Add(3 * time.Minute),
		}},
		Ledger: &workspace.JourneyLedgerEvent{
			StreamKey:      "worker/" + fixtureWorkerID,
			Sequence:       12,
			SchemaRef:      "hcmnext.people.v1.WorkerPromoted",
			Digest:         "sha256:fixture-ledger-digest",
			IdempotencyKey: "idem-9f2a",
			OccurredAt:     base.Add(4 * time.Minute),
			EffectiveAt:    base.Add(5 * time.Minute),
			RecordedAt:     base.Add(6 * time.Minute),
		},
		EvidenceIDs: []string{"ev:gateway:1", "ev:execution:2"},
		Timeline: []workspace.JourneyEvent{{
			At:     base,
			Actor:  fixtureSubject,
			Kind:   "INTENT_CREATED",
			Title:  "Promotion proposed",
			Detail: "manager submitted the proposal",
			Ref:    fixtureIntentID,
		}},
		Approver: "approver-lee",
	}
}

// fixtureWorkItem is the approval work item the instance parked on. Its
// identifiers are canonical uuids because the projection renders them
// through uuid.UUID.String.
func fixtureWorkItem() workitem.WorkItem {
	base := fixtureTime()
	item := workitem.WorkItem{
		ItemVersion: 3,
		Kind:        workitem.KindApproval,
		WorkType:    "promotion_approval",
		Status:      workitem.StatusClaimed,
		NodeID:      "approve_promotion",
		OwnerRef:    "approver-lee",
		DeadlineAt:  base.Add(48 * time.Hour),
		ClaimedBy:   "approver-lee",
		ClaimedAt:   new(base.Add(10 * time.Minute)),
		CreatedAt:   base.Add(3 * time.Minute),
	}
	item.ClaimExpiresAt = new(base.Add(70 * time.Minute))
	item.Assignment.ChosenOwner = "approver-lee"
	return item
}
