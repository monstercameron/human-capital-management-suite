package application

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Task Catcher and Reminder are placed in the working channel with "Reads every
// message here" off: the channel's administrator sees both switches in
// Conversation details, neither agent has read anything, and turning one on is
// the administrator's act. A second run places nothing again.
func TestTodo_AGENTUX_049_Placements_Integration(t *testing.T) {
	fixture, _, _ := agentUXAmbientFixture(t)
	store := fixture.DB.(*chatstore.Store)
	ctx := t.Context()
	room := localDevPersonaDemoConversationID("tenant-a", localAgentDemoAmbientRoom)
	incident := localDevPersonaDemoConversationID("tenant-a", localAgentDemoSupportRoom)
	if err := store.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO chat_conversation(id,tenant_id,kind,owner_id) VALUES($1,'tenant-a','PUBLIC_CHANNEL','author'),($2,'tenant-a','PRIVATE_CHANNEL','author')`, room, incident); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO chat_membership(tenant_id,conversation_id,member_id,home_tenant_id,role) VALUES('tenant-a',$1,'author','tenant-a','manager'),('tenant-a',$2,'author','tenant-a','manager')`, room, incident)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	created, err := ensureLocalAgentDemoAmbientInstallations(ctx, store, "tenant-a", "author", now)
	if err != nil || created != 2 {
		t.Fatalf("placed %d agents: %v", created, err)
	}
	if again, err := ensureLocalAgentDemoAmbientInstallations(ctx, store, "tenant-a", "author", now); err != nil || again != 0 {
		t.Fatalf("a second run placed %d: %v", again, err)
	}
	grants, _, err := fixture.Grants(ctx, "tenant-a", room, "author")
	if err != nil || len(grants) != 2 {
		t.Fatalf("the switches in Conversation details: %+v %v", grants, err)
	}
	for _, grant := range grants {
		if grant.Enabled || grant.AutomaticPublic || grant.Paused {
			t.Fatalf("%s reads every message without the administrator turning it on: %+v", grant.Agent, grant)
		}
	}
	var reads int
	if err := store.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM agentux_ambient_read WHERE tenant_id='tenant-a' AND conversation_id=$1`, room).Scan(&reads)
	}); err != nil || reads != 0 {
		t.Fatalf("an agent read a message before it was switched on: %d %v", reads, err)
	}
	if err := fixture.SetGrant(ctx, "tenant-a", room, "luis", AgentUXAmbientGrant{Agent: "reminder", Enabled: true}); err == nil {
		t.Fatal("a member turned the switch on")
	}
	if err := fixture.SetGrant(ctx, "tenant-a", room, "author", AgentUXAmbientGrant{Agent: "reminder", Enabled: true}); err != nil {
		t.Fatalf("the administrator could not turn the switch on: %v", err)
	}

	member, err := ensureLocalAgentDemoSupportAlertMember(ctx, store, "tenant-a")
	if err != nil || member != 1 {
		t.Fatalf("support desk added to the incident channel %d times: %v", member, err)
	}
	if again, err := ensureLocalAgentDemoSupportAlertMember(ctx, store, "tenant-a"); err != nil || again != 0 {
		t.Fatalf("a second run added it %d times: %v", again, err)
	}
	// A cell without the channel is left alone.
	if none, err := ensureLocalAgentDemoSupportAlertMember(ctx, store, "tenant-b"); err != nil || none != 0 {
		t.Fatalf("a tenant with no incident channel: %d %v", none, err)
	}
}
