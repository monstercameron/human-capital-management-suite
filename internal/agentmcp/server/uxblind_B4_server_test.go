package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
)

type b4Verifier struct {
	claims       map[string]TokenClaims
	seenAudience string
}

func (v *b4Verifier) Verify(_ context.Context, token, audience string) (TokenClaims, error) {
	v.seenAudience = audience
	claims, ok := v.claims[token]
	if !ok {
		return TokenClaims{}, errors.New("unknown token")
	}
	return claims, nil
}

type b4Gateway struct {
	records       []agentskills.SkillRecord
	listCalls     int
	callCalls     int
	lastCaller    Caller
	lastArguments json.RawMessage
}

func (g *b4Gateway) ListSkills(_ context.Context, caller Caller) ([]agentskills.SkillRecord, error) {
	g.listCalls++
	g.lastCaller = caller
	return append([]agentskills.SkillRecord(nil), g.records...), nil
}

func (g *b4Gateway) CallSkill(_ context.Context, caller Caller, record agentskills.SkillRecord, arguments json.RawMessage) (SkillResult, error) {
	g.callCalls++
	g.lastCaller = caller
	g.lastArguments = append(json.RawMessage(nil), arguments...)
	return SkillResult{Content: []Content{{Type: "text", Text: record.Definition.ID + " called"}}}, nil
}

type b4Approvals struct{ approval Approval }

func (a b4Approvals) CreateApproval(_ context.Context, req ApprovalRequest) (Approval, error) {
	if req.Tier < agentskills.TierSubmitGoverned || len(req.Arguments) == 0 || req.ArgumentsDigest == "" {
		return Approval{}, errors.New("approval request was not exact")
	}
	approval := a.approval
	approval.Digest = req.ArgumentsDigest
	return approval, nil
}

