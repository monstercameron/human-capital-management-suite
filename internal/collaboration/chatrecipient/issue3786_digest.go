package chatrecipient

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3786: the personal digest. Optional mentions that a person's quiet
// hours or a muted conversation kept from reaching them, and followed threads
// that moved, are gathered into one digest the person receives at a time they
// choose. Nothing is stored per message: the digest is worked out when it is
// read, from messages the person can read right now, so an item never outlives
// the person's access to it and no message text is kept. Only the person's
// choice and the moment they last saw a digest are stored. Governed HCM
// notices are never held here; they stay on the notification plane.

// Digest cadences.
const (
	DigestOff      = "off"
	DigestDaily    = "daily"
	DigestQuietEnd = "quiet_end"
)

// Why an item is in the digest.
const (
	DigestWhyQuietHours = "quiet_hours"
	DigestWhyMuted      = "muted"
	DigestWhyFollowed   = "followed"
)

// Digest bounds.
const (
	DigestWindow    = 30 * 24 * time.Hour
	DigestScanLimit = 200
	DigestItemLimit = 50
)

// DigestPref is a person's digest choice. Minute is the local time of day for
// the daily cadence; the zone is the person's own.
type DigestPref struct {
	Cadence      string
	Minute       int
	Timezone     string
	LastDigestAt time.Time
}

// DigestItem is one thing in the digest. Count is how many held messages it
// stands for (a followed thread is one item however many replies it has).
type DigestItem struct {
	// Kind is what the store found: NotificationMention or NotificationReply.
	Kind string
	// Why is set by the composer: quiet_hours, muted or followed.
	Why                string
	ConversationID     string
	ConversationName   string
	ConversationKind   string
	PostID             string
	Sequence           uint64
	ThreadRootID       string
	ThreadRootSequence uint64
	AuthorID           string
	At                 time.Time
	Count              int
	// Muted and Read are facts from the store; they are cleared on the way out.
	Muted bool
	Read  bool
}

// Digest is what a person is shown. Due is false until the chosen time has
// passed with something held back; Items is then empty.
type Digest struct {
	Pref DigestPref
	Due  bool
	// Through is the moment the digest was worked out; the person's "seen"
	// answer names it so a message that arrived meanwhile is kept for the next.
	Through time.Time
	Items   []DigestItem
}

// DigestStore is the optional storage side of the digest.
type DigestStore interface {
	DigestPref(ctx context.Context, home, subject string) (DigestPref, bool, error)
	PutDigestPref(ctx context.Context, home, subject string, pref DigestPref) error
	// DigestCandidates lists mentions of the person and replies in threads they
	// follow, since the given time, that the person may read now, whatever their
	// notification choices. Newest first, at most limit.
	DigestCandidates(ctx context.Context, host, home, subject string, since time.Time, limit int) ([]DigestItem, error)
	// AdvanceDigest records that the person saw the digest through the moment;
	// it never moves backwards.
	AdvanceDigest(ctx context.Context, home, subject string, through time.Time) error
}

func (s *Service) digestStore() (DigestStore, error) {
	if s == nil || s.Conversations == nil || s.Repo == nil {
		return nil, chat.ErrUnavailable
	}
	store, ok := s.Repo.(DigestStore)
	if !ok {
		return nil, chat.ErrUnavailable
	}
	return store, nil
}

func lastLocalMinute(now time.Time, zone string, minute int) (time.Time, bool) {
	loc, err := time.LoadLocation(zone)
	if err != nil || minute < 0 || minute > 1439 {
		return time.Time{}, false
	}
	local := now.In(loc)
	at := time.Date(local.Year(), local.Month(), local.Day(), minute/60, minute%60, 0, 0, loc)
	if at.After(now) {
		local = local.AddDate(0, 0, -1)
		at = time.Date(local.Year(), local.Month(), local.Day(), minute/60, minute%60, 0, 0, loc)
	}
	return at.UTC(), true
}

// DigestInstant is the most recent moment the person's digest was due: their
// chosen local time for a daily digest, the end of their quiet hours for the
// other. It is false when the digest is off or has nothing to be timed by.
func DigestInstant(pref DigestPref, quiet QuietHours, now time.Time) (time.Time, bool) {
	switch pref.Cadence {
	case DigestDaily:
		return lastLocalMinute(now, pref.Timezone, pref.Minute)
	case DigestQuietEnd:
		if quiet.Enabled && quiet.Schedule.Week.Active {
			return LastQuietResume(quiet, now)
		}
		if !quiet.Enabled || quiet.StartMinute == quiet.EndMinute {
			return time.Time{}, false
		}
		return lastLocalMinute(now, quiet.Timezone, quiet.EndMinute)
	}
	return time.Time{}, false
}

// DigestPrefs returns the caller's digest choice; a person who never chose has
// it off.
func (s *Service) DigestPrefs(ctx context.Context, p chat.Principal) (DigestPref, error) {
	store, err := s.digestStore()
	if err != nil {
		return DigestPref{}, err
	}
	if p.TenantID == "" || p.SubjectID == "" {
		return DigestPref{}, chat.ErrInvalidArgument
	}
	pref, found, err := store.DigestPref(ctx, p.TenantID, p.SubjectID)
	if err != nil {
		return DigestPref{}, err
	}
	if !found {
		return DigestPref{Cadence: DigestOff, Minute: 480, Timezone: "UTC"}, nil
	}
	return pref, nil
}

