package application

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func agentRolloutPortableAdmission(t *testing.T, principal *trust.Principal) (transport.Config, string) {
	t.Helper()
	at := time.Now().UTC()
	clock := func() time.Time { return at }
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(strings.Repeat("r", 32)), Issuer: "agent-design-test", Audience: "agent-design-test", Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	bearer, err := verifier.Issue(trust.Claims{Issuer: "agent-design-test", Audience: "agent-design-test", Subject: principal.Subject(), SubjectKind: "human", Tenant: string(principal.Tenant()), Purposes: []string{"agent_design"}, AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "design-session", IssuedAtUnix: at.Add(-time.Minute).Unix(), ExpiresAtUnix: at.Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return transport.Config{Verifier: verifier, Now: clock}, bearer
}

func agentDesignHTTP(t *testing.T, handler http.Handler, bearer, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	r.Header.Set("Authorization", "Bearer "+bearer)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestTodo_AGENT_044_HTTP_AuthenticatedCommands(t *testing.T) {
	ctx, service, store, _ := versionRolloutFixture(t)
	principal, _ := trust.FromContext(ctx)
	admission, bearer := agentRolloutPortableAdmission(t, principal)
	h := OverlayAgentVersionRolloutHTTP(nil, service, admission)
	w := agentDesignHTTP(t, h, bearer, AgentVersionRolloutPath, versionRolloutPreview())
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("preview status %d: %s", w.Code, w.Body.String())
	}
	var receipt AgentVersionRolloutReceipt
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Plan.TenantID != string(principal.Tenant()) || receipt.Progress.Stage != "PREVIEWED" || store.writes != 1 {
		t.Fatal("edge lost trusted principal or changed actual installation")
	}
	for _, action := range []string{"APPROVE", "ADVANCE", "PROMOTE", "ADVANCE"} {
		w = agentDesignHTTP(t, h, bearer, AgentVersionRolloutPath, versionRolloutAction(action, receipt))
		if w.Code != http.StatusOK {
			t.Fatalf("%s status %d: %s", action, w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
			t.Fatal(err)
		}
	}
	if receipt.Progress.Stage != "COMPLETE" || store.installations["install-b"].PersonaVersion != 2 {
		t.Fatal("edge did not complete explicit rollout")
	}
	w = agentDesignHTTP(t, h, "tampered", AgentVersionRolloutPath, AgentVersionRolloutCommand{Action: "CATALOG"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("tampered bearer status=%d", w.Code)
	}
}

func TestTodo_AGENT_045_HTTP_AuthenticatedPortableDraft(t *testing.T) {
	ctx, service, request, recorder := portableMappedFixture(t)
	principal, _ := trust.FromContext(ctx)
	admission, bearer := agentRolloutPortableAdmission(t, principal)
	h := OverlayAgentPortableHTTP(nil, service, admission)
	w := agentDesignHTTP(t, h, bearer, AgentPortableImportPath, request)
	if w.Code != http.StatusOK || len(recorder.drafts) != 1 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("import status %d: %s", w.Code, w.Body.String())
	}
	var receipt map[string]json.RawMessage
	if json.Unmarshal(w.Body.Bytes(), &receipt) != nil || string(receipt["state"]) != `"DRAFT"` {
		t.Fatal("edge returned no draft receipt")
	}
	if recorder.drafts[0].Manifest.OwnerID != principal.Subject() || recorder.drafts[0].TenantID != string(principal.Tenant()) || len(recorder.drafts[0].Manifest.ContextGrants) != 0 {
		t.Fatal("edge accepted foreign identity or imported authority")
	}
	w = agentDesignHTTP(t, h, bearer, AgentPortableDraftPath, map[string]string{"definition_id": recorder.drafts[0].Manifest.ID})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), recorder.drafts[0].Manifest.ID) || !strings.Contains(w.Body.String(), recorder.drafts[0].Instructions) {
		t.Fatalf("persisted draft read status%d: %s", w.Code, w.Body.String())
	}
	w = agentDesignHTTP(t, h, "tampered", AgentPortableImportPath, request)
	if w.Code != http.StatusUnauthorized || len(recorder.drafts) != 1 {
		t.Fatalf("tampered bearer changed drafts status=%d", w.Code)
	}
	manifest := portableManifestForApplication("Exact reusable instruction body.")
	service.Manifests = portableManifestSourceFake{manifest}
	w = agentDesignHTTP(t, h, bearer, AgentPortableExportPath, AgentPortableExportRequest{ManifestID: manifest.ID, ManifestVersion: manifest.Version})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Exact reusable instruction body.") {
		t.Fatalf("export status %d: %s", w.Code, w.Body.String())
	}
	service.Authorizer = portableAuthorizerFake{ErrAgentPortableDenied}
	w = agentDesignHTTP(t, h, bearer, AgentPortableExportPath, AgentPortableExportRequest{ManifestID: manifest.ID, ManifestVersion: manifest.Version})
	if w.Code != http.StatusForbidden {
		t.Fatalf("role denial status=%d", w.Code)
	}
	w = agentDesignHTTP(t, OverlayAgentPortableHTTP(nil, nil, admission), bearer, AgentPortableImportPath, request)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing service status=%d", w.Code)
	}
}

func TestTodo_AGENT_045_HTTP_ClosedBoundary(t *testing.T) {
	ctx, service, _, recorder := portableMappedFixture(t)
	principal, _ := trust.FromContext(ctx)
	admission, bearer := agentRolloutPortableAdmission(t, principal)
	h := OverlayAgentPortableHTTP(nil, service, admission)
	for _, tc := range []struct {
		method, content, body string
		want                  int
	}{
		{http.MethodGet, "application/json", `{}`, http.StatusMethodNotAllowed},
		{http.MethodPost, "text/plain", `{}`, http.StatusUnsupportedMediaType},
		{http.MethodPost, "application/json", `{"tenant_id":"foreign"}`, http.StatusBadRequest},
		{http.MethodPost, "application/json", `{} {}`, http.StatusBadRequest},
	} {
		r := httptest.NewRequest(tc.method, AgentPortableImportPath, strings.NewReader(tc.body))
		r.Header.Set("Authorization", "Bearer "+bearer)
		r.Header.Set("Content-Type", tc.content)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want || len(recorder.drafts) != 0 {
			t.Fatalf("%s %s got%d writes%d", tc.method, tc.body, w.Code, len(recorder.drafts))
		}
	}
	if portableHTTPErrorStatus(ErrAgentPortableUnavailable) != http.StatusServiceUnavailable || portableHTTPErrorStatus(ErrAgentPortableInvalid) != http.StatusBadRequest {
		t.Fatal("portable error projection lost status")
	}
}
