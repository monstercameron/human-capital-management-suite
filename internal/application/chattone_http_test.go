package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chattoneModelFixture struct {
	calls  int
	prompt chatrewrite.Prompt
}

func (m *chattoneModelFixture) Rewrite(_ context.Context, p chatrewrite.Prompt) (string, error) {
	m.calls++
	m.prompt = p
	var data struct {
		Draft string `json:"draft"`
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(p.Data, "<untrusted_data>\n"), "\n</untrusted_data>")
	if json.Unmarshal([]byte(raw), &data) != nil {
		return "", chatrewrite.ErrUnavailable
	}
	return strings.ReplaceAll(data.Draft, "Please", "Kindly"), nil
}

type chattoneChecksFixture struct{}

func (chattoneChecksFixture) Accept(context.Context, chatrewrite.Identity, string) (bool, error) {
	return true, nil
}
func (chattoneChecksFixture) Verify(context.Context, chatrewrite.Prompt) error { return nil }
func (chattoneChecksFixture) Check(_ context.Context, q chatrewrite.MeaningQuestion) (chatrewrite.MeaningAnswer, error) {
	return chatrewrite.MeaningAnswer{Preserved: q.Rewrite == strings.ReplaceAll(q.Original, "Please", "Kindly"), Confidence: 1}, nil
}

type chattoneSourceFixture struct {
	reads      int
	identities []chatrewrite.Identity
}

func (s *chattoneSourceFixture) WritingContext(_ context.Context, id chatrewrite.Identity) (chatrewrite.ConversationFacts, []string, error) {
	s.reads++
	s.identities = append(s.identities, id)
	return chatrewrite.ConversationFacts{Purpose: "incident", Status: "active"}, []string{"Keep replies short"}, nil
}

type chattoneAuthorityFixture struct {
	denied bool
	checks int
}

