package timeclockstore

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
)

// PhotoDispositionAdapter exposes the store-level retention lease to the
// clock application. Claim and tombstone must commit before an artifact
// delete is attempted; a failed delete leaves the tombstone for retry.
type PhotoDispositionAdapter struct{ Store *timestore.Store }

// ClaimPhotoDisposition claims a due photo under the supplied store revision.
func (a PhotoDispositionAdapter) ClaimPhotoDisposition(ctx context.Context, tenant, photoID string, expectedRevision int64, idempotencyKey string, asOf time.Time) (timestore.PhotoDisposition, error) {
	if a.Store == nil {
		return timestore.PhotoDisposition{}, clockservice.ErrUnavailable
	}
	return a.Store.ClaimPhotoDisposition(ctx, tenant, photoID, expectedRevision, idempotencyKey, asOf)
}

// DeletePhotoAfterTombstone performs the protected lease protocol. The
// artifact boundary is injected from the real artifact service; this adapter
// never invents a physical storage delete implementation.
func (a PhotoDispositionAdapter) DeletePhotoAfterTombstone(ctx context.Context, tenant, photoID string, expectedRevision int64, idempotencyKey string, _ string, artifacts clockservice.PhotoArtifactStore) (timestore.PhotoDisposition, error) {
	if a.Store == nil || artifacts == nil {
		return timestore.PhotoDisposition{}, clockservice.ErrUnavailable
	}
	tombstone, err := a.Store.TombstonePhotoDisposition(ctx, tenant, photoID, expectedRevision, idempotencyKey+":tombstone")
	if err != nil {
		return timestore.PhotoDisposition{}, err
	}
	if err := artifacts.DeletePhoto(ctx, tenant, tombstone.ArtifactRef); err != nil {
		return tombstone, err
	}
	return a.Store.FinalizePhotoDisposition(ctx, tenant, photoID, tombstone.Revision, idempotencyKey+":finalize")
}
