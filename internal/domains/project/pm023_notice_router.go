package project

import (
	"context"
	"errors"
	"sync"
	"time"
)

type ProjectNoticeKind string

const (
	NoticeAssigned  ProjectNoticeKind = "ASSIGNED"
	NoticeMentioned ProjectNoticeKind = "MENTIONED"
	NoticeDue       ProjectNoticeKind = "DUE"
	NoticeStatus    ProjectNoticeKind = "STATUS"
)

var (
	ErrInvalidProjectNotice = errors.New("project: invalid notice")
	ErrProjectNoticeReplay  = errors.New("project: notice replay")
)

type ProjectNoticeEvent struct {
	EventID, TenantID, ProjectID, TaskID string
	Kind                                 ProjectNoticeKind
	At                                   time.Time
	MandatoryHCM                         bool
}

type ProjectNoticeAudience struct {
	Revision   uint64
	Recipients []string
	Revoked    map[string]bool
}

type ProjectNoticePreference struct {
	Muted      bool
	QuietHours bool
}

type ProjectNotice struct {
	Event            ProjectNoticeEvent
	Recipient        string
	AudienceRevision uint64
}

type ProjectNoticeRouter struct {
	Audience   func(context.Context, ProjectNoticeEvent) (ProjectNoticeAudience, error)
	Preference func(context.Context, string, ProjectNoticeEvent) (ProjectNoticePreference, error)
	Deliver    func(context.Context, ProjectNotice) error

	mu   sync.Mutex
	seen map[string]struct{}
}

// Route resolves the audience at delivery time, applies optional project
// preferences, and delegates delivery to Messaging. Mandatory HCM events
// bypass project mute and quiet hours. Successful deliveries are replay-safe;
// a failed delivery is not marked as delivered so the outbox can retry it.
func (r *ProjectNoticeRouter) Route(ctx context.Context, event ProjectNoticeEvent) error {
	if r == nil || r.Audience == nil || r.Preference == nil || r.Deliver == nil || event.EventID == "" || event.TenantID == "" || event.ProjectID == "" || event.TaskID == "" || event.At.IsZero() || !validProjectNoticeKind(event.Kind) {
		return ErrInvalidProjectNotice
	}
	audience, err := r.Audience(ctx, event)
	if err != nil {
		return err
	}
	if audience.Revision == 0 {
		return ErrInvalidProjectNotice
	}
	for _, recipient := range audience.Recipients {
		if recipient == "" || audience.Revoked != nil && audience.Revoked[recipient] {
			continue
		}
		preference, err := r.Preference(ctx, recipient, event)
		if err != nil {
			return err
		}
		if !event.MandatoryHCM && (preference.Muted || preference.QuietHours) {
			continue
		}
		receipt := ProjectNotice{Event: event, Recipient: recipient, AudienceRevision: audience.Revision}
		if !r.claimDelivery(event.EventID, recipient) {
			continue
		}
		if err := r.Deliver(ctx, receipt); err != nil {
			r.releaseDelivery(event.EventID, recipient)
			return err
		}
	}
	return nil
}

func (r *ProjectNoticeRouter) claimDelivery(eventID, recipient string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen == nil {
		r.seen = make(map[string]struct{})
	}
	key := eventID + "\x00" + recipient
	if _, ok := r.seen[key]; ok {
		return false
	}
	r.seen[key] = struct{}{}
	return true
}

func (r *ProjectNoticeRouter) releaseDelivery(eventID, recipient string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen != nil {
		delete(r.seen, eventID+"\x00"+recipient)
	}
}

func validProjectNoticeKind(kind ProjectNoticeKind) bool {
	switch kind {
	case NoticeAssigned, NoticeMentioned, NoticeDue, NoticeStatus:
		return true
	default:
		return false
	}
}
