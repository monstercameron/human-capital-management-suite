package application

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentconnect"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/secrets"
)

// These tests drive the agent access, connection console, cost and extend-budget
// routes through the real HTTP overlay (OverlayAgentAccess) that the served
// assembly mounts, over a real core database and an agent database opened as
// the runtime role, which has SELECT, INSERT and UPDATE only. The overlay's
// services come from composeAgentAccess, as they do when the cell is served.
// People are told apart by their bearer credential; nothing in a request body
// can name who is asking.

const (
	servedAccessAdminOne = "ir-001-walt-brennan"
	servedAccessAdminTwo = "ir-002-second-admin"
	servedAccessOwner    = "owner-olive"
	servedAccessStranger = "ir-099-stranger"
)

type agentAccessServed struct {
	t        *testing.T
	f        *agentFixture
	agentDB  *pgtest.DB
	agents   *agentstore.Store
	services *agentAccessServices
	handler  http.Handler
	tenant   string
	tenantID uuid.UUID
	now      func() time.Time
	// offset moves the services' clock forward from the real one: a day is passed by
	// adding 24 hours, not by freezing the clock before the work being tested.
	offset time.Duration
	tokens map[string]*trust.Principal
}

func servedAccessConnection(t *testing.T, id, tenant string) *connectivity.ConnectorConnection {
	t.Helper()
	version, err := connectivity.ParseVersion("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := connectivity.ParseCredentialRef("vault://tenant/connector")
	if err != nil {
		t.Fatal(err)
	}
	capability := connectivity.Capability{Object: connectivity.ObjectWorker, Operation: connectivity.OperationRead}
	definition := connectivity.ConnectorDefinition{ConnectorID: "hris", Version: version, AuthModes: []connectivity.AuthMode{connectivity.AuthOAuth2ClientCredentials}, Capabilities: []connectivity.Capability{capability}, Bounds: connectivity.Bounds{MaxPageSize: 100, MaxPagesPerRun: 10, MaxRecordsPerRun: 1000, MaxRecordBytes: 4096}}
	connection, err := connectivity.NewConnection(connectivity.Publication{Definition: definition}, connectivity.ConnectionSpec{ConnectionID: id, TenantID: tenant, OrgID: "org-a", SystemID: "system-a", Environment: connectivity.EnvironmentProduction, Residency: "us", ConnectorID: "hris", ConnectorVersion: version, AuthMode: connectivity.AuthOAuth2ClientCredentials, CredentialRef: credential, Scopes: []string{"worker.read"}, EndpointPolicy: connectivity.EndpointPolicy{AllowedHosts: []string{"hris.example"}, RequireTLS: true, EgressProfile: "egress/us"}, Capabilities: []connectivity.Capability{capability}, Bounds: definition.Bounds, CreatedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []connectivity.LifecycleState{connectivity.StateValidating, connectivity.StateReady, connectivity.StateActive} {
		if err := connection.Transition(state, connectivity.TransitionEvidence{Reason: "test", ActorRef: "admin:test", EvidenceRef: "evidence:test", OccurredAt: time.Unix(2, 0)}); err != nil {
			t.Fatal(err)
		}
	}
	return connection
}

type servedAccessProviders struct{ tenant string }

func (p servedAccessProviders) Provider(tenant, id string) (agentaccess.Provider, bool) {
	if tenant != p.tenant || id != "hris" {
		return agentaccess.Provider{}, false
	}
	return agentaccess.Provider{ConnectionID: "hris", Name: "HRIS", AuthorizeURL: "https://hris.example/oauth/authorize", ClientID: "hcm-next", RedirectURL: "https://cell.test/workspace/app/chat/agents?view=access", Scopes: []string{"worker.read"}}, true
}

type servedAccessSnapshots struct{}

func (servedAccessSnapshots) Tools(string, string) ([]agentaccess.MCPTool, error) {
	return []agentaccess.MCPTool{{Name: "workers.read", ReadOnly: true}}, nil
}

func servedAccessPrincipal(t *testing.T, tenant, subject string, roles []string, assurance trust.Assurance) *trust.Principal {
	t.Helper()
	now := time.Now().UTC()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: kernelvalues.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: servedAccessOrgScope, Roles: roles, Purposes: []string{"self_service_view"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: assurance, SessionRef: "session-" + subject,
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:" + subject,
	})
	if err != nil {
		t.Fatalf("principal %s: %v", subject, err)
	}
	return principal
}

var servedAccessOrgScope = demoworkforce.IronridgePack.OrgScope()

func newAgentAccessServed(t *testing.T) *agentAccessServed {
	t.Helper()
	ctx := context.Background()
	f := newAgentFixture(t)
	f.setEnabled(t, true)
	f.cell.AgentController = f.runtime.Controller
	agentDB := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, agentDB.SQL); err != nil {
		t.Fatal(err)
	}
	mapper := tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID)
	tenantID := mapper(kernelvalues.TenantId(f.tenant))
	agentDB.Exec(t, `INSERT INTO tenant (tenant_id) VALUES ($1)`, tenantID)
	createdAt := time.Now().UTC().Add(-time.Hour)
	agentDB.Exec(t, `INSERT INTO persona_versions(tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at) VALUES($1,'policy-helper',6,'agent.policy@6','policy-helper','Policy Helper','{}','sha256:`+strings.Repeat("a", 64)+`',$2)`, tenantID, createdAt)
	agentDB.Exec(t, `INSERT INTO persona_owners(tenant_id,persona_id,owner_role,principal_id,assigned_by,assigned_at) VALUES($1,'policy-helper','BUSINESS_OWNER',$2,'system',$3),($1,'policy-helper','TECHNICAL_STEWARD','sam','system',$3)`, tenantID, servedAccessOwner, createdAt)
	agents := commonAgentOpenIntegrationStore(t, agentDB)
	s := &agentAccessServed{t: t, f: f, agentDB: agentDB, agents: agents, tenant: f.tenant, tenantID: tenantID, tokens: map[string]*trust.Principal{}}
	s.now = func() time.Time { return time.Now().UTC().Add(s.offset) }
	s.compose()
	s.tokens["ana"] = agentTestPrincipal(t, f.tenant, agentTestWorker)
	s.tokens["admin-one"] = servedAccessPrincipal(t, f.tenant, servedAccessAdminOne, []string{"hcm_admin"}, trust.AssuranceSubstantial)
	s.tokens["admin-two"] = servedAccessPrincipal(t, f.tenant, servedAccessAdminTwo, []string{"hcm_admin"}, trust.AssuranceSubstantial)
	s.tokens["admin-two-low"] = servedAccessPrincipal(t, f.tenant, servedAccessAdminTwo, []string{"hcm_admin"}, trust.AssuranceLow)
	s.tokens["owner"] = servedAccessPrincipal(t, f.tenant, servedAccessOwner, []string{"worker_self"}, trust.AssuranceSubstantial)
	s.tokens["stranger"] = servedAccessPrincipal(t, f.tenant, servedAccessStranger, []string{"worker_self"}, trust.AssuranceSubstantial)
	return s
}

