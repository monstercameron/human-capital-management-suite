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
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type issue3804Port struct {
	proposed []chat.MemoryProposeRequest
	approved []chat.MemoryActRequest
	forgot   []chat.MemoryActRequest
	err      error
}

func (p *issue3804Port) Propose(_ context.Context, r chat.MemoryProposeRequest) (chat.ChannelMemory, error) {
	p.proposed = append(p.proposed, r)
	return chat.ChannelMemory{ID: "m1", ConversationID: r.ConversationID, Text: "Enrollment ends Nov 30", SourcePostID: r.SourcePostID, Status: chat.MemoryProposed, TTLDays: 90,
		ProposerID: r.Principal.SubjectID, ProposerHomeTenantID: r.Principal.TenantID, CreatedAt: time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)}, p.err
}

func (p *issue3804Port) List(_ context.Context, _ chat.Principal, tenant, conversation string) (chat.MemoryList, error) {
	expires := time.Date(2027, 2, 17, 0, 0, 0, 0, time.UTC)
	return chat.MemoryList{CanRemember: true, IsManager: true, Items: []chat.MemoryView{{ChannelMemory: chat.ChannelMemory{ID: "m1", Text: "Enrollment ends Nov 30", Status: chat.MemoryApproved,
		Source: chat.MemorySourceLive, ApprovedAt: expires.AddDate(0, 0, -90), ExpiresAt: expires, TTLDays: 90}, CanForget: true}}}, p.err
}

func (p *issue3804Port) Approve(_ context.Context, r chat.MemoryActRequest) (chat.ChannelMemory, error) {
	p.approved = append(p.approved, r)
	return chat.ChannelMemory{ID: r.ID, Status: chat.MemoryApproved, Text: "Enrollment ends Nov 30"}, p.err
}

func (p *issue3804Port) Forget(_ context.Context, r chat.MemoryActRequest) error {
	p.forgot = append(p.forgot, r)
	return p.err
}

func issue3804Serve(t *testing.T, kind trust.SubjectKind, port ChatMemoryPort, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body)).WithContext(chatsaveHTTPContext(t, kind))
	recorder := httptest.NewRecorder()
	ChatMemoryHandler{Memory: port}.ServeHTTP(recorder, request)
	return recorder
}

func TestIssue3804_MemoryRoutesUseTheSessionPerson(t *testing.T) {
	port := &issue3804Port{}
	proposed := issue3804Serve(t, trust.SubjectKindHuman, port, http.MethodPost, ChatMemoryPath+"/propose", `{"conversation_id":"room","source_post_id":"p1"}`)
	if proposed.Code != http.StatusOK || len(port.proposed) != 1 {
		t.Fatalf("status=%d body=%s", proposed.Code, proposed.Body.String())
	}
	if got := port.proposed[0]; got.Principal.SubjectID != "person" || got.Principal.TenantID != "tenant-a" || got.TenantID != "tenant-a" || got.SourcePostID != "p1" {
		t.Fatalf("request %+v", got)
	}
	var reply ChatMemoryReply
	if err := json.Unmarshal(proposed.Body.Bytes(), &reply); err != nil || reply.Memory == nil || reply.Memory.Status != "proposed" || reply.Memory.ProposerID != "person" {
		t.Fatalf("reply=%s err=%v", proposed.Body.String(), err)
	}
	list := issue3804Serve(t, trust.SubjectKindHuman, port, http.MethodPost, ChatMemoryPath+"/list", `{"conversation_id":"room"}`)
	reply = ChatMemoryReply{}
	if err := json.Unmarshal(list.Body.Bytes(), &reply); err != nil || list.Code != http.StatusOK || len(reply.Items) != 1 || !reply.CanRemember || !reply.IsManager ||
		reply.Items[0].Status != "approved" || reply.Items[0].Source != "live" || reply.Items[0].ExpiresAt == nil || !reply.Items[0].CanForget || reply.Items[0].CanApprove {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	approved := issue3804Serve(t, trust.SubjectKindHuman, port, http.MethodPost, ChatMemoryPath+"/approve", `{"conversation_id":"room","id":"m1"}`)
	forgot := issue3804Serve(t, trust.SubjectKindHuman, port, http.MethodPost, ChatMemoryPath+"/forget", `{"conversation_id":"room","id":"m1"}`)
	if approved.Code != http.StatusOK || forgot.Code != http.StatusOK || len(port.approved) != 1 || len(port.forgot) != 1 || port.approved[0].Principal.SubjectID != "person" || port.forgot[0].ID != "m1" {
		t.Fatalf("approve=%d forget=%d %+v %+v", approved.Code, forgot.Code, port.approved, port.forgot)
	}
}

func TestIssue3804_MemoryRouteRefusalShapes_Security(t *testing.T) {
	good := `{"conversation_id":"room","source_post_id":"p1"}`
	cases := []struct {
		name   string
		kind   trust.SubjectKind
		method string
		path   string
		body   string
		err    error
		want   int
		code   string
	}{
		{"agent subject", trust.SubjectKindService, http.MethodPost, "/propose", good, nil, http.StatusForbidden, "permission_denied"},
		{"unknown field", trust.SubjectKindHuman, http.MethodPost, "/propose", `{"conversation_id":"room","proposer_id":"someone-else"}`, nil, http.StatusBadRequest, "invalid_argument"},
		{"wrong method", trust.SubjectKindHuman, http.MethodGet, "/list", "", nil, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"denied", trust.SubjectKindHuman, http.MethodPost, "/approve", `{"conversation_id":"room","id":"m"}`, chat.ErrPermissionDenied, http.StatusForbidden, "permission_denied"},
		{"gone", trust.SubjectKindHuman, http.MethodPost, "/forget", `{"conversation_id":"room","id":"m"}`, chat.ErrNotFound, http.StatusNotFound, "not_found"},
		{"conflict", trust.SubjectKindHuman, http.MethodPost, "/approve", `{"conversation_id":"room","id":"m"}`, chat.ErrConflict, http.StatusConflict, "conflict"},
		{"store down", trust.SubjectKindHuman, http.MethodPost, "/list", `{"conversation_id":"room"}`, context.DeadlineExceeded, http.StatusServiceUnavailable, "unavailable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			port := &issue3804Port{err: c.err}
			got := issue3804Serve(t, c.kind, port, c.method, ChatMemoryPath+c.path, c.body)
			if got.Code != c.want || !strings.Contains(got.Body.String(), `"`+c.code+`"`) {
				t.Fatalf("status=%d body=%s", got.Code, got.Body.String())
			}
			if len(port.proposed)+len(port.approved)+len(port.forgot) != 0 && c.kind != trust.SubjectKindHuman {
				t.Fatalf("a refused caller reached the service")
			}
		})
	}
	if got := issue3804Serve(t, trust.SubjectKindHuman, nil, http.MethodPost, ChatMemoryPath+"/list", `{"conversation_id":"room"}`); got.Code != http.StatusServiceUnavailable {
		t.Fatalf("no service: %d", got.Code)
	}
	if got := issue3804Serve(t, trust.SubjectKindHuman, &issue3804Port{}, http.MethodPost, ChatMemoryPath+"/other", `{}`); got.Code != http.StatusNotFound {
		t.Fatalf("unknown action: %d", got.Code)
	}
}
