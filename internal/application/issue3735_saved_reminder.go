package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

// Issue 3735: a saved message's reminder must reach the person. The chat
// service already finds due reminders and the store already delivers each one
// once (DispatchSavedReminders, DeliverSavedReminder); this is the served caller
// that runs it for every person of every served workspace, and the delivery that
// writes the private notice.

// savedReminderInterval is how often due reminders are looked for. A reminder is
// set for a time of day, so a few minutes of lateness is acceptable.
const savedReminderInterval = 5 * time.Minute

type chatstoreSavedReminderNotice = chatstore.SavedReminderNotice

// savedReminderPeople lists the people of one workspace with a reminder due.
type savedReminderPeople interface {
	ListSavedReminderPeople(ctx context.Context, tenant string) ([]string, error)
}

// savedReminderDispatcher delivers one person's due reminders through a sink.
type savedReminderDispatcher interface {
	DispatchSavedReminders(ctx context.Context, person chat.Principal, tenant string, sink chat.SavedReminderSink) error
}

// savedReminderNoticeWriter is the store's private-notice write.
type savedReminderNoticeWriter interface {
	NotifySavedReminder(ctx context.Context, n chatstoreSavedReminderNotice) error
}

// savedReminderDelivery is the recipient notification path of the product: a
// private notice for the person, shown on the Moderation page and counted in the
// sidebar. It names the message by reference only.
type savedReminderDelivery struct{ Notices savedReminderNoticeWriter }

func (d savedReminderDelivery) DeliverPrivateSavedNotice(ctx context.Context, n PrivateSavedNotice) error {
	if isNilPersonaOutputPort(d.Notices) {
		return chat.ErrUnavailable
	}
	return d.Notices.NotifySavedReminder(ctx, chatstoreSavedReminderNotice{Home: n.RecipientHomeTenantID, Person: n.RecipientSubjectID, Host: n.HostTenantID, Conversation: n.ConversationID, Post: n.PostID, Key: n.IdempotencyKey})
}

// sweepSavedReminders delivers the due reminders of every given workspace, each
// person as themselves in their own workspace. A person or workspace that fails
// is logged and the rest are still reminded. It returns how many people were
// reminded without error.
func sweepSavedReminders(ctx context.Context, people savedReminderPeople, dispatcher savedReminderDispatcher, sink chat.SavedReminderSink, tenants []string, logger personaInvocationLogger) int {
	reminded := 0
	fail := func(tenant string, err error) {
		if ctx.Err() == nil && !isNilPersonaOutputPort(logger) {
			logger.Error("hcmnext.chat_saved_reminder_failed", "tenant_id", tenant, "error_type", fmt.Sprintf("%T", err))
		}
	}
	for _, tenant := range tenants {
		if ctx.Err() != nil {
			return reminded
		}
		due, err := people.ListSavedReminderPeople(ctx, tenant)
		if err != nil {
			fail(tenant, err)
			continue
		}
		for _, who := range due {
			if ctx.Err() != nil {
				return reminded
			}
			if err = dispatcher.DispatchSavedReminders(ctx, chat.Principal{TenantID: tenant, SubjectID: who}, tenant, sink); err != nil {
				fail(tenant, err)
				continue
			}
			reminded++
		}
	}
	return reminded
}

// savedReminderWorkload is the served reminder run. It is nil when it has
// nothing to run with.
func savedReminderWorkload(people savedReminderPeople, dispatcher savedReminderDispatcher, sink chat.SavedReminderSink, tenants []string, logger personaInvocationLogger, interval time.Duration) *bootstrap.Workload {
	if isNilPersonaOutputPort(people) || isNilPersonaOutputPort(dispatcher) || isNilPersonaOutputPort(sink) || interval <= 0 {
		return nil
	}
	scoped := make([]string, 0, len(tenants))
	seen := make(map[string]bool, len(tenants))
	for _, tenant := range tenants {
		if strings.TrimSpace(tenant) != tenant || values.TenantId(tenant).Validate() != nil {
			return nil
		}
		if !seen[tenant] {
			seen[tenant] = true
			scoped = append(scoped, tenant)
		}
	}
	if len(scoped) == 0 {
		return nil
	}
	return &bootstrap.Workload{Name: "chat-saved-reminders", Run: func(ctx context.Context) error {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			sweepSavedReminders(ctx, people, dispatcher, sink, scoped, logger)
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	}}
}