// compose builds the overlay the way the served assembly does, from the same
// databases. Calling it again is a restart: new services over the same rows.
func (s *agentAccessServed) compose() {
	s.t.Helper()
	exchange := func(_ context.Context, provider agentaccess.Provider, code, verifier string) (agentconnect.CredentialBinding, string, error) {
		if code == "" || verifier == "" {
			return agentconnect.CredentialBinding{}, "", agentaccess.ErrInvalid
		}
		return servedAccessBinding(s.tenant, "ana-grant"), "ana@hris.example", nil
	}
	services, err := composeAgentAccess(agentServedAssemblyInput{
		Core: s.f.pool, AgentDatabase: composedAgentDatabase{store: s.agents}, Cell: s.f.cell, Now: s.now,
		TenantUUID: tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID),
		AgentAccess: AgentAccessOptions{
			Providers: servedAccessProviders{tenant: s.tenant}, Exchange: exchange, Snapshots: servedAccessSnapshots{},
			Connections: []*connectivity.ConnectorConnection{servedAccessConnection(s.t, "hris", s.tenant)},
		},
	}, nil)
	if err != nil || services == nil {
		s.t.Fatalf("composeAgentAccess = %v, %v", services, err)
	}
	s.services = services
	admission := transport.Config{Verifier: trust.VerifierFunc(func(_ context.Context, credential trust.Credential) (*trust.Principal, error) {
		if credential.Scheme != "Bearer" {
			return nil, trust.ErrNoCredential
		}
		if principal, ok := s.tokens[credential.Token]; ok {
			return principal, nil
		}
		return nil, trust.ErrNoCredential
	})}
	s.handler = OverlayAgentAccess(http.NotFoundHandler(), services.HTTP, admission)
}

func servedAccessBinding(tenant, id string) agentconnect.CredentialBinding {
	return agentconnect.CredentialBinding{Reference: secrets.SecretReference{ID: id, Kind: secrets.OAuthGrant, Version: "v1", Provider: "vault", ProviderPath: "opaque/path", Tenant: tenant, Region: "us", State: secrets.Active}, Handle: custody.Handle{ID: id, Kind: custody.Secret, Version: "v1", Tenant: tenant, Region: "us"}}
}

// call sends one request as the person holding token (empty for nobody) and
// returns the status and the body.
func (s *agentAccessServed) call(method, path, token string, body any) (int, []byte) {
	s.t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			s.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	request := httptest.NewRequest(method, path, reader)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	s.handler.ServeHTTP(recorder, request)
	return recorder.Code, recorder.Body.Bytes()
}

func (s *agentAccessServed) ok(method, path, token string, body any, into any) {
	s.t.Helper()
	status, raw := s.call(method, path, token, body)
	if status != http.StatusOK {
		s.t.Fatalf("%s %s as %s = %d %s", method, path, token, status, raw)
	}
	if into != nil {
		if err := json.Unmarshal(raw, into); err != nil {
			s.t.Fatalf("%s %s: %v: %s", method, path, err, raw)
		}
	}
}

func (s *agentAccessServed) refused(want int, code, method, path, token string, body any) {
	s.t.Helper()
	status, raw := s.call(method, path, token, body)
	if status != want || !strings.Contains(string(raw), `"code":"`+code+`"`) {
		s.t.Fatalf("%s %s as %s = %d %s, want %d %s", method, path, token, status, raw, want, code)
	}
}

// anaRoles are the roles the access pages match grants against for Ana: the
// ones the role store assigned to her.
func (s *agentAccessServed) anaRoles() []string {
	s.t.Helper()
	snapshot, err := s.f.cell.RoleAccess.Load(context.Background(), kernelvalues.TenantId(s.tenant), servedAccessOrgScope)
	if err != nil {
		s.t.Fatal(err)
	}
	roles := roleaccess.AssignedRoles(snapshot, agentTestWorker, nil)
	if len(roles) == 0 {
		roles = []string{"employee"}
	}
	return roles
}

