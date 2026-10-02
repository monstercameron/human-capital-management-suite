package application

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/ownerops"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestAgentUXOps3_ServerReproduction(t *testing.T) {
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
	tasks := make([]string, 0, 2)
	for _, goal := range []string{"completed persona run", "failed persona run"} {
		task, startErr := f.runtime.Starter.StartTaskMode(ctx, owner, goal, agentclient.StartLongTask)
		if startErr != nil {
			t.Fatal(startErr)
		}
		tasks = append(tasks, task.ID)
	}
	tenantID := mapper(values.TenantId(f.tenant))
	finishedAt := time.Now().UTC().Add(time.Second)
	f.db.Exec(t, `UPDATE agent_task SET state='COMPLETED', updated_at=$3 WHERE tenant_id=$1 AND task_id=$2`, tenantID, tasks[0], finishedAt)
	f.db.Exec(t, `UPDATE agent_task SET state='FAILED', failure_code='MODEL_UNAVAILABLE', updated_at=$3 WHERE tenant_id=$1 AND task_id=$2`, tenantID, tasks[1], finishedAt)
	for _, taskID := range tasks {
		f.db.Exec(t, `INSERT INTO agent_owner_binding(tenant_id,task_id,owner_id,agent_id,agent_version,installation_id) VALUES($1,$2,$3,'policy-helper','2','general') ON CONFLICT DO NOTHING`, tenantID, taskID, owner.Subject())
		f.db.Exec(t, `INSERT INTO agent_owner_grant VALUES($1,$7,$2,'MEMBER',$3,ARRAY['agent.operations.read','agent.installation.pause'],$6,$4,$5,NULL,'review:ops3')`, tenantID, owner.Subject(), ownerops.PurposeOwnerDashboard, now.Add(-time.Minute), now.Add(time.Hour), taskID, "ops3-owner-"+taskID)
	}
	// This matches the served cell: owner operations are present and the
	// optional identity source is absent. Before the fix, either terminal run
	// made the whole endpoint return unavailable.
	surface := &AgentControlsSurface{Operations: &AgentOwnerControls{Owners: operations}}
	admission, bearer := agentRolloutPortableAdmission(t, owner)
	response := getAgentControls(t, OverlayAgentControlsSurface(nil, surface, admission), bearer)
	if response.Code != http.StatusOK {
		t.Fatalf("served controls status=%d body=%s", response.Code, response.Body.String())
	}
	var reply agentcontrols.Reply
	if err := json.Unmarshal(response.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	foundFailure := false
	for _, run := range reply.Snapshot.Runs {
		foundFailure = foundFailure || run.Failure == "MODEL_UNAVAILABLE"
	}
	if !reply.Snapshot.Available || len(reply.Snapshot.Runs) != 2 || reply.Snapshot.Runs[0].Name != "Policy Helper" || !foundFailure {
		t.Fatalf("terminal run projection = %+v", reply.Snapshot)
	}
}

type agentUXOps3Directory struct{}

func (agentUXOps3Directory) ResolvePersonaCatalogTarget(_ context.Context, _ values.TenantId, id string) (productui.PersonaAdminTarget, error) {
	label := id
	if id == "walt" {
		label = "Walt Brennan"
	}
	return productui.PersonaAdminTarget{ID: id, Label: label}, nil
}

func TestAgentUXOps3_RolloutConversationNames(t *testing.T) {
	ctx, service, store, _ := versionRolloutFixture(t)
	principal, _ := trust.FromContext(ctx)
	store.row.DisplayName = "Policy Helper"
	store.row.CreatedAt = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	general := store.installations["install-a"]
	general.ConversationID, general.PersonaVersion = "general", 2
	direct := store.installations["install-b"]
	direct.ConversationID, direct.PersonaVersion = "direct", 2
	hidden := general
	hidden.InstallationID, hidden.ConversationID = "install-hidden", "hidden"
	store.installations["install-a"], store.installations["install-b"], store.installations["install-hidden"] = general, direct, hidden
	service.Conversations = &ChatDirectoryPersonaCatalogTargets{
		Chat: personaCatalogChatFake{
			rooms: []chat.Conversation{
				{ID: "general", TenantID: principal.Tenant().String(), Name: "general", Kind: chat.PublicChannel, MemberCount: 18},
				{ID: "direct", TenantID: principal.Tenant().String(), Name: "direct", Kind: chat.Direct, MemberCount: 2},
			},
			members: []chat.Membership{
				{ConversationID: "direct", TenantID: principal.Tenant().String(), HomeTenantID: principal.Tenant().String(), SubjectID: principal.Subject()},
				{ConversationID: "direct", TenantID: principal.Tenant().String(), HomeTenantID: principal.Tenant().String(), SubjectID: "walt"},
			},
		},
		Directory: agentUXOps3Directory{},
	}
	receipt, err := service.Execute(ctx, AgentVersionRolloutCommand{Action: "CATALOG"})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Catalog == nil || len(receipt.Catalog.Installations) != 3 || receipt.Catalog.Versions[0].PublishedAt != "2026-09-30" || !receipt.Catalog.Versions[0].Current {
		t.Fatalf("catalog = %+v", receipt.Catalog)
	}
	byID := map[string]AgentVersionRolloutCatalogInstallation{}
	for _, item := range receipt.Catalog.Installations {
		byID[item.ID] = item
	}
	if got := byID["install-a"]; !got.Visible || got.Name != "general" || got.Kind != string(chat.PublicChannel) || got.MemberCount != 18 {
		t.Fatalf("public conversation = %+v", got)
	}
	if got := byID["install-b"]; !got.Visible || got.Name != "Walt Brennan" || got.Kind != string(chat.Direct) {
		t.Fatalf("direct conversation = %+v", got)
	}
	if got := byID["install-hidden"]; got.Visible || got.Name != "" {
		t.Fatalf("hidden conversation = %+v", got)
	}
}
