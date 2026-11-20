package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type issue3790HTTPPort struct {
	converted []ChatWorkConvertRequest
	err       error
}

func (f *issue3790HTTPPort) Projects(context.Context, *trust.Principal) ([]ChatWorkProject, error) {
	return []ChatWorkProject{{ID: "ops", Name: "IT operations"}}, f.err
}
func (f *issue3790HTTPPort) Linked(_ context.Context, _ *trust.Principal, conversation string) ([]ChatWorkLink, error) {
	return []ChatWorkLink{{PostID: "m1", TaskID: "t1", Title: conversation, Status: "To do"}}, f.err
}
func (f *issue3790HTTPPort) Convert(_ context.Context, _ *trust.Principal, r ChatWorkConvertRequest) (ChatWorkLink, error) {
	f.converted = append(f.converted, r)
	return ChatWorkLink{PostID: r.MessageID, TaskID: "t1", Title: r.Title, Status: "To do"}, f.err
}

func issue3790Serve(t *testing.T, kind trust.SubjectKind, port ChatMessageWorkPort, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body)).WithContext(chatsaveHTTPContext(t, kind))
	recorder := httptest.NewRecorder()
	ChatMessageWorkHandler{Work: port}.ServeHTTP(recorder, request)
	return recorder
}

// Issue 3790: the route takes the message id from the path, never from the
// body, and answers with the link the message now shows.
func TestIssue3790_ConvertRouteTakesTheMessageFromThePath_Integration(t *testing.T) {
	port := &issue3790HTTPPort{}
	body := `{"conversation_id":"general","target":"TASK","project_id":"ops","title":"Swap laptop","owner_id":"person","due_date":"2026-12-04","idempotency_key":"attempt-0001"}`
	recorder := issue3790Serve(t, trust.SubjectKindHuman, port, http.MethodPost, "/api/chat/messages/m1/convert", body)
	if recorder.Code != http.StatusOK || len(port.converted) != 1 || port.converted[0].MessageID != "m1" || port.converted[0].ProjectID != "ops" || port.converted[0].IdempotencyKey != "attempt-0001" {
		t.Fatalf("status=%d converted=%+v body=%s", recorder.Code, port.converted, recorder.Body.String())
	}
	var reply chatMessageWorkReply
	if err := json.Unmarshal(recorder.Body.Bytes(), &reply); err != nil || reply.Link == nil || reply.Link.Status != "To do" {
		t.Fatalf("reply=%s err=%v", recorder.Body.String(), err)
	}
	read := issue3790Serve(t, trust.SubjectKindHuman, port, http.MethodGet, "/api/chat/messages/linked-work?conversation_id=general", "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"post_id":"m1"`) {
		t.Fatalf("linked-work status=%d body=%s", read.Code, read.Body.String())
	}
	options := issue3790Serve(t, trust.SubjectKindHuman, port, http.MethodGet, "/api/chat/messages/convert-options", "")
	if options.Code != http.StatusOK || !strings.Contains(options.Body.String(), "IT operations") {
		t.Fatalf("options status=%d body=%s", options.Code, options.Body.String())
	}
}

func TestIssue3790_ConvertRouteRefusalShapes_Security(t *testing.T) {
	good := `{"conversation_id":"general","target":"TASK","project_id":"ops","title":"t","idempotency_key":"attempt-0001"}`
	cases := []struct {
		name   string
		kind   trust.SubjectKind
		method string
		path   string
		body   string
		err    error
		want   int
	}{
		{"agent subject", trust.SubjectKindService, http.MethodPost, "/api/chat/messages/m1/convert", good, nil, http.StatusForbidden},
		{"unknown field", trust.SubjectKindHuman, http.MethodPost, "/api/chat/messages/m1/convert", `{"conversation_id":"general","admin":true}`, nil, http.StatusBadRequest},
		{"wrong method", trust.SubjectKindHuman, http.MethodGet, "/api/chat/messages/m1/convert", "", nil, http.StatusMethodNotAllowed},
		{"nested path", trust.SubjectKindHuman, http.MethodPost, "/api/chat/messages/a/b/convert", good, nil, http.StatusNotFound},
		{"unreadable message", trust.SubjectKindHuman, http.MethodPost, "/api/chat/messages/m1/convert", good, ErrChatWorkNotFound, http.StatusNotFound},
		{"no create access", trust.SubjectKindHuman, http.MethodPost, "/api/chat/messages/m1/convert", good, ErrChatWorkDenied, http.StatusForbidden},
		{"store down", trust.SubjectKindHuman, http.MethodPost, "/api/chat/messages/m1/convert", good, context.DeadlineExceeded, http.StatusServiceUnavailable},
	}
	for _, c := range cases {
		recorder := issue3790Serve(t, c.kind, &issue3790HTTPPort{err: c.err}, c.method, c.path, c.body)
		if recorder.Code != c.want {
			t.Errorf("%s: status=%d want %d body=%s", c.name, recorder.Code, c.want, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "deadline") {
			t.Errorf("%s: the body carried the cause: %s", c.name, recorder.Body.String())
		}
	}
	if recorder := issue3790Serve(t, trust.SubjectKindHuman, nil, http.MethodGet, "/api/chat/messages/convert-options", ""); recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("no service: status=%d", recorder.Code)
	}
}
