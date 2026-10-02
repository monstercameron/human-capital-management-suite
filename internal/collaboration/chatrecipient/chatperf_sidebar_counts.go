package chatrecipient

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CHATBUG-014: the sidebar asked for its unread and mention counts one
// conversation at a time, nineteen calls on the review data and again after
// every new message. The store already reads a person's counts for many
// conversations in one query; this is the way to ask for them in one call.

// SidebarCountsLimit is how many conversations one sidebar read may name.
const SidebarCountsLimit = 100

// SidebarCounter is a repository that reads one person's counts for several
// conversations of a host tenant in one query. A repository without it is
// asked one conversation at a time.
type SidebarCounter interface {
	ChatscaleSidebarCounts(ctx context.Context, host, home, subject string, conversations []string) (map[string]Counts, error)
}

// SidebarCounts returns the principal's counts for the named conversations of
// one host tenant, keyed by conversation. Each conversation is admitted exactly
// as a single Counts read admits it; one the principal may not read, or that
// does not exist, is left out of the answer rather than failing the others,
// which is what a sidebar needs when a membership was revoked a moment ago.
func (s *Service) SidebarCounts(ctx context.Context, p chat.Principal, host string, conversations []string) (map[string]Counts, error) {
	if s == nil || s.Conversations == nil || s.Repo == nil {
		return nil, chat.ErrUnavailable
	}
	if p.TenantID == "" || p.SubjectID == "" || host == "" || len(conversations) > SidebarCountsLimit {
		return nil, chat.ErrInvalidArgument
	}
	admitted := make([]string, 0, len(conversations))
	seen := make(map[string]struct{}, len(conversations))
	for _, conversation := range conversations {
		if conversation == "" {
			return nil, chat.ErrInvalidArgument
		}
		if _, dup := seen[conversation]; dup {
			continue
		}
		seen[conversation] = struct{}{}
		if _, err := s.admit(ctx, p, host, conversation); err != nil {
			if errors.Is(err, chat.ErrPermissionDenied) || errors.Is(err, chat.ErrNotFound) {
				continue
			}
			return nil, err
		}
		admitted = append(admitted, conversation)
	}
	out := make(map[string]Counts, len(admitted))
	if len(admitted) == 0 {
		return out, nil
	}
	if counter, ok := s.Repo.(SidebarCounter); ok {
		counts, err := counter.ChatscaleSidebarCounts(ctx, host, p.TenantID, p.SubjectID, admitted)
		if err != nil {
			return nil, err
		}
		// A conversation with nothing unread has no row; it still has an answer.
		for _, conversation := range admitted {
			out[conversation] = counts[conversation]
		}
		return out, nil
	}
	for _, conversation := range admitted {
		counts, err := s.Repo.Counts(ctx, Identity{HostTenantID: host, HomeTenantID: p.TenantID, SubjectID: p.SubjectID, ConversationID: conversation})
		if err != nil {
			return nil, err
		}
		out[conversation] = counts
	}
	return out, nil
}
