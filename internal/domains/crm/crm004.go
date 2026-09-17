// CRM-004: deliver campaigns and track permitted engagement.
//
// Deliver binds a sealed campaign to an explicit delivery plan: one channel,
// a closed set of approved tracking signals, and per-recipient
// consent/preference/bounce evidence. Tracking observes only approved
// signals: any other signal kind, or a kind outside the plan's approved set,
// is refused as covert surveillance without naming the subject. Engagement
// events reconcile each delivery through SENT -> DELIVERED -> ENGAGED, park
// bounces on the fallback channel (or as BOUNCED when no fallback applies),
// suppress opt-outs and close tracking after consent withdrawal. All state
// transitions are pure copies sealed under a canonical digest; clocks arrive
// as parameters, never from the wall.
package crm

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrInvalidDelivery reports a delivery plan or batch that cannot be
	// trusted: a forged campaign digest, an ineligible campaign, a foreign
	// recipient, a duplicate contact or a tampered seal.
	ErrInvalidDelivery = errors.New("crm: invalid campaign delivery")
	// ErrSurveillanceRefused reports an engagement signal outside the closed
	// vocabulary or outside the plan's approved set. The refusal never
	// names the subject.
	ErrSurveillanceRefused = errors.New("crm: unapproved engagement signal refused")
	// ErrContactSuppressed reports tracking attempted against a suppressed
	// or unsubscribed contact.
	ErrContactSuppressed = errors.New("crm: contact is suppressed")
)

// SignalKind is the closed vocabulary of trackable engagement signals.
// Anything else is not tracked, ever.
type SignalKind string

// Trackable signals.
const (
	SignalDelivery         SignalKind = "DELIVERY"
	SignalReply            SignalKind = "REPLY"
	SignalClick            SignalKind = "CLICK"
	SignalBounce           SignalKind = "BOUNCE"
	SignalOptOut           SignalKind = "PREFERENCE_OPT_OUT"
	SignalConsentWithdrawn SignalKind = "CONSENT_WITHDRAWN"
)

// Valid reports whether the kind belongs to the closed signal vocabulary.
func (k SignalKind) Valid() bool {
	switch k {
	case SignalDelivery, SignalReply, SignalClick, SignalBounce, SignalOptOut, SignalConsentWithdrawn:
		return true
	default:
		return false
	}
}

// RecipientContact is the per-recipient delivery evidence: consent,
// preference and bounce state known before sending.
type RecipientContact struct {
	Subject           values.EntityRef
	HasConsent        bool
	PreferenceOptOut  bool
	PreviouslyBounced bool
}

// DeliveryPlan pins what may be sent, where, to whom and what may be
// tracked. SendAt is injected.
type DeliveryPlan struct {
	CampaignDigest  string
	Channel         values.EntityRef
	FallbackChannel values.EntityRef
	ApprovedSignals []SignalKind
	Recipients      []RecipientContact
	SendAt          values.Instant
}

// ContactState is the reconciled state of one delivery.
type ContactState string

// Contact states.
const (
	ContactSent           ContactState = "SENT"
	ContactDelivered      ContactState = "DELIVERED"
	ContactEngaged        ContactState = "ENGAGED"
	ContactBounced        ContactState = "BOUNCED"
	ContactSuppressed     ContactState = "SUPPRESSED"
	ContactFallbackQueued ContactState = "FALLBACK_QUEUED"
	ContactUnsubscribed   ContactState = "UNSUBSCRIBED"
)

// DeliveryRecord is one recipient's reconciled delivery state.
type DeliveryRecord struct {
	DeliveryID     string
	Subject        values.EntityRef
	State          ContactState
	Channel        values.EntityRef
	SuppressReason string
}

// DeliveryBatch is the sealed delivery outcome with its approved signals.
type DeliveryBatch struct {
	CampaignDigest  string
	Channel         values.EntityRef
	FallbackChannel values.EntityRef
	ApprovedSignals []SignalKind
	Records         []DeliveryRecord
	SentAt          values.Instant
	CanonicalDigest string
}