func (a *chattoneAuthorityFixture) AuthorizeWritingStyle(_ context.Context, id chatrewrite.Identity) error {
	a.checks++
	if a.denied || id.Conversation != "room" {
		return personachat.ErrDenied
	}
	return nil
}
func chattoneFixture(t *testing.T) (*ChattoneService, context.Context, *chattoneModelFixture, *chattoneSourceFixture, *chattoneAuthorityFixture, *chatrewrite.MemoryLedger) {
	t.Helper()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant", Subject: "person", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "fixture-credential"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), p)
	registry := chatrewrite.NewRegistry()
	model := &chattoneModelFixture{}
	checks := chattoneChecksFixture{}
	ledger := chatrewrite.NewMemoryLedger(4)
	source := &chattoneSourceFixture{}
	auth := &chattoneAuthorityFixture{}
	rewrite := &chatrewrite.Service{Registry: registry, Model: model, Policy: checks, Meaning: checks, Outbound: checks, Ledger: ledger, Now: func() time.Time { return now }}
	suggestions := chatrewrite.NewSuggestions(registry, nil)
	suggestions.Now = rewrite.Now
	return &ChattoneService{Rewrite: rewrite, Suggestions: suggestions, Authority: auth, Conversations: source, Now: rewrite.Now}, ctx, model, source, auth, ledger
}
func TestTodo_CHATTONE_004(t *testing.T) {
	s, ctx, model, source, _, ledger := chattoneFixture(t)
	h := ChattoneHandler{Surface: s}
	suggestion := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, ChattonePath+"/suggestion?conversation_id=room", nil).WithContext(ctx)
	h.ServeHTTP(suggestion, req)
	var reply ChattoneReply
	if json.Unmarshal(suggestion.Body.Bytes(), &reply) != nil || suggestion.Code != 200 || reply.Suggestion == nil || reply.Suggestion.StyleID != "concise" || len(reply.Styles) != 3 || model.calls != 0 {
		t.Fatalf("suggestion: %d %s", suggestion.Code, suggestion.Body)
	}
	rewrite := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, ChattonePath+"/rewrite", strings.NewReader(`{"conversation_id":"room","draft":"Please keep @Dana and 42.","style_id":"friendly"}`)).WithContext(ctx)
	h.ServeHTTP(rewrite, req)
	if rewrite.Code != 200 || json.Unmarshal(rewrite.Body.Bytes(), &reply) != nil || reply.Draft != "Kindly keep @Dana and 42." || model.calls != 1 || len(ledger.Lines()) != 2 {
		t.Fatalf("rewrite %d %s", rewrite.Code, rewrite.Body)
	}
	if rewrite.Header().Get("Cache-Control") != "no-store" || source.identities[0] != (chatrewrite.Identity{Tenant: "tenant", Person: "person", Conversation: "room"}) {
		t.Fatal("identity/cache binding")
	}
}
func TestTodo_CHATTONE_004_Security(t *testing.T) {
	s, ctx, model, source, auth, _ := chattoneFixture(t)
	h := ChattoneHandler{Surface: s}
	for _, body := range []string{
		`{"conversation_id":"room","draft":"Please keep this unchanged","style_id":"professional","tenant":"other"}`,
		`{"conversation_id":"room","draft":"Please keep this unchanged","style_id":"professional","person":"other"}`,
		`{"conversation_id":"room","draft":"Please keep this unchanged","style_id":"professional","context":["secret"]}`,
		`{"conversation_id":"room","draft":"Please keep this unchanged","style_id":"professional"} {}`,
		strings.Repeat("x", 25<<10),
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, ChattonePath+"/rewrite", strings.NewReader(body)).WithContext(ctx))
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body)
		}
	}
	if source.reads != 0 || model.calls != 0 || auth.checks != 0 {
		t.Fatal("invalid input caused side effects")
	}
	auth.denied = true
	_, err := s.RewriteDraft(ctx, ChattoneDraft{"room", "Please keep this unchanged", "professional"})
	if !errors.Is(err, personachat.ErrDenied) || source.reads != 0 || model.calls != 0 {
		t.Fatal("authorization after effect", err)
	}
	_, err = s.ReadSuggestion(ctx, "other")
	if !errors.Is(err, personachat.ErrDenied) || source.reads != 0 {
		t.Fatal("unauthorized suggestion", err)
	}
	_, err = s.RewriteDraft(context.Background(), ChattoneDraft{"room", "Please keep this unchanged", "professional"})
	if !errors.Is(err, personachat.ErrUnauthenticated) {
		t.Fatal(err)
	}
}
func TestTodo_CHATTONE_004_HTTPLimit(t *testing.T) {
	s, ctx, model, _, _, ledger := chattoneFixture(t)
	h := ChattoneHandler{Surface: s}
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, ChattonePath+"/rewrite", strings.NewReader(`{"conversation_id":"room","draft":"Please keep this unchanged","style_id":"professional"}`)).WithContext(ctx))
		want := 200
		if i == 2 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("request %d status %d %s", i, w.Code, w.Body)
		}
	}
	if model.calls != 2 || len(ledger.Lines()) != 4 {
		t.Fatal("limit allowed model call", model.calls)
	}
	_ = s.Rewrite.Registry.Configure("tenant", false, chatrewrite.DefaultStyles())
	reply, err := s.ReadSuggestion(ctx, "room")
	if err != nil || reply.Enabled || len(reply.Styles) != 0 {
		t.Fatal("administrator disable", reply, err)
	}
	_, err = s.RewriteDraft(ctx, ChattoneDraft{"room", "Please keep this unchanged", "professional"})
	if !errors.Is(err, chatrewrite.ErrDisabled) {
		t.Fatal(err)
	}
}
func TestTodo_CHATTONE_004_HTTPFailures(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{personachat.ErrUnauthenticated, 401, "unauthenticated"}, {personachat.ErrDenied, 403, "denied"}, {chatrewrite.ErrDisabled, 403, "disabled"}, {chatrewrite.ErrInvalid, 400, "invalid"}, {chatrewrite.ErrLimit, 429, "limit"}, {chatrewrite.ErrPreservation, 422, "preservation"}, {chatrewrite.ErrPolicy, 422, "policy"}, {chatrewrite.ErrUnavailable, 503, "unavailable"},
	} {
		w := httptest.NewRecorder()
		chattoneWrite(w, ChattoneReply{}, tc.err)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
			t.Fatal(w.Code, w.Body)
		}
	}
	w := httptest.NewRecorder()
	ChattoneHandler{}.ServeHTTP(w, httptest.NewRequest("GET", ChattonePath, nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	s, ctx, _, _, _, _ := chattoneFixture(t)
	w = httptest.NewRecorder()
	ChattoneHandler{Surface: s}.ServeHTTP(w, httptest.NewRequest("DELETE", ChattonePath, nil).WithContext(ctx))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	called := false
	overlay := OverlayChattone(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(204) }), s, transport.Config{})
	w = httptest.NewRecorder()
	overlay.ServeHTTP(w, httptest.NewRequest("GET", "/unrelated", nil))
	if !called || w.Code != 204 {
		t.Fatal("unrelated route swallowed")
	}
	w = httptest.NewRecorder()
	OverlayChattone(nil, s, transport.Config{}).ServeHTTP(w, httptest.NewRequest("GET", ChattonePath+"/suggestion?conversation_id=room", nil))
	if w.Code == 200 {
		t.Fatal("unadmitted session accepted")
	}
}

