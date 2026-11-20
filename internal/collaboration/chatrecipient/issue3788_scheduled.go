package chatrecipient

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3788: Send later. A person writes a message now and chooses when it
// goes out. Until then it is the author's private draft: only the author can
// list, change or cancel it. Nothing is decided about the author's authority
// when the message is scheduled beyond reading the conversation; at the moment
// of sending the ordinary Send service runs as the author, so membership,
// history, the content filter and DLP, and the channel's status are all checked
// again (see application.ScheduledPostDelivery).

// Scheduled post states.
const (
	ScheduledPending = "pending"
	ScheduledSending = "sending"
	ScheduledFailed  = "failed"
)

// Reasons a due message was not sent. They are short codes; the page words
// them. A reason is read only by the author.
const (
	ScheduledRefusedAccess      = "no_access"
	ScheduledRefusedChannel     = "channel_closed"
	ScheduledRefusedContent     = "content_blocked"
	ScheduledRefusedUnavailable = "unavailable"
)

// Bounds.
const (
	ScheduledBodyRunes    = 4000
	ScheduledMinLead      = time.Minute
	ScheduledMaxLead      = 365 * 24 * time.Hour
	ScheduledPerPersonMax = 50
	// ScheduledMaxAttempts is how many times a delivery that could not reach the
	// store is tried before the author is told it was not sent.
	ScheduledMaxAttempts = 5
	// ScheduledClaimFor is how long one delivery attempt may take before another
	// worker may pick the row up again.
	ScheduledClaimFor = 2 * time.Minute
)

var (
	// ErrScheduledLimit says the person already has the most scheduled messages.
	ErrScheduledLimit = errors.New("chat: scheduled message limit reached")
	// ErrScheduledTime says the chosen time is not a moment in the allowed window.
	ErrScheduledTime = errors.New("chat: scheduled time is outside the allowed window")
)

// ScheduledPost is one message waiting to be sent by its author.
type ScheduledPost struct {
	ID             string
	HostTenantID   string
	HomeTenantID   string
	AuthorID       string
	OrgScopeID     string
	ConversationID string
	Body           string
	SendAt         time.Time
	// Zone is the author's time zone when they chose the time.
	Zone     string
	State    string
	Refusal  string
	Attempts int
	Revision uint64
	// QuietPeople counts the people written to whose quiet hours include
	// SendAt. It is worked out when the row is returned and is never stored; it
	// names nobody and says nothing about their hours.
	QuietPeople int
	CreatedAt   time.Time
}

// ScheduledStore is the optional storage side of Send later.
type ScheduledStore interface {
	InsertScheduled(ctx context.Context, post ScheduledPost, limit int) (ScheduledPost, error)
	ListScheduled(ctx context.Context, host, home, author string) ([]ScheduledPost, error)
	UpdateScheduled(ctx context.Context, host, home, author, id, body string, sendAt time.Time, zone string, expected uint64) (ScheduledPost, error)
	DeleteScheduled(ctx context.Context, host, home, author, id string) error
	// The delivery side. DueScheduled lists every author's due rows of one
	// workspace; ClaimScheduled moves one to "sending" for ScheduledClaimFor and
	// reports false when another worker holds it or it changed; FinishScheduled
	// deletes a sent row; FailScheduled keeps the row with a reason for its
	// author; ReleaseScheduled puts a row back after an attempt that could not
	// finish, and fails it with "unavailable" after ScheduledMaxAttempts.
	DueScheduled(ctx context.Context, host string, now time.Time, limit int) ([]ScheduledPost, error)
	ClaimScheduled(ctx context.Context, host, id string, now time.Time, lease time.Duration) (ScheduledPost, bool, error)
	FinishScheduled(ctx context.Context, host, id string) error
	FailScheduled(ctx context.Context, host, id, refusal string) error
	ReleaseScheduled(ctx context.Context, host, id string, maxAttempts int) error
}

func (s *Service) scheduledStore() (ScheduledStore, error) {
	if s == nil || s.Conversations == nil || s.Repo == nil {
		return nil, chat.ErrUnavailable
	}
	store, ok := s.Repo.(ScheduledStore)
	if !ok {
		return nil, chat.ErrUnavailable
	}
	return store, nil
}

// ScheduledSendTime reads the time a person chose. It must be a real moment at
// least a minute away and within a year, and the zone must be one this server
// knows (an empty zone means UTC).
func ScheduledSendTime(at time.Time, zone string, now time.Time) (time.Time, string, error) {
	zone = strings.TrimSpace(zone)
	if zone == "" {
		zone = "UTC"
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return time.Time{}, "", chat.ErrInvalidArgument
	}
	if at.IsZero() || at.Before(now.Add(ScheduledMinLead)) || at.After(now.Add(ScheduledMaxLead)) {
		return time.Time{}, "", ErrScheduledTime
	}
	return at.UTC(), zone, nil
}

func scheduledBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > ScheduledBodyRunes {
		return "", chat.ErrInvalidArgument
	}
	return body, nil
}

