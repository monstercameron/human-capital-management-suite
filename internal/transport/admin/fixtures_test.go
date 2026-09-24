package admin_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// fixtureTenant, fixtureOperatorSubject and fixtureUserSubject name the two
// bearer tokens fakeVerifier recognizes.
const (
	fixtureTenant          = "harborcare-demo"
	fixtureOperatorToken   = "test-operator-token"
	fixtureOrdinaryToken   = "test-ordinary-user-token"
	fixtureUnknownToken    = "test-unrecognized-token"
	fixtureOperatorSubject = "operator-jane"
	fixtureUserSubject     = "user-jane"
)

// fakeVerifier is a hermetic trust.Verifier recognizing exactly the three
// tokens above, so a security/integration test exercises the real
// transport.Admit admission pipeline (deadline capping, reserved-metadata
// screening, authentication, strict validation, trusted-field derivation)
// without standing up an identity provider.
type fakeVerifier struct{}

func (fakeVerifier) Verify(_ context.Context, cred trust.Credential) (*trust.Principal, error) {
	now := time.Now()
	spec := trust.PrincipalSpec{
		Tenant:               fixtureTenant,
		SubjectKind:          trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "session-fixture",
		Purposes:             []string{"operator_diagnostics"},
		IssuedAt:             now.Add(-time.Minute),
		ExpiresAt:            now.Add(time.Hour),
		CredentialDigest:     "digest:" + cred.Token,
	}
	switch cred.Token {
	case fixtureOperatorToken:
		spec.Subject = fixtureOperatorSubject
		spec.Roles = []string{admin.OperatorRole}
	case fixtureOrdinaryToken:
		spec.Subject = fixtureUserSubject
		spec.Roles = []string{"intent_author"}
	default:
		return nil, trust.ErrInvalidCredential
	}
	return trust.NewPrincipal(spec)
}

// startTestServer builds a *grpc.Server carrying the exact shared trusted-
// request interceptor chain (internal/transport/grpcserver.UnaryInterceptor)
// plus AdminService registered via [admin.Register], listening on a loopback
// port chosen by the OS. It returns a ready client connection and a cleanup
// function.
func startTestServer(t *testing.T, deps admin.Dependencies) (*grpc.ClientConn, func()) {
	t.Helper()

	cfg := transport.Config{Verifier: fakeVerifier{}}
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(cfg)))
	admin.Register(srv, deps)

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

	cleanup := func() {
		_ = conn.Close()
		srv.Stop()
		_ = lis.Close()
	}
	return conn, cleanup
}

// fakeIntentHandler is a minimal transport.IntentHandler exercising only
// ListIntents, which is all AdminService forwards to it.
type fakeIntentHandler struct {
	calls int
}

func (f *fakeIntentHandler) ListIntents(_ context.Context, req *intentsv1.ListIntentsRequest) (*intentsv1.ListIntentsResponse, error) {
	f.calls++
	return &intentsv1.ListIntentsResponse{
		Intents: nil,
		Page:    &commonv1.PageResponse{NextCursor: ""},
	}, nil
}

