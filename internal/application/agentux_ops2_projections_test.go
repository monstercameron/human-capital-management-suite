package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/ownerops"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentUXOps2RunSource struct {
	rows  []AgentOwnerRunProjection
	err   error
	calls int
}

func (s *agentUXOps2RunSource) DashboardProjection(context.Context, *trust.Principal, ownerops.Audience) ([]AgentOwnerRunProjection, error) {
	s.calls++
	return s.rows, s.err
}

type agentUXOps2IdentitySource struct {
	identity AgentControlIdentity
	err      error
	calls    int
	agentID  string
	version  string
}

func (s *agentUXOps2IdentitySource) ResolveAgentControlIdentity(_ context.Context, _ values.TenantId, agentID, version string) (AgentControlIdentity, error) {
	s.calls++
	s.agentID, s.version = agentID, version
	return s.identity, s.err
}

func TestTodo_AGENTUX_015(t *testing.T) {
	principal := portableApplicationPrincipal(t)
	since := time.Date(2026, 9, 30, 14, 5, 0, 0, time.FixedZone("test", -4*60*60))
	runs := &agentUXOps2RunSource{rows: []AgentOwnerRunProjection{{
		View:    ownerops.TaskView{TaskID: "task-1", AgentID: "policy-helper", Version: "2", InstallationID: "benefits-room", State: "RUNNING", Revision: 7, CanPause: true},
		OwnerID: "owner-1", RequestedBy: "walt-brennan", StartedAt: since.Add(-2 * time.Minute), UpdatedAt: since,
	}}}
	identities := &agentUXOps2IdentitySource{identity: AgentControlIdentity{Name: "Policy Helper", OwnerName: "Maya Chen"}}
	service := &AgentOwnerControls{Runs: runs, Identities: identities}
	reply, err := service.Snapshot(trust.WithPrincipal(context.Background(), principal))
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.Snapshot.Runs) != 1 {
		t.Fatalf("runs = %+v", reply.Snapshot.Runs)
	}
	run := reply.Snapshot.Runs[0]
	if run.Name != "Policy Helper" || run.Version != "2" || run.Since != "2026-09-30T18:05:00Z" || reply.Snapshot.OwnerName != "Maya Chen" {
		t.Fatalf("readable projection = %+v owner=%q", run, reply.Snapshot.OwnerName)
	}
	if len(run.Actions) != 1 || run.Actions[0] != "pause" || identities.agentID != "policy-helper" || identities.version != "2" {
		t.Fatalf("authorized actions or identity lookup = %+v, %q/%q", run.Actions, identities.agentID, identities.version)
	}
}

func TestTodo_AGENTUX_015_Security(t *testing.T) {
	principal := portableApplicationPrincipal(t)
	runs := &agentUXOps2RunSource{err: ownerops.ErrDenied}
	identities := &agentUXOps2IdentitySource{identity: AgentControlIdentity{Name: "Secret agent", OwnerName: "Secret owner"}}
	_, err := (&AgentOwnerControls{Runs: runs, Identities: identities}).Snapshot(trust.WithPrincipal(context.Background(), principal))
	if !errors.Is(err, agentcontrols.ErrDenied) || identities.calls != 0 {
		t.Fatalf("denied viewer error=%v identity_reads=%d", err, identities.calls)
	}
	if _, err := (AgentPersonaControlIdentities{}).ResolveAgentControlIdentity(context.Background(), "tenant-a", "agent", "1"); !errors.Is(err, ErrPersonaCatalogDirectoryUnavailable) {
		t.Fatalf("missing governed identity sources = %v", err)
	}
}

