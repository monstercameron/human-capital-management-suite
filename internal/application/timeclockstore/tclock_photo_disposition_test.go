package timeclockstore

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
)

func TestPhotoDispositionAdapter_FailsClosedWithoutStore(t *testing.T) {
	ctx := context.Background()
	adapter := PhotoDispositionAdapter{}
	if _, err := adapter.ClaimPhotoDisposition(ctx, "tenant", "photo", 1, "claim", time.Now()); err != clockservice.ErrUnavailable {
		t.Fatalf("claim error=%v, want ErrUnavailable", err)
	}
	if _, err := adapter.DeletePhotoAfterTombstone(ctx, "tenant", "photo", 1, "delete", "artifact://caller-supplied", nil); err != clockservice.ErrUnavailable {
		t.Fatalf("delete error=%v, want ErrUnavailable", err)
	}
}

func TestTodo_TCLOCK_007_Integration(t *testing.T) {
	ctx := context.Background()
	adapter := PhotoDispositionAdapter{}
	if _, err := adapter.ClaimPhotoDisposition(ctx, "tenant-a", "photo-1", 1, "claim-1", time.Unix(1_700_000_000, 0)); err != clockservice.ErrUnavailable {
		t.Fatalf("claim without durable disposition store err=%v", err)
	}
	if _, err := adapter.DeletePhotoAfterTombstone(ctx, "tenant-a", "photo-1", 1, "delete-1", "ignored-by-adapter", &photoArtifactStub{}); err != clockservice.ErrUnavailable {
		t.Fatalf("delete without durable disposition store err=%v", err)
	}
}

type photoArtifactStub struct{}

func (*photoArtifactStub) UploadPhoto(context.Context, string, string, []byte, string) (string, error) {
	return "", clockservice.ErrUnavailable
}

func (*photoArtifactStub) DeletePhoto(context.Context, string, string) error {
	return clockservice.ErrUnavailable
}