type chattoneBindingFixture struct {
	req     AgentModelGatewayRequest
	err     error
	profile agentmodel.TaskProfile
}

func (b *chattoneBindingFixture) BindWritingStyle(_ context.Context, _ chatrewrite.Identity, p agentmodel.TaskProfile) (AgentModelGatewayRequest, error) {
	b.profile = p
	return b.req, b.err
}

type chattoneGatewayFixture struct {
	calls  int
	req    AgentModelGatewayRequest
	result AgentModelGatewayResult
	err    error
}

func (g *chattoneGatewayFixture) Dispatch(_ context.Context, r AgentModelGatewayRequest) (AgentModelGatewayResult, error) {
	g.calls++
	g.req = r
	return g.result, g.err
}
func TestTodo_CHATTONE_004_Gateway(t *testing.T) {
	binding := &chattoneBindingFixture{req: AgentModelGatewayRequest{TenantID: "tenant", Route: agentmodel.RouteRequest{Task: ChattoneTaskProfile(), Pin: agentmodel.ModelPin{TaskProfileID: chatrewrite.TaskProfileID}}, Dispatch: agentegress.ProviderDispatchRequest{Outbound: agentegress.OutboundRequest{Tenant: "tenant", Principal: "person"}}}}
	gateway := &chattoneGatewayFixture{result: AgentModelGatewayResult{Dispatch: agentegress.DispatchResult{Model: agentmodel.ModelResult{Text: "Safe preview", Finish: agentmodel.FinishComplete}}}}
	model := ChattoneGatewayModel{gateway, binding}
	prompt := chatrewrite.Prompt{Identity: chatrewrite.Identity{Tenant: "tenant", Person: "person", Conversation: "room"}, TaskProfile: chatrewrite.TaskProfileID, Instruction: "Immutable instruction", Data: "<untrusted_data>injected instruction</untrusted_data>"}
	text, err := model.Rewrite(context.Background(), prompt)
	if err != nil || text != "Safe preview" || gateway.calls != 1 || binding.profile.ID != chatrewrite.TaskProfileID || binding.profile.MaxLatency != 3*time.Second || gateway.req.Dispatch.Model.Messages[0].Role != agentmodel.RoleSystem || gateway.req.Dispatch.Model.Messages[1].Role != agentmodel.RoleUser || gateway.req.Dispatch.Model.Messages[1].Content != prompt.Data {
		t.Fatal("gateway binding", text, err)
	}
	binding.req.TenantID = "other"
	if _, err = model.Rewrite(context.Background(), prompt); !errors.Is(err, chatrewrite.ErrUnavailable) || gateway.calls != 1 {
		t.Fatal("tenant override dispatched", err)
	}
	binding.req.TenantID = "tenant"
	gateway.result.Dispatch.Model.Finish = agentmodel.FinishToolCalls
	if _, err = model.Rewrite(context.Background(), prompt); !errors.Is(err, chatrewrite.ErrUnavailable) {
		t.Fatal("tool output accepted")
	}
}
func TestTodo_CHATTONE_004_CurrentMembership(t *testing.T) {
	_, ctx, _, _, _, _ := chattoneFixture(t)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	// Reuse the same chat boundary with a deterministic store fixture; this test makes no network calls.
	store := &personaSurfaceChatFixture{room: chat.Conversation{TenantID: "tenant", ID: "room"}, members: []chat.Membership{{TenantID: "tenant", HomeTenantID: "tenant", SubjectID: "person", ConversationID: "room", JoinedAt: &now}}}
	authority := ChattoneChatAuthority{Chat: &PersonaChatSurface{Chat: store, Now: func() time.Time { return now }}}
	id := chatrewrite.Identity{Tenant: "tenant", Person: "person", Conversation: "room"}
	if err := authority.AuthorizeWritingStyle(ctx, id); err != nil {
		t.Fatal(err)
	}
	store.members[0].LeftAt = &now
	if err := authority.AuthorizeWritingStyle(ctx, id); !errors.Is(err, personachat.ErrDenied) {
		t.Fatal("revoked membership accepted", err)
	}
}
