package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type issue3792Port struct {
	recorded []chat.DecisionRecordRequest
	listed   []string
	err      error
}

func (p *issue3792Port) Record(_ context.Context, r chat.DecisionRecordRequest) (chat.Decision, error) {
	p.recorded = append(p.recorded, r)
	return chat.Decision{ID: "d1", ConversationID: r.ConversationID, Text: r.Text, OwnerID: r.Principal.SubjectID, OwnerHomeTenantID: r.Principal.TenantID, DecidedAt: time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC), Informal: true}, p.err
}

func (p *issue3792Port) List(_ context.Context, _ chat.Principal, tenant, conversation string) (chat.DecisionList, error) {
	p.listed = append(p.listed, tenant+"/"+conversation)
	return chat.DecisionList{CanRecord: true, Decisions: []chat.DecisionView{{Decision: chat.Decision{ID: "d1", Text: "Freeze", Informal: true}, CanReplace: true}}}, p.err
}

func issue3792Serve(t *testing.T, kind trust.SubjectKind, port ChatDecisionsPort, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body)).WithContext(chatsaveHTTPContext(t, kind))
	recorder := httptest.NewRecorder()
	ChatDecisionsHandler{Decisions: port}.ServeHTTP(recorder, request)
	return recorder
}

// Issue 3792: the person is the session's, never a field of the body, and the
// answer says informal and whether the reader may replace each decision.
func TestIssue3792_DecisionRoutesUseTheSessionPerson(t *testing.T) {
	port := &issue3792Port{}
	body := `{"conversation_id":"room","text":"Freeze promotions","rationale":"calibration","idempotency_key":"attempt-1","label":"Decision (informal)","supersedes_id":"old"}`
	recorded := issue3792Serve(t, trust.SubjectKindHuman, port, http.MethodPost, ChatDecisionsPath+"/record", body)
	if recorded.Code != http.StatusOK || len(port.recorded) != 1 {
		t.Fatalf("status=%d body=%s", recorded.Code, recorded.Body.String())
	}
	got := port.recorded[0]
	if got.Principal.SubjectID != "person" || got.Principal.TenantID != "tenant-a" || got.TenantID != "tenant-a" || got.SupersedesID != "old" || got.IdempotencyKey != "attempt-1" || got.Text != "Freeze promotions" {
		t.Fatalf("request %+v", got)
	}
	var reply ChatDecisionsReply
	if err := json.Unmarshal(recorded.Body.Bytes(), &reply); err != nil || reply.Decision == nil || reply.Decision.OwnerID != "person" || !reply.Decision.Informal {
		t.Fatalf("reply=%s err=%v", recorded.Body.String(), err)
	}
	list := issue3792Serve(t, trust.SubjectKindHuman, port, http.MethodPost, ChatDecisionsPath+"/list", `{"conversation_id":"room"}`)
	if err := json.Unmarshal(list.Body.Bytes(), &reply); err != nil || list.Code != http.StatusOK || len(reply.Decisions) != 1 || !reply.CanRecord || !reply.Decisions[0].CanReplace || !reply.Decisions[0].Informal {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	if len(port.listed) != 1 || port.listed[0] != "tenant-a/room" {
		t.Fatalf("listed %v", port.listed)
	}
}

func TestIssue3792_DecisionRouteRefusalShapes_Security(t *testing.T) {
	good := `{"conversation_id":"room","text":"Freeze","idempotency_key":"k"}`
	cases := []struct {
		name   string
		kind   trust.SubjectKind
		method string
		path   string
		body   string
		err    error
		want   int
	}{
		{"agent subject", trust.SubjectKindService, http.MethodPost, "/record", good, nil, http.StatusForbidden},
		{"unknown field", trust.SubjectKindHuman, http.MethodPost, "/record", `{"conversation_id":"room","owner_id":"someone-else"}`, nil, http.StatusBadRequest},
		{"wrong method", trust.SubjectKindHuman, http.MethodGet, "/record", "", nil, http.StatusMethodNotAllowed},
		{"unknown action", trust.SubjectKindHuman, http.MethodPost, "/delete", good, nil, http.StatusNotFound},
		{"not a member", trust.SubjectKindHuman, http.MethodPost, "/record", good, chat.ErrPermissionDenied, http.StatusForbidden},
		{"unreadable room", trust.SubjectKindHuman, http.MethodPost, "/list", good, chat.ErrNotFound, http.StatusNotFound},
		{"replaced already", trust.SubjectKindHuman, http.MethodPost, "/record", good, chat.ErrConflict, http.StatusConflict},
		{"bad words", trust.SubjectKindHuman, http.MethodPost, "/record", good, chat.ErrInvalidArgument, http.StatusBadRequest},
		{"store down", trust.SubjectKindHuman, http.MethodPost, "/record", good, context.DeadlineExceeded, http.StatusServiceUnavailable},
	}
	for _, c := range cases {
		recorder := issue3792Serve(t, c.kind, &issue3792Port{err: c.err}, c.method, ChatDecisionsPath+c.path, c.body)
		if recorder.Code != c.want {
			t.Errorf("%s: status=%d want %d body=%s", c.name, recorder.Code, c.want, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "deadline") {
			t.Errorf("%s: the body carried the cause: %s", c.name, recorder.Body.String())
		}
	}
	if recorder := issue3792Serve(t, trust.SubjectKindHuman, nil, http.MethodPost, ChatDecisionsPath+"/list", `{"conversation_id":"room"}`); recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("no service: status=%d", recorder.Code)
	}
	unauthenticated := httptest.NewRecorder()
	ChatDecisionsHandler{Decisions: &issue3792Port{}}.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodPost, ChatDecisionsPath+"/list", strings.NewReader(`{}`)))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Errorf("no session: status=%d", unauthenticated.Code)
	}
}

func TestIssue3792_OverlayLeavesOtherRoutesAlone(t *testing.T) {
	passed := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { passed = true })
	OverlayChatDecisions(next, &issue3792Port{}, transport.Config{}).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/chat/other", nil))
	if !passed {
		t.Fatal("another route was swallowed")
	}
	if chatDecisionsPort(composedChat{}) != nil {
		t.Fatal("a port was composed without Chat")
	}
}
