package workspace

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// memoryAgentSettings is the in-memory form of the tenant agents setting.
type memoryAgentSettings struct {
	mu      sync.Mutex
	enabled map[values.TenantId]bool
	actors  []string
	readErr error
}

func (s *memoryAgentSettings) AgentsEnabled(_ context.Context, tenant values.TenantId) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled[tenant], s.readErr
}

func (s *memoryAgentSettings) SetAgentsEnabled(_ context.Context, tenant values.TenantId, enabled bool, actor string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.enabled == nil {
		s.enabled = map[values.TenantId]bool{}
	}
	s.enabled[tenant] = enabled
	s.actors = append(s.actors, actor)
	return nil
}

// ownerAgentClient answers each principal with only their own task, and
// records who asked.
type ownerAgentClient struct {
	mu       sync.Mutex
	requests []productui.AgentSnapshotRequest
	err      error
}

func (c *ownerAgentClient) Snapshot(_ context.Context, req productui.AgentSnapshotRequest) (productui.AgentSnapshot, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req)
	c.mu.Unlock()
	if c.err != nil {
		return productui.AgentSnapshot{}, c.err
	}
	return productui.AgentSnapshot{Availability: productui.AgentsAvailable, Tasks: []productui.AgentTask{{
		ID: "task-" + req.Principal, Version: 8, Title: "Task owned by " + req.Principal, Goal: "Goal of " + req.Principal, State: productui.AgentTaskRunning,
		Actions: productui.AgentTaskActionPolicy{Pause: true, Cancel: true},
		Steps:   []productui.AgentTaskStep{{Name: "skill.lookup", State: "running", Tier: "T0"}},
	}}}, nil
}

type uxblind122Persona struct {
	id, name, subject string
	roles             []string
}

var (
	uxblind122Admin  = uxblind122Persona{id: "admin", name: "Rafael Torres", subject: "hc-050-rafael-torres", roles: []string{productui.RoleHCMAdmin, "comp_admin"}}
	uxblind122Worker = uxblind122Persona{id: "individual-contributor", name: "Linh Tran", subject: "hc-051-linh-tran", roles: []string{"worker_self"}}
)

func uxblind122Server(t *testing.T, settings AgentSettings, client productui.AgentClient) *httptest.Server {
	t.Helper()
	handler, _ := newShellHandler(t, true)
	handler.roleAccess = launcherRoleAccessStore{snapshot: roleaccess.Snapshot{PagePermissions: roleaccess.DefaultPagePermissions()}}
	handler.now = func() time.Time { return time.Now().UTC() }
	handler.agentSettings, handler.agents = settings, client
	handler.devPersonas = map[string]DevPersona{}
	for _, persona := range []uxblind122Persona{uxblind122Admin, uxblind122Worker} {
		handler.devPersonas[persona.id] = DevPersona{ID: persona.id, Name: persona.name, WorkerRef: persona.subject, Token: frontendE2EToken(t, persona.subject, persona.roles)}
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func uxblind122SignIn(t *testing.T, server *httptest.Server, persona uxblind122Persona) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	// server.Client() is shared; each persona needs its own client and jar.
	client := &http.Client{Transport: server.Client().Transport}
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	login, err := client.PostForm(server.URL+PathLogin, url.Values{paramLoginPersona: {persona.id}})
	if err != nil {
		t.Fatal(err)
	}
	login.Body.Close()
	if login.StatusCode != http.StatusSeeOther {
		t.Fatalf("sign in as %s = %d", persona.id, login.StatusCode)
	}
	return client
}

func uxblind122Get(t *testing.T, client *http.Client, target string) string {
	t.Helper()
	response, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d (%v)", target, response.StatusCode, err)
	}
	return string(body)
}

func uxblind122Post(t *testing.T, client *http.Client, target, contentType, body string) int {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", contentType)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	return response.StatusCode
}