func (f *fakeIntentHandler) CreateIntent(context.Context, *intentsv1.CreateIntentRequest) (*intentsv1.CreateIntentResponse, error) {
	return nil, fmt.Errorf("fakeIntentHandler: CreateIntent must never be called by AdminService")
}
func (f *fakeIntentHandler) GetIntent(context.Context, *intentsv1.GetIntentRequest) (*intentsv1.GetIntentResponse, error) {
	return nil, fmt.Errorf("fakeIntentHandler: GetIntent must never be called by AdminService")
}
func (f *fakeIntentHandler) SimulateIntent(context.Context, *intentsv1.SimulateIntentRequest) (*intentsv1.SimulateIntentResponse, error) {
	return nil, fmt.Errorf("fakeIntentHandler: SimulateIntent must never be called by AdminService")
}
func (f *fakeIntentHandler) ExecuteIntent(context.Context, *intentsv1.ExecuteIntentRequest) (*intentsv1.ExecuteIntentResponse, error) {
	return nil, fmt.Errorf("fakeIntentHandler: ExecuteIntent must never be called by AdminService")
}
func (f *fakeIntentHandler) SubmitIntent(context.Context, *intentsv1.SubmitIntentRequest) (*intentsv1.SubmitIntentResponse, error) {
	return nil, fmt.Errorf("fakeIntentHandler: SubmitIntent must never be called by AdminService")
}
func (f *fakeIntentHandler) CancelIntent(context.Context, *intentsv1.CancelIntentRequest) (*intentsv1.CancelIntentResponse, error) {
	return nil, fmt.Errorf("fakeIntentHandler: CancelIntent must never be called by AdminService")
}
func (f *fakeIntentHandler) SupersedeIntent(context.Context, *intentsv1.SupersedeIntentRequest) (*intentsv1.SupersedeIntentResponse, error) {
	return nil, fmt.Errorf("fakeIntentHandler: SupersedeIntent must never be called by AdminService")
}
func (f *fakeIntentHandler) ExplainIntent(context.Context, *intentsv1.ExplainIntentRequest) (*intentsv1.ExplainIntentResponse, error) {
	return nil, fmt.Errorf("fakeIntentHandler: ExplainIntent must never be called by AdminService")
}
func (f *fakeIntentHandler) ListIntentTimeline(context.Context, *intentsv1.ListIntentTimelineRequest) (*intentsv1.ListIntentTimelineResponse, error) {
	return nil, fmt.Errorf("fakeIntentHandler: ListIntentTimeline must never be called by AdminService")
}
func (f *fakeIntentHandler) RecommendIntentAction(context.Context, *intentsv1.RecommendIntentActionRequest) (*intentsv1.RecommendIntentActionResponse, error) {
	return nil, fmt.Errorf("fakeIntentHandler: RecommendIntentAction must never be called by AdminService")
}
func (f *fakeIntentHandler) GetIntentDeepLink(context.Context, *intentsv1.GetIntentDeepLinkRequest) (*intentsv1.GetIntentDeepLinkResponse, error) {
	return nil, fmt.Errorf("fakeIntentHandler: GetIntentDeepLink must never be called by AdminService")
}
func (f *fakeIntentHandler) InspectIntentFields(context.Context, *intentsv1.InspectIntentFieldsRequest) (*intentsv1.InspectIntentFieldsResponse, error) {
	return nil, fmt.Errorf("fakeIntentHandler: InspectIntentFields must never be called by AdminService")
}
func (f *fakeIntentHandler) ExportIntentFields(context.Context, *intentsv1.ExportIntentFieldsRequest) (*intentsv1.ExportIntentFieldsResponse, error) {
	return nil, fmt.Errorf("fakeIntentHandler: ExportIntentFields must never be called by AdminService")
}

// fixtureTransactionID is the one transaction fakeTransactionHistory answers.
// It must be a canonical UUID: values.EntityId.Validate requires either a
// 36-byte UUID or a 26-byte ULID.
const fixtureTransactionID = "99999999-9999-4999-8999-999999999999"

// fixtureUnknownTransactionID and fixtureUnknownWorkerID are well-formed
// UUIDs that name no record in any fixture, for exercising the
// authorized-but-absent path.
const (
	fixtureUnknownTransactionID = "00000000-0000-4000-8000-000000000001"
	fixtureUnknownWorkerID      = "00000000-0000-4000-8000-000000000002"
)

// fakeTransactionHistory is a minimal intelligence.TransactionHistory
// answering exactly one known transaction with a REQUEST section, so
// ExplainTransaction can be exercised without a ledger.
type fakeTransactionHistory struct{}

func (fakeTransactionHistory) TransactionRecordAt(_ context.Context, q intelligence.TransactionQuery) (intelligence.TransactionRecord, error) {
	if q.Transaction.Id != fixtureTransactionID {
		return intelligence.TransactionRecord{Transaction: q.Transaction, Exists: false}, nil
	}
	watermark, err := values.NewSequenceRevision("txn-stream", 1)
	if err != nil {
		return intelligence.TransactionRecord{}, err
	}
	recordedAt, err := values.NewRecordedAt(values.NewInstant(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		return intelligence.TransactionRecord{}, err
	}
	return intelligence.TransactionRecord{
		Transaction: q.Transaction,
		Exists:      true,
		Watermark:   watermark,
		Request: intelligence.Request{
			IntentType:     "hcmnext.people.promote_worker",
			IntentVersion:  "v1",
			Family:         "people",
			Mode:           "PROPOSE_APPROVE_EXECUTE",
			Purpose:        "operator_diagnostics",
			Requester:      intelligence.PrincipalRef{Kind: "human", Id: fixtureOperatorSubject},
			RequestedAt:    recordedAt,
			RequestDigest:  "sha256:fixture-request-digest",
			ProposalDigest: "sha256:fixture-proposal-digest",
			Subjects:       []values.EntityRef{{Tenant: fixtureTenant, Kind: "worker", Id: "11111111-1111-4111-8111-111111111111"}},
		},
	}, nil
}

// dialAdminClient returns the generated Go admin client bound to conn.
func dialAdminClient(conn *grpc.ClientConn) adminv1.AdminServiceClient {
	return adminv1.NewAdminServiceClient(conn)
}

// withToken attaches a bearer token as outgoing gRPC metadata for one client
// call, using the same metadata key transport.PreAdmit reads
// (transport.AuthorizationMetadataKey).
func withToken(ctx context.Context, token string) context.Context {
	if token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, "Bearer "+token)
}