// publishConnection runs the console's whole path for the HRIS connection over
// HTTP: create, set a tier, grant a read skill and a governed submission skill
// to Ana's roles, request a second administrator, have one approve, publish.
func (s *agentAccessServed) publishConnection() string {
	s.t.Helper()
	const console = AgentAccessPath + "/console"
	var snapshot productui.AgentAdminAccessSnapshot
	s.ok(http.MethodPost, console+"/create", "admin-one", agentAccessCreateBody{Revision: productui.AgentConnectionRevision{
		ID: "hris", Provider: "HRIS", CredentialMode: "USER_DELEGATED",
		Grants: []productui.AgentAdminSkillGrant{{SkillID: "workers.read", Tier: "T0"}, {SkillID: "promotions.submit", Tier: "T3"}},
	}}, &snapshot)
	revision := "hris#1"
	grants := []agentAccessGrantBody{{ID: "staff", Roles: s.anaRoles(), Population: "employees", OrganizationScopes: []string{servedAccessOrgScope}, Skills: []string{"workers.read", "promotions.submit"}}}
	s.ok(http.MethodPost, console+"/grants", "admin-one", agentAccessGrantsBody{RevisionID: revision, Grants: grants}, &snapshot)
	s.ok(http.MethodPost, console+"/request", "admin-one", agentAccessRevisionBody{RevisionID: revision}, &snapshot)
	s.ok(http.MethodPost, console+"/approve", "admin-two", agentAccessRevisionBody{RevisionID: revision}, &snapshot)
	s.ok(http.MethodPost, console+"/publish", "admin-one", agentAccessRevisionBody{RevisionID: revision}, &snapshot)
	return revision
}

func (s *agentAccessServed) startLongTask() (string, uint64) {
	s.t.Helper()
	ctx := context.Background()
	started, err := s.f.runtime.Starter.StartTaskMode(ctx, s.tokens["ana"], "Prepare my promotion request", agentclient.StartLongTask)
	if err != nil || started.ID == "" {
		s.t.Fatalf("StartTaskMode = %+v, %v", started, err)
	}
	return started.ID, started.Version
}