// renderIslandPage renders the Agents route from the served island through
// the same projection the browser client installs.
func renderIslandPage(t *testing.T, config JourneyConfig, locale string) string {
	t.Helper()
	view := productui.ApplyLocale(productui.NewView(productui.PageAgents, config.Tenant, config.Subject, ""), productui.ResolveProductLocale(locale))
	view = productui.ApplyPagePermissions(view, productPagePermissions(config.PagePermissions))
	view = productui.ApplyAgentsAvailability(view, ProductAgentsAvailability(config.Agents))
	markup, err := ui.RenderToString(productui.BuildAgentsSurface(view))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

const agentsNavHref = `href="/workspace/app/chat/agents"`

// TestTodo_UXBLIND_122_Browser drives the browser-facing HTTP contract through
// a real loopback server with signed-in personas: the served navigation, the
// config island, the settings write and the page rendered from that island.
// It does not launch a browser; the interactive pass is recorded in the
// devlog/report.
func TestTodo_UXBLIND_122_Browser(t *testing.T) {
	settings := &memoryAgentSettings{}
	agents := &ownerAgentClient{}
	server := uxblind122Server(t, settings, agents)
	admin := uxblind122SignIn(t, server, uxblind122Admin)
	worker := uxblind122SignIn(t, server, uxblind122Worker)
	agentsURL := server.URL + "/workspace/app/chat/agents"

	// Disabled tenant: the worker's menu has no Agents entry and a direct
	// visit is a truthful unavailable state; the admin sees the entry, a
	// reason and the link to the setting.
	workerBody := uxblind122Get(t, worker, server.URL+PathProductHome)
	if strings.Contains(workerBody, agentsNavHref) {
		t.Fatal("a regular viewer's menu advertises Agents on a tenant without agents")
	}
	workerIsland := island(t, uxblind122Get(t, worker, agentsURL))
	if workerIsland.Agents == nil || workerIsland.Agents.Enabled || workerIsland.Agents.ViewerIsAdmin || workerIsland.Agents.SettingsHref != "" || len(workerIsland.Agents.Tasks) != 0 {
		t.Fatalf("worker island = %+v", workerIsland.Agents)
	}
	if page := renderIslandPage(t, workerIsland, "en-US"); !strings.Contains(page, `data-agents-surface="disabled_hidden"`) || strings.Contains(page, "chat-settings") {
		t.Fatalf("worker direct visit = %s", page)
	}
	adminBody := uxblind122Get(t, admin, agentsURL)
	if !strings.Contains(adminBody, agentsNavHref) {
		t.Fatal("an administrator's menu hides Agents instead of explaining it")
	}
	adminIsland := island(t, adminBody)
	if adminIsland.Agents == nil || adminIsland.Agents.Enabled || !adminIsland.Agents.ViewerIsAdmin || adminIsland.Agents.SettingsHref != productui.AgentsSettingsHref() {
		t.Fatalf("admin island = %+v", adminIsland.Agents)
	}
	for locale, want := range map[string]string{"en-US": "Agents are turned off for your organization", "de-DE": "Agenten sind für Ihre Organisation ausgeschaltet", "ar": "الوكلاء متوقفون لمؤسستك"} {
		page := renderIslandPage(t, adminIsland, locale)
		if !strings.Contains(page, want) || !strings.Contains(page, `href="/workspace/app/admin/chat-settings"`) {
			t.Fatalf("%s admin disabled page = %s", locale, page)
		}
	}
	if len(agents.requests) != 0 {
		t.Fatalf("the agent runtime was read for a disabled tenant: %+v", agents.requests)
	}

	// The setting is no longer writable over HTTP: the route is gone, and the
	// only write path is the AgentService RPC guarded by CanChangeAgentsSetting.
	// The worker may not change the setting; the administrator may.
	store := launcherRoleAccessStore{snapshot: roleaccess.Snapshot{PagePermissions: roleaccess.DefaultPagePermissions()}}
	if ok, err := CanChangeAgentsSetting(context.Background(), store, uxblind122PrincipalFor(t, uxblind122Worker)); err != nil || ok {
		t.Fatalf("worker may change the agents setting: ok=%v err=%v", ok, err)
	}
	if ok, err := CanChangeAgentsSetting(context.Background(), store, uxblind122PrincipalFor(t, uxblind122Admin)); err != nil || !ok {
		t.Fatalf("admin may not change the agents setting: ok=%v err=%v", ok, err)
	}
	if status := uxblind122Post(t, admin, server.URL+"/workspace/agent-settings", "application/json", `{"enabled":true}`); status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
		t.Fatalf("the retired HTTP agent-settings route answered %d", status)
	}
	if settings.enabled[shellTenant] {
		t.Fatal("the retired HTTP route turned agents on")
	}
	if err := settings.SetAgentsEnabled(context.Background(), shellTenant, true, uxblind122Admin.subject); err != nil {
		t.Fatal(err)
	}
	if got := settings.actors; len(got) != 1 || got[0] != uxblind122Admin.subject {
		t.Fatalf("setting actors = %v", got)
	}

	// Enabled tenant: every viewer sees the entry and only their own tasks.
	workerBody = uxblind122Get(t, worker, agentsURL+"?principal=hc-050-rafael-torres")
	if !strings.Contains(workerBody, agentsNavHref) {
		t.Fatal("enabling agents did not advertise the entry")
	}
	workerIsland = island(t, workerBody)
	if workerIsland.Agents == nil || !workerIsland.Agents.Enabled || workerIsland.Agents.Service != "available" ||
		len(workerIsland.Agents.Tasks) != 1 || workerIsland.Agents.Tasks[0].ID != "task-hc-051-linh-tran" ||
		workerIsland.Agents.Tasks[0].Version != 8 || !workerIsland.Agents.Tasks[0].Actions.Pause || !workerIsland.Agents.Tasks[0].Actions.Cancel {
		t.Fatalf("worker enabled island = %+v", workerIsland.Agents)
	}
	pageProjection := ProductAgentsAvailability(workerIsland.Agents)
	if len(pageProjection.Snapshot.Tasks) != 1 || pageProjection.Snapshot.Tasks[0].Version != 8 ||
		!pageProjection.Snapshot.Tasks[0].Actions.Pause || !pageProjection.Snapshot.Tasks[0].Actions.Cancel {
		t.Fatalf("workspace projection lost task control state: %+v", pageProjection.Snapshot.Tasks)
	}
	page := renderIslandPage(t, workerIsland, "en-US")
	if !strings.Contains(page, "Task owned by hc-051-linh-tran") || strings.Contains(page, "hc-050-rafael-torres") {
		t.Fatalf("worker enabled page = %s", page)
	}
	last := agents.requests[len(agents.requests)-1]
	if last.Principal != uxblind122Worker.subject || last.TenantID != shellTenant {
		t.Fatalf("snapshot requested for %+v, want the signed-in subject", last)
	}
}

