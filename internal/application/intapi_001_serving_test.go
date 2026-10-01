package application

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/machine"
)

func TestTodo_INTAPI_001_ServedComposition(t *testing.T) {
	now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	key, err := machine.GenerateServerKey("served-test", now.Add(time.Hour), now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("GenerateServerKey: %v", err)
	}
	serverVerifier, err := machine.NewVerifier([]machine.ServerKey{key}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	hmac, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: []byte("01234567890123456789012345678901"), Issuer: "dev", Audience: "api",
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewHMACVerifier: %v", err)
	}
	served := &trust.ServedVerifier{
		Machine: serverVerifier,
		Request: machine.VerifyRequest{Issuer: "issuer", Audience: []string{"api"}},
		Now:     func() time.Time { return now },
	}
	base, err := hmac.Issue(trust.Claims{
		Issuer: "dev", Audience: "api", Subject: "human", SubjectKind: "human",
		Tenant: "tenant-a", SessionRef: "session-a", Assurance: "substantial",
		IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("Issue HMAC token: %v", err)
	}
	if _, err := served.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: base}); err == nil {
		t.Fatal("standard served verifier accepted an HMAC token without a local-dev fallback")
	}

	var gotPath string
	edge := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	routes := http.NewServeMux()
	routes.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	routes.HandleFunc("GET /.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	handler := mountMachineAuth(edge, routes)
	for _, tc := range []struct {
		name   string
		method string
		path   string
		status int
	}{
		{name: "token", method: http.MethodPost, path: "/oauth2/token", status: http.StatusCreated},
		{name: "jwks", method: http.MethodGet, path: "/.well-known/jwks.json", status: http.StatusAccepted},
		{name: "edge", method: http.MethodGet, path: "/healthz", status: http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotPath = ""
			res := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			handler.ServeHTTP(res, req)
			if res.Code != tc.status {
				t.Fatalf("status = %d, want %d", res.Code, tc.status)
			}
			if tc.name == "edge" && gotPath != tc.path {
				t.Fatalf("edge path = %q, want %q", gotPath, tc.path)
			}
		})
	}
}