// PutDigestPrefs saves the caller's digest choice. Turning a digest on starts
// it from now, so the first one holds only what is held back after the choice.
func (s *Service) PutDigestPrefs(ctx context.Context, p chat.Principal, want DigestPref, now time.Time) (DigestPref, error) {
	store, err := s.digestStore()
	if err != nil {
		return DigestPref{}, err
	}
	if p.TenantID == "" || p.SubjectID == "" {
		return DigestPref{}, chat.ErrInvalidArgument
	}
	switch want.Cadence {
	case DigestOff, DigestDaily, DigestQuietEnd:
	default:
		return DigestPref{}, chat.ErrInvalidArgument
	}
	if want.Minute < 0 || want.Minute > 1439 {
		return DigestPref{}, chat.ErrInvalidArgument
	}
	if want.Timezone == "" {
		want.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(want.Timezone); err != nil {
		return DigestPref{}, chat.ErrInvalidArgument
	}
	current, found, err := store.DigestPref(ctx, p.TenantID, p.SubjectID)
	if err != nil {
		return DigestPref{}, err
	}
	want.LastDigestAt = current.LastDigestAt
	if !found || current.Cadence == DigestOff || want.Cadence == DigestOff || want.LastDigestAt.IsZero() {
		want.LastDigestAt = now.UTC()
	}
	if err := store.PutDigestPref(ctx, p.TenantID, p.SubjectID, want); err != nil {
		return DigestPref{}, err
	}
	return want, nil
}

// Digest works out the caller's digest. It is empty and not due until the
// chosen time has passed since the last one the person saw. Each conversation
// is admitted again here, so a conversation the caller can no longer open
// leaves no item.
func (s *Service) Digest(ctx context.Context, p chat.Principal, host string, now time.Time) (Digest, error) {
	store, err := s.digestStore()
	if err != nil {
		return Digest{}, err
	}
	if p.TenantID == "" || p.SubjectID == "" || host == "" {
		return Digest{}, chat.ErrInvalidArgument
	}
	pref, err := s.DigestPrefs(ctx, p)
	if err != nil {
		return Digest{}, err
	}
	out := Digest{Pref: pref, Through: now.UTC()}
	if pref.Cadence == DigestOff {
		return out, nil
	}
	quiet, err := s.QuietHours(ctx, p)
	if err != nil {
		return Digest{}, err
	}
	instant, ok := DigestInstant(pref, quiet, now)
	if !ok || !pref.LastDigestAt.Before(instant) {
		return out, nil
	}
	since := pref.LastDigestAt
	if floor := now.Add(-DigestWindow); since.Before(floor) {
		since = floor
	}
	rows, err := store.DigestCandidates(ctx, host, p.TenantID, p.SubjectID, since, DigestScanLimit)
	if err != nil {
		return Digest{}, err
	}
	admitted := map[string]bool{}
	threads := map[string]*DigestItem{}
	var items []DigestItem
	for _, row := range rows {
		if row.Read || row.At.After(now) {
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
				return Digest{}, err
			}
			admitted[row.ConversationID] = ok
		}
		if !ok {
			continue
		}
		if row.Kind == NotificationMention {
			// A mention is held back by a muted conversation or by quiet hours;
			// otherwise it was delivered as usual and has no place here.
			if row.Muted {
				row.Why = DigestWhyMuted
			} else {
				if deliver, err := ShouldNotify(OptionalMention, AllMessages, quiet, row.At); err != nil || deliver {
					continue
				}
				row.Why = DigestWhyQuietHours
			}
			row.Count = 1
			items = append(items, row)
			continue
		}
		key := row.ConversationID + "/" + row.ThreadRootID
		if t := threads[key]; t != nil {
			t.Count++
			if row.At.After(t.At) {
				t.At, t.PostID, t.Sequence, t.AuthorID = row.At, row.PostID, row.Sequence, row.AuthorID
			}
			continue
		}
		row.Why, row.Count = DigestWhyFollowed, 1
		first := row
		threads[key] = &first
	}
	for _, t := range threads {
		items = append(items, *t)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].At.Equal(items[j].At) {
			return items[i].At.After(items[j].At)
		}
		return items[i].PostID < items[j].PostID
	})
	if len(items) > DigestItemLimit {
		items = items[:DigestItemLimit]
	}
	for i := range items {
		items[i].Muted, items[i].Read = false, false
	}
	out.Due, out.Items = len(items) > 0, items
	return out, nil
}

// DigestSeen records that the caller saw their digest through the moment it
// was worked out. It never moves backwards and never past now.
func (s *Service) DigestSeen(ctx context.Context, p chat.Principal, through, now time.Time) error {
	store, err := s.digestStore()
	if err != nil {
		return err
	}
	if p.TenantID == "" || p.SubjectID == "" || through.IsZero() {
		return chat.ErrInvalidArgument
	}
	if through.After(now) {
		through = now
	}
	return store.AdvanceDigest(ctx, p.TenantID, p.SubjectID, through.UTC())
}
