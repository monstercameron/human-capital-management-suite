package application

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
)

// #3808: a committed clock event becomes a card in Chat. ClockChatCards is a
// clockservice.ClockWebhookProvider, so it is driven by the same publisher as
// any other consumer of the clock outbox: the publisher owns the cursor, the
// subscription's field grants and the delivery journal; this adapter owns only
// "who is told what, in which conversation".

// clockMissingOutKind is the exception kind the clock raises for a session that
// is open past its shift with no OUT.
const clockMissingOutKind = "MISSING_OUT"

var (
	// ErrClockCardRecipientDenied is returned when a card is asked for a person
	// who is neither the worker nor in the worker's reporting line.
	ErrClockCardRecipientDenied = errors.New("clock card: recipient is not the worker or in the worker's reporting line")
	// ErrClockCardBudget is returned when the tenant has used its cards for the
	// minute. The delivery is retried, not dropped.
	ErrClockCardBudget = errors.New("clock card: tenant budget for this minute is used")
	// ErrClockCardUnavailable is returned when the adapter is not fully wired.
	ErrClockCardUnavailable = errors.New("clock card: adapter is unavailable")
)

// ClockCardPeople answers the two questions a card needs, from the HR sources
// of truth rather than from the event.
type ClockCardPeople interface {
	// ChatSubject is the Chat identifier of a clock worker, or false when the
	// worker has no Chat identity in the tenant.
	ChatSubject(ctx context.Context, tenant, workerRef string) (subject string, ok bool, err error)
	// Reviewers are the Chat identifiers of the people in the worker's reporting
	// line who may review the worker's time.
	Reviewers(ctx context.Context, tenant, workerRef string) ([]string, error)
}

// ClockCardPoster writes a card into a person's direct conversation with the
// system sender; chat.Service.PostClockCard is the served implementation.
type ClockCardPoster interface {
	PostClockCard(context.Context, chat.ClockCardPost) (chat.Post, error)
}

// ClockChatCards turns missing-punch exceptions into Chat cards.
type ClockChatCards struct {
	Sessions clockservice.MissingPunchSessionReader
	People   ClockCardPeople
	Poster   ClockCardPoster
	// Sender is the system identity cards are posted as, per tenant.
	Sender func(tenant string) chat.Principal
	// Budget caps cards per tenant per minute; nil is no cap.
	Budget *ClockCardBudget
	// MaxReviewers caps how many people in the reporting line are told; zero is 5.
	MaxReviewers int
	Now          func() time.Time
}

// MissingPunchNotice is one card for one person.
type MissingPunchNotice struct {
	Tenant, SessionID, WorkerRef, Day, Recipient string
	// EventKey names the event the card answers, so a replay is the same post.
	EventKey string
}

func (a *ClockChatCards) ready() bool {
	return a != nil && a.Sessions != nil && a.People != nil && a.Poster != nil && a.Sender != nil
}

func (a *ClockChatCards) now() time.Time {
	if a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}

