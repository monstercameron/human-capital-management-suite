package transport_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/clients"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/machine"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestTodo_REV_100_02_Integration(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	devVerifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	key, err := machine.GenerateServerKey("quota-test-key", now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := machine.NewIssuer([]machine.ServerKey{key}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	machineVerifier, err := machine.NewVerifier([]machine.ServerKey{key}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	quotaToken := func(subject, clientID, session, tokenID string) string {
		t.Helper()
		raw, err := issuer.Issue(machine.IssueRequest{
			Issuer: "https://issuer.test.hcm-next.invalid", Audience: []string{transporttest.Audience},
			Subject: subject, Client: clientID, Tenant: transporttest.Tenant, Session: session,
			Assurance: "substantial", TokenID: tokenID, Lifetime: 10 * time.Minute,
		})
		if err != nil {
			t.Fatal(err)
		}
		return "Bearer " + raw
	}
	clientAToken := quotaToken("service-subject-a", "machine-client-a", "session-a", "token-a-1")
	verifier := &trust.ServedVerifier{
		Machine: machineVerifier,
		Request: machine.VerifyRequest{Issuer: "https://issuer.test.hcm-next.invalid", Audience: []string{transporttest.Audience}},
		Dev:     devVerifier,
	}
	limiter := admission.NewCredentialLimiter()
	policy := admission.CredentialPolicy{RateLimit: 1, RateWindow: 30 * time.Second, QuotaLimit: 2, QuotaWindow: 24 * time.Hour}
	cfg := transporttest.Config(verifier, func() time.Time { return now }, "req-rev-100-02", nil)
	cfg.CredentialQuota = func(at time.Time, tenant, client string) (transport.CredentialQuotaOutcome, int, error) {
		decision, err := limiter.Admit(at, admission.CredentialIdentity{TenantID: tenant, ClientID: client}, policy)
		return transport.CredentialQuotaOutcome(decision.Outcome), decision.RetryAfter, err
	}
	intentHandler := &transporttest.IntentHandler{}
	registryHandler := &transporttest.RegistryHandler{}
	grpcServer, err := grpcserver.NewServer(grpcserver.Options{Config: cfg, Intent: intentHandler, Registry: registryHandler})
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() { grpcServer.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///rev10002",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	grpcClient := intentsv1.NewIntentServiceClient(conn)

	edgeHandler, err := edge.NewHandler(edge.Options{Config: cfg, Intent: intentHandler, Registry: registryHandler})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(edgeHandler)
	t.Cleanup(httpServer.Close)
	client := httpServer.Client()
	responseCapture := &retryAfterCapture{base: client.Transport}
	client.Transport = responseCapture
	edgeClient := clients.NewIntentClientConnect(client, httpServer.URL)
	request := func() *intentsv1.GetIntentRequest {
		return &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID}
	}
	grpcContext := func() context.Context {
		return metadata.AppendToOutgoingContext(context.Background(), "authorization", clientAToken, "x-request-id", "req-rev-100-02")
	}
	callHTTP := func(credential string) (*intentsv1.GetIntentResponse, error) {
		return edgeClient.GetIntent(context.Background(), request(), clients.WithHeader("authorization", credential), clients.WithHeader("x-request-id", "req-rev-100-02"))
	}

	if _, err := grpcClient.GetIntent(grpcContext(), request()); err != nil {
		t.Fatalf("first gRPC request: %v", err)
	}
	if _, err := callHTTP(clientAToken); err == nil {
		t.Fatal("second request on HTTP edge bypassed the shared rate limit")
	} else {
		assertQuotaWireError(t, err, "RATE_LIMITED")
		if retryAfter := responseCapture.retryAfter(); retryAfter != "30" {
			t.Fatalf("HTTP Retry-After header = %q, want 30 seconds", retryAfter)
		}
	}

	now = now.Add(time.Minute)
	if _, err := callHTTP(clientAToken); err != nil {
		t.Fatalf("request after rate window reset: %v", err)
	}
	now = now.Add(31 * time.Second)
	var trailer metadata.MD
	_, err = grpcClient.GetIntent(grpcContext(), request(), grpc.Trailer(&trailer))
	if err == nil {
		t.Fatal("third request bypassed the shared daily quota")
	}
	assertQuotaWireError(t, err, "QUOTA_EXCEEDED")
	if got := trailer.Get("Retry-After"); len(got) != 1 || got[0] == "" {
		t.Fatalf("gRPC Retry-After trailer = %v, want a positive interval", got)
	}

	rotatedToken := quotaToken("service-subject-a-rotated", "machine-client-a", "session-rotated-a", "token-a-2")
	if _, err := callHTTP(rotatedToken); err == nil {
		t.Fatal("rotated token with a distinct subject reset the machine client's exhausted quota")
	} else {
		assertQuotaWireError(t, err, "QUOTA_EXCEEDED")
	}

	otherToken := quotaToken("service-subject-b", "machine-client-b", "session-b", "token-b-1")
	if _, err := callHTTP(otherToken); err != nil {
		t.Fatalf("second application credential shared the first client's exhausted quota: %v", err)
	}
	humanTokenRaw, err := devVerifier.IssueIdentity(trust.Claims{
		Issuer: transporttest.Issuer, Audience: transporttest.Audience,
		Subject: "interactive-user", SubjectKind: "human", Tenant: transporttest.Tenant,
		AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "human-session",
		IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	humanToken := "Bearer " + humanTokenRaw
	if _, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: humanTokenRaw, Audience: transporttest.Audience}); err != nil {
		t.Fatalf("human fallback verification: %v", err)
	}
	_, err = grpcClient.GetIntent(metadata.AppendToOutgoingContext(context.Background(), "authorization", humanToken), request())
	if err != nil {
		t.Fatalf("interactive user's request was charged against the machine-client quota: %v", err)
	}

	calls := intentHandler.Calls()
	if len(calls) != 4 {
		t.Fatalf("tenant handlers saw %d calls, want the two admitted original requests plus the isolated client and human requests", len(calls))
	}
}

func TestTodo_REV_100_02_Fault(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	claims := transporttest.DefaultClaims(now)
	claims.Subject = "partner-client-fault"
	claims.SubjectKind = "integration"
	claims.ClientID = "machine-client-fault"
	token, err := transporttest.BearerToken(verifier, claims)
	if err != nil {
		t.Fatal(err)
	}
	cfg := transporttest.Config(verifier, func() time.Time { return now }, "req-rev-100-02-fault", nil)
	cfg.CredentialQuota = func(time.Time, string, string) (transport.CredentialQuotaOutcome, int, error) {
		return transport.CredentialQuotaAdmit, 0, errors.New("private limiter backing store detail")
	}
	_, _, owned := transport.PreAdmit(context.Background(), cfg,
		transport.MapMetadata{"authorization": {token}}, "/hcmnext.intents.v1.IntentService/GetIntent")
	if owned == nil || owned.Code() != envelope.CodeUnavailable || owned.ReasonRef() != "inbound_credential_quota.unavailable" || owned.CorrelationID() != "req-rev-100-02-fault" {
		t.Fatalf("limiter fault projection = %+v, want correlated UNAVAILABLE", owned)
	}
	if strings.Contains(owned.Message(), "private limiter") {
		t.Fatalf("private limiter diagnostic escaped: %q", owned.Message())
	}
}

func assertQuotaWireError(t *testing.T, err error, reason string) {
	t.Helper()
	var owned *envelope.Error
	if errors.As(err, &owned) {
		if owned.Code() != envelope.CodeResourceExhausted || owned.ReasonRef() != reason || !owned.Retryable() || owned.RetryAfter() <= 0 {
			t.Fatalf("HTTP typed quota error = %+v, want RESOURCE_EXHAUSTED/%s with retry-after", owned, reason)
		}
		return
	}
	grpcStatus, ok := status.FromError(err)
	if !ok || grpcStatus.Code() != codes.ResourceExhausted {
		t.Fatalf("gRPC quota error = %v, want RESOURCE_EXHAUSTED", err)
	}
	for _, detail := range grpcStatus.Details() {
		if owned, ok := detail.(*commonv1.ErrorDetail); ok && owned.GetCode() == commonv1.ErrorCode_ERROR_CODE_RESOURCE_EXHAUSTED && owned.GetReasonRef() == reason && owned.GetRetryable() && owned.GetRetryAfterSeconds() > 0 {
			return
		}
	}
	t.Fatalf("gRPC quota error lacks typed %s detail: %v", reason, grpcStatus.Details())
}

type retryAfterCapture struct {
	mu     sync.Mutex
	base   http.RoundTripper
	header string
}

func (c *retryAfterCapture) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := c.base.RoundTrip(request)
	if err == nil {
		c.mu.Lock()
		c.header = response.Header.Get("Retry-After")
		c.mu.Unlock()
	}
	return response, err
}

func (c *retryAfterCapture) retryAfter() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.header
}
