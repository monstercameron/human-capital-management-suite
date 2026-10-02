package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
)

type proactiveFailEditSchedule struct{ AgentAnnouncementScheduleOwner }

func (proactiveFailEditSchedule) UpdateAnnouncementSchedule(context.Context, AgentAnnouncementActor, agentstore.Announcement, string) (*time.Time, error) {
	return nil, ErrAgentAnnouncementUnavailable
}

type proactiveGrantChanges struct{ documents []string }

func (s *proactiveGrantChanges) SetAnnouncementDocumentShares(_ context.Context, _ AgentAnnouncementActor, record agentstore.Announcement) error {
	s.documents = append(s.documents, record.Documents[0].DocumentID)
	return nil
}

func TestAgentUXProactive_FailedEditRestoresDocumentScope_Security(t *testing.T) {
	surface, repository, _, _, _, _, draft := proactiveControls(t)
	ctx := context.Background()
	reply, err := surface.CreateAnnouncement(ctx, draft)
	if err != nil {
		t.Fatal(err)
	}
	shares := &proactiveGrantChanges{}
	surface.Service.Shares = shares
	surface.Service.Schedules = proactiveFailEditSchedule{surface.Service.Schedules}
	draft.ID, draft.ExpectedRevision, draft.IdempotencyKey = reply.Snapshot.Rows[0].ID, 1, "failed-edit-123"
	draft.Documents = []agentdocref.Reference{{DocumentID: "replacement-guide", VersionMode: agentdocref.ModeLatestPublished, Label: "Replacement guide"}}
	if _, err = surface.UpdateAnnouncement(ctx, draft); !errors.Is(err, agentcontrols.ErrUnavailable) {
		t.Fatalf("failed edit=%v", err)
	}
	if len(shares.documents) != 2 || shares.documents[0] != "replacement-guide" || shares.documents[1] != "holiday-guide" || repository.record.Revision != 1 || repository.record.Documents[0].DocumentID != "holiday-guide" {
		t.Fatalf("failed edit changed service scope: grants=%v announcement=%+v", shares.documents, repository.record)
	}
}
