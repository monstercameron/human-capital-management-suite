package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3788: the delivery of a message its author chose to send later.
//
// The job does not post on its own authority and does not trust what was true
// when the message was scheduled. For each due message it acts as the author
// and calls the same Send service the composer uses, so the author's
// membership and history, the content filter and DLP, and the channel's
// status (locked, announcement-only, archived) are all decided again at the
// moment of sending. A refusal posts nothing; the message stays in the
// author's Scheduled tray with a short reason that only the author can read.
// Each attempt is keyed by the message's id, so a retry after a crash cannot
// post it twice.

// scheduledPostInterval is how often due messages are looked for. A message is
// chosen by minute, so under a minute of lateness is the aim.
const scheduledPostInterval = 30 * time.Second

// scheduledPostBatch bounds one workspace's work in one pass.
const scheduledPostBatch = 100

// scheduledDeliveryKey marks a context as the delivery job acting as one
// author. Only code in this package can set it.
type scheduledDeliveryKey struct{}

type scheduledDeliveryAuthor struct{ tenant, subject string }

func withScheduledDelivery(ctx context.Context, tenant, subject string) context.Context {
	return context.WithValue(ctx, scheduledDeliveryKey{}, scheduledDeliveryAuthor{tenant, subject})
}

// scheduledDeliveryFor reports whether ctx is the delivery job acting as this
// exact author. The author's browser session is not required then: the author
// chose the time while signed in, and may be signed out when it comes. What is
// still read fresh is everything else: the worker record, the role assignment,
// the membership and the channel.
func scheduledDeliveryFor(ctx context.Context, tenant, subject string) bool {
	who, ok := ctx.Value(scheduledDeliveryKey{}).(scheduledDeliveryAuthor)
	return ok && who.tenant == tenant && who.subject == subject
}

// scheduledPostSender is the ordinary Send service.
type scheduledPostSender interface {
	SendPost(context.Context, chat.SendPostRequest) (chat.Post, error)
}

// ScheduledPostDelivery sends the due messages of served workspaces.
type ScheduledPostDelivery struct {
	Store  chatrecipient.ScheduledStore
	Send   scheduledPostSender
	Now    func() time.Time
	Logger personaInvocationLogger
}

func (d ScheduledPostDelivery) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}

// scheduledAuthorContext is the context the author's message is sent under: a
// short-lived human principal for exactly the author, carrying the
// organization scope kept when the message was scheduled.
func scheduledAuthorContext(ctx context.Context, post chatrecipient.ScheduledPost, now time.Time) (context.Context, error) {
	ref := "scheduled-post:" + post.ID
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(post.HomeTenantID), Subject: post.AuthorID, SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: post.OrgScopeID, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow,
		SessionRef: ref, CredentialDigest: ref, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(5 * time.Minute)})
	if err != nil {
		return ctx, err
	}
	return withScheduledDelivery(trust.WithPrincipal(ctx, principal), post.HomeTenantID, post.AuthorID), nil
}

// scheduledRefusal sorts a Send error into what the author is told. It returns
// "" for an error that may pass on its own (the store was busy), which is
// tried again.
func scheduledRefusal(err error) string {
	var blocked *chatfilter.BlockedError
	switch {
	case errors.Is(err, chat.ErrChannelStatus), errors.Is(err, chat.ErrChannelHeld):
		return chatrecipient.ScheduledRefusedChannel
	case errors.As(err, &blocked), errors.Is(err, chatfilter.ErrBlocked), errors.Is(err, chat.ErrInvalidArgument):
		return chatrecipient.ScheduledRefusedContent
	case errors.Is(err, chat.ErrPermissionDenied), errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrUnauthenticated), errors.Is(err, chat.ErrAudienceChanged), errors.Is(err, chatfilter.ErrDenied):
		return chatrecipient.ScheduledRefusedAccess
	}
	return ""
}

func (d ScheduledPostDelivery) log(host string, id, outcome string) {
	if !isNilPersonaOutputPort(d.Logger) {
		d.Logger.Error("hcmnext.chat_scheduled_post", "tenant_id", host, "scheduled_id", id, "outcome", outcome)
	}
}

