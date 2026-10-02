package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
)

// TestTodo_CHATBUG_069_PausedFollows: a schedule its owner had paused follows the
// agent to the new installation and stays paused, so the owner can resume or edit
// it instead of being refused for an installation that no longer exists.
func TestTodo_CHATBUG_069_PausedFollows(t *testing.T) {
	s, repo, schedules, owner, _, _, draft := proactiveControls(t)
	ctx := context.Background()
	if _, err := s.CreateAnnouncement(ctx, draft); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ControlAnnouncement(ctx, agentcontrols.AnnouncementCommand{ID: repo.record.ID, Action: "PAUSE", ExpectedRevision: repo.record.Revision, IdempotencyKey: "pause-069-1"}); err != nil {
		t.Fatal(err)
	}
	active := map[string]bool{"install-2": true}
	authority := reinstallAuthority{proactiveOwner: owner, active: active}
	s.Service.Authority = authority
	installs := catalogInstalls{{ID: "install-2", PersonaID: "policy-helper", PersonaVersion: 2, ConversationID: "general", Conversation: "#general", Kind: "PUBLIC_CHANNEL", Active: true}}
	s.Names = AgentAnnouncementCatalogNames{
		Versions:      catalogVersions{{Profile: agentpersona.PersonaVersion{Profile: agentpersona.PersonaProfile{PersonaID: "policy-helper", Version: 2, DisplayName: "Policy Helper"}}}},
		Installations: &installs,
		Authority:     authority,
		OwnerNames: func(context.Context, string, []string) (map[string]string, error) {
			return map[string]string{"owner": "Alex Example"}, nil
		},
	}
	reply, err := s.ListAnnouncements(ctx)
	if err != nil || len(reply.Snapshot.Rows) != 1 || reply.Snapshot.Rows[0].Retired {
		t.Fatalf("the list failed after the paused schedule's agent was installed again: %+v %v", reply, err)
	}
	if repo.record.InstallationID != "install-2" || repo.record.State != agentstore.AnnouncementPaused || schedules.pauses != 2 {
		t.Fatalf("the paused schedule did not follow the agent and stay paused: %+v pauses=%d", repo.record, schedules.pauses)
	}
	if _, err := s.ControlAnnouncement(ctx, agentcontrols.AnnouncementCommand{ID: repo.record.ID, Action: "RESUME", ExpectedRevision: repo.record.Revision, IdempotencyKey: "resume-069-1"}); err != nil {
		t.Fatalf("the owner cannot resume a schedule that followed its agent: %v", err)
	}
}
