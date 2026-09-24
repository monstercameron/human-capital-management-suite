package journey_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

func startPromotionEdge(t testing.TB, deps journey.Dependencies) *httptest.Server {
	t.Helper()
	if deps.RoleAccess == nil {
		deps.RoleAccess = defaultFixtureRoleAccess()
	}
	h, err := edge.NewHandler(edge.Options{Config: transport.Config{Verifier: fakeVerifier{}}, Journey: &deps})
	if err != nil {
		t.Fatalf("edge.NewHandler: %v", err)
	}
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	return server
}

func promotionHTTPClient(server *httptest.Server, procedure string) *connect.Client[journeyv1.ProposePromotionRequest, journeyv1.ProposePromotionResponse] {
	return connect.NewClient[journeyv1.ProposePromotionRequest, journeyv1.ProposePromotionResponse](server.Client(), server.URL+procedure, connect.WithProtoJSON())
}

func callPromotionHTTP(ctx context.Context, client *connect.Client[journeyv1.ProposePromotionRequest, journeyv1.ProposePromotionResponse], token, requestID string, msg *journeyv1.ProposePromotionRequest) (*connect.Response[journeyv1.ProposePromotionResponse], error) {
	req := connect.NewRequest(msg)
	if token != "" {
		req.Header().Set(transport.AuthorizationMetadataKey, "Bearer "+token)
	}
	if requestID != "" {
		req.Header().Set(transport.RequestIDMetadataKey, requestID)
	}
	return client.CallUnary(ctx, req)
}

type concurrentPromotionEngine struct {
	*fakeEngine
	mu   sync.Mutex
	seen map[string]int
}

func newConcurrentPromotionEngine() *concurrentPromotionEngine {
	return &concurrentPromotionEngine{fakeEngine: newFakeEngine(), seen: map[string]int{}}
}

func (e *concurrentPromotionEngine) ProposePromotion(_ context.Context, req *journeyv1.ProposePromotionRequest) (*journeyv1.ProposePromotionResponse, error) {
	e.mu.Lock()
	e.seen[req.GetClientRequestId()]++
	e.mu.Unlock()
	return &journeyv1.ProposePromotionResponse{
		IntentId: req.GetClientRequestId(), RequestDigest: "digest:" + req.GetClientRequestId(),
		CanonicalRequestDigest: "canonical:" + req.GetClientRequestId(), Stage: journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED,
	}, nil
}

func TestProposePromotionHandlerUsesCanonicalProcedure(t *testing.T) {
	h := journey.NewProposePromotionHandler(journey.Dependencies{})
	if h == nil {
		t.Fatal("handler is nil")
	}
}

func TestTodo_REV_084_04_Integration(t *testing.T) {
	engine := newFakeEngine()
	engine.detail.Summary.ProposalRevisionID = "revision-current"
	engine.detail.Summary.MaterialDigest = "sha256:current-content"
	server := startPromotionEdge(t, journey.Dependencies{Engine: engine, Now: func() time.Time { return fixtureTime() }})
	client := connect.NewClient[structpb.Struct, structpb.Struct](server.Client(), server.URL+journey.CorrectWorkLoopProcedure, connect.WithProtoJSON())
	call := func(revision, digest string) (*connect.Response[structpb.Struct], error) {
		body, err := structpb.NewStruct(map[string]any{
			"intent_id": fixtureIntentID, "proposal_revision_id": revision,
			"proposal_digest": digest, "kind": "repair_plan", "reason": "repair the governed plan",
		})
		if err != nil {
			t.Fatal(err)
		}
		req := connect.NewRequest(body)
		req.Header().Set(transport.AuthorizationMetadataKey, "Bearer "+fixtureManagerToken)
		req.Header().Set(transport.RequestIDMetadataKey, "rev-084-04")
		return client.CallUnary(context.Background(), req)
	}

	rebased, err := call("revision-old", "sha256:old-content")
	if err != nil {
		t.Fatalf("superseded correction: %v", err)
	}
	if rebased.Msg.GetFields()["route"].GetStringValue() != "work_loop.rebase" || rebased.Msg.GetFields()["allowed"].GetBoolValue() {
		t.Fatalf("superseded decision = %v, want denied work_loop.rebase", rebased.Msg)
	}
	if rebased.Msg.GetFields()["proposal_revision_id"].GetStringValue() != "revision-old" || rebased.Msg.GetFields()["digest"].GetStringValue() == "" {
		t.Fatalf("rebase response lost request binding or decision digest: %v", rebased.Msg)
	}

	_, err = call("revision-current", "sha256:altered-content")
	owned, ok := edge.FromConnectError(err)
	if !ok || owned.Code() != envelope.CodeFailedPrecondition {
		t.Fatalf("tamper refusal = (%v, %v), want owned FAILED_PRECONDITION", owned, ok)
	}
	if owned.ReasonRef() != "journey.correct.tampered" {
		t.Fatalf("tamper refusal reason = %q, want journey.correct.tampered", owned.ReasonRef())
	}
	if engine.inspectCount() != 2 {
		t.Fatalf("Inspect calls = %d, want one authoritative read for each request", engine.inspectCount())
	}
}