// Deliver sends what is due in one workspace and returns how many messages
// were posted. A message that cannot be posted is refused or tried again; it
// never stops the others.
func (d ScheduledPostDelivery) Deliver(ctx context.Context, host string) (int, error) {
	if isNilPersonaOutputPort(d.Store) || isNilPersonaOutputPort(d.Send) {
		return 0, chat.ErrUnavailable
	}
	now := d.now()
	due, err := d.Store.DueScheduled(ctx, host, now, scheduledPostBatch)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, row := range due {
		if ctx.Err() != nil {
			return sent, ctx.Err()
		}
		post, ok, err := d.Store.ClaimScheduled(ctx, host, row.ID, now, chatrecipient.ScheduledClaimFor)
		if err != nil {
			d.log(host, row.ID, "claim_failed")
			continue
		}
		if !ok {
			continue
		}
		if d.deliverOne(ctx, host, post, now) {
			sent++
		}
	}
	return sent, nil
}

func (d ScheduledPostDelivery) deliverOne(ctx context.Context, host string, post chatrecipient.ScheduledPost, now time.Time) bool {
	asAuthor, err := scheduledAuthorContext(ctx, post, now)
	if err != nil {
		d.fail(ctx, host, post.ID, chatrecipient.ScheduledRefusedAccess)
		return false
	}
	_, err = d.Send.SendPost(asAuthor, chat.SendPostRequest{
		Principal:      chat.Principal{TenantID: post.HomeTenantID, SubjectID: post.AuthorID},
		TenantID:       post.HostTenantID,
		ConversationID: post.ConversationID,
		Body:           post.Body,
		IdempotencyKey: "scheduled:" + post.ID,
	})
	if err == nil {
		if finishErr := d.Store.FinishScheduled(ctx, host, post.ID); finishErr != nil {
			// The message is posted. The row stays claimed until the lease runs
			// out and the retry posts nothing new: the key finds the same post.
			d.log(host, post.ID, "finish_failed")
			return true
		}
		d.log(host, post.ID, "sent")
		return true
	}
	if refusal := scheduledRefusal(err); refusal != "" {
		d.fail(ctx, host, post.ID, refusal)
		return false
	}
	if releaseErr := d.Store.ReleaseScheduled(ctx, host, post.ID, chatrecipient.ScheduledMaxAttempts); releaseErr != nil {
		d.log(host, post.ID, "release_failed")
		return false
	}
	d.log(host, post.ID, "retry")
	return false
}

func (d ScheduledPostDelivery) fail(ctx context.Context, host, id, refusal string) {
	if err := d.Store.FailScheduled(ctx, host, id, refusal); err != nil {
		d.log(host, id, "fail_failed")
		return
	}
	d.log(host, id, refusal)
}

// scheduledPostWorkload is the served delivery run. It is nil when it has
// nothing to run with.
func scheduledPostWorkload(delivery ScheduledPostDelivery, tenants []string, interval time.Duration) *bootstrap.Workload {
	if isNilPersonaOutputPort(delivery.Store) || isNilPersonaOutputPort(delivery.Send) || interval <= 0 {
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
	return &bootstrap.Workload{Name: "chat-scheduled-posts", Run: func(ctx context.Context) error {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			for _, tenant := range scoped {
				if ctx.Err() != nil {
					return nil
				}
				if _, err := delivery.Deliver(ctx, tenant); err != nil && ctx.Err() == nil && !isNilPersonaOutputPort(delivery.Logger) {
					delivery.Logger.Error("hcmnext.chat_scheduled_post_pass_failed", "tenant_id", tenant, "error_type", fmt.Sprintf("%T", err))
				}
			}
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	}}
}

// scheduledPostDeliveryFor binds the delivery to the composed Chat: the
// recipient-state store for the rows and the routed service every ordinary
// send goes through.
func scheduledPostDeliveryFor(runtime composedChat, now func() time.Time, logger personaInvocationLogger) ScheduledPostDelivery {
	return ScheduledPostDelivery{Store: chatstore.NewRecipientStateStore(runtime.store), Send: runtime.service, Now: now, Logger: logger}
}