func b4Record(id string, version uint32, tier agentskills.SideEffectTier) agentskills.SkillRecord {
	return agentskills.SkillRecord{
		Definition: agentskills.SkillDefinition{
			ID: id, Version: version, Description: id + " skill", SideEffectTier: tier,
			InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`),
		},
		Status: agentskills.StatusActive, HighestCapabilityTier: tier,
	}
}

func b4Fixture(t *testing.T) (*Server, *b4Verifier, *b4Gateway) {
	t.Helper()
	now := time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC)
	verifier := &b4Verifier{claims: map[string]TokenClaims{
		"user-token": {Subject: "user-1", Tenant: "tenant-1", ClientID: "client-1", Audience: []string{"https://hcm.example/mcp"}, Scopes: []string{"hcm.skills"}, Active: true, CurrentUser: true, ClientApproved: true, ExpiresAt: now.Add(time.Hour)},
	}}
	gateway := &b4Gateway{records: []agentskills.SkillRecord{
		b4Record("people.lookup", 1, agentskills.TierRead),
		b4Record("draft.note", 2, agentskills.TierPrivateDraft),
		b4Record("promotion.submit", 1, agentskills.TierSubmitGoverned),
		b4Record("vendor.write", 1, agentskills.TierExternalWrite),
	}}
	server, err := New(Config{
		Resource: "https://hcm.example/mcp", MetadataURL: "https://hcm.example/.well-known/oauth-protected-resource",
		AuthorizationServers: []string{"https://login.example"}, ScopesSupported: []string{"hcm.skills"},
		Verifier: verifier, Skills: gateway, ApprovalURLBase: "https://hcm.example/agent-approvals", Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return server, verifier, gateway
}

func b4Request(t *testing.T, h http.Handler, method, path, token string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func b4RPC(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("response JSON: %v (%s)", err, rec.Body.String())
	}
	return response
}

// TestTodo_AGENT2_009 proves the endpoint binds discovery and invocation to
// the current caller and returns a product approval link instead of executing
// a governed write.
func TestTodo_AGENT2_009(t *testing.T) {
	server, _, gateway := b4Fixture(t)
	h := server.Handler()
	initialized := b4Request(t, h, http.MethodPost, "/mcp", "user-token", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	if initialized.Code != http.StatusOK || b4RPC(t, initialized)["result"] == nil {
		t.Fatalf("initialize = %d %s", initialized.Code, initialized.Body.String())
	}
	listed := b4Request(t, h, http.MethodPost, "/mcp", "user-token", `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	if listed.Code != http.StatusOK {
		t.Fatalf("tools/list status = %d", listed.Code)
	}
	var list struct {
		Result agentskills.MCPToolsList `json:"result"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Result.Tools) != 2 || list.Result.Tools[0].Name != "draft.note/v2" || list.Result.Tools[1].Name != "people.lookup/v1" {
		t.Fatalf("T0/T1 discovery = %#v", list.Result.Tools)
	}
	called := b4Request(t, h, http.MethodPost, "/mcp", "user-token", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"people.lookup/v1","arguments":{"subject":"worker-1"}}}`)
	if called.Code != http.StatusOK || b4RPC(t, called)["error"] != nil || gateway.callCalls != 1 {
		t.Fatalf("T0 call = %d %s calls=%d", called.Code, called.Body.String(), gateway.callCalls)
	}
	approval := b4Request(t, h, http.MethodPost, "/mcp", "user-token", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"promotion.submit/v1","arguments":{"worker":"worker-1"}}}`)
	if approval.Code != http.StatusOK || !strings.Contains(approval.Body.String(), "agent-approvals") || gateway.callCalls != 1 {
		t.Fatalf("T3 approval = %d %s calls=%d", approval.Code, approval.Body.String(), gateway.callCalls)
	}
	if gateway.lastCaller.UserID != "user-1" || string(gateway.lastArguments) != `{"subject":"worker-1"}` {
		t.Fatalf("trusted caller/arguments not passed to gate: %+v %s", gateway.lastCaller, gateway.lastArguments)
	}
}

// TestTodo_AGENT2_009_Security covers audience confusion, inactive/non-user
// subjects, unapproved clients, duplicate bearer headers, and hidden skills.
func TestTodo_AGENT2_009_Security(t *testing.T) {
	server, verifier, _ := b4Fixture(t)
	now := time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC)
	verifier.claims["wrong-audience"] = TokenClaims{Subject: "user-1", Tenant: "tenant-1", ClientID: "client-1", Audience: []string{"https://other.example"}, Active: true, CurrentUser: true, ClientApproved: true, ExpiresAt: now.Add(time.Hour)}
	verifier.claims["agent-subject"] = TokenClaims{Subject: "agent-1", Tenant: "tenant-1", ClientID: "client-1", Audience: []string{"https://hcm.example/mcp"}, Active: true, CurrentUser: false, ClientApproved: true, ExpiresAt: now.Add(time.Hour)}
	verifier.claims["unapproved"] = TokenClaims{Subject: "user-1", Tenant: "tenant-1", ClientID: "client-2", Audience: []string{"https://hcm.example/mcp"}, Active: true, CurrentUser: true, ClientApproved: false, ExpiresAt: now.Add(time.Hour)}
	for _, token := range []string{"wrong-audience", "agent-subject", "unapproved", "missing"} {
		rec := b4Request(t, server, http.MethodPost, "/mcp", token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("token %q status=%d www-auth=%q", token, rec.Code, rec.Header().Get("WWW-Authenticate"))
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Add("Authorization", "Bearer user-token")
	req.Header.Add("Authorization", "Bearer wrong-audience")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("duplicate authorization status=%d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "promotion.submit") || strings.Contains(rec.Body.String(), "vendor.write") {
		t.Fatalf("unauthorized high-tier skills disclosed: %s", rec.Body.String())
	}
}

// TestTodo_AGENT2_009_Integration exercises the real HTTP client boundary,
// including the protected-resource challenge and the JSON-RPC projection.
func TestTodo_AGENT2_009_Integration(t *testing.T) {
	server, _, _ := b4Fixture(t)
	ts := httptest.NewServer(server)
	defer ts.Close()
	res, err := http.Post(ts.URL+"/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized || !strings.Contains(res.Header.Get("WWW-Authenticate"), "resource_metadata") {
		t.Fatalf("unauthenticated integration response = %d %q", res.StatusCode, res.Header.Get("WWW-Authenticate"))
	}
	req, err := http.NewRequest(http.MethodGet, ts.URL+metadataPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer metadata.Body.Close()
	body, _ := io.ReadAll(metadata.Body)
	if metadata.StatusCode != http.StatusOK || !strings.Contains(string(body), "https://hcm.example/mcp") {
		t.Fatalf("metadata = %d %s", metadata.StatusCode, body)
	}
}

// TestTodo_AGENT2_009_Conformance pins the protocol revision, metadata shape,
// MCP tool schema projection, and the approval digest contract.
func TestTodo_AGENT2_009_Conformance(t *testing.T) {
	_, _, gateway := b4Fixture(t)
	if ProtocolRevision != agentskills.MCPProtocolRevision {
		t.Fatalf("protocol revision = %q", ProtocolRevision)
	}
	if err := agentskills.ValidateMCPProjection(ProtocolRevision, agentskills.MCPToolsList{Tools: []agentskills.MCPTool{{Name: "x/v1", Description: "x", InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`)}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Resource: "", Verifier: &b4Verifier{}, Skills: gateway}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid config error = %v", err)
	}
	approvalServer, err := New(Config{Resource: "https://hcm.example/mcp", Verifier: &b4Verifier{claims: map[string]TokenClaims{"ok": {Subject: "u", Tenant: "t", ClientID: "c", Audience: []string{"https://hcm.example/mcp"}, Active: true, CurrentUser: true, ClientApproved: true, ExpiresAt: time.Now().Add(time.Hour)}}}, Skills: gateway, Approvals: b4Approvals{approval: Approval{URL: "https://hcm.example/approve/1"}}})
	if err != nil {
		t.Fatal(err)
	}
	rec := b4Request(t, approvalServer, http.MethodPost, "/mcp", "ok", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"promotion.submit/v1","arguments":{"worker":"w"}}}`)
	if !strings.Contains(rec.Body.String(), "https://hcm.example/approve/1") || !strings.Contains(rec.Body.String(), "sha256:") {
		t.Fatalf("approval conformance body = %s", rec.Body.String())
	}
}