// ScheduleSend keeps a message to be sent at sendAt. The conversation is read
// as the caller first, so a conversation the caller cannot open is refused
// without a trace. orgScope is the caller's organization scope from the
// verified session; delivery needs it to act as the caller later.
func (s *Service) ScheduleSend(ctx context.Context, p chat.Principal, host, conversation, body string, sendAt time.Time, zone, orgScope string, now time.Time) (ScheduledPost, error) {
	store, err := s.scheduledStore()
	if err != nil {
		return ScheduledPost{}, err
	}
	if p.TenantID == "" || p.SubjectID == "" || host == "" || conversation == "" {
		return ScheduledPost{}, chat.ErrInvalidArgument
	}
	if body, err = scheduledBody(body); err != nil {
		return ScheduledPost{}, err
	}
	if sendAt, zone, err = ScheduledSendTime(sendAt, zone, now); err != nil {
		return ScheduledPost{}, err
	}
	if _, err = s.admit(ctx, p, host, conversation); err != nil {
		return ScheduledPost{}, err
	}
	saved, err := store.InsertScheduled(ctx, ScheduledPost{HostTenantID: host, HomeTenantID: p.TenantID, AuthorID: p.SubjectID, OrgScopeID: orgScope, ConversationID: conversation, Body: body, SendAt: sendAt, Zone: zone, State: ScheduledPending}, ScheduledPerPersonMax)
	if err != nil {
		return ScheduledPost{}, err
	}
	saved.QuietPeople = s.quietPeople(ctx, p, saved)
	return saved, nil
}

// ScheduledPosts lists the caller's own waiting and refused messages, soonest
// first. Other people's rows are never read.
func (s *Service) ScheduledPosts(ctx context.Context, p chat.Principal, host string) ([]ScheduledPost, error) {
	store, err := s.scheduledStore()
	if err != nil {
		return nil, err
	}
	if p.TenantID == "" || p.SubjectID == "" || host == "" {
		return nil, chat.ErrInvalidArgument
	}
	rows, err := store.ListScheduled(ctx, host, p.TenantID, p.SubjectID)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].OrgScopeID = ""
		if rows[i].State != ScheduledFailed {
			rows[i].QuietPeople = s.quietPeople(ctx, p, rows[i])
		}
	}
	return rows, nil
}

// EditScheduled changes the text or the time of one of the caller's messages.
// A refused message is scheduled again by editing it. A message that is being
// sent right now cannot be changed.
func (s *Service) EditScheduled(ctx context.Context, p chat.Principal, host, id, body string, sendAt time.Time, zone string, expected uint64, now time.Time) (ScheduledPost, error) {
	store, err := s.scheduledStore()
	if err != nil {
		return ScheduledPost{}, err
	}
	if p.TenantID == "" || p.SubjectID == "" || host == "" || strings.TrimSpace(id) == "" || expected == 0 {
		return ScheduledPost{}, chat.ErrInvalidArgument
	}
	if body, err = scheduledBody(body); err != nil {
		return ScheduledPost{}, err
	}
	if sendAt, zone, err = ScheduledSendTime(sendAt, zone, now); err != nil {
		return ScheduledPost{}, err
	}
	saved, err := store.UpdateScheduled(ctx, host, p.TenantID, p.SubjectID, id, body, sendAt, zone, expected)
	if err != nil {
		return ScheduledPost{}, err
	}
	saved.OrgScopeID = ""
	saved.QuietPeople = s.quietPeople(ctx, p, saved)
	return saved, nil
}

// CancelScheduled removes one of the caller's messages and its text.
func (s *Service) CancelScheduled(ctx context.Context, p chat.Principal, host, id string) error {
	store, err := s.scheduledStore()
	if err != nil {
		return err
	}
	if p.TenantID == "" || p.SubjectID == "" || host == "" || strings.TrimSpace(id) == "" {
		return chat.ErrInvalidArgument
	}
	return store.DeleteScheduled(ctx, host, p.TenantID, p.SubjectID, id)
}

// quietPeople counts the people the message is written to whose quiet hours
// include its send time. Only a direct message or a group names the people it
// is written to; a channel addresses nobody in particular. A failure to find
// out counts as nobody: the warning is advice, not a gate.
func (s *Service) quietPeople(ctx context.Context, p chat.Principal, post ScheduledPost) int {
	conversation, err := s.Conversations.GetConversation(ctx, chat.GetConversationRequest{Principal: p, TenantID: post.HostTenantID, ConversationID: post.ConversationID})
	if err != nil || (conversation.Kind != chat.Direct && conversation.Kind != chat.Group) {
		return 0
	}
	members, err := s.Conversations.ListMemberships(ctx, chat.ListMembershipsRequest{Principal: p, TenantID: post.HostTenantID, ConversationID: post.ConversationID, Page: chat.Page{PageSize: 100}})
	if err != nil {
		return 0
	}
	count := 0
	for _, member := range members.Memberships {
		if member.LeftAt != nil || (member.SubjectID == p.SubjectID && member.HomeTenantID == p.TenantID) {
			continue
		}
		home := member.HomeTenantID
		if home == "" {
			home = post.HostTenantID
		}
		quiet, err := s.Repo.QuietHours(ctx, home, member.SubjectID)
		if err == nil && InQuietHours(quiet, post.SendAt) {
			count++
		}
	}
	return count
}

// InQuietHours reports whether at falls inside the quiet hours, read in their
// own zone. A window that ends before it starts runs overnight.
func InQuietHours(q QuietHours, at time.Time) bool {
	if q.Schedule.Week.Active || q.Schedule.Pause.Kind != "" {
		return QuietAt(q, at)
	}
	if !q.Enabled || q.StartMinute == q.EndMinute {
		return false
	}
	zone, err := time.LoadLocation(q.Timezone)
	if err != nil {
		return false
	}
	local := at.In(zone)
	minute := local.Hour()*60 + local.Minute()
	if q.StartMinute < q.EndMinute {
		return minute >= q.StartMinute && minute < q.EndMinute
	}
	return minute >= q.StartMinute || minute < q.EndMinute
}