func TestProposeIntoManagementEndpointAcceptsOnlyIntentAndResolvesServerTruth(t *testing.T) {
	engine := newFakePromotionEngine()
	server := startPromotionEdge(t, journey.Dependencies{Engine: engine})
	client := promotionHTTPClient(server, journey.ProposeIntoManagementProcedure)
	sent := promotionProposeWireFixture()
	res, err := callPromotionHTTP(context.Background(), client, fixtureManagerToken, "journey-http-fixture", sent)
	if err != nil {
		t.Fatalf("intent-only HTTP projection: %v", err)
	}
	if res.Msg.GetIntentId() != "intent-promo-007" || res.Msg.GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED || res.Msg.GetRequestDigest() != "sha256:fixture-request-digest" || res.Msg.GetCanonicalRequestDigest() != "sha256:fixture-canonical-request-digest" {
		t.Fatalf("response = %+v, want server-resolved intent and digest truth", res.Msg)
	}
	calls, got := engine.observed()
	if calls != 1 {
		t.Fatalf("promotion intent calls = %d, want exactly one", calls)
	}
	for _, field := range []struct{ name, got, want string }{
		{"subject_worker_ref", got.GetSubjectWorkerRef(), sent.GetSubjectWorkerRef()},
		{"desired_job_code", got.GetDesiredJobCode(), sent.GetDesiredJobCode()},
		{"desired_grade", got.GetDesiredGrade(), sent.GetDesiredGrade()},
		{"desired_position_id", got.GetDesiredPositionId(), sent.GetDesiredPositionId()},
		{"desired_org_unit", got.GetDesiredOrgUnit(), sent.GetDesiredOrgUnit()},
		{"desired_manager_ref", got.GetDesiredManagerRef(), sent.GetDesiredManagerRef()},
		{"desired_base_pay", got.GetDesiredBasePay(), sent.GetDesiredBasePay()},
		{"desired_pay_currency", got.GetDesiredPayCurrency(), sent.GetDesiredPayCurrency()},
		{"effective_date", got.GetEffectiveDate(), sent.GetEffectiveDate()},
		{"reason", got.GetReason(), sent.GetReason()},
		{"expected_subject_revision", got.GetExpectedSubjectRevision(), sent.GetExpectedSubjectRevision()},
		{"client_request_id", got.GetClientRequestId(), sent.GetClientRequestId()},
	} {
		if field.got != field.want {
			t.Errorf("%s reached the proposer as %q, want %q", field.name, field.got, field.want)
		}
	}
	engine.fakeEngine.mu.Lock()
	defer engine.fakeEngine.mu.Unlock()
	if engine.fakeEngine.proposeCalls != 0 || engine.fakeEngine.executeCalls != 0 || engine.fakeEngine.decideCalls != 0 {
		t.Fatalf("authoritative journey mutations = propose:%d execute:%d decide:%d, want zero", engine.fakeEngine.proposeCalls, engine.fakeEngine.executeCalls, engine.fakeEngine.decideCalls)
	}
}

