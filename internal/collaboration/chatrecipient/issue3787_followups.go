package chatrecipient

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3787: the person's follow-ups, computed at read time from posts they
// can already read. Nothing here sends a nudge, closes a question on its own
// or infers a commitment: a reaction or a later post is shown as evidence the
// person can review, and only the person's own choice hides a row.

// Directions of a follow-up row.
const (
	FollowUpOwed    = "owed"    // someone asked me; waiting on me
	FollowUpWaiting = "waiting" // I asked; waiting on them
)

// The person's choices. "kept" says: I looked at the evidence and this is
// still open.
const (
	FollowUpDismissed = "dismissed"
	FollowUpResolved  = "resolved"
	FollowUpSnoozed   = "snoozed"
	FollowUpKept      = "kept"
)

// Evidence kinds. Both are only signs of a possible answer.
const (
	FollowUpEvidenceReacted     = "reacted"
	FollowUpEvidencePostedLater = "posted_later"
	// FollowUpEvidenceReplied is a reply in the question's own thread by the
	// people asked. It may answer a different question, so the person decides.
	FollowUpEvidenceReplied = "replied"
)

// Bounds and defaults.
const (
	FollowUpsLimit    = 100
	FollowUpMinAge    = time.Hour
	FollowUpWindow    = 30 * 24 * time.Hour
	FollowUpSnoozeMax = 90 * 24 * time.Hour
)

// FollowUp is one row as the caller may see it right now.
type FollowUp struct {
	HostTenantID   string
	ConversationID string
	PostID         string
	Sequence       uint64
	Direction      string
	// AuthorID asked the question (owed) or is the caller (waiting).
	AuthorID string
	// ThreadRootID is the thread the post sits in; empty for a top-level post.
	ThreadRootID string
	Body         string
	AskedAt      time.Time
	// WaitingOn lists the people or agents the caller asked (waiting). It is
	// empty for a plain question and for owed rows.
	WaitingOn []string
	// Evidence is a sign of a possible answer ("" when there is none) and
	// EvidenceAt when it happened.
	Evidence   string
	EvidenceAt time.Time
	// Kept is true when the caller chose to keep the row open.
	Kept bool
}

// FollowUpStore is the optional storage side of the follow-up list.
type FollowUpStore interface {
	ListFollowUps(ctx context.Context, host, home, subject string, now time.Time, minAge time.Duration, limit int) ([]FollowUp, error)
	PutFollowUpState(ctx context.Context, id Identity, postID, direction, status string, until time.Time) error
}

func (s *Service) followUpStore() (FollowUpStore, error) {
	if s == nil || s.Conversations == nil || s.Repo == nil {
		return nil, chat.ErrUnavailable
	}
	store, ok := s.Repo.(FollowUpStore)
	if !ok {
		return nil, chat.ErrUnavailable
	}
	return store, nil
}

// FollowUps lists what is waiting on the caller and what the caller is waiting
// on in the host company. The store filters by membership, history and post
// audience; the service then admits each conversation again, so a conversation
// the caller can no longer open is dropped without a trace.
func (s *Service) FollowUps(ctx context.Context, p chat.Principal, host string, now time.Time) ([]FollowUp, error) {
	store, err := s.followUpStore()
	if err != nil {
		return nil, err
	}
	if p.TenantID == "" || p.SubjectID == "" || host == "" {
		return nil, chat.ErrInvalidArgument
	}
	rows, err := store.ListFollowUps(ctx, host, p.TenantID, p.SubjectID, now, FollowUpMinAge, FollowUpsLimit)
	if err != nil {
		return nil, err
	}
	admitted := map[string]bool{}
	out := make([]FollowUp, 0, len(rows))
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
		row.Body = FollowedPreview(row.Body)
		out = append(out, row)
	}
	// Oldest first: age is what the list is for.
	sort.SliceStable(out, func(i, j int) bool { return out[i].AskedAt.Before(out[j].AskedAt) })
	return out, nil
}

// SetFollowUp records the caller's choice for one row after admitting the
// conversation. status is one of the Follow-up choices; until is required for
// "snoozed" and must lie in the future and within FollowUpSnoozeMax.
func (s *Service) SetFollowUp(ctx context.Context, p chat.Principal, host, conversation, post, direction, status string, until, now time.Time) error {
	store, err := s.followUpStore()
	if err != nil {
		return err
	}
	if p.TenantID == "" || p.SubjectID == "" || host == "" || conversation == "" || strings.TrimSpace(post) == "" {
		return chat.ErrInvalidArgument
	}
	switch direction {
	case FollowUpOwed, FollowUpWaiting:
	default:
		return chat.ErrInvalidArgument
	}
	switch status {
	case FollowUpDismissed, FollowUpResolved, FollowUpKept:
		until = time.Time{}
	case FollowUpSnoozed:
		if !until.After(now) || until.After(now.Add(FollowUpSnoozeMax)) {
			return chat.ErrInvalidArgument
		}
	default:
		return chat.ErrInvalidArgument
	}
	if _, err := s.admit(ctx, p, host, conversation); err != nil {
		return err
	}
	return store.PutFollowUpState(ctx, Identity{HostTenantID: host, HomeTenantID: p.TenantID, SubjectID: p.SubjectID, ConversationID: conversation}, post, direction, status, until)
}
