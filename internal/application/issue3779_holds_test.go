package application

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatauthority"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// issue3779Conversations lets the caller open "general" and "ops" only.
type issue3779Conversations struct {
	chatcore.ConversationService
}

func (issue3779Conversations) GetConversation(_ context.Context, r chatcore.GetConversationRequest) (chatcore.Conversation, error) {
	switch r.ConversationID {
	case "general", "ops":
		return chatcore.Conversation{ID: r.ConversationID, TenantID: r.TenantID, Name: "Name of " + r.ConversationID, Kind: chatcore.PrivateChannel, Revision: 1}, nil
	case "forbidden":
		return chatcore.Conversation{}, chatcore.ErrPermissionDenied
	}
	return chatcore.Conversation{}, chatcore.ErrNotFound
}

func (issue3779Conversations) ListConversations(_ context.Context, r chatcore.ListConversationsRequest) (chatcore.ListConversationsResponse, error) {
	return chatcore.ListConversationsResponse{Conversations: []chatcore.Conversation{
		{ID: "general", TenantID: r.TenantID, Name: "General", Kind: chatcore.PublicChannel},
		{ID: "ops", TenantID: r.TenantID, Name: "Ops", Kind: chatcore.PrivateChannel},
		{ID: "old", TenantID: r.TenantID, Name: "Old", Archived: true},
	}}, nil
}

func issue3779Extensions(t *testing.T, role string) (*ChatExtensions, context.Context, *chatrecords.MemoryRepository) {
	t.Helper()
	at := time.Now().UTC()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: "person", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "s", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "d"})
	if err != nil {
		t.Fatal(err)
	}
	repo := chatrecords.NewMemoryRepository()
	records := &chatrecords.Service{Repo: repo, Auth: ChatRecordAuthority{}}
	return &ChatExtensions{Conversations: issue3779Conversations{}, Records: records, Grants: NewChatCompanyGrants(chatAdminFacts{role: role}, chatauthority.New(nil))}, trust.WithPrincipal(context.Background(), principal), repo
}

func TestIssue3779_AdminPlacesListsAndReleasesAHold_Integration(t *testing.T) {
	x, ctx, repo := issue3779Extensions(t, "hcm_admin")
	p := chatcore.Principal{TenantID: "host", SubjectID: "person"}
	placed, err := x.PlaceHold(ctx, p, "general", "Matter 7", "Preserve for the claim")
	if err != nil || !placed.Active || placed.ID == "" || placed.ConversationName != "Name of general" || !placed.PlacedByMe {
		t.Fatalf("placed=%+v err=%v", placed, err)
	}
	list, err := x.ListHolds(ctx, p)
	if err != nil || len(list.Holds) != 1 || list.Holds[0].ID != placed.ID || list.Holds[0].ConversationName != "General" {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if len(list.Conversations) != 2 {
		t.Fatalf("the form offers the openable, non-archived conversations only: %+v", list.Conversations)
	}
	state, err := x.ConversationHold(ctx, p, "general")
	if err != nil || !state.Held || state.Count != 1 {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	if other, err := x.ConversationHold(ctx, p, "ops"); err != nil || other.Held {
		t.Fatalf("ops must not be held: %+v err=%v", other, err)
	}
	released, err := x.ReleaseHold(ctx, p, placed.ID, "Claim settled")
	if err != nil || released.Active || !released.ReleasedByMe || released.ReleaseReason != "Claim settled" {
		t.Fatalf("released=%+v err=%v", released, err)
	}
	if state, _ = x.ConversationHold(ctx, p, "general"); state.Held {
		t.Fatalf("a released hold must clear the badge: %+v", state)
	}
	events, _ := repo.Events(ctx, "host")
	if len(events) != 2 || events[0].Action != "hold.place" || events[1].Action != "hold.release" || events[0].PolicyEvidence != "chat:tenant-admin:host" {
		t.Fatalf("every hold change must be audited: %+v", events)
	}
}

func TestIssue3779_NonAdminCannotUseHolds_Security(t *testing.T) {
	x, ctx, repo := issue3779Extensions(t, "employee")
	p := chatcore.Principal{TenantID: "host", SubjectID: "person"}
	if _, err := x.PlaceHold(ctx, p, "general", "", "reason"); err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("an employee placed a hold: %v", err)
	}
	if _, err := x.ListHolds(ctx, p); err == nil {
		t.Fatal("an employee listed holds")
	}
	if _, err := x.ReleaseHold(ctx, p, "h", "reason"); err == nil {
		t.Fatal("an employee released a hold")
	}
	if _, err := x.ConversationHold(ctx, p, "general"); err == nil {
		t.Fatal("an employee learned whether a conversation is held")
	}
	if holds, _ := repo.Holds(ctx, "host"); len(holds) != 0 {
		t.Fatalf("a refused call wrote a hold: %+v", holds)
	}
	admin, actx, _ := issue3779Extensions(t, "hcm_admin")
	if _, err := admin.PlaceHold(actx, chatcore.Principal{TenantID: "elsewhere", SubjectID: "person"}, "general", "", "reason"); err == nil {
		t.Fatal("a principal naming another tenant placed a hold")
	}
	if _, err := admin.PlaceHold(actx, chatcore.Principal{TenantID: "host", SubjectID: "someone-else"}, "general", "", "reason"); err == nil {
		t.Fatal("a principal that is not the verified caller placed a hold")
	}
	var none *ChatExtensions
	if _, err := none.ListHolds(actx, p); err == nil {
		t.Fatal("an unavailable service answered")
	}
}

func TestIssue3779_HoldOnAConversationTheAdminCannotOpenIsNotFound_Security(t *testing.T) {
	x, ctx, repo := issue3779Extensions(t, "hcm_admin")
	p := chatcore.Principal{TenantID: "host", SubjectID: "person"}
	for _, id := range []string{"forbidden", "missing", "  "} {
		if _, err := x.PlaceHold(ctx, p, id, "", "reason"); err != chatcore.ErrNotFound {
			t.Fatalf("%q: err=%v, a conversation the caller cannot open must read as not found", id, err)
		}
		if _, err := x.ConversationHold(ctx, p, id); err != chatcore.ErrNotFound {
			t.Fatalf("%q: state err=%v", id, err)
		}
	}
	if holds, _ := repo.Holds(ctx, "host"); len(holds) != 0 {
		t.Fatalf("a refused place wrote a hold: %+v", holds)
	}
	if _, err := x.PlaceHold(ctx, p, "general", "", "   "); err == nil {
		t.Fatal("a hold needs a reason")
	}
}

func issue3779Post(t *testing.T, h http.Handler, ctx context.Context, action string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, ChatHoldsPath+action, bytes.NewReader(raw))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r.WithContext(ctx))
	return w
}

