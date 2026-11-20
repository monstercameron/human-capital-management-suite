package application

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/clienttelemetry"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const issue3798Body = `{"v":1,"build":"abc123","locale":"ar","reason":"pagehide","session":"0123456789abcdef","marks":{"go-main":100,"hydrated":800},"stream":{"open":2,"gave-up":1},"panics":{"history.push":1}}`

func issue3798Ctx(t *testing.T, tenant string) context.Context {
	t.Helper()
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: "person-secret-id", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow, SessionRef: "session", IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CredentialDigest: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), p)
}

type issue3798Log struct{ lines []string }

func (l *issue3798Log) Info(msg string, args ...any) {
	l.lines = append(l.lines, msg+" "+fmt.Sprint(args...))
}

func issue3798Post(h http.Handler, ctx context.Context, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, ChatClientTelemetryPath, strings.NewReader(body)).WithContext(ctx))
	return w
}

func issue3798Handler(log *issue3798Log) ChatClientTelemetryHandler {
	return ChatClientTelemetryHandler{Limiter: clienttelemetry.NewLimiter(2, time.Minute, 100, nil), Logger: log}
}

func TestIssue3798_AcceptedBeaconIsLoggedWithoutRawIdentifiers(t *testing.T) {
	log := &issue3798Log{}
	w := issue3798Post(issue3798Handler(log), issue3798Ctx(t, "tenant-a"), issue3798Body)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	if len(log.lines) != 1 {
		t.Fatalf("log lines = %v", log.lines)
	}
	line := log.lines[0]
	for _, want := range []string{"hcmnext.chat_client", "tenant-a", "abc123", "history.push=1", "gave-up=1", "go-main=100"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line missing %q: %s", want, line)
		}
	}
	for _, leak := range []string{"person-secret-id", "0123456789abcdef"} {
		if strings.Contains(line, leak) {
			t.Errorf("log line leaks %q: %s", leak, line)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("response must not be cacheable")
	}
}

func TestIssue3798_BeaconRefusals_Security(t *testing.T) {
	cases := []struct {
		name   string
		method string
		ctx    context.Context
		body   string
		want   int
	}{
		{"anonymous", http.MethodPost, context.Background(), issue3798Body, http.StatusUnauthorized},
		{"wrong method", http.MethodGet, issue3798Ctx(t, "tenant-a"), "", http.StatusMethodNotAllowed},
		{"unknown field", http.MethodPost, issue3798Ctx(t, "tenant-a"), strings.Replace(issue3798Body, `"v":1`, `"v":1,"text":"hi"`, 1), http.StatusBadRequest},
		{"too large", http.MethodPost, issue3798Ctx(t, "tenant-a"), strings.Repeat("x", clienttelemetry.MaxBodyBytes+10), http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		log := &issue3798Log{}
		w := httptest.NewRecorder()
		ChatClientTelemetryHandler{Limiter: clienttelemetry.NewLimiter(2, time.Minute, 100, nil), Logger: log}.ServeHTTP(w, httptest.NewRequest(tc.method, ChatClientTelemetryPath, strings.NewReader(tc.body)).WithContext(tc.ctx))
		if w.Code != tc.want {
			t.Errorf("%s: status = %d, want %d", tc.name, w.Code, tc.want)
		}
		if len(log.lines) != 0 {
			t.Errorf("%s: a refused beacon was logged: %v", tc.name, log.lines)
		}
	}
}

func TestIssue3798_BeaconIsRateLimitedPerTenantSession_Security(t *testing.T) {
	log := &issue3798Log{}
	h := issue3798Handler(log)
	ctx := issue3798Ctx(t, "tenant-a")
	for i := 0; i < 2; i++ {
		if w := issue3798Post(h, ctx, issue3798Body); w.Code != http.StatusNoContent {
			t.Fatalf("beacon %d status = %d", i, w.Code)
		}
	}
	if w := issue3798Post(h, ctx, issue3798Body); w.Code != http.StatusTooManyRequests {
		t.Fatalf("third beacon status = %d, want 429", w.Code)
	}
	if len(log.lines) != 2 {
		t.Fatalf("log lines = %d, want 2", len(log.lines))
	}
	// The same nonce in another tenant is another session: not throttled by the first.
	if w := issue3798Post(h, issue3798Ctx(t, "tenant-b"), issue3798Body); w.Code != http.StatusNoContent {
		t.Fatalf("other tenant status = %d", w.Code)
	}
}

func TestIssue3798_OverlayRoutesOnlyItsPathAndAdmitsFirst(t *testing.T) {
	passed := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { passed = true; w.WriteHeader(http.StatusTeapot) })
	overlay := OverlayChatClientTelemetry(next, transport.Config{}, nil)
	w := httptest.NewRecorder()
	overlay.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/elsewhere", nil))
	if !passed || w.Code != http.StatusTeapot {
		t.Fatalf("other paths must reach the next handler (passed=%v code=%d)", passed, w.Code)
	}
	w = httptest.NewRecorder()
	overlay.ServeHTTP(w, httptest.NewRequest(http.MethodPost, ChatClientTelemetryPath, strings.NewReader(issue3798Body)))
	if w.Code == http.StatusNoContent {
		t.Fatal("an unadmitted request must never be accepted")
	}
}

func TestIssue3798_FreshNoncesCannotFloodTheLog_Security(t *testing.T) {
	log := &issue3798Log{}
	h := ChatClientTelemetryHandler{
		Limiter:   clienttelemetry.NewLimiter(2, time.Minute, 100, nil),
		PerPerson: clienttelemetry.NewLimiter(3, time.Minute, 100, nil),
		Logger:    log,
	}
	ctx := issue3798Ctx(t, "tenant-a")
	accepted, refused := 0, 0
	for i := 0; i < 10; i++ {
		body := strings.Replace(issue3798Body, "0123456789abcdef", fmt.Sprintf("%016x", i+1), 1)
		switch w := issue3798Post(h, ctx, body); w.Code {
		case http.StatusNoContent:
			accepted++
		case http.StatusTooManyRequests:
			refused++
		default:
			t.Fatalf("status = %d", w.Code)
		}
	}
	if accepted != 3 || refused != 7 || len(log.lines) != 3 {
		t.Fatalf("accepted=%d refused=%d logged=%d, want 3/7/3", accepted, refused, len(log.lines))
	}
}

func TestIssue3798_BeaconIsServedThroughTheChatComposition_Integration(t *testing.T) {
	admission, bearer := integrate1Admission(t, "tenant", "alice", time.Now)
	handler := overlayIntegrate1Chat(http.NotFoundHandler(), composedChat{}, VoiceService{}, admission)
	call := func(auth bool) int {
		request := httptest.NewRequest(http.MethodPost, ChatClientTelemetryPath, strings.NewReader(issue3798Body))
		request.Header.Set("Content-Type", "application/json")
		if auth {
			request.Header.Set("Authorization", bearer)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response.Code
	}
	if code := call(false); code != http.StatusUnauthorized {
		t.Fatalf("anonymous beacon status = %d, want 401", code)
	}
	if code := call(true); code != http.StatusNoContent {
		t.Fatalf("signed-in beacon status = %d, want 204", code)
	}
}
