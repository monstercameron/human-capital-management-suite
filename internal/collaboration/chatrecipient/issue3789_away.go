package chatrecipient

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3789: a person's own away setting. A person says they are away until a
// date, may name a colleague who covers, may keep the whole thing private, and
// may ask for the cover to be told about mentions of them. It is a chat
// preference only. It never grants the cover access to a conversation and never
// makes the cover an approver or delegate of any governed work: that stays with
// the human-work directory and the owning workflow. It carries no reason and no
// leave details, so there is nothing of that kind to leak.

// NotificationCover is a mention of someone who is away, shown to the person
// they named as cover. Unlike a mention of the reader it never carries text.
const NotificationCover = "cover"

// Bounds.
const (
	AwayMaxDuration = 366 * 24 * time.Hour
	AwayLookupLimit = 50
	CoverLimit      = 20
)

// Away is the person's setting. Since is when the person set it; it bounds what
// a cover is told about. The zero value is "not away".
type Away struct {
	Since, Until time.Time
	CoverID      string
	// Hidden keeps the absence and the cover from everyone but the person.
	Hidden bool
	// NotifyCover offers the cover an optional notice for each mention of the
	// person while they are away.
	NotifyCover bool
	Revision    uint64
}

// Active is whether the person is away at the moment.
func (a Away) Active(now time.Time) bool { return !a.Until.IsZero() && a.Until.After(now) }

// AwayPublic is what a colleague may see of someone else's absence: until when
// and who covers. Nothing else about it is ever returned.
type AwayPublic struct {
	MemberID string
	Until    time.Time
	CoverID  string
}

// AwayStore is the optional storage side of the away setting.
type AwayStore interface {
	Away(ctx context.Context, home, subject string) (Away, error)
	// PutAway stores the setting when expected is the revision the caller read.
	// A cover who is not a person of the company is chat.ErrInvalidArgument.
	PutAway(ctx context.Context, home, subject string, a Away, expected uint64) (Away, error)
	// AwayOf returns, for the named people, the absences that are running at now
	// and not hidden by their owner.
	AwayOf(ctx context.Context, home string, subjects []string, now time.Time) ([]AwayPublic, error)
	// CoverNotices lists mentions, since the away setting began, of the people
	// who named cover as their cover with notices on, in conversations cover may
	// read right now. Body is always empty.
	CoverNotices(ctx context.Context, host, home, cover string, since time.Time, now time.Time, limit int) ([]Notification, error)
	// MarkCoverRead records cover's read state for the named cover notices (all
	// of them when all is true) and returns how many it marked.
	MarkCoverRead(ctx context.Context, host, home, cover string, since time.Time, now time.Time, postIDs []string, all bool) (int64, error)
}

func (s *Service) awayStore() (AwayStore, error) {
	if s == nil || s.Repo == nil {
		return nil, chat.ErrUnavailable
	}
	store, ok := s.Repo.(AwayStore)
	if !ok {
		return nil, chat.ErrUnavailable
	}
	return store, nil
}

// Away returns the caller's own setting, with an ended absence reported as none.
func (s *Service) Away(ctx context.Context, p chat.Principal, now time.Time) (Away, error) {
	store, err := s.awayStore()
	if err != nil {
		return Away{}, err
	}
	if p.TenantID == "" || p.SubjectID == "" {
		return Away{}, chat.ErrInvalidArgument
	}
	a, err := store.Away(ctx, p.TenantID, p.SubjectID)
	if err != nil {
		return Away{}, err
	}
	if !a.Active(now) {
		a.Since, a.Until, a.CoverID, a.NotifyCover = time.Time{}, time.Time{}, "", false
	}
	return a, nil
}

// PutAway keeps the caller's own setting against the revision they read. A
// zero Until ends the absence. The person always comes from the session.
func (s *Service) PutAway(ctx context.Context, p chat.Principal, a Away, expected uint64, now time.Time) (Away, error) {
	store, err := s.awayStore()
	if err != nil {
		return Away{}, err
	}
	if p.TenantID == "" || p.SubjectID == "" || expected == 0 {
		return Away{}, chat.ErrInvalidArgument
	}
	a.CoverID = strings.TrimSpace(a.CoverID)
	switch {
	case a.Until.IsZero():
		if a.CoverID != "" || a.NotifyCover {
			return Away{}, chat.ErrInvalidArgument
		}
		a.Since = time.Time{}
	case !a.Until.After(now) || a.Until.After(now.Add(AwayMaxDuration)):
		return Away{}, chat.ErrInvalidArgument
	case a.CoverID == p.SubjectID || len(a.CoverID) > 256 || (a.NotifyCover && a.CoverID == ""):
		return Away{}, chat.ErrInvalidArgument
	default:
		a.Until = a.Until.UTC()
		a.Since = now.UTC()
		// A change to a running absence keeps the moment it began, so a cover
		// is not told again about what came before.
		if cur, err := store.Away(ctx, p.TenantID, p.SubjectID); err == nil && cur.Active(now) {
			a.Since = cur.Since.UTC()
		}
	}
	return store.PutAway(ctx, p.TenantID, p.SubjectID, a, expected)
}

// AwayOf returns what the caller may see of the named people's absences: only
// running, unhidden ones, in the caller's own company. A person with nothing to
// show is absent from the answer, so it cannot tell "hidden" from "not away".
func (s *Service) AwayOf(ctx context.Context, p chat.Principal, ids []string, now time.Time) ([]AwayPublic, error) {
	store, err := s.awayStore()
	if err != nil {
		return nil, err
	}
	if p.TenantID == "" || p.SubjectID == "" {
		return nil, chat.ErrInvalidArgument
	}
	seen := map[string]bool{}
	clean := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 256 {
			return nil, chat.ErrInvalidArgument
		}
		if !seen[id] {
			seen[id] = true
			clean = append(clean, id)
		}
	}
	if len(clean) > AwayLookupLimit {
		return nil, chat.ErrInvalidArgument
	}
	if len(clean) == 0 {
		return nil, nil
	}
	return store.AwayOf(ctx, p.TenantID, clean, now)
}

// coverNotifications adds the cover notices to a feed. A store that does not
// keep the away setting adds nothing. Every conversation is admitted again, so a
// notice for a conversation the cover can no longer open is dropped without a
// trace, and a post already listed as a mention of the cover is not repeated.
func (s *Service) coverNotifications(ctx context.Context, p chat.Principal, host string, now time.Time, rows []Notification) ([]Notification, error) {
	store, err := s.awayStore()
	if err != nil {
		return rows, nil
	}
	cover, err := store.CoverNotices(ctx, host, p.TenantID, p.SubjectID, now.Add(-NotificationsWindow), now, CoverLimit)
	if err != nil {
		return nil, err
	}
	listed := map[string]bool{}
	for _, row := range rows {
		listed[row.PostID] = true
	}
	admitted := map[string]bool{}
	for _, row := range cover {
		if listed[row.PostID] {
			continue
		}
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
		row.Kind = NotificationCover
		row.Body = ""
		rows = append(rows, row)
	}
	return rows, nil
}