// TestTodo_AGENT2_017_ServedIntegration: the task view's Extend budget over the
// served route. A task paused at its ceiling is extended by exactly the policy's
// allowance for its owner, once for one request id, against its current budget
// revision; it stays paused; nobody else can, and a task that is not theirs
// answers like one that does not exist.
func TestTodo_AGENT2_017_ServedIntegration(t *testing.T) {
	s := newAgentAccessServed(t)
	ctx := context.Background()
	taskID, version := s.startLongTask()
	runner, err := s.f.runtime.Platform.ForTenant(ctx, kernelvalues.TenantId(s.tenant))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	confirmed, err := runner.Runtime.ConfirmPlan(ctx, taskID, agentTestWorker, version, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Runtime.Pause(ctx, taskID, confirmed.Version, now); err != nil {
		t.Fatal(err)
	}
	// An estimate past the task's ceiling stops it there, as a run that used it up would.
	if _, err := s.f.runtime.Budget.Reserve(ctx, agentbudget.Request{TaskID: taskID, StepID: "s1", Fingerprint: "s1", Estimate: agentbudget.Usage{Steps: 500, Tokens: 1, WallClock: 1, SpendMicros: 1}}); err == nil {
		t.Fatal("the estimate did not stop the task at its ceiling")
	}
	limit := func() int64 {
		for _, task := range s.f.runtime.Budget.Snapshot().Tasks {
			if task.ID == taskID {
				return task.Limit.Steps
			}
		}
		t.Fatal("the task has no budget")
		return 0
	}
	before, allowance := limit(), s.f.runtime.Budget.ExtensionAllowance(taskID).Steps
	if allowance == 0 {
		t.Fatal("the policy offers no extension")
	}
	const path = AgentTaskBudgetPath
	body := agentTaskBudgetBody{TaskID: taskID, RequestID: "extend-click-1"}

	// Who may not.
	s.refused(http.StatusUnauthorized, "request_denied", http.MethodPost, path, "", body)
	s.refused(http.StatusForbidden, "permission_denied", http.MethodPost, path, "admin-one", body)
	s.refused(http.StatusForbidden, "permission_denied", http.MethodPost, path, "stranger", body)
	s.refused(http.StatusForbidden, "permission_denied", http.MethodPost, path, "ana", agentTaskBudgetBody{TaskID: "agt_" + strings.Repeat("0", 32), RequestID: "extend-click-2"})
	s.refused(http.StatusMethodNotAllowed, "method_not_allowed", http.MethodPut, path, "ana", body)
	s.refused(http.StatusBadRequest, "invalid_request", http.MethodPost, path, "ana", map[string]string{"TaskID": taskID, "RequestID": "x", "Steps": "100"})
	if got := limit(); got != before {
		t.Fatalf("a refused request changed the budget: %d, was %d", got, before)
	}

	// The owner's click.
	var reply struct{ Extended bool }
	s.ok(http.MethodPost, path, "ana", body, &reply)
	if !reply.Extended || limit() != before+allowance {
		t.Fatalf("extended = %v, steps %d, want %d", reply.Extended, limit(), before+allowance)
	}
	// The same click again, as a slow double-click would send it, adds nothing.
	status, _ := s.call(http.MethodPost, path, "ana", body)
	if status != http.StatusOK && status != http.StatusConflict || limit() != before+allowance {
		t.Fatalf("a repeated click = %d, steps %d", status, limit())
	}
	// A task is extended once: a new click on the task that has been extended is refused, not applied.
	if again, _ := s.call(http.MethodPost, path, "ana", agentTaskBudgetBody{TaskID: taskID, RequestID: "extend-click-3"}); again != http.StatusConflict || limit() != before+allowance {
		t.Fatalf("a second extension = %d, steps %d; want it refused and the budget unchanged", again, limit())
	}
	// Extending does not resume it.
	task := s.f.task(t, taskID)
	if task.State != agentrun.StatePaused {
		t.Fatalf("task state after the extension = %s, want it left paused", task.State)
	}
	// A tenant that has turned agents off answers the call as disabled.
	s.f.setEnabled(t, false)
	s.refused(http.StatusConflict, "agents_disabled", http.MethodPost, path, "ana", agentTaskBudgetBody{TaskID: taskID, RequestID: "extend-click-4"})
	s.f.setEnabled(t, true)
}

// TestTodo_AGENT2_018_ServedIntegration: the person's own access page over the
// served routes. Only what an administrator granted her is listed; linking
// starts here with PKCE and completes from the provider's return with the state
// it was given; a state is good once; unlinking and revoking a task's grant take
// effect at once; nothing in a request names who is asking.
func TestTodo_AGENT2_018_ServedIntegration(t *testing.T) {
	s := newAgentAccessServed(t)
	const base = AgentAccessPath
	var snapshot productui.AgentAccessSnapshot
	// Before anything is published nobody is offered a connection.
	s.ok(http.MethodGet, base+"/snapshot", "ana", nil, &snapshot)
	if len(snapshot.Connections) != 0 {
		t.Fatalf("a connection nobody published is listed: %+v", snapshot.Connections)
	}
	s.publishConnection()
	s.refused(http.StatusUnauthorized, "request_denied", http.MethodGet, base+"/snapshot", "", nil)
	s.ok(http.MethodGet, base+"/snapshot", "ana", nil, &snapshot)
	if len(snapshot.Connections) != 1 || snapshot.Connections[0].ID != "hris" || snapshot.Connections[0].LinkState != "not_linked" || !snapshot.Connections[0].CanLink || snapshot.Connections[0].CanUnlink {
		t.Fatalf("snapshot = %+v", snapshot.Connections)
	}
	tiers := map[string]string{}
	for _, skill := range snapshot.Connections[0].Skills {
		tiers[skill.ID] = skill.Tier
	}
	if tiers["workers.read"] != "T0_READ" || tiers["promotions.submit"] != "T3_SUBMIT_GOVERNED" || len(tiers) != 2 {
		t.Fatalf("skills and tiers = %v", tiers)
	}
	// Someone the grant does not name sees none of it.
	s.tokens["outsider"] = servedAccessPrincipal(t, s.tenant, "ir-098-outsider", []string{"nobody_role"}, trust.AssuranceSubstantial)
	var outsider productui.AgentAccessSnapshot
	s.ok(http.MethodGet, base+"/snapshot", "outsider", nil, &outsider)
	if len(outsider.Connections) != 0 {
		t.Fatalf("a person the grant does not name is offered %+v", outsider.Connections)
	}

	// Linking starts with the provider's authorization and PKCE.
	var start productui.AgentAuthorizationStart
	s.ok(http.MethodPost, base+"/link/start", "ana", agentAccessLinkBody{ConnectionID: "hris"}, &start)
	if !start.ValidPKCE() || start.State == "" || start.CodeChallengeMethod != "S256" {
		t.Fatalf("link start = %+v", start)
	}
	target, err := url.Parse(start.AuthorizationURL)
	if err != nil || target.Query().Get("code_challenge") == "" || target.Query().Get("state") != start.State || target.Query().Get("response_type") != "code" {
		t.Fatalf("authorization url = %q (%v)", start.AuthorizationURL, err)
	}
	s.refused(http.StatusNotFound, "not_found", http.MethodPost, base+"/link/start", "ana", agentAccessLinkBody{ConnectionID: "payroll"})
	s.refused(http.StatusUnauthorized, "request_denied", http.MethodPost, base+"/link/start", "", agentAccessLinkBody{ConnectionID: "hris"})
	// The return must carry the state this person was given: another person's
	// state, a made-up one and a replay are refused.
	s.refused(http.StatusBadRequest, "state_rejected", http.MethodPost, base+"/link/complete", "outsider", agentAccessCompleteBody{State: start.State, Code: "code-1"})
	s.refused(http.StatusBadRequest, "state_rejected", http.MethodPost, base+"/link/complete", "ana", agentAccessCompleteBody{State: "made-up", Code: "code-1"})
	var done struct{ ConnectionID string }
	s.ok(http.MethodPost, base+"/link/complete", "ana", agentAccessCompleteBody{State: start.State, Code: "code-1"}, &done)
	if done.ConnectionID != "hris" {
		t.Fatalf("completed link = %+v", done)
	}
	s.refused(http.StatusBadRequest, "state_rejected", http.MethodPost, base+"/link/complete", "ana", agentAccessCompleteBody{State: start.State, Code: "code-1"})
	s.ok(http.MethodGet, base+"/snapshot", "ana", nil, &snapshot)
	if got := snapshot.Connections[0]; got.LinkState != "linked" || !got.CanUnlink || got.AccountLabel == "" {
		t.Fatalf("after linking = %+v", got)
	}
	// The link is kept in the core database, so a restart still has it.
	s.compose()
	s.ok(http.MethodGet, base+"/snapshot", "ana", nil, &snapshot)
	if got := snapshot.Connections[0]; got.LinkState != "linked" {
		t.Fatalf("after a restart = %+v", got)
	}

	// Her task's grant is listed with its expiry, can be revoked, and only by her.
	taskID, _ := s.startLongTask()
	s.ok(http.MethodGet, base+"/snapshot", "ana", nil, &snapshot)
	var grant productui.AgentDelegationGrant
	for _, candidate := range snapshot.Delegations {
		if candidate.TaskID == taskID {
			grant = candidate
		}
	}
	if grant.ID == "" || grant.ExpiresAt == "" {
		t.Fatalf("the task's grant is not listed: %+v", snapshot.Delegations)
	}
	var strangerView productui.AgentAccessSnapshot
	s.ok(http.MethodGet, base+"/snapshot", "stranger", nil, &strangerView)
	for _, other := range strangerView.Delegations {
		if other.TaskID == taskID {
			t.Fatalf("another person's task grant is listed: %+v", other)
		}
	}
	s.ok(http.MethodPost, base+"/grants/revoke", "stranger", agentAccessRevokeBody{TaskID: taskID}, nil)
	s.ok(http.MethodGet, base+"/snapshot", "ana", nil, &snapshot)
	if !containsDelegation(snapshot.Delegations, taskID) {
		t.Fatal("someone else's revoke removed her grant")
	}
	s.ok(http.MethodPost, base+"/grants/revoke", "ana", agentAccessRevokeBody{TaskID: taskID}, nil)
	s.ok(http.MethodGet, base+"/snapshot", "ana", nil, &snapshot)
	if containsDelegation(snapshot.Delegations, taskID) {
		t.Fatalf("a revoked grant is still listed: %+v", snapshot.Delegations)
	}

	// Unlinking takes effect at once, and a second unlink is not an error the page shows.
	s.ok(http.MethodPost, base+"/unlink", "ana", agentAccessLinkBody{ConnectionID: "hris"}, nil)
	s.ok(http.MethodGet, base+"/snapshot", "ana", nil, &snapshot)
	if got := snapshot.Connections[0]; got.LinkState != "not_linked" || got.CanUnlink {
		t.Fatalf("after unlinking = %+v", got)
	}
	// The body cannot carry an identity or anything the route does not name.
	s.refused(http.StatusBadRequest, "invalid_request", http.MethodPost, base+"/unlink", "ana", map[string]string{"ConnectionID": "hris", "UserID": servedAccessStranger})
}

func containsDelegation(grants []productui.AgentDelegationGrant, taskID string) bool {
	for _, grant := range grants {
		if grant.TaskID == taskID {
			return true
		}
	}
	return false
}

// TestTodo_AGENT2_019_ServedIntegration: the administrator console over the
// served routes. Create, tier, grants, request, approve by a second
// administrator who has passed step-up, publish, rollback and preview; a
// T3 grant cannot be published by its author, a person who is not an
// administrator cannot reach any of it, and the revision history is stored.
func TestTodo_AGENT2_019_ServedIntegration(t *testing.T) {
	s := newAgentAccessServed(t)
	const console = AgentAccessPath + "/console"
	var snapshot productui.AgentAdminAccessSnapshot
	// Not an administrator: no route of the console answers.
	for _, token := range []string{"ana", "owner", "stranger"} {
		s.refused(http.StatusForbidden, "permission_denied", http.MethodGet, console, token, nil)
		s.refused(http.StatusForbidden, "permission_denied", http.MethodPost, console+"/create", token, agentAccessCreateBody{})
	}
	s.refused(http.StatusUnauthorized, "request_denied", http.MethodGet, console, "", nil)

	s.ok(http.MethodPost, console+"/create", "admin-one", agentAccessCreateBody{Revision: productui.AgentConnectionRevision{
		ID: "hris", Provider: "HRIS", CredentialMode: "USER_DELEGATED",
		Grants: []productui.AgentAdminSkillGrant{{SkillID: "workers.read", Tier: "T0"}, {SkillID: "promotions.submit", Tier: "T3"}},
	}}, &snapshot)
	if len(snapshot.Revisions) != 1 || snapshot.Revisions[0].ID != "hris#1" || snapshot.Revisions[0].Status != "DRAFT" {
		t.Fatalf("created = %+v", snapshot.Revisions)
	}
	// A connection that is not one of the workspace's cannot be published.
	s.refused(http.StatusBadRequest, "invalid_request", http.MethodPost, console+"/create", "admin-one", agentAccessCreateBody{Revision: productui.AgentConnectionRevision{}})

	// Tier: an unknown tier is the most restrictive one, and a lower one is a draft change.
	s.ok(http.MethodPost, console+"/tier", "admin-one", agentAccessTierBody{RevisionID: "hris#1", SkillID: "workers.read", Tier: "T1"}, &snapshot)
	s.ok(http.MethodPost, console+"/tier", "admin-one", agentAccessTierBody{RevisionID: "hris#1", SkillID: "workers.read", Tier: "T0"}, &snapshot)

	// Granting a T3 skill to everyone she names needs a second administrator.
	grants := []agentAccessGrantBody{{ID: "staff", Roles: s.anaRoles(), Population: "employees", OrganizationScopes: []string{servedAccessOrgScope}, Skills: []string{"workers.read", "promotions.submit"}}}
	s.ok(http.MethodPost, console+"/grants", "admin-one", agentAccessGrantsBody{RevisionID: "hris#1", Grants: grants}, &snapshot)
	if !snapshot.Revisions[0].RequiresSecondAdmin {
		t.Fatalf("a T3 grant does not ask for a second administrator: %+v", snapshot.Revisions[0])
	}
	s.refused(http.StatusBadRequest, "invalid_request", http.MethodPost, console+"/grants", "admin-one", agentAccessGrantsBody{RevisionID: "hris#1", Grants: []agentAccessGrantBody{{ID: "bad", Roles: []string{"x"}, Population: "employees", OrganizationScopes: []string{"org"}, Skills: []string{"unknown.skill"}}}})

	// The preview answers for a chosen person before anything is published.
	var preview productui.AgentAdminAccessSnapshot
	s.ok(http.MethodPost, console+"/preview", "admin-one", agentAccessPreviewBody{RevisionID: "hris#1", UserID: agentTestWorker, Label: "Ana Flores"}, &preview)
	if !preview.PreviewReady || preview.PreviewSubject != "Ana Flores" || len(preview.Preview.Connections) != 1 || len(preview.Preview.Connections[0].Skills) != 2 {
		t.Fatalf("preview for Ana = %+v", preview.Preview)
	}
	if !containsWarning(preview.Preview.Warnings, "second administrator") {
		t.Fatalf("the preview does not warn that publishing needs a second administrator: %v", preview.Preview.Warnings)
	}
	s.ok(http.MethodPost, console+"/preview", "admin-one", agentAccessPreviewBody{RevisionID: "hris#1", UserID: servedAccessStranger}, &preview)
	s.refused(http.StatusBadRequest, "invalid_request", http.MethodPost, console+"/preview", "admin-one", agentAccessPreviewBody{RevisionID: "hris#1"})

	// Publishing without the second administrator is refused, for the author too.
	s.refused(http.StatusForbidden, "separation_of_duties", http.MethodPost, console+"/publish", "admin-one", agentAccessRevisionBody{RevisionID: "hris#1"})
	s.ok(http.MethodPost, console+"/request", "admin-one", agentAccessRevisionBody{RevisionID: "hris#1"}, &snapshot)
	// Approval needs another administrator who has passed step-up: not the author, and not a session without it.
	s.refused(http.StatusForbidden, "separation_of_duties", http.MethodPost, console+"/approve", "admin-one", agentAccessRevisionBody{RevisionID: "hris#1"})
	s.refused(http.StatusForbidden, "separation_of_duties", http.MethodPost, console+"/approve", "admin-two-low", agentAccessRevisionBody{RevisionID: "hris#1"})
	s.refused(http.StatusForbidden, "separation_of_duties", http.MethodPost, console+"/publish", "admin-one", agentAccessRevisionBody{RevisionID: "hris#1"})
	s.ok(http.MethodPost, console+"/approve", "admin-two", agentAccessRevisionBody{RevisionID: "hris#1"}, &snapshot)
	if snapshot.Revisions[0].Status != "APPROVED" || !snapshot.Revisions[0].SecondAdminApproved {
		t.Fatalf("after approval = %+v", snapshot.Revisions[0])
	}
	s.ok(http.MethodPost, console+"/publish", "admin-one", agentAccessRevisionBody{RevisionID: "hris#1"}, &snapshot)
	if snapshot.Revisions[0].Status != "PUBLISHED" {
		t.Fatalf("after publishing = %+v", snapshot.Revisions[0])
	}
	// A published revision is history: it cannot be edited or published again.
	s.refused(http.StatusConflict, "conflict", http.MethodPost, console+"/grants", "admin-one", agentAccessGrantsBody{RevisionID: "hris#1", Grants: grants})
	s.refused(http.StatusConflict, "conflict", http.MethodPost, console+"/publish", "admin-one", agentAccessRevisionBody{RevisionID: "hris#1"})
	// What it published is what Ana is offered.
	var anaView productui.AgentAccessSnapshot
	s.ok(http.MethodGet, AgentAccessPath+"/snapshot", "ana", nil, &anaView)
	if len(anaView.Connections) != 1 || len(anaView.Connections[0].Skills) != 2 {
		t.Fatalf("what Ana is offered after publishing = %+v", anaView.Connections)
	}

	// A second revision that drops the T3 skill is read-only, so its author may publish it alone.
	s.ok(http.MethodPost, console+"/create", "admin-one", agentAccessCreateBody{Revision: productui.AgentConnectionRevision{
		ID: "hris", Provider: "HRIS", CredentialMode: "USER_DELEGATED", Grants: []productui.AgentAdminSkillGrant{{SkillID: "workers.read", Tier: "T0"}},
	}}, &snapshot)
	readOnly := []agentAccessGrantBody{{ID: "staff", Roles: s.anaRoles(), Population: "employees", OrganizationScopes: []string{servedAccessOrgScope}, Skills: []string{"workers.read"}}}
	s.ok(http.MethodPost, console+"/grants", "admin-one", agentAccessGrantsBody{RevisionID: "hris#2", Grants: readOnly}, &snapshot)
	s.ok(http.MethodPost, console+"/publish", "admin-one", agentAccessRevisionBody{RevisionID: "hris#2"}, &snapshot)
	s.ok(http.MethodGet, AgentAccessPath+"/snapshot", "ana", nil, &anaView)
	if len(anaView.Connections[0].Skills) != 1 {
		t.Fatalf("after the read-only revision Ana is offered %+v", anaView.Connections[0].Skills)
	}
	// Rolling back republishes the first revision under a new number; its approval still covers it.
	s.ok(http.MethodPost, console+"/rollback", "admin-one", agentAccessRevisionBody{RevisionID: "hris#1"}, &snapshot)
	if snapshot.Revisions[0].ID != "hris#3" || snapshot.Revisions[0].Status != "PUBLISHED" {
		t.Fatalf("after rollback = %+v", snapshot.Revisions)
	}
	s.ok(http.MethodGet, AgentAccessPath+"/snapshot", "ana", nil, &anaView)
	if len(anaView.Connections[0].Skills) != 2 {
		t.Fatalf("after the rollback Ana is offered %+v", anaView.Connections[0].Skills)
	}
	// Rolling back to a revision that is not history is refused.
	s.refused(http.StatusConflict, "conflict", http.MethodPost, console+"/rollback", "admin-one", agentAccessRevisionBody{RevisionID: "hris#3"})

	// The history is stored: a restart of the services shows the same revisions and the live one.
	s.compose()
	var after productui.AgentAdminAccessSnapshot
	s.ok(http.MethodGet, console, "admin-two", nil, &after)
	statuses := map[string]string{}
	for _, revision := range after.Revisions {
		statuses[revision.ID] = revision.Status
	}
	if statuses["hris#1"] != "SUPERSEDED" || statuses["hris#2"] != "SUPERSEDED" || statuses["hris#3"] != "PUBLISHED" || len(statuses) != 3 {
		t.Fatalf("revisions after a restart = %v", statuses)
	}
	s.ok(http.MethodGet, AgentAccessPath+"/snapshot", "ana", nil, &anaView)
	if len(anaView.Connections) != 1 || len(anaView.Connections[0].Skills) != 2 {
		t.Fatalf("what Ana is offered after a restart = %+v", anaView.Connections)
	}
	// Every step is in the audit trail, with who took it.
	var actions int
	s.agentDB.QueryRow(context.Background(), `SELECT count(*) FROM agent_connection_console_audit WHERE tenant_id=$1 AND actor IN ($2,$3)`, s.tenantID, servedAccessAdminOne, servedAccessAdminTwo).Scan(&actions)
	if actions < 9 {
		t.Fatalf("console audit rows = %d, want a row for each of the steps taken", actions)
	}
}

func containsWarning(warnings []string, fragment string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, fragment) {
			return true
		}
	}
	return false
}