func TestTodo_AGENTUX_015_Integration(t *testing.T) {
	f := newAgentFixture(t)
	f.setEnabled(t, true)
	ctx := context.Background()
	mapper := tenantKeyMapper[values.TenantId](pgstore.TenantID)
	now := time.Now().UTC()
	operations, err := NewAgentOwnerOperations(f.pool, mapper, f.cell.RoleAccess, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	owner := agentTestPrincipal(t, f.tenant, agentTestWorker)
	task, err := f.runtime.Starter.StartTaskMode(ctx, owner, "private goal", agentclient.StartLongTask)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := mapper(values.TenantId(f.tenant))
	f.db.Exec(t, `INSERT INTO agent_owner_binding(tenant_id,task_id,owner_id,agent_id,agent_version,installation_id) VALUES($1,$2,$3,'policy-helper','2','benefits-room') ON CONFLICT DO NOTHING`, tenantID, task.ID, owner.Subject())
	f.db.Exec(t, `INSERT INTO agent_owner_grant VALUES($1,'ops2-owner',$2,'MEMBER',$3,ARRAY['agent.operations.read','agent.installation.pause'],$6,$4,$5,NULL,'review:ops2')`, tenantID, owner.Subject(), ownerops.PurposeOwnerDashboard, now.Add(-time.Minute), now.Add(time.Hour), task.ID)
	identities := &agentUXOps2IdentitySource{identity: AgentControlIdentity{Name: "Policy Helper", OwnerName: "Maya Chen"}}
	surface := &AgentControlsSurface{Operations: &AgentOwnerControls{Owners: operations, Identities: identities}}
	admission, ownerBearer := agentRolloutPortableAdmission(t, owner)
	handler := OverlayAgentControlsSurface(nil, surface, admission)
	ownerResponse := getAgentControls(t, handler, ownerBearer)
	var ownerReply agentcontrols.Reply
	if ownerResponse.Code != http.StatusOK || json.Unmarshal(ownerResponse.Body.Bytes(), &ownerReply) != nil {
		t.Fatalf("owner snapshot status=%d body=%s", ownerResponse.Code, ownerResponse.Body.String())
	}
	if len(ownerReply.Snapshot.Runs) != 1 || ownerReply.Snapshot.Runs[0].Name != "Policy Helper" || ownerReply.Snapshot.Runs[0].Since == "" || len(ownerReply.Snapshot.Runs[0].Actions) != 1 {
		t.Fatalf("owner served projection = %+v", ownerReply.Snapshot)
	}

	role, err := f.cell.RoleAccess.SaveRole(ctx, values.TenantId(f.tenant), "system:ops2", roleaccess.Role{ID: "ops2_viewer", Name: "Ops2 viewer", Active: true, Reason: "integration non-owner"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.cell.RoleAccess.Load(ctx, values.TenantId(f.tenant), agentOrgScope(values.TenantId(f.tenant)))
	if err != nil {
		t.Fatal(err)
	}
	for _, assignment := range snapshot.Assignments {
		if assignment.WorkerRef == agentTestAdmin {
			assignment.RoleIDs = []string{role.ID}
			assignment.Reason = "ops2 integration"
			if _, err := f.cell.RoleAccess.SaveAssignment(ctx, values.TenantId(f.tenant), "system:ops2", assignment); err != nil {
				t.Fatal(err)
			}
		}
	}
	nonOwner := agentTestPrincipal(t, f.tenant, agentTestAdmin)
	_, nonOwnerBearer := agentRolloutPortableAdmission(t, nonOwner)
	nonOwnerResponse := getAgentControls(t, handler, nonOwnerBearer)
	if nonOwnerResponse.Code != http.StatusOK || identities.calls != 1 {
		t.Fatalf("non-owner status=%d body=%s identity_reads=%d", nonOwnerResponse.Code, nonOwnerResponse.Body.String(), identities.calls)
	}
	if body := nonOwnerResponse.Body.String(); body == "" || containsAny(body, "Policy Helper", "Maya Chen", "pause") {
		t.Fatalf("non-owner response leaked projection: %s", body)
	}
}

func getAgentControls(t *testing.T, handler http.Handler, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, agentcontrols.Path, nil)
	request.Header.Set("Authorization", "Bearer "+bearer)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

var _ AgentOwnerRunSource = (*agentUXOps2RunSource)(nil)
var _ AgentControlIdentitySource = (*agentUXOps2IdentitySource)(nil)
