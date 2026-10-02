package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
)

// reinstallAuthority lets the owner manage only the installations that are
// active now, the way the real authority reads the installation table.
type reinstallAuthority struct {
	*proactiveOwner
	active map[string]bool
}

func (a reinstallAuthority) ManageAgentInstallation(_ context.Context, actor AgentAnnouncementActor, install, persona, conversation string) (bool, error) {
	return a.active[install] && actor == a.actor && persona == "policy-helper" && conversation == "general", nil
}

func (a reinstallAuthority) AgentInstallationRetired(_ context.Context, _ AgentAnnouncementActor, install, _, _ string) (bool, error) {
	return !a.active[install], nil
}

// TestTodo_CHATBUG_069_Reinstall installs an agent, makes a scheduled
// announcement, installs the agent again (the first installation is retired) and
// loads the Announcements read: the schedule follows the agent to the new
// installation. When the agent leaves the conversation instead, the record
// stays listed as such with its schedule paused for a stated reason and its
// owner can still delete it.
func TestTodo_CHATBUG_069_Reinstall(t *testing.T) {
	s, repo, schedules, owner, _, _, draft := proactiveControls(t)
	ctx := context.Background()
	if _, err := s.CreateAnnouncement(ctx, draft); err != nil {
		t.Fatal(err)
	}
	active := map[string]bool{"install": true}
	authority := reinstallAuthority{proactiveOwner: owner, active: active}
	s.Service.Authority = authority
	installs := catalogInstalls{{ID: "install", PersonaID: "policy-helper", PersonaVersion: 1, ConversationID: "general", Conversation: "#general", Kind: "PUBLIC_CHANNEL", Active: true}}
	s.Names = AgentAnnouncementCatalogNames{
		Versions: catalogVersions{
			{Profile: agentpersona.PersonaVersion{Profile: agentpersona.PersonaProfile{PersonaID: "policy-helper", Version: 1, DisplayName: "Policy Helper"}}},
			{Profile: agentpersona.PersonaVersion{Profile: agentpersona.PersonaProfile{PersonaID: "policy-helper", Version: 2, DisplayName: "Policy Helper"}}},
		},
		Installations: &installs,
		Authority:     authority,
		OwnerNames: func(context.Context, string, []string) (map[string]string, error) {
			return map[string]string{"owner": "Alex Example"}, nil
		},
	}
	reply, err := s.ListAnnouncements(ctx)
	if err != nil || len(reply.Snapshot.Rows) != 1 || reply.Snapshot.Rows[0].Retired || schedules.updates != 0 {
		t.Fatalf("an announcement with a live installation changed: %+v %v", reply, err)
	}

	// Installed again at version 2: the first installation is retired.
	delete(active, "install")
	active["install-2"] = true
	installs = catalogInstalls{
		{ID: "install", PersonaID: "policy-helper", PersonaVersion: 1, ConversationID: "general", Conversation: "#general", Kind: "PUBLIC_CHANNEL"},
		{ID: "install-2", PersonaID: "policy-helper", PersonaVersion: 2, ConversationID: "general", Conversation: "#general", Kind: "PUBLIC_CHANNEL", Active: true},
	}
	reply, err = s.ListAnnouncements(ctx)
	if err != nil || len(reply.Snapshot.Rows) != 1 {
		t.Fatalf("the Announcements read failed after the agent was installed again: %+v %v", reply, err)
	}
	row := reply.Snapshot.Rows[0]
	if row.Retired || row.AgentName != "Policy Helper" || row.ConversationName != "#general" || repo.record.InstallationID != "install-2" || schedules.updates != 1 || repo.record.State != agentstore.AnnouncementActive {
		t.Fatalf("the schedule did not move to the new installation: %+v %+v updates=%d", row, repo.record, schedules.updates)
	}

	// The agent leaves the conversation: nothing is left to move the schedule to.
	delete(active, "install-2")
	installs = catalogInstalls{{ID: "elsewhere", PersonaID: "policy-helper", PersonaVersion: 2, ConversationID: "random", Conversation: "#random", Kind: "PUBLIC_CHANNEL", Active: true}}
	reply, err = s.ListAnnouncements(ctx)
	if err != nil || len(reply.Snapshot.Rows) != 1 {
		t.Fatalf("the Announcements read failed after the agent left: %+v %v", reply, err)
	}
	row = reply.Snapshot.Rows[0]
	if !row.Retired || row.ID == "" || row.Instruction == "" || row.State != agentstore.AnnouncementPaused || row.Reason != announcementRetiredReason || schedules.pauses != 1 {
		t.Fatalf("a retired record is not shown paused with its reason: %+v pauses=%d", row, schedules.pauses)
	}
	if _, err := s.ControlAnnouncement(ctx, agentcontrols.AnnouncementCommand{ID: row.ID, Action: "POST_NOW", ExpectedRevision: row.Revision, IdempotencyKey: "post-retired-1"}); err == nil {
		t.Fatal("a record whose agent left the conversation was posted")
	}
	if _, err := s.ControlAnnouncement(ctx, agentcontrols.AnnouncementCommand{ID: row.ID, Action: "DELETE", ExpectedRevision: row.Revision, IdempotencyKey: "delete-retired-1"}); err != nil {
		t.Fatalf("the owner cannot delete a record whose agent left: %v", err)
	}
	if repo.record.State != agentstore.AnnouncementDeleted || schedules.deletes != 1 {
		t.Fatalf("the retired record was not deleted: %+v deletes=%d", repo.record, schedules.deletes)
	}
}
