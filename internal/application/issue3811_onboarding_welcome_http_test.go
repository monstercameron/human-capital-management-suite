package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type welcomePortStub struct {
	views map[string]OnboardingWelcomeView
	err   error
	posts []string
}

func (s *welcomePortStub) Views(_ context.Context, _ *trust.Principal, _ string, posts []string) (map[string]OnboardingWelcomeView, error) {
	s.posts = posts
	return s.views, s.err
}

func TestIssue3811_WelcomeRouteShapes(t *testing.T) {
	for path, want := range map[string]bool{
		"/api/chat/conversations/c1/onboarding":       true,
		"/api/chat/conversations/c1/onboarding/extra": false,
		"/api/chat/conversations//onboarding":         false,
		"/api/chat/conversations/c1/forms":            false,
		"/api/chat/other/c1/onboarding":               false,
	} {
		if _, ok := onboardingWelcomeRoute(path); ok != want {
			t.Errorf("route(%q) = %v, want %v", path, ok, want)
		}
	}
}

func TestIssue3811_WelcomeHandlerStatusCodes_Security(t *testing.T) {
	stub := &welcomePortStub{views: map[string]OnboardingWelcomeView{"p1": {PostID: "p1", State: OnboardingWelcomeActive}}}
	human := welcomeHuman(t, "taylor")
	call := func(method string, port OnboardingWelcomePort, principal *trust.Principal) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/chat/conversations/dm/onboarding?post=p1&post=p2", nil)
		if principal != nil {
			req = req.WithContext(trust.WithPrincipal(req.Context(), principal))
		}
		rec := httptest.NewRecorder()
		OnboardingWelcomeHandler{Welcome: port}.ServeHTTP(rec, req)
		return rec
	}
	ok := call(http.MethodGet, stub, human)
	var reply onboardingWelcomeReply
	if ok.Code != http.StatusOK || json.Unmarshal(ok.Body.Bytes(), &reply) != nil || reply.Views["p1"].State != OnboardingWelcomeActive || len(stub.posts) != 2 || ok.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("ok = %d %s posts %v", ok.Code, ok.Body.String(), stub.posts)
	}
	if rec := call(http.MethodPost, stub, human); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d", rec.Code)
	}
	if rec := call(http.MethodGet, stub, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d", rec.Code)
	}
	agent, _ := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "bot", SubjectKind: trust.SubjectKindAgent, Purposes: []string{"chat"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "s", IssuedAt: human.IssuedAt(), ExpiresAt: human.ExpiresAt(), CredentialDigest: "c"})
	if rec := call(http.MethodGet, stub, agent); rec.Code != http.StatusForbidden {
		t.Errorf("agent = %d", rec.Code)
	}
	if rec := call(http.MethodGet, nil, human); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("unwired = %d", rec.Code)
	}
	for err, want := range map[error]int{
		ErrOnboardingWelcomeInvalid:     http.StatusBadRequest,
		ErrOnboardingWelcomeDenied:      http.StatusForbidden,
		ErrOnboardingWelcomeUnavailable: http.StatusServiceUnavailable,
	} {
		if rec := call(http.MethodGet, &welcomePortStub{err: err}, human); rec.Code != want {
			t.Errorf("%v = %d, want %d", err, rec.Code, want)
		}
	}
}
