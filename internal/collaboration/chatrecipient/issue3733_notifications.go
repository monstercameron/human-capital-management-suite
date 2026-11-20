package chatrecipient

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3733: the person's chat notifications, the feed behind the shell bell.
// A notification is one message that needs the person: a mention of them, a
// reply in a thread they follow, or a direct message. The feed is computed at
// read time from messages the person can read right now, so it follows every
// rule the conversation does: a person who left, a message that was deleted or
// held back from them, a muted conversation and a mentions-only choice all
// leave no item. The only thing stored is which items the person has read, so
// the read state is the same in every session. Nothing here sends anything.

// Kinds of notification.
const (
	NotificationMention = "mention"
	NotificationDirect  = "direct"
	NotificationReply   = "reply"
)

// Bounds. A person sees at most NotificationsLimit items from the last
// NotificationsWindow; marking read accepts at most NotificationsMarkLimit ids.
const (
	NotificationsLimit     = 50
	NotificationsMarkLimit = 100
	NotificationsWindow    = 30 * 24 * time.Hour
)

// Notification is one item as the caller may see it right now.
type Notification struct {
	HostTenantID string
	// ID is the id of the message the item is about; it is the item's key.
	ID               string
	Kind             string
	ConversationID   string
	ConversationName string
	// ConversationKind is the conversation's kind (PUBLIC_CHANNEL, DIRECT...).
	ConversationKind string
	PostID           string
	Sequence         uint64
	// ThreadRootID and ThreadRootSequence name the thread a reply sits in; they
	// are empty for a top-level message.
	ThreadRootID       string
	ThreadRootSequence uint64
	AuthorID           string
	// Body is a short preview, empty when it could not be shown.
	Body string
	At   time.Time
	Read bool
}

// NotificationStore is the optional storage side of the feed.
type NotificationStore interface {
	ListNotifications(ctx context.Context, host, home, subject string, since time.Time, limit int) ([]Notification, error)
	// MarkNotificationsRead records the person's read state for the named
	// messages that are notifications for them right now (all of them when all
	// is true) and returns how many items it marked.
	MarkNotificationsRead(ctx context.Context, host, home, subject string, since time.Time, postIDs []string, all bool) (int64, error)
}

func (s *Service) notificationStore() (NotificationStore, error) {
	if s == nil || s.Conversations == nil || s.Repo == nil {
		return nil, chat.ErrUnavailable
	}
	store, ok := s.Repo.(NotificationStore)
	if !ok {
		return nil, chat.ErrUnavailable
	}
	return store, nil
}

// Notifications lists the caller's notifications in the host company, newest
// first. The store filters by membership, history, post audience and the
// caller's notification choices; the service then admits each conversation
// again, so a conversation the caller can no longer open is dropped without a
// trace.
func (s *Service) Notifications(ctx context.Context, p chat.Principal, host string, now time.Time) ([]Notification, error) {
	store, err := s.notificationStore()
	if err != nil {
		return nil, err
	}
	if p.TenantID == "" || p.SubjectID == "" || host == "" {
		return nil, chat.ErrInvalidArgument
	}
	rows, err := store.ListNotifications(ctx, host, p.TenantID, p.SubjectID, now.Add(-NotificationsWindow), NotificationsLimit)
	if err != nil {
		return nil, err
	}
	admitted := map[string]bool{}
	out := make([]Notification, 0, len(rows))
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
		row.ID = row.PostID
		row.Body = FollowedPreview(row.Body)
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out, nil
}

// MarkNotificationsRead marks the named notifications read, or every one when
// all is true. A message that is not a notification for the caller is skipped
// without saying whether it exists. The result is how many were marked.
func (s *Service) MarkNotificationsRead(ctx context.Context, p chat.Principal, host string, postIDs []string, all bool, now time.Time) (int64, error) {
	store, err := s.notificationStore()
	if err != nil {
		return 0, err
	}
	if p.TenantID == "" || p.SubjectID == "" || host == "" {
		return 0, chat.ErrInvalidArgument
	}
	ids := make([]string, 0, len(postIDs))
	seen := map[string]bool{}
	for _, id := range postIDs {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 256 {
			return 0, chat.ErrInvalidArgument
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if all == (len(ids) > 0) || len(ids) > NotificationsMarkLimit {
		return 0, chat.ErrInvalidArgument
	}
	return store.MarkNotificationsRead(ctx, host, p.TenantID, p.SubjectID, now.Add(-NotificationsWindow), ids, all)
}
