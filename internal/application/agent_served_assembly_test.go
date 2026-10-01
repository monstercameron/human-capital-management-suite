package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_AGENT_031_ServedAssemblyConfiguration(t *testing.T) {
	assembly, err := composeAgentServedAssembly(agentServedAssemblyInput{})
	if err != nil || assembly != nil {
		t.Fatalf("unconfigured isolated database = %v %v", assembly, err)
	}
	if _, err := composeAgentServedAssembly(agentServedAssemblyInput{BackgroundRequired: true}); !errors.Is(err, agentrun.ErrAuthorityMissing) {
		t.Fatalf("required background accepted missing store: %v", err)
	}
	if _, err := composeAgentServedAssembly(agentServedAssemblyInput{ActionAuthorityFactory: func(*CommonAgentRuntime) (AgentActionAuthority, error) {
		t.Fatal("action authority factory called without durable runtime")
		return nil, nil
	}}); !errors.Is(err, agentrun.ErrAuthorityMissing) {
		t.Fatalf("configured action authority accepted missing store: %v", err)
	}
	if _, err := composeAgentServedAssembly(agentServedAssemblyInput{AgentDatabase: composedAgentDatabase{store: &agentstore.Store{}}}); err == nil {
		t.Fatal("configured service accepted missing core authority")
	}
	var absent *agentServedAssembly
	if err := absent.BindPlatform(nil); err == nil {
		t.Fatal("unconfigured platform memory bound")
	}
	if err := absent.TickTenant(context.Background(), "tenant"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	absent.Overlay(nil, transport.Config{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/other", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("absent assembly fabricated route: %d", w.Code)
	}
}

func TestTodo_AGENT_035_ServedAssemblyPlatformMemory_Integration(t *testing.T) {
	f := newAgentFixture(t)
	now := func() time.Time { return time.Now().UTC() }
	mapper := tenantKeyMapper[values.TenantId](pgstore.TenantID)
	memory, err := NewAgentMemoryOperations(f.pool, mapper, f.cell.RoleAccess, now)
	if err != nil {
		t.Fatal(err)
	}
	owners, err := NewAgentOwnerOperations(f.pool, mapper, f.cell.RoleAccess, now)
	if err != nil {
		t.Fatal(err)
	}
	assembly := &agentServedAssembly{Memory: memory, Owners: owners}
	if err := assembly.BindPlatform(f.runtime); err != nil {
		t.Fatal(err)
	}
	if f.runtime.Owner.memory != memory || memory.taskAuthority != f.runtime.Platform {
		t.Fatal("worker retention and served controls use different authority owners")
	}
	if err := assembly.BindPlatform(f.runtime); err == nil {
		t.Fatal("already served retention owner rebound")
	}
}

func TestTodo_AGENT_031_ServedNativeSourceKeys(t *testing.T) {
	request := commonAgentTestRequest(time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC))
	request.Source = agentrun.SourceIdentity{TenantID: "common-tenant", Kind: agentrun.SourceWorkflow, Ref: "workflow:" + uuid.NewString() + ":invoke"}
	keys := agentServedSourceKeys{}
	key, err := keys.ResolveSourceKey(context.Background(), request)
	expected, convertErr := (agentrun.CanonicalSourceConverter{}).ConvertSource(context.Background(), request)
	if err != nil || convertErr != nil || key == "" || key != expected.Key {
		t.Fatalf("workflow occurrence recovery = %q %v", key, err)
	}
	request.Source.Kind = agentrun.SourceSchedule
	if _, err := keys.ResolveSourceKey(context.Background(), request); !errors.Is(err, agentrun.ErrAuthorityMissing) {
		t.Fatalf("native schedule key fabricated without owner: %v", err)
	}
	request.Source.Kind = agentrun.SourceAPI
	if _, err := keys.ResolveSourceKey(context.Background(), request); !errors.Is(err, agentrun.ErrSourceConverter) {
		t.Fatalf("unconfigured source accepted: %v", err)
	}
}

func TestTodo_AGENT_036_ServedAssembly_Integration(t *testing.T) {
	f := newAgentActionFixture(t, true)
	core, err := pgxadapter.NewPool(context.Background(), f.db.URL, map[string]string{"search_path": f.db.Schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(core.Close)
	agentDB := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(context.Background(), agentDB.SQL); err != nil {
		t.Fatal(err)
	}
	agents := commonAgentOpenIntegrationStore(t, agentDB)
	assembly, err := composeAgentServedAssembly(agentServedAssemblyInput{Core: core, AgentDatabase: composedAgentDatabase{store: agents}, Cell: f.cell, ActionAuthority: f.authority})
	if err != nil || assembly == nil || assembly.Actions == nil || assembly.Portable == nil || assembly.Controls == nil {
		t.Fatalf("actual owner assembly = %+v %v", assembly, err)
	}
	if assembly.Common != nil || assembly.Schedules != nil {
		t.Fatal("unconfigured background model authority enabled inference admission")
	}
	if _, err := composeAgentServedAssembly(agentServedAssemblyInput{Core: core, AgentDatabase: composedAgentDatabase{store: agents}, Cell: f.cell, BackgroundRequired: true}); !errors.Is(err, agentrun.ErrAuthorityMissing) {
		t.Fatalf("required background accepted missing model authority: %v", err)
	}
	if _, err := composeAgentServedAssembly(agentServedAssemblyInput{Core: core, AgentDatabase: composedAgentDatabase{store: agents}, Cell: f.cell, Models: &commonAgentTestModelPolicy{}}); !errors.Is(err, agentrun.ErrAuthorityMissing) {
		t.Fatalf("configured model policy accepted missing native execution owners: %v", err)
	}
	principal, ok := trust.FromContext(f.ctx)
	if !ok {
		t.Fatal("fixture omitted actual principal")
	}
	admission := transport.Config{Now: func() time.Time { return principal.IssuedAt().Add(time.Minute) }, Verifier: trust.VerifierFunc(func(_ context.Context, credential trust.Credential) (*trust.Principal, error) {
		if credential.Token != "served-assembly-credential" || credential.Scheme != "Bearer" {
			return nil, trust.ErrNoCredential
		}
		return principal, nil
	})}
	fallbackCalls := 0
	handler := assembly.Overlay(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fallbackCalls++; w.WriteHeader(http.StatusNoContent) }), admission)
	body, err := json.Marshal(f.req)
	if err != nil {
		t.Fatal(err)
	}
	compile := func(headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, AgentActionsPath+"compile", bytes.NewReader(body))
		for key, value := range headers {
			r.Header.Set(key, value)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := compile(nil); w.Code != http.StatusUnauthorized || f.count(t, "intent_instance") != 0 {
		t.Fatalf("anonymous command reached durable owner: %d %s", w.Code, w.Body.String())
	}
	if w := compile(map[string]string{"Authorization": "Bearer served-assembly-credential", "X-Tenant-ID": "foreign"}); w.Code != http.StatusBadRequest || f.count(t, "intent_instance") != 0 {
		t.Fatalf("caller selected tenant reached durable owner: %d %s", w.Code, w.Body.String())
	}
	w := compile(map[string]string{"Authorization": "Bearer served-assembly-credential"})
	var draft AgentActionState
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &draft) != nil || draft.IntentID == "" || draft.ProposalDigest == "" || f.count(t, "intent_instance") != 1 || f.count(t, "workflow_instance") != 0 {
		t.Fatalf("admitted HTTP command did not reach real draft/simulation owner: %d %s", w.Code, w.Body.String())
	}
	// The recomposed transport reads the durable draft by its owner ID.
	restarted, err := composeAgentServedAssembly(agentServedAssemblyInput{Core: core, AgentDatabase: composedAgentDatabase{store: agents}, Cell: f.cell})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, AgentActionsPath+draft.IntentID, nil)
	r.Header.Set("Authorization", "Bearer served-assembly-credential")
	w = httptest.NewRecorder()
	restarted.Overlay(nil, admission).ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), draft.IntentID) || !strings.Contains(w.Body.String(), "draft") {
		t.Fatalf("restarted transport lost real durable result: %d %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest(http.MethodGet, "/other", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || fallbackCalls != 1 {
		t.Fatalf("assembly swallowed unrelated route: %d calls=%d", w.Code, fallbackCalls)
	}
}