// Deliver sends a sealed campaign to the plan's recipients, suppressing
// unconsented and opted-out contacts and queueing previously bounced ones
// on the fallback channel.
func Deliver(plan DeliveryPlan, campaign CampaignRevision, now values.Instant) (DeliveryBatch, error) {
	if err := campaign.Validate(); err != nil {
		return DeliveryBatch{}, fmt.Errorf("%w: campaign: %v", ErrInvalidDelivery, err)
	}
	if plan.CampaignDigest == "" || plan.CampaignDigest != campaign.CanonicalDigest {
		return DeliveryBatch{}, fmt.Errorf("%w: plan is not bound to the sealed campaign", ErrInvalidDelivery)
	}
	if err := campaign.EligibleAt(now); err != nil {
		return DeliveryBatch{}, fmt.Errorf("%w: campaign is not eligible: %v", ErrInvalidDelivery, err)
	}
	approved, err := approvedSignals(plan.ApprovedSignals)
	if err != nil {
		return DeliveryBatch{}, err
	}
	if err := plan.Channel.Validate(); err != nil || plan.Channel.Tenant != campaign.CampaignID.Tenant {
		return DeliveryBatch{}, fmt.Errorf("%w: channel is invalid or crosses tenant", ErrInvalidDelivery)
	}
	needsFallback := false
	for _, recipient := range plan.Recipients {
		if recipient.PreviouslyBounced {
			needsFallback = true
		}
	}
	if needsFallback {
		if err := plan.FallbackChannel.Validate(); err != nil || plan.FallbackChannel.Tenant != campaign.CampaignID.Tenant {
			return DeliveryBatch{}, fmt.Errorf("%w: bounced contacts require a valid fallback channel", ErrInvalidDelivery)
		}
	}
	if len(plan.Recipients) == 0 {
		return DeliveryBatch{}, fmt.Errorf("%w: no recipients", ErrInvalidDelivery)
	}
	seen := make(map[string]struct{}, len(plan.Recipients))
	records := make([]DeliveryRecord, 0, len(plan.Recipients))
	for i, recipient := range plan.Recipients {
		if err := recipient.Subject.Validate(); err != nil || recipient.Subject.Tenant != campaign.CampaignID.Tenant {
			return DeliveryBatch{}, fmt.Errorf("%w: recipient %d is invalid or crosses tenant", ErrInvalidDelivery, i)
		}
		key := recipient.Subject.String()
		if _, dup := seen[key]; dup {
			return DeliveryBatch{}, fmt.Errorf("%w: duplicate recipient %d", ErrInvalidDelivery, i)
		}
		seen[key] = struct{}{}
		record := DeliveryRecord{
			DeliveryID: fmt.Sprintf("delivery-%04d", i+1),
			Subject:    recipient.Subject,
			State:      ContactSent,
			Channel:    plan.Channel,
		}
		switch {
		case !recipient.HasConsent:
			record.State, record.SuppressReason = ContactSuppressed, "consent:not-granted"
		case recipient.PreferenceOptOut:
			record.State, record.SuppressReason = ContactSuppressed, "preference:opt-out"
		case recipient.PreviouslyBounced:
			record.State, record.Channel = ContactFallbackQueued, plan.FallbackChannel
		}
		records = append(records, record)
	}
	batch := DeliveryBatch{
		CampaignDigest:  plan.CampaignDigest,
		Channel:         plan.Channel,
		FallbackChannel: plan.FallbackChannel,
		ApprovedSignals: approved,
		Records:         records,
		SentAt:          now,
	}
	batch.CanonicalDigest = deliveryDigest(batch)
	return batch, nil
}

