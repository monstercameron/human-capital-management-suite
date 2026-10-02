package chat

import (
	"context"
	"time"
)

// ModerationNoticeWindow is how long a notice counts as new for the person it
// was sent to: the Moderation entry in the sidebar shows a count for that long.
const ModerationNoticeWindow = 14 * 24 * time.Hour

// ModerationSummary is what the Chat page needs to know about moderation for
// one person, in one small read: whether to show the Moderation entry, how many
// open items they can open, where the message menu may offer Remove, and the
// notices they were sent (a removal with its reason, a report's outcome).
type ModerationSummary struct {
	// Open counts the open items this person can open and nothing else.
	Open int
	// Moderator is true when the person holds a moderation permission somewhere.
	Moderator bool
	// RemoveEverywhere and ReviewEverywhere are true for a workspace-wide holder.
	RemoveEverywhere, ReviewEverywhere bool
	// Removable and Reviewable list the conversations where the person holds the
	// permission through their channel role or a channel override.
	Removable, Reviewable []string
	// Notices are the person's own notices, newest first; NoticeCount is how many
	// are inside ModerationNoticeWindow.
	Notices     []ModerationNotice
	NoticeCount int
}

// ModerationSummaryStore is implemented by the store that holds the queue.
type ModerationSummaryStore interface {
	ModerationSummary(context.Context, Principal, string) (ModerationSummary, error)
}

// ModerationTargetStore reads the message a removal or a report is about, as the
// person acting may see it (a removed message reads as removed).
type ModerationTargetStore interface {
	ModerationTarget(context.Context, Principal, string, string, string) (Post, error)
}

// FilterModerationCounter is implemented by the filter queue so the summary
// counts the same items the queue lists.
type FilterModerationCounter interface {
	CountModeration(context.Context, Principal, string) (int, error)
}

// Summary returns the person's moderation summary. Filter hits flagged for
// review are added to the open count when the filter queue is composed.
func (s *ModerationService) Summary(ctx context.Context, p Principal, tenant string) (ModerationSummary, error) {
	if err := validatePrincipal(p, tenant); err != nil {
		return ModerationSummary{}, err
	}
	if p.TenantID != tenant {
		return ModerationSummary{}, ErrPermissionDenied
	}
	store, ok := s.Store.(ModerationSummaryStore)
	if !ok {
		return ModerationSummary{}, ErrUnavailable
	}
	out, err := store.ModerationSummary(ctx, p, tenant)
	if err != nil {
		return ModerationSummary{}, err
	}
	if counter, ok := s.Filters.(FilterModerationCounter); ok && out.Moderator {
		n, e := counter.CountModeration(ctx, p, tenant)
		if e != nil {
			return ModerationSummary{}, e
		}
		out.Open += n
	}
	return out, nil
}

// Target returns the message a removal or report dialog is about.
func (s *ModerationService) Target(ctx context.Context, p Principal, tenant, conversation, post string) (Post, error) {
	if err := validatePrincipal(p, tenant); err != nil {
		return Post{}, err
	}
	if p.TenantID != tenant || conversation == "" || post == "" {
		return Post{}, ErrPermissionDenied
	}
	store, ok := s.Store.(ModerationTargetStore)
	if !ok {
		return Post{}, ErrUnavailable
	}
	return store.ModerationTarget(ctx, p, tenant, conversation, post)
}