func TestIssue3779_HoldsHTTP(t *testing.T) {
	x, ctx, _ := issue3779Extensions(t, "hcm_admin")
	ctx = transport.WithInvocation(ctx, &transport.Invocation{})
	h := ChatHoldsHTTP{Service: x}

	w := issue3779Post(t, h, ctx, "place", map[string]any{"conversation_id": "ops", "matter": "M-1", "reason": "Legal matter"})
	var placed ChatHoldView
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &placed) != nil || !placed.Active || placed.ConversationID != "ops" {
		t.Fatalf("place: %d %s", w.Code, w.Body)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("a hold answer must not be cached: %v", w.Header())
	}
	if strings.Contains(w.Body.String(), "person") {
		t.Fatalf("a person id leaked: %s", w.Body)
	}
	w = issue3779Post(t, h, ctx, "list", map[string]any{})
	var list ChatHoldsList
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Holds) != 1 {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	w = issue3779Post(t, h, ctx, "conversation", map[string]any{"conversation_id": "ops"})
	var state ChatHoldState
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &state) != nil || !state.Held {
		t.Fatalf("conversation: %d %s", w.Code, w.Body)
	}
	for name, c := range map[string]struct {
		action string
		body   map[string]any
		code   int
	}{
		"no reason":          {"place", map[string]any{"conversation_id": "ops"}, 400},
		"unknown field":      {"place", map[string]any{"conversation_id": "ops", "reason": "r", "x": 1}, 400},
		"unopenable":         {"place", map[string]any{"conversation_id": "forbidden", "reason": "r"}, 404},
		"release no reason":  {"release", map[string]any{"id": placed.ID}, 400},
		"release unknown":    {"release", map[string]any{"id": "nope", "reason": "r"}, 404},
		"unknown action":     {"nope", map[string]any{}, 404},
		"conversation empty": {"conversation", map[string]any{}, 404},
	} {
		if got := issue3779Post(t, h, ctx, c.action, c.body); got.Code != c.code {
			t.Errorf("%s: %d %s, want %d", name, got.Code, got.Body, c.code)
		}
	}
	if got := issue3779Post(t, h, ctx, "release", map[string]any{"id": placed.ID, "reason": "done"}); got.Code != 200 {
		t.Fatalf("release: %d %s", got.Code, got.Body)
	}
	if got := issue3779Post(t, h, ctx, "release", map[string]any{"id": placed.ID, "reason": "again"}); got.Code != 409 {
		t.Fatalf("second release: %d", got.Code)
	}
	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest(http.MethodGet, ChatHoldsPath+"list", nil).WithContext(ctx))
	if get.Code != 405 {
		t.Fatalf("GET: %d", get.Code)
	}
	noCaller := httptest.NewRecorder()
	h.ServeHTTP(noCaller, httptest.NewRequest(http.MethodPost, ChatHoldsPath+"list", strings.NewReader("{}")))
	if noCaller.Code != 403 && noCaller.Code != 401 {
		t.Fatalf("no verified caller: %d", noCaller.Code)
	}
}

func TestIssue3779_HoldsHTTPRefusesNonAdmins_Security(t *testing.T) {
	x, ctx, repo := issue3779Extensions(t, "employee")
	ctx = transport.WithInvocation(ctx, &transport.Invocation{})
	h := ChatHoldsHTTP{Service: x}
	for action, body := range map[string]map[string]any{
		"list":         {},
		"place":        {"conversation_id": "general", "reason": "r"},
		"release":      {"id": "h", "reason": "r"},
		"conversation": {"conversation_id": "general"},
	} {
		if got := issue3779Post(t, h, ctx, action, body); got.Code != 403 {
			t.Errorf("%s as employee: %d %s", action, got.Code, got.Body)
		}
	}
	if holds, _ := repo.Holds(ctx, "host"); len(holds) != 0 {
		t.Fatalf("a refused call wrote a hold: %+v", holds)
	}
}

func TestIssue3779_HoldsOverlayServesItsPathOnly(t *testing.T) {
	x, _, _ := issue3779Extensions(t, "hcm_admin")
	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { reached = true })
	h := OverlayChatHolds(next, x, transport.Config{})
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/chat/other", nil))
	if !reached {
		t.Fatal("other paths must fall through")
	}
}
