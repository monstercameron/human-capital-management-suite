package chatrecipient

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3916: the person's followed threads, listed from the same follow rows
// the thread header reads and writes. Nothing here creates a second
// subscription, changes read state or says a reply is owed.

// FollowedThreadsLimit bounds one list. A person who follows more threads than
// this sees the most recently active ones.
const FollowedThreadsLimit = 100

// FollowedPreviewRunes bounds the root message preview a list row carries.
const FollowedPreviewRunes = 200

// FollowedThread is one followed thread as the caller may see it right now.
type FollowedThread struct {
	HostTenantID   string
	ConversationID string
	RootPostID     string
	RootSequence   uint64
	RootAuthorID   string
	// RootBody is a short preview of the root message. It is empty when the
	// root message was deleted (RootRemoved) or could not be shown.
	RootBody    string
	RootRemoved bool
	ReplyCount  uint64
	// LastActivity is the newest visible reply, or the root message when there
	// are no replies yet.
	LastActivity time.Time
	// Revision is the follow row's revision, which Unfollow needs.
	Revision uint64
}

// FollowedLister is the optional storage side of the followed-thread list.
type FollowedLister interface {
	ListFollowed(ctx context.Context, host, home, subject string, limit int) ([]FollowedThread, error)
}

// FollowedThreads lists the threads the caller follows in the host company's
// conversations. The store filters by current membership, history visibility
// and post audience; the service then admits each conversation again, so a
// conversation the caller can no longer open is dropped without a trace.
func (s *Service) FollowedThreads(ctx context.Context, p chat.Principal, host string) ([]FollowedThread, error) {
	if s == nil || s.Conversations == nil || s.Repo == nil {
		return nil, chat.ErrUnavailable
	}
	if p.TenantID == "" || p.SubjectID == "" || host == "" {
		return nil, chat.ErrInvalidArgument
	}
	lister, ok := s.Repo.(FollowedLister)
	if !ok {
		return nil, chat.ErrUnavailable
	}
	rows, err := lister.ListFollowed(ctx, host, p.TenantID, p.SubjectID, FollowedThreadsLimit)
	if err != nil {
		return nil, err
	}
	admitted := map[string]bool{}
	out := make([]FollowedThread, 0, len(rows))
	for _, row := range rows {
		ok, seen := admitted[row.ConversationID]
		if !seen {
			_, err := s.admit(ctx, p, host, row.ConversationID)
			switch {
			case err == nil:
				ok = true
			case errors.Is(err, chat.ErrPermissionDenied), errors.Is(err, chat.ErrNotFound):
			default:
				return nil, err
			}
			admitted[row.ConversationID] = ok
		}
		if !ok {
			continue
		}
		row.HostTenantID = host
		row.RootBody = FollowedPreview(row.RootBody)
		out = append(out, row)
	}
	return out, nil
}

// FollowedPreview shortens a root message to the preview length, on a rune
// boundary, and marks the cut.
func FollowedPreview(body string) string {
	runes := []rune(body)
	if len(runes) <= FollowedPreviewRunes {
		return body
	}
	return string(runes[:FollowedPreviewRunes-1]) + "…"
}
