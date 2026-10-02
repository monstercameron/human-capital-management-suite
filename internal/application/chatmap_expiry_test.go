package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type chatmapSweepRepo struct {
	chatmapPictureRepo
	tenant string
	at     time.Time
	calls  int
}

func (r *chatmapSweepRepo) SweepLocations(_ context.Context, tenant string, at time.Time) (int64, error) {
	r.tenant = tenant
	r.at = at
	r.calls++
	return 1, nil
}
func TestTodo_CHATMAP_002_Expiry(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	repo := &chatmapSweepRepo{}
	sweep := ChatmapExpirySweep{Repo: repo, Now: func() time.Time { return now }}
	n, err := sweep.RunOnce(context.Background(), "tenant-a")
	if err != nil || n != 1 || repo.tenant != "tenant-a" || !repo.at.Equal(now) {
		t.Fatal(n, err)
	}
	if _, err = sweep.RunOnce(context.Background(), ""); err != chat.ErrInvalidArgument || repo.calls != 1 {
		t.Fatal("unscoped sweep", err)
	}
	if _, err = (ChatmapExpirySweep{}).RunOnce(context.Background(), "tenant-a"); err != chat.ErrUnavailable {
		t.Fatal(err)
	}
}
