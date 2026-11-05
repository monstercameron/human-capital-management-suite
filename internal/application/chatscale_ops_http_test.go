package application

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chatscaleOpsPortStub struct {
	view chatstore.ChatscaleOpsView
	err  error
}

func (s chatscaleOpsPortStub) ChatscaleOpsView(context.Context) (chatstore.ChatscaleOpsView, error) {
	return s.view, s.err
}

func chatscaleOpsVerifier() trust.Verifier {
	return trust.VerifierFunc(func(_ context.Context, credential trust.Credential) (*trust.Principal, error) {
		roles := []string{admin.OperatorRole}
		if credential.Token == "ordinary" {
			roles = nil
		} else if credential.Token != "operator" {
			return nil, fmt.Errorf("unknown fixture credential")
		}
		return trust.NewPrincipal(trust.PrincipalSpec{
			Tenant: values.TenantId("tenant-a"), Subject: "test-operator", SubjectKind: trust.SubjectKindHuman,
			Roles: roles, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
			Assurance: trust.AssuranceHigh, SessionRef: "test-session", IssuedAt: time.Now().Add(-time.Minute),
			ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "fixture:" + credential.Token,
		})
	})
}

func TestChatscaleOpsOverlay(t *testing.T) {
	admission := transport.Config{Verifier: chatscaleOpsVerifier()}
	figure := chatstore.ChatscaleOpsView{
		GeneratedAt: time.Now().UTC(),
		Budgets:     chatstore.DefaultChatscaleBudgets(),
		Tables:      []chatstore.ChatscaleTableGrowth{{Table: "chat_post", HeapBytes: 8, IndexBytes: 8, TotalBytes: 16}},
	}
	handler := OverlayChatscaleOps(http.NotFoundHandler(), chatscaleOpsPortStub{view: figure}, admission)

	get := func(token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, ChatscaleOpsPath, nil)
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	ok := get("operator")
	if ok.Code != http.StatusOK {
		t.Fatalf("operator status=%d body=%s", ok.Code, ok.Body.String())
	}
	if ctype := ok.Header().Get("Content-Type"); ctype != "application/json" && ctype != "application/json; charset=utf-8" {
		t.Fatalf("content-type=%q want application/json", ctype)
	}
	if ok.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("operations figures must not be cached")
	}
	var decoded chatstore.ChatscaleOpsView
	if err := json.Unmarshal(ok.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode operations view: %v", err)
	}
	if decoded.Budgets != chatstore.DefaultChatscaleBudgets() || len(decoded.Tables) != 1 || decoded.Tables[0].Table != "chat_post" {
		t.Fatalf("operations view=%+v lost the budgets or table figures", decoded)
	}

	if denied := get("ordinary"); denied.Code != http.StatusForbidden {
		t.Fatalf("non-operator status=%d want 403", denied.Code)
	}
	if anonymous := get(""); anonymous.Code == http.StatusOK {
		t.Fatal("anonymous request admitted to the operations view")
	}
	post := httptest.NewRequest(http.MethodPost, ChatscaleOpsPath, nil)
	post.Header.Set("Authorization", "Bearer operator")
	postRecorder := httptest.NewRecorder()
	handler.ServeHTTP(postRecorder, post)
	if postRecorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d want 405", postRecorder.Code)
	}

	unavailable := OverlayChatscaleOps(http.NotFoundHandler(), nil, admission)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, ChatscaleOpsPath, nil)
	request.Header.Set("Authorization", "Bearer operator")
	unavailable.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("uncomposed store status=%d want 503", recorder.Code)
	}

	fell := 0
	OverlayChatscaleOps(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fell++; w.WriteHeader(http.StatusTeapot) }), chatscaleOpsPortStub{view: figure}, admission).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/other", nil))
	if fell != 1 {
		t.Fatal("operations overlay swallowed an unrelated path")
	}
}
