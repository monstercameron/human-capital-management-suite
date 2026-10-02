package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
)

func TestAgentUXProactive_PreviewMatchesPost(t *testing.T) {
	for _, changed := range []bool{false, true} {
		s, repo, _, _, _, delivery, draft := proactiveControls(t)
		preview, err := s.PreviewAnnouncement(context.Background(), draft)
		if err != nil || preview.Preview == nil || preview.Preview.Digest == "" {
			t.Fatalf("preview: %+v %v", preview, err)
		}
		draft.Cadence, draft.Weekdays = "NOW", nil
		draft.PreviewDigest = preview.Preview.Digest
		if changed {
			draft.PreviewDigest = personaRunBytesDigest([]byte("different preview"))
		}
		reply, err := s.CreateAnnouncement(context.Background(), draft)
		if err != nil {
			t.Fatal(err)
		}
		if changed {
			if delivery.calls != 0 || repo.record.LastResult != agentstore.AnnouncementRefused || reply.Snapshot.Rows[0].Reason != announcementPreviewChangedReason {
				t.Fatalf("changed preview posted: %+v calls=%d", reply, delivery.calls)
			}
		} else if delivery.calls != 1 || reply.Snapshot.Rows[0].ResultCode != agentstore.AnnouncementPosted {
			t.Fatalf("matching preview not posted: %+v calls=%d", reply, delivery.calls)
		}
	}
}