// DeliverClockEvent implements clockservice.ClockWebhookProvider. An event that
// is not a missing punch is accepted untouched so the cursor moves past it.
func (a *ClockChatCards) DeliverClockEvent(ctx context.Context, attempt clockservice.ClockWebhookAttempt) (subscription.ProviderReceipt, error) {
	receipt := subscription.ProviderReceipt{Provider: "chat", IdempotencyKey: attempt.Delivery.OperationKey, Destination: attempt.Delivery.Request.Destination, Accepted: true, ReceivedAt: a.now()}
	receipt.ReceiptID = "chat-card:" + attempt.Delivery.OperationKey
	event := attempt.Event
	if event.EventType != clockservice.ClockExceptionRaised {
		return receipt, nil
	}
	kind, _ := event.Payload["kind"].(string)
	subjectKind, _ := event.Payload["subject_kind"].(string)
	sessionID, _ := event.Payload["subject_id"].(string)
	if kind != clockMissingOutKind || subjectKind != "clock_session" || strings.TrimSpace(sessionID) == "" {
		return receipt, nil
	}
	if !a.ready() {
		return subscription.ProviderReceipt{}, ErrClockCardUnavailable
	}
	tenant := event.Tenant
	// The payload is a claim. The session, read in the event's own tenant, is
	// the fact: a punch fixed since, a closed session or another tenant's id
	// gets no card.
	session, err := a.Sessions.GetMissingPunchSession(ctx, tenant, sessionID)
	if err != nil {
		if errors.Is(err, chat.ErrNotFound) {
			return receipt, nil
		}
		return subscription.ProviderReceipt{}, err
	}
	if session.TenantID != tenant || session.ID != sessionID || session.WorkerRef == "" || session.Status != "OPEN" || !session.MissingOut {
		return receipt, nil
	}
	worker, ok, err := a.People.ChatSubject(ctx, tenant, session.WorkerRef)
	if err != nil {
		return subscription.ProviderReceipt{}, err
	}
	if !ok || strings.TrimSpace(worker) == "" {
		return receipt, nil
	}
	reviewers, err := a.People.Reviewers(ctx, tenant, session.WorkerRef)
	if err != nil {
		return subscription.ProviderReceipt{}, err
	}
	if a.Budget != nil && !a.Budget.Allow(tenant, a.now()) {
		return subscription.ProviderReceipt{}, ErrClockCardBudget
	}
	day := event.Envelope.EffectiveAt.UTC().Format("2006-01-02")
	key := strconv.FormatInt(event.Sequence, 10)
	limit := a.MaxReviewers
	if limit <= 0 {
		limit = 5
	}
	recipients := []string{worker}
	for _, r := range reviewers {
		if r = strings.TrimSpace(r); r != "" && r != worker && len(recipients) <= limit {
			recipients = append(recipients, r)
		}
	}
	for _, recipient := range recipients {
		err := a.PostMissingPunch(ctx, MissingPunchNotice{Tenant: tenant, SessionID: sessionID, WorkerRef: session.WorkerRef, Day: day, Recipient: recipient, EventKey: key})
		switch {
		case err == nil:
		case errors.Is(err, chat.ErrPermissionDenied), errors.Is(err, ErrClockCardRecipientDenied):
			// Chat will not let this person be written to; the others are still
			// told and the delivery is not retried forever for one of them.
		default:
			return subscription.ProviderReceipt{}, err
		}
	}
	return receipt, nil
}

// PostMissingPunch posts one card. Who may receive it is decided here, before
// anything is written: the worker, or a person the HR reporting line names as
// the worker's reviewer. A reviewer's card names the worker and has no action;
// the worker's card opens the missing-punch page, which asks the worker's own
// session for permission, not the system sender's.
func (a *ClockChatCards) PostMissingPunch(ctx context.Context, n MissingPunchNotice) error {
	if !a.ready() {
		return ErrClockCardUnavailable
	}
	if n.Tenant == "" || n.SessionID == "" || n.WorkerRef == "" || n.Recipient == "" || n.EventKey == "" {
		return chat.ErrInvalidArgument
	}
	worker, ok, err := a.People.ChatSubject(ctx, n.Tenant, n.WorkerRef)
	if err != nil {
		return err
	}
	if !ok {
		return ErrClockCardRecipientDenied
	}
	audience := chat.ClockCardSelf
	if n.Recipient != worker {
		audience = chat.ClockCardReview
		reviewers, err := a.People.Reviewers(ctx, n.Tenant, n.WorkerRef)
		if err != nil {
			return err
		}
		inLine := false
		for _, r := range reviewers {
			inLine = inLine || strings.TrimSpace(r) == n.Recipient
		}
		if !inLine {
			return ErrClockCardRecipientDenied
		}
	}
	_, err = a.Poster.PostClockCard(ctx, chat.ClockCardPost{
		TenantID:       n.Tenant,
		Sender:         a.Sender(n.Tenant),
		Recipient:      n.Recipient,
		Card:           chat.ClockCard{Kind: chat.ClockCardMissingPunch, Audience: audience, Subject: worker, Day: n.Day, Ref: n.SessionID},
		IdempotencyKey: n.EventKey + ":" + n.SessionID + ":" + n.Recipient,
	})
	return err
}

// ClockCardBudget is the per-tenant, one-minute ceiling on cards. It is
// deliberately small and in-process: cards are a courtesy, not a queue, and a
// refused event is retried by the delivery journal rather than dropped.
type ClockCardBudget struct {
	mu     sync.Mutex
	limit  int
	events map[string][]time.Time
}

// NewClockCardBudget allows perMinute events per tenant per minute.
func NewClockCardBudget(perMinute int) *ClockCardBudget {
	return &ClockCardBudget{limit: perMinute, events: map[string][]time.Time{}}
}

// Allow records one event for the tenant and reports whether it fits.
func (b *ClockCardBudget) Allow(tenant string, now time.Time) bool {
	if b == nil || b.limit <= 0 {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	cutoff := now.Add(-time.Minute)
	kept := b.events[tenant][:0]
	for _, at := range b.events[tenant] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) >= b.limit {
		b.events[tenant] = kept
		return false
	}
	b.events[tenant] = append(kept, now)
	return true
}