// servedCostInner stands for the model executor under the cost meter: it returns
// an answer that cost a fixed amount, with no model behind it.
type servedCostInner struct{ micros int64 }

func (i servedCostInner) Execute(context.Context, AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	return AgentModelExecutorResult{Result: agentmodel.ModelResult{Text: "answer", Usage: agentmodel.ModelUsage{CostMicros: i.micros}}}, nil
}

// TestTodo_AGENTCOST_006_ServedIntegration: the cost report, the limits and the
// limit card over the served routes and the runtime database role. An owner
// reads what her agent cost and sets its daily limits; an administrator may only
// tighten; a stranger cannot read or change anything; a run that would pass the
// day's limit is refused before the model is called and the person is told which
// agent, which conversation and when the day ends; a restart keeps all of it.
func TestTodo_AGENTCOST_006_ServedIntegration(t *testing.T) {
	s := newAgentAccessServed(t)
	const cost = AgentCostPath
	gate := s.services.Gate
	request := agentinvoke.RunRequest{TenantID: s.tenant, PersonaID: "policy-helper", ConversationID: "general", InvokerID: agentTestWorker}
	run := func(task string, micros int64) {
		t.Helper()
		metered := gate.meter(servedCostInner{micros: micros}, request)
		if _, err := metered.Execute(context.Background(), AgentModelExecutorRequest{Task: TrustedModelTask{TenantID: s.tenant, TaskID: task, AgentID: "policy-helper"}, StepID: "answer"}); err != nil {
			t.Fatal(err)
		}
	}
	var report AgentCostReportView
	s.refused(http.StatusUnauthorized, "request_denied", http.MethodGet, cost+"/report", "", nil)
	s.ok(http.MethodGet, cost+"/report", "owner", nil, &report)
	if len(report.Agents) != 1 || report.Agents[0].ID != "policy-helper" || report.Agents[0].Name != "Policy Helper" || len(report.Report.Runs) != 0 {
		t.Fatalf("the report before any run = %+v", report)
	}
	// What she sees of her agent: its runs, its spend by day, its month and a forecast.
	run("task-1", 600_000)
	run("task-2", 150_000)
	s.ok(http.MethodGet, cost+"/report", "owner", nil, &report)
	if len(report.Report.Runs) != 2 || report.Report.MonthToDateMicros != 750_000 || report.Report.ForecastMonthMicros < 750_000 {
		t.Fatalf("report after two runs = %+v", report.Report)
	}
	var day int64
	for _, row := range report.Report.PerAgentDay {
		if row.Agent == "Policy Helper" {
			day += row.Micros
		}
	}
	if day != 750_000 || len(report.Report.Trend) != 30 {
		t.Fatalf("per agent and day = %+v, trend %d days", report.Report.PerAgentDay, len(report.Report.Trend))
	}
	// A stranger's report shows none of it; the cost of another owner's agent is not hers to read.
	var strangers AgentCostReportView
	s.ok(http.MethodGet, cost+"/report", "stranger", nil, &strangers)
	if len(strangers.Agents) != 0 || len(strangers.Report.Runs) != 0 || strangers.Report.MonthToDateMicros != 0 {
		t.Fatalf("a stranger reads %+v", strangers)
	}

	// Limits: only the owner raises; the stranger reads nothing and changes nothing.
	var views []AgentSpendLimitView
	s.refused(http.StatusForbidden, "permission_denied", http.MethodPost, cost+"/limit", "stranger", agentCostLimitBody{AgentID: "policy-helper", MaxRunsPerDay: 100})
	s.ok(http.MethodPost, cost+"/limits", "stranger", agentCostLimitsBody{AgentIDs: []string{"policy-helper"}}, &views)
	if len(views) != 1 || !views[0].Denied || views[0].MaxRunsPerDay != 0 {
		t.Fatalf("a stranger reads the limits: %+v", views)
	}
	s.ok(http.MethodPost, cost+"/limit", "owner", agentCostLimitBody{AgentID: "policy-helper", MaxRunsPerDay: 4, MaxSpendMicrosPerDay: 5_000_000}, &views)
	if len(views) != 1 || views[0].MaxRunsPerDay != 4 || views[0].MaxSpendMicrosPerDay != 5_000_000 || views[0].Reached != "" {
		t.Fatalf("after the owner's save = %+v", views)
	}
	// An administrator may tighten and may not loosen.
	s.refused(http.StatusForbidden, "permission_denied", http.MethodPost, cost+"/limit", "admin-one", agentCostLimitBody{AgentID: "policy-helper", MaxRunsPerDay: 50, MaxSpendMicrosPerDay: 5_000_000})
	s.ok(http.MethodPost, cost+"/limit", "admin-one", agentCostLimitBody{AgentID: "policy-helper", MaxRunsPerDay: 2, MaxSpendMicrosPerDay: 5_000_000}, &views)
	if views[0].MaxRunsPerDay != 2 {
		t.Fatalf("after the administrator tightened it = %+v", views)
	}
	s.refused(http.StatusBadRequest, "invalid_request", http.MethodPost, cost+"/limit", "owner", map[string]any{"AgentID": "policy-helper", "MaxRunsPerDay": 3, "Owner": "someone"})
	s.refused(http.StatusForbidden, "permission_denied", http.MethodPost, cost+"/limit", "owner", agentCostLimitBody{AgentID: "someone-elses-agent", MaxRunsPerDay: 1})
	// Every change is audited with who made it.
	events, err := s.services.HTTP.Meter.Gate.AuditTrail(s.tenant, "policy-helper", servedAccessOwner)
	if err != nil || len(events) != 2 || events[0].Actor != servedAccessOwner || events[1].Actor != servedAccessAdminOne || events[1].After == nil || events[1].After.MaxRuns != 2 {
		t.Fatalf("audit = %+v, %v", events, err)
	}

	// The two runs already made have used the day's two: the next is refused before any call.
	refused := gate.admit(request)
	var failure *PersonaRunFailure
	if !asPersonaRunFailure(refused, &failure) || failure.Code != "DAILY_LIMIT_REACHED" {
		t.Fatalf("the third run was admitted: %v", refused)
	}
	if code, retryable := personaPostFailureClassification(refused); code != "DAILY_LIMIT_REACHED" || retryable {
		t.Fatalf("classification of the refusal = %q retryable %v", code, retryable)
	}
	// What the person sees: the agent, the conversation and when the day ends.
	card := chat.AgentAnswerDailyLimit("en-US", "Policy Helper", "#general")
	if card.Sentence != "Policy Helper reached today's limit for #general. It resets at 00:00 UTC." || card.Retryable {
		t.Fatalf("limit card = %+v", card)
	}
	// What the owner sees on Agent setup, from the same state.
	s.ok(http.MethodPost, cost+"/limits", "owner", agentCostLimitsBody{AgentIDs: []string{"policy-helper"}}, &views)
	if views[0].Reached != "Policy Helper reached today's limit. It resets at 00:00 UTC." {
		t.Fatalf("the setup card's sentence = %q", views[0].Reached)
	}
	// Another agent, or the same one tomorrow, is not stopped.
	if other := gate.admit(agentinvoke.RunRequest{TenantID: s.tenant, PersonaID: "benefits", ConversationID: "general"}); other != nil {
		t.Fatalf("an agent with no limit was stopped: %v", other)
	}
	s.offset = 24 * time.Hour
	s.compose()
	if again := s.services.Gate.admit(request); again != nil {
		t.Fatalf("the limit did not reset the next day: %v", again)
	}
	// A restart on the same day keeps the day's use and the limit.
	s.offset = 0
	s.compose()
	if refusedAgain := s.services.Gate.admit(request); refusedAgain == nil {
		t.Fatal("a restart forgot the day's use")
	}
	s.ok(http.MethodPost, cost+"/limits", "owner", agentCostLimitsBody{AgentIDs: []string{"policy-helper"}}, &views)
	if views[0].MaxRunsPerDay != 2 {
		t.Fatalf("a restart forgot the limit: %+v", views)
	}
}

func asPersonaRunFailure(err error, into **PersonaRunFailure) bool {
	failure, ok := err.(*PersonaRunFailure)
	if ok {
		*into = failure
	}
	return ok
}
