package application

import (
	"context"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CaptureThreadSnapshot preserves the owner's authorized read through the
// audit decorator. Snapshot reads produce no new post or post audit record.
func (s *auditedChatService) CaptureThreadSnapshot(ctx context.Context, request chat.ThreadSnapshotRequest) (chat.ThreadSnapshot, error) {
	if s == nil || ctx == nil {
		return chat.ThreadSnapshot{}, chat.ErrThreadSnapshotUnavailable
	}
	source, ok := s.ConversationService.(chat.ThreadSnapshotStore)
	if !ok {
		return chat.ThreadSnapshot{}, chat.ErrThreadSnapshotUnavailable
	}
	return source.CaptureThreadSnapshot(ctx, request)
}

// WithThreadSnapshotFence delegates the current reader check and locked
// admission callback without replacing either with cached audit metadata.
func (s *auditedChatService) WithThreadSnapshotFence(ctx context.Context, snapshot chat.ThreadSnapshot, fn func() error) error {
	if s == nil || ctx == nil || fn == nil {
		return chat.ErrThreadSnapshotUnavailable
	}
	source, ok := s.ConversationService.(chat.ThreadSnapshotStore)
	if !ok {
		return chat.ErrThreadSnapshotUnavailable
	}
	return source.WithThreadSnapshotFence(ctx, snapshot, fn)
}