func TestTodo_UXBLIND_122_Security(t *testing.T) {
	settings := &memoryAgentSettings{enabled: map[values.TenantId]bool{shellTenant: true}}
	server := uxblind122Server(t, settings, &ownerAgentClient{err: errors.New("runtime down")})
	admin := uxblind122SignIn(t, server, uxblind122Admin)
	// An erroring agent client fails closed: enabled, service unavailable,
	// no tasks, and the page states it.
	adminIsland := island(t, uxblind122Get(t, admin, server.URL+"/workspace/app/chat/agents"))
	if adminIsland.Agents == nil || !adminIsland.Agents.Enabled || adminIsland.Agents.Service != "unavailable" || len(adminIsland.Agents.Tasks) != 0 {
		t.Fatalf("erroring client island = %+v", adminIsland.Agents)
	}
	if page := renderIslandPage(t, adminIsland, "en-US"); !strings.Contains(page, "Agents are not available yet") {
		t.Fatalf("erroring client page = %s", page)
	}
	// The authority check fails closed: no principal, and a role store that
	// errors, are both refusals, and an unrelated tenant setting stays put.
	if ok, err := CanChangeAgentsSetting(context.Background(), launcherRoleAccessStore{snapshot: roleaccess.Snapshot{PagePermissions: roleaccess.DefaultPagePermissions()}}, nil); ok || err != nil {
		t.Fatalf("anonymous authority = %v, %v", ok, err)
	}
	if ok, err := CanChangeAgentsSetting(context.Background(), uxblind122FailingRoles{err: errors.New("db down")}, uxblind122PrincipalFor(t, uxblind122Admin)); ok || err == nil {
		t.Fatalf("role store failure authority = %v, %v", ok, err)
	}
	if !settings.enabled[shellTenant] {
		t.Fatal("a refused authority check changed the setting")
	}
	// A failed setting read keeps agents off; a missing store keeps them off
	// and refuses writes.
	h := &Handler{agentSettings: &memoryAgentSettings{enabled: map[values.TenantId]bool{shellTenant: true}, readErr: errors.New("db down")}, agents: &ownerAgentClient{}}
	principal := uxblind122Principal(t)
	if got := h.resolveAgents(context.Background(), principal, productAccess{roles: []string{"worker_self"}}); got == nil || got.Enabled || len(got.Tasks) != 0 {
		t.Fatalf("failed read = %+v", got)
	}
	if got := (&Handler{}).resolveAgents(context.Background(), principal, productAccess{roles: []string{"comp_admin"}}); got == nil || got.Enabled || !got.ViewerIsAdmin {
		t.Fatalf("no store = %+v", got)
	}
	if got := h.resolveAgents(context.Background(), nil, productAccess{}); got != nil {
		t.Fatalf("anonymous projection = %+v", got)
	}
	if got := ProductAgentsAvailability(nil); got.Enabled || got.ViewerIsAdmin {
		t.Fatalf("missing island projection = %+v", got)
	}
	// A forged island that claims tasks while disabled renders none.
	forged := ProductAgentsAvailability(&AgentsConfig{ViewerIsAdmin: true, SettingsHref: "https://evil.example", Tasks: []AgentTaskConfig{{ID: "x", Title: "forged", State: "running"}}})
	if len(forged.Snapshot.Tasks) != 0 || forged.SettingsHref != productui.AgentsSettingsHref() {
		t.Fatalf("forged island = %+v", forged)
	}
}

func uxblind122Principal(t *testing.T) *trust.Principal {
	t.Helper()
	return uxblind122PrincipalFor(t, uxblind122Worker)
}

func uxblind122PrincipalFor(t *testing.T, persona uxblind122Persona) *trust.Principal {
	t.Helper()
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: shellSigningKey, Issuer: shellIssuer, Audience: shellAudience, Now: func() time.Time { return shellNow }})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: frontendE2EToken(t, persona.subject, persona.roles), Audience: shellAudience})
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

// uxblind122FailingRoles is a role store whose read always fails.
type uxblind122FailingRoles struct {
	roleaccess.Store
	err error
}

func (s uxblind122FailingRoles) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	return roleaccess.Snapshot{}, s.err
}
