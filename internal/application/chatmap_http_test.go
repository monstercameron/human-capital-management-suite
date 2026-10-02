package application

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chatmapSurfaceFixture struct {
	calls int
	err   error
}

func (f *chatmapSurfaceFixture) Attach(_ context.Context, r chat.AttachLocationRequest) (chat.LocationShare, error) {
	f.calls++
	return chat.LocationShare{Version: 1, TenantID: r.Principal.TenantID, SharerID: r.Principal.SubjectID}, f.err
}
func (f *chatmapSurfaceFixture) Read(context.Context, chat.Principal, chat.LocationKey) (chat.LocationShare, error) {
	f.calls++
	return chat.LocationShare{Version: 1}, f.err
}
func (f *chatmapSurfaceFixture) End(context.Context, chat.Principal, chat.LocationKey) error {
	f.calls++
	return f.err
}
func chatmapHTTPContext(t *testing.T) context.Context {
	t.Helper()
	now := time.Now()
	tenant := values.TenantId("chatmap-tenant")
	actor, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: tenant, Subject: "alice", SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: "org", AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "chatmap-session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:chatmap-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), actor)
}
func TestTodo_CHATMAP_002(t *testing.T) {
	f := &chatmapSurfaceFixture{}
	h := ChatmapHTTP{Surface: f}
	for _, action := range []string{"attach", "read", "end"} {
		req := httptest.NewRequest(http.MethodPost, ChatmapPath+"/"+action, strings.NewReader(`{"TenantID":"chatmap-tenant","ConversationID":"c","PostID":"m","ID":"s"}`)).WithContext(chatmapHTTPContext(t))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(action, w.Code, w.Body.String())
		}
	}
	if f.calls != 3 {
		t.Fatal(f.calls)
	}
}
func TestTodo_CHATMAP_002_Security(t *testing.T) {
	f := &chatmapSurfaceFixture{}
	h := ChatmapHTTP{Surface: f}
	for _, tc := range []struct {
		path, body string
		session    bool
		status     int
	}{
		{ChatmapPath + "/attach", "{}", false, 401},
		{ChatmapPath + "/attach?latitude=42", "{}", true, 400},
		{ChatmapPath + "/attach", `{"Principal":{"SubjectID":"admin"}}`, true, 400},
		{ChatmapPath + "/attach", "{} {}", true, 400},
	} {
		req := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		if tc.session {
			req = req.WithContext(chatmapHTTPContext(t))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if f.calls != 0 {
		t.Fatal("effect before admission")
	}
	f.err = chat.ErrPermissionDenied
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", ChatmapPath+"/read", strings.NewReader("{}")).WithContext(chatmapHTTPContext(t)))
	if w.Code != 403 || strings.Contains(w.Body.String(), "42") {
		t.Fatal(w.Code, w.Body.String())
	}
}
