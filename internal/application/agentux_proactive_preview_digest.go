package application

import (
	"context"
	"errors"
)

const announcementPreviewChangedReason = "The preview expired or its documents or authority changed. Preview again before posting."

var ErrAgentAnnouncementPreviewChanged = errors.New(announcementPreviewChangedReason)

type announcementPreviewDigestKey struct{}

func withAnnouncementPreviewDigest(ctx context.Context, digest string) context.Context {
	if ctx == nil || digest == "" {
		return ctx
	}
	return context.WithValue(ctx, announcementPreviewDigestKey{}, digest)
}
