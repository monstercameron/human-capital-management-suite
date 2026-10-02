package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
)

type proactiveLostOccurrence struct {
	agentAnnouncementRepository
	lose bool
}

func (s *proactiveLostOccurrence) RecordOccurrence(ctx context.Context, occurrence agentstore.AnnouncementOccurrence) (bool, error) {
	if s.lose {
		s.lose = false
		return false, errors.New("fixture: occurrence write interrupted")
	}
	return s.agentAnnouncementRepository.RecordOccurrence(ctx, occurrence)
}

func TestAgentUXProactive_CommittedOccurrenceReplay_Integration(t *testing.T) {
	surface, _, store, model, _, draft := proactiveLiveFixture(t)
	surface.Runner.Store = &proactiveLostOccurrence{agentAnnouncementRepository: surface.Service.Store, lose: true}
	if _, err := surface.CreateAnnouncement(context.Background(), draft); err == nil {
		t.Fatal("interrupted projection reported success")
	}
	proactiveLiveAssertPosts(t, context.Background(), store, 1, false)
	reply, err := surface.CreateAnnouncement(context.Background(), draft)
	if err != nil || reply.Snapshot.Rows[0].ResultCode != agentstore.AnnouncementPosted || model.calls != 1 {
		t.Fatalf("committed replay=%+v calls=%d %v", reply, model.calls, err)
	}
	proactiveLiveAssertPosts(t, context.Background(), store, 1, false)
}
