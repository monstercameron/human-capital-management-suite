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

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chatgateHTTPFixture struct {
	calls   int
	request ChatgateRequest
	err     error
}

func (f *chatgateHTTPFixture) GateRequest(_ context.Context, r ChatgateRequest) (ChatgateReply, error) {
	f.calls++
	f.request = r
	return ChatgateReply{Result: "ok"}, f.err
}
func TestTodo_CHATGATE_008(t *testing.T) {
	surface := &chatgateHTTPFixture{}
	h := ChatgateHTTP{Surface: surface}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", ChatgatePath, strings.NewReader(`{"Conversation":"room","Action":"submit","Key":"once","ExpectedRevision":2,"Answers":{"team":"Payroll"}}`)))
	if w.Code != 200 || surface.calls != 1 || surface.request.Key != "once" || surface.request.ExpectedRevision != 2 || string(surface.request.Answers["team"]) != `"Payroll"` {
		t.Fatalf("HTTP %d %+v", w.Code, surface)
	}
	for _, body := range []string{`{"Conversation":"room"}{}`, strings.Repeat("x", (1<<20)+1)} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", ChatgatePath, strings.NewReader(body)))
		if w.Code != 400 || surface.calls != 1 {
			t.Fatal("invalid body reached port", w.Code, surface.calls)
		}
	}
}
func TestTodo_CHATGATE_008_Contract(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{{chatgate.ErrInvalid, 400, "invalid_argument"}, {chatgate.ErrDenied, 403, "permission_denied"}, {chatgate.ErrConflict, 409, "conflict"}, {chatgate.ErrRequired, 422, "answers_required"}, {chatgate.ErrUnavailable, 503, "unavailable"}} {
		surface := &chatgateHTTPFixture{err: test.err}
		w := httptest.NewRecorder()
		ChatgateHTTP{Surface: surface}.ServeHTTP(w, httptest.NewRequest("GET", ChatgatePath+"?conversation=room", nil))
		var reply struct{ Error string }
		if e := json.Unmarshal(w.Body.Bytes(), &reply); e != nil || w.Code != test.status || reply.Error != test.code {
			t.Fatalf("typed error %d %s", w.Code, w.Body.String())
		}
	}
}
func TestTodo_CHATGATE_008_Security(t *testing.T) {
	surface := &chatgateHTTPFixture{}
	h := OverlayChatgates(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }), surface, transport.Config{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", ChatgatePath+"?conversation=room", nil))
	if surface.calls != 0 || w.Code == 200 {
		t.Fatal("unauthenticated gate request admitted")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/other", nil))
	if w.Code != 204 {
		t.Fatal("overlay captured sibling")
	}
	app := &ChatgateApplication{Service: &chatgate.Service{}}
	if _, e := app.GateRequest(context.Background(), ChatgateRequest{Conversation: "room"}); !errors.Is(e, chatgate.ErrDenied) {
		t.Fatal(e)
	}
}

type chatgateAppAuthority struct{}

type chatgateAppDirectory struct{}

func (chatgateAppDirectory) GateDirectory(context.Context, chatgate.Actor, chatgate.Scope) (map[string][]chatui.GateChoice, map[string]string, string, error) {
	return nil, nil, "Join", nil
}
func (chatgateAppDirectory) GateReviewers(context.Context, chatgate.Actor, chatgate.Scope) ([]string, error) {
	return []string{"Dana Review"}, nil
}

func (chatgateAppAuthority) Check(_ context.Context, a chatgate.Actor, _ chatgate.Scope, permission string) error {
	if permission == "admin" || permission == "export" {
		if a.Person != "admin" {
			return chatgate.ErrDenied
		}
	}
	return nil
}
func (chatgateAppAuthority) Policy(context.Context, chatgate.Scope) (chatgate.Policy, error) {
	return chatgate.Policy{}, nil
}
func (chatgateAppAuthority) Facts(context.Context, chatgate.Actor, chatgate.Scope) (map[string]string, error) {
	return map[string]string{}, nil
}
func (chatgateAppAuthority) Reference(context.Context, chatgate.Actor, chatgate.Scope, chatgate.Field, json.RawMessage) error {
	return nil
}
func TestTodo_CHATGATE_005_Security(t *testing.T) {
	now := time.Now()
	p, e := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant"), Subject: "member", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:fixture"})
	if e != nil {
		t.Fatal(e)
	}
	s := &chatgate.Service{Repository: chatgate.NewMemoryRepository(), Authority: chatgateAppAuthority{}, Registry: chatgate.NewRegistry()}
	scope := chatgate.Scope{Tenant: "tenant", Conversation: "room"}
	c := chatgate.Command{Scope: scope, Actor: chatgate.Actor{Tenant: "tenant", Person: "admin"}, Key: "define"}
	d := chatgate.Definition{Mode: "review", Purpose: "Join", Fields: []chatgate.Field{{ID: "team", Kind: "short_text", KindVersion: "1.0.0", Label: "Team", Purpose: "Welcome", DataClass: "INTERNAL", RetentionDays: 30, Required: true, Visibility: chatgate.Visibility{Administrators: true}}}}
	if e = s.Define(t.Context(), c, d); e != nil {
		t.Fatal(e)
	}
	c.Key = "publish"
	c.ExpectedRevision = 1
	if _, e = s.Publish(t.Context(), c, chatgate.Version{Major: 1}); e != nil {
		t.Fatal(e)
	}
	app := &ChatgateApplication{Service: s, Directory: chatgateAppDirectory{}}
	reply, e := app.GateRequest(trust.WithPrincipal(t.Context(), p), ChatgateRequest{Conversation: "room", Action: "submit", Version: "1.0.0", Key: "submit", ExpectedRevision: 2, Answers: map[string]json.RawMessage{"team": json.RawMessage(`"Payroll"`)}})
	if e != nil || reply.View.Administrator || reply.View.Submission.Person != "member" || reply.View.Submission.Status != "review" || len(reply.View.Submissions) != 0 {
		t.Fatalf("applicant %+v %v", reply, e)
	}
	if len(reply.View.Reviewers) != 1 || reply.View.Reviewers[0] != "Dana Review" {
		t.Fatal("reviewer projection missing", reply.View.Reviewers)
	}
}