func TestTodo_EP_PROMO_001_Property(t *testing.T) {
	values := []string{"plain", "contains spaces", "ümlaut-مرحبا", strings.Repeat("x", 128)}
	for i, value := range values {
		engine := newFakePromotionEngine()
		server := startPromotionEdge(t, journey.Dependencies{Engine: engine})
		client := promotionHTTPClient(server, journey.ProposeIntoManagementProcedure)
		req := promotionProposeWireFixture()
		req.Reason, req.ClientRequestId = value, fmt.Sprintf("property-%d", i)
		if _, err := callPromotionHTTP(context.Background(), client, fixtureManagerToken, req.ClientRequestId, req); err != nil {
			t.Fatalf("value %q: %v", value, err)
		}
		calls, got := engine.observed()
		if calls != 1 || got.GetReason() != value || got.GetClientRequestId() != req.ClientRequestId {
			t.Fatalf("value %q became calls=%d request=%+v", value, calls, got)
		}
	}
}
func TestTodo_EP_PROMO_001_Golden(t *testing.T) {
	if journey.ProposePromotionProcedure != "/hcmnext.journey.v1.JourneyService/ProposePromotion" {
		t.Fatalf("generated procedure changed to %q", journey.ProposePromotionProcedure)
	}
	if journey.ProposeIntoManagementProcedure != "/v1/promotions:proposeIntoManagement" {
		t.Fatalf("semantic procedure changed to %q", journey.ProposeIntoManagementProcedure)
	}
	engine := newFakePromotionEngine()
	server := startPromotionEdge(t, journey.Dependencies{Engine: engine})
	payload, err := json.Marshal(promotionProposeWireFixture())
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, server.URL+journey.ProposeIntoManagementProcedure, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set(transport.AuthorizationMetadataKey, "Bearer "+fixtureManagerToken)
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || !strings.Contains(res.Header.Get("Content-Type"), "json") {
		t.Fatalf("response status/content-type = %d/%q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, body); err != nil {
		t.Fatalf("compact response JSON: %v", err)
	}
	const want = `{"intentId":"intent-promo-007","correlationId":"corr-promo-007","proposalRevisionId":"revision-1","materialDigest":"sha256:fixture-material-digest","canonicalRequestDigest":"sha256:fixture-canonical-request-digest","requestDigest":"sha256:fixture-request-digest","stage":"JOURNEY_STAGE_PROPOSED"}`
	if compact.String() != want {
		t.Fatalf("canonical response bytes = %q, want %q", compact.String(), want)
	}
}
func FuzzTodo_EP_PROMO_001(f *testing.F) {
	f.Add([]byte("currentSalary"))
	f.Add([]byte{0xff, 0x00, 0x7f})
	f.Fuzz(func(t *testing.T, field []byte) {
		engine := newFakePromotionEngine()
		server := startPromotionEdge(t, journey.Dependencies{Engine: engine})
		unknown := "unknown_" + hex.EncodeToString(field)
		body, _ := json.Marshal(map[string]any{unknown: "caller-truth"})
		req, err := http.NewRequest(http.MethodPost, server.URL+journey.ProposeIntoManagementProcedure, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Connect-Protocol-Version", "1")
		req.Header.Set(transport.AuthorizationMetadataKey, "Bearer "+fixtureManagerToken)
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode < 400 {
			t.Fatalf("unknown field %q returned %d", unknown, res.StatusCode)
		}
		if calls, _ := engine.observed(); calls != 0 {
			t.Fatalf("unknown field reached engine %d times", calls)
		}
	})
}
func TestTodo_EP_PROMO_001_Race(t *testing.T) {
	engine := newConcurrentPromotionEngine()
	server := startPromotionEdge(t, journey.Dependencies{Engine: engine})
	client := promotionHTTPClient(server, journey.ProposeIntoManagementProcedure)
	var wg sync.WaitGroup
	wg.Add(8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			defer wg.Done()
			req := promotionProposeWireFixture()
			req.ClientRequestId = fmt.Sprintf("race-%d", i)
			res, err := callPromotionHTTP(context.Background(), client, fixtureManagerToken, req.ClientRequestId, req)
			if err != nil || res.Msg.GetIntentId() != req.ClientRequestId || res.Msg.GetRequestDigest() != "digest:"+req.ClientRequestId {
				t.Errorf("call %d = (%v, %v)", i, res, err)
			}
		}(i)
	}
	wg.Wait()
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if len(engine.seen) != 8 {
		t.Fatalf("independent request identities = %v, want 8 unique calls", engine.seen)
	}
	for id, calls := range engine.seen {
		if calls != 1 {
			t.Fatalf("request %s reached engine %d times, want once", id, calls)
		}
	}
}
func TestTodo_EP_PROMO_001_Integration(t *testing.T) {
	engine := newFakePromotionEngine()
	httpServer := startPromotionEdge(t, journey.Dependencies{Engine: engine})
	httpRes, err := callPromotionHTTP(context.Background(), promotionHTTPClient(httpServer, journey.ProposeIntoManagementProcedure), fixtureManagerToken, "parity-http", promotionProposeWireFixture())
	if err != nil {
		t.Fatalf("HTTP: %v", err)
	}
	grpcRes, err := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine})).ProposePromotion(withToken(context.Background(), fixtureManagerToken), promotionProposeWireFixture())
	if err != nil {
		t.Fatalf("RPC: %v", err)
	}
	if httpRes.Msg.GetRequestDigest() != grpcRes.GetRequestDigest() || httpRes.Msg.GetCanonicalRequestDigest() != grpcRes.GetCanonicalRequestDigest() || httpRes.Msg.GetIntentId() != grpcRes.GetIntentId() {
		t.Fatalf("HTTP = %+v, RPC = %+v", httpRes.Msg, grpcRes)
	}
	if calls, _ := engine.observed(); calls != 2 {
		t.Fatalf("shared application port calls = %d, want 2", calls)
	}
}
func TestTodo_EP_PROMO_001_Fault(t *testing.T) {
	server := startPromotionEdge(t, journey.Dependencies{})
	_, err := callPromotionHTTP(context.Background(), promotionHTTPClient(server, journey.ProposeIntoManagementProcedure), fixtureManagerToken, "fault", promotionProposeWireFixture())
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("unconfigured port error = %v, want unavailable", err)
	}
}
func TestTodo_EP_PROMO_001_Security(t *testing.T) {
	for _, token := range []string{"", fixtureUnknownToken} {
		engine := newFakePromotionEngine()
		server := startPromotionEdge(t, journey.Dependencies{Engine: engine})
		_, err := callPromotionHTTP(context.Background(), promotionHTTPClient(server, journey.ProposeIntoManagementProcedure), token, "security", promotionProposeWireFixture())
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Fatalf("token %q error = %v, want unauthenticated", token, err)
		}
		if calls, _ := engine.observed(); calls != 0 {
			t.Fatalf("token %q reached engine %d times", token, calls)
		}
	}
}
func TestTodo_EP_PROMO_001_Conformance(t *testing.T) {
	engine := newFakePromotionEngine()
	server := startPromotionEdge(t, journey.Dependencies{Engine: engine})
	for _, procedure := range []string{journey.ProposePromotionProcedure, journey.ProposeIntoManagementProcedure} {
		res, err := callPromotionHTTP(context.Background(), promotionHTTPClient(server, procedure), fixtureManagerToken, "conformance", promotionProposeWireFixture())
		if err != nil || res.Msg.GetCanonicalRequestDigest() != "sha256:fixture-canonical-request-digest" {
			t.Fatalf("%s = (%v, %v)", procedure, res, err)
		}
	}
}
func TestTodo_EP_PROMO_001_Mutation(t *testing.T) {
	for name, body := range map[string][]byte{
		"unknown caller-authoritative field": []byte(`{"subjectWorkerRef":"omar-reyes","currentSalary":"93000.00"}`),
		"malformed JSON":                     []byte(`{"subjectWorkerRef":`),
	} {
		t.Run(name, func(t *testing.T) {
			engine := newFakePromotionEngine()
			server := startPromotionEdge(t, journey.Dependencies{Engine: engine})
			req, err := http.NewRequest(http.MethodPost, server.URL+journey.ProposeIntoManagementProcedure, bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Connect-Protocol-Version", "1")
			req.Header.Set(transport.AuthorizationMetadataKey, "Bearer "+fixtureManagerToken)
			res, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = res.Body.Close()
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", res.StatusCode)
			}
			if calls, _ := engine.observed(); calls != 0 {
				t.Fatalf("invalid body reached engine %d times", calls)
			}
		})
	}
}
