package chatroutingadapter

import (
	"context"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CaptureThreadSnapshot preserves the optional atomic thread API across the
// route decorator. Admission snapshots require a current writable placement;
// the chat owner still authorizes the reader and captures its visible posts.
func (s *Service) CaptureThreadSnapshot(ctx context.Context, request chat.ThreadSnapshotRequest) (chat.ThreadSnapshot, error) {
	if s == nil || ctx == nil {
		return chat.ThreadSnapshot{}, chat.ErrThreadSnapshotUnavailable
	}
	source, ok := s.ConversationService.(chat.ThreadSnapshotStore)
	if !ok {
		return chat.ThreadSnapshot{}, chat.ErrThreadSnapshotUnavailable
	}
	leased, err := s.routeContext(ctx, request.TenantID, request.ConversationID)
	if err != nil {
		return chat.ThreadSnapshot{}, err
	}
	return source.CaptureThreadSnapshot(leased, request)
}

// WithThreadSnapshotFence forwards the chat owner's locked snapshot check.
// An unknown, moved, or unavailable route cannot run the admission callback.
func (s *Service) WithThreadSnapshotFence(ctx context.Context, snapshot chat.ThreadSnapshot, fn func() error) error {
	if s == nil || ctx == nil || fn == nil {
		return chat.ErrThreadSnapshotUnavailable
	}
	source, ok := s.ConversationService.(chat.ThreadSnapshotStore)
	if !ok {
		return chat.ErrThreadSnapshotUnavailable
	}
	leased, err := s.routeContext(ctx, snapshot.TenantID, snapshot.ConversationID)
	if err != nil {
		return err
	}
	return source.WithThreadSnapshotFence(leased, snapshot, fn)
}