func TestTodo_AGENT_041_ServedAssemblyAdmission(t *testing.T) {
	surface := &AgentControlsSurface{Schedules: &agentControlsSurfaceFixture{reply: agentcontrols.Reply{}}}
	handler := (&agentServedAssembly{Controls: surface}).Overlay(nil, transport.Config{Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return nil, trust.ErrNoCredential })})
	for _, path := range []string{agentcontrols.Path, AgentPortableExportPath, AgentPortableImportPath, AgentPortableDraftPath, AgentActionsPath + "compile"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"invalid":true}`)))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s decoded before verified admission: %d %s", path, w.Code, w.Body.String())
		}
	}
}

func TestTodo_AGENT_036_ServedAssemblyCookieCSRF(t *testing.T) {
	principal := agentUserCatalogPrincipal(t)
	verifications := 0
	admission := transport.Config{Verifier: trust.VerifierFunc(func(_ context.Context, credential trust.Credential) (*trust.Principal, error) {
		verifications++
		if credential.Scheme != "Bearer" || credential.Token != "session-credential" {
			return nil, trust.ErrNoCredential
		}
		return principal, nil
	})}
	assembly := &agentServedAssembly{browserLogin: true}
	handler := assembly.Overlay(nil, admission)
	request := func(origin string, csrf bool) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "http://cell.test"+AgentActionsPath+"compile", strings.NewReader(`{}`))
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		r.AddCookie(&http.Cookie{Name: "hcmnext_session", Value: "session-credential"})
		if csrf {
			r.AddCookie(&http.Cookie{Name: edge.BrowserCSRFCookieName, Value: "csrf-proof"})
		}
		return r
	}
	for _, r := range []*http.Request{request("https://foreign.test", true), request("http://cell.test", false), request("", false)} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden || verifications != 0 {
			t.Fatalf("browser proof refused after identity/owner access: %d verification=%d", w.Code, verifications)
		}
	}
	r := request("http://cell.test", true)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest || verifications != 1 || r.Header.Get("Authorization") != "" {
		t.Fatalf("valid session did not reach admitted decoder or mutated original: %d verification=%d", w.Code, verifications)
	}
	assembly.browserLogin = false
	w = httptest.NewRecorder()
	assembly.Overlay(nil, admission).ServeHTTP(w, request("http://cell.test", true))
	if w.Code != http.StatusUnauthorized || verifications != 2 {
		t.Fatalf("disabled browser login accepted session cookie: %d verification=%d", w.Code, verifications)
	}
}