func approvedSignals(signals []SignalKind) ([]SignalKind, error) {
	if len(signals) == 0 {
		return nil, fmt.Errorf("%w: no approved tracking signal", ErrInvalidDelivery)
	}
	seen := make(map[SignalKind]struct{}, len(signals))
	out := make([]SignalKind, 0, len(signals))
	for _, signal := range signals {
		if !signal.Valid() {
			return nil, fmt.Errorf("%w: signal %q is outside the closed vocabulary", ErrInvalidDelivery, signal)
		}
		if _, dup := seen[signal]; dup {
			return nil, fmt.Errorf("%w: duplicate approved signal %q", ErrInvalidDelivery, signal)
		}
		seen[signal] = struct{}{}
		out = append(out, signal)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// EngagementEvent is one observed signal for one delivery. At is injected.
type EngagementEvent struct {
	DeliveryID string
	Signal     SignalKind
	At         values.Instant
}

// RecordEngagement reconciles one approved signal against the batch,
// returning an updated sealed copy. The sealed receiver is never mutated.
func (b DeliveryBatch) RecordEngagement(event EngagementEvent) (DeliveryBatch, error) {
	if !event.Signal.Valid() {
		return DeliveryBatch{}, fmt.Errorf("%w: signal %q is outside the closed vocabulary", ErrSurveillanceRefused, event.Signal)
	}
	if !approved(event.Signal, b.ApprovedSignals) {
		return DeliveryBatch{}, fmt.Errorf("%w: signal %q is not an approved tracking signal", ErrSurveillanceRefused, event.Signal)
	}
	index := -1
	for i := range b.Records {
		if b.Records[i].DeliveryID == event.DeliveryID {
			index = i
			break
		}
	}
	if index < 0 {
		return DeliveryBatch{}, fmt.Errorf("%w: unknown delivery", ErrInvalidDelivery)
	}
	record := b.Records[index]
	switch record.State {
	case ContactSuppressed, ContactUnsubscribed:
		return DeliveryBatch{}, fmt.Errorf("%w: tracking a suppressed contact is refused", ErrContactSuppressed)
	}
	next := record
	switch event.Signal {
	case SignalConsentWithdrawn:
		next.State, next.SuppressReason = ContactUnsubscribed, "consent:withdrawn"
	case SignalDelivery:
		switch record.State {
		case ContactSent, ContactDelivered, ContactEngaged:
			next.State = ContactDelivered
			if record.State == ContactEngaged {
				next.State = ContactEngaged
			}
		default:
			return DeliveryBatch{}, fmt.Errorf("%w: delivery confirmation does not apply", ErrInvalidDelivery)
		}
	case SignalReply, SignalClick:
		switch record.State {
		case ContactSent, ContactDelivered, ContactEngaged:
			next.State = ContactEngaged
		default:
			return DeliveryBatch{}, fmt.Errorf("%w: engagement does not apply", ErrInvalidDelivery)
		}
	case SignalBounce:
		switch record.State {
		case ContactSent, ContactDelivered:
			next.State = ContactBounced
			if b.FallbackChannel != (values.EntityRef{}) {
				next.State, next.Channel = ContactFallbackQueued, b.FallbackChannel
			}
		default:
			return DeliveryBatch{}, fmt.Errorf("%w: bounce does not apply", ErrInvalidDelivery)
		}
	case SignalOptOut:
		next.State, next.SuppressReason = ContactSuppressed, "preference:opt-out"
	}
	out := b
	out.Records = append([]DeliveryRecord(nil), b.Records...)
	out.Records[index] = next
	out.CanonicalDigest = deliveryDigest(out)
	return out, nil
}

func approved(signal SignalKind, approved []SignalKind) bool {
	for _, kind := range approved {
		if kind == signal {
			return true
		}
	}
	return false
}

// SuppressedOrParkedFor reports whether the delivery rests in a non-contact
// state: suppressed, bounced, fallback-queued or unsubscribed.
func (b DeliveryBatch) SuppressedOrParkedFor(deliveryID string) bool {
	for _, record := range b.Records {
		if record.DeliveryID != deliveryID {
			continue
		}
		switch record.State {
		case ContactSuppressed, ContactBounced, ContactFallbackQueued, ContactUnsubscribed:
			return true
		default:
			return false
		}
	}
	return false
}

// DeliverySummary reconciles the batch into per-state counts.
type DeliverySummary struct {
	Total           int
	Sent            int
	Delivered       int
	Engaged         int
	Bounced         int
	Suppressed      int
	FallbackQueued  int
	Unsubscribed    int
	CanonicalDigest string
}

// Summary reconciles the batch into per-state counts.
func (b DeliveryBatch) Summary() DeliverySummary {
	summary := DeliverySummary{Total: len(b.Records)}
	for _, record := range b.Records {
		switch record.State {
		case ContactSent:
			summary.Sent++
		case ContactDelivered:
			summary.Delivered++
		case ContactEngaged:
			summary.Engaged++
		case ContactBounced:
			summary.Bounced++
		case ContactSuppressed:
			summary.Suppressed++
		case ContactFallbackQueued:
			summary.FallbackQueued++
		case ContactUnsubscribed:
			summary.Unsubscribed++
		}
	}
	w := canonicalbytes.New("hcmnext.domains.crm.DeliverySummary", 1).
		String("batch", b.CanonicalDigest).
		Int("total", int64(summary.Total)).Int("sent", int64(summary.Sent)).
		Int("delivered", int64(summary.Delivered)).Int("engaged", int64(summary.Engaged)).
		Int("bounced", int64(summary.Bounced)).Int("suppressed", int64(summary.Suppressed)).
		Int("fallback_queued", int64(summary.FallbackQueued)).Int("unsubscribed", int64(summary.Unsubscribed))
	raw, err := w.Bytes()
	if err == nil {
		summary.CanonicalDigest = canonicalbytes.Digest(raw)
	}
	return summary
}

func instantString(instant values.Instant) string {
	return instant.Time().UTC().Format(time.RFC3339Nano)
}

func deliveryDigest(batch DeliveryBatch) string {
	signals := make([]string, 0, len(batch.ApprovedSignals))
	for _, signal := range batch.ApprovedSignals {
		signals = append(signals, string(signal))
	}
	w := canonicalbytes.New("hcmnext.domains.crm.DeliveryBatch", 1).
		String("campaign", batch.CampaignDigest).
		Value("channel", batch.Channel).
		String("sent_at", instantString(batch.SentAt)).
		Count("records", len(batch.Records))
	for _, record := range batch.Records {
		w.String("delivery", record.DeliveryID).Value("subject", record.Subject).
			String("state", string(record.State)).Value("record_channel", record.Channel).
			String("suppress_reason", record.SuppressReason)
	}
	w.SortedStrings("approved_signal", signals)
	if batch.FallbackChannel != (values.EntityRef{}) {
		w.Value("fallback_channel", batch.FallbackChannel)
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Verify checks the seal and internal consistency of a previously delivered
// batch: delivery identities must be unique, states valid and the digest
// must match.
func (b DeliveryBatch) Verify() error {
	if b.CampaignDigest == "" || b.CanonicalDigest == "" || len(b.Records) == 0 {
		return ErrInvalidDelivery
	}
	seen := make(map[string]struct{}, len(b.Records))
	for _, record := range b.Records {
		if record.DeliveryID == "" {
			return ErrInvalidDelivery
		}
		if _, dup := seen[record.DeliveryID]; dup {
			return ErrInvalidDelivery
		}
		seen[record.DeliveryID] = struct{}{}
		switch record.State {
		case ContactSent, ContactDelivered, ContactEngaged, ContactBounced, ContactSuppressed, ContactFallbackQueued, ContactUnsubscribed:
		default:
			return ErrInvalidDelivery
		}
		if err := record.Subject.Validate(); err != nil {
			return ErrInvalidDelivery
		}
	}
	if deliveryDigest(b) != b.CanonicalDigest {
		return ErrInvalidDelivery
	}
	return nil
}
