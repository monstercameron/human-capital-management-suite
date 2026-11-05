// Operational notice binding for SMB-CORE-005.
//
// Domain triggers (quote, appointment, job, invoice, PO events) converge on
// one purpose-bound message intent per logical event. Templates publish only
// allowlisted, non-sensitive fields; recipient authorization, contact
// validity and channel consent are re-evaluated at every send; and delivery
// runs through the existing provider-neutral delivery runner, so provider
// acceptance is observed as Sent or Delivery pending — never Read — while
// bounce and opt-out surface visible failure with a safe manual next action.
//
// Event kinds here are test-only fixtures. Production BILL/SALE/JOB/SCHED
// owners register their own triggers against these same contracts; this
// ledger never invents their domain events.
package messaging

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/delivery"
)

// Stable notice-binding refusal causes.
var (
	ErrSMBNoticeInvalid   = errors.New("messaging: invalid notice request")
	ErrSMBNoticeNotFound  = errors.New("messaging: notice is not visible")
	ErrSMBNoticeForbidden = errors.New("messaging: notice send is forbidden")
	ErrSMBNoticeConflict  = errors.New("messaging: notice idempotency key was reused for another event")
	ErrSMBNoticeConsent   = errors.New("messaging: recipient channel consent is not present")
	ErrSMBNoticeSnapshot  = errors.New("messaging: notice snapshot is invalid")
)

// SMBNoticeState is the served message lifecycle. There is deliberately no
// Read state: provider acceptance proves submission, never readership.
type SMBNoticeState string

const (
	SMBNoticeQueued  SMBNoticeState = "queued"
	SMBNoticeSent    SMBNoticeState = "sent"
	SMBNoticeFailed  SMBNoticeState = "failed"
	SMBNoticeBlocked SMBNoticeState = "blocked"
	SMBNoticeReview  SMBNoticeState = "review"
)

// SMB provider observation kinds.
type SMBObservationKind string

const (
	SMBObservationAccepted SMBObservationKind = "accepted"
	SMBObservationBounced  SMBObservationKind = "bounced"
	SMBObservationOptedOut SMBObservationKind = "opted-out"
)

// SMBNoticeTemplate is one published, field-restricted template.
type SMBNoticeTemplate struct {
	TemplateRef   string
	Purpose       Purpose
	Channels      []delivery.Channel
	AllowedFields []string
}

// SMBNoticeTrigger is one domain event delivery request.
type SMBNoticeTrigger struct {
	TenantID          string
	EventKind         string
	EventID           string
	RecipientRef      string
	AggregateRef      string
	AggregateRevision int64
	Actor             string
	At                time.Time
}

// SMBNoticeRecipient is the directory's authorized recipient projection.
// Identity stays behind the directory; views render only safe summaries.
type SMBNoticeRecipient struct {
	RecipientRef  string
	EndpointRef   string
	ContactValid  bool
	AccessRevoked bool
	EmailOptedOut bool
	SMSOptedOut   bool
	Locale        string
}

// SMBNoticeDirectory resolves recipients. Production binds the CRM/portal
// owners; tests bind an explicit fixture map.
type SMBNoticeDirectory interface {
	Resolve(ctx context.Context, tenantID, recipientRef string) (SMBNoticeRecipient, error)
}

// SMBProviderObservation is one provider-side delivery signal.
type SMBProviderObservation struct {
	Kind        SMBObservationKind
	ProviderRef string
	At          time.Time
}

// SMBNoticeMessage is one logical message: exactly one accepted intent per
// deduplicated domain event, with its delivery lifecycle.
type SMBNoticeMessage struct {
	IntentID     string
	TenantID     string
	EventKind    string
	EventID      string
	RecipientRef string
	TemplateRef  string
	Channel      delivery.Channel
	State        SMBNoticeState
	ProviderRef  string
	NextAction   string
	Intent       MessageIntent
}

// SMBNoticeView is the served message state with a recipient-safe summary.
type SMBNoticeView struct {
	IntentID      string
	State         SMBNoticeState
	Actions       []string
	Language      string
	NextAction    string
	AttachmentRef string
	ActionLink    string
	RecipientSafe string
}

// SMBTemplatePreview is the allowed-token preview for one template.
type SMBTemplatePreview struct {
	Ref    string
	Tokens []string
}

type smbTriggerBinding struct {
	TemplateRef string
	Purpose     Purpose
	Requirement DeliveryRequirement
}

type smbNoticeRecord struct {
	Message   SMBNoticeMessage
	Intent    MessageIntent
	Recipient SMBNoticeRecipient
}

type smbNoticeEffect struct {
	IntentID string
	EventKey string
}

// smbForbiddenFieldPrefixes can never appear in a published template or
// preview: payroll, identity and private-note content stays out of
// operational communications.
var smbForbiddenFieldPrefixes = []string{"payroll.", "salary", "ssn", "private-note", "private_note", "tax-id"}

// SMBNoticeLedger binds triggers to intents, templates and delivery.
type SMBNoticeLedger struct {
	mu        sync.Mutex
	runner    delivery.Runner
	directory SMBNoticeDirectory
	triggers  map[string]smbTriggerBinding
	templates map[string]SMBNoticeTemplate
	messages  map[string]*smbNoticeRecord
	byEvent   map[string]string
	effects   map[string]smbNoticeEffect
}

// NewSMBNoticeLedger builds the binding over a delivery runner and a
// recipient directory.
func NewSMBNoticeLedger(runner delivery.Runner, directory SMBNoticeDirectory) *SMBNoticeLedger {
	return &SMBNoticeLedger{
		runner: runner, directory: directory,
		triggers: make(map[string]smbTriggerBinding), templates: make(map[string]SMBNoticeTemplate),
		messages: make(map[string]*smbNoticeRecord), byEvent: make(map[string]string),
		effects: make(map[string]smbNoticeEffect),
	}
}

// RegisterTrigger binds one event kind to a published template and purpose.
func (l *SMBNoticeLedger) RegisterTrigger(eventKind, templateRef string, purpose Purpose, requirement DeliveryRequirement) error {
	if strings.TrimSpace(eventKind) == "" || strings.TrimSpace(templateRef) == "" {
		return fmt.Errorf("%w: event kind and template ref are required", ErrSMBNoticeInvalid)
	}
	if !purpose.valid() || !requirement.valid() {
		return fmt.Errorf("%w: unsupported purpose or requirement", ErrSMBNoticeInvalid)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.templates[templateRef]; !ok {
		return fmt.Errorf("%w: template %q is not published", ErrSMBNoticeInvalid, templateRef)
	}
	l.triggers[eventKind] = smbTriggerBinding{TemplateRef: templateRef, Purpose: purpose, Requirement: requirement}
	return nil
}

// PublishTemplate publishes one field-restricted template. Forbidden fields
// fail publication and can never reach a provider.
func (l *SMBNoticeLedger) PublishTemplate(template SMBNoticeTemplate) error {
	if strings.TrimSpace(template.TemplateRef) == "" || !template.Purpose.valid() || len(template.Channels) == 0 || len(template.AllowedFields) == 0 {
		return fmt.Errorf("%w: template ref, purpose, channels and allowed fields are required", ErrSMBNoticeInvalid)
	}
	for _, channel := range template.Channels {
		switch channel {
		case delivery.ChannelEmail, delivery.ChannelSMS, delivery.ChannelPush, delivery.ChannelInbox:
		default:
			return fmt.Errorf("%w: unsupported channel %q", ErrSMBNoticeInvalid, channel)
		}
	}
	for _, field := range template.AllowedFields {
		if smbForbiddenField(field) {
			return fmt.Errorf("%w: field %q must never leave its owning domain", ErrSMBNoticeForbidden, field)
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.templates[template.TemplateRef] = template
	return nil
}

// PreviewTemplate renders the allowed-token preview. Any requested field
// outside the allowlist — or on the forbidden list — refuses the preview.
func (l *SMBNoticeLedger) PreviewTemplate(templateRef string, fields map[string]string) (SMBTemplatePreview, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	template, ok := l.templates[templateRef]
	if !ok {
		return SMBTemplatePreview{}, fmt.Errorf("%w: template %q is not published", ErrSMBNoticeNotFound, templateRef)
	}
	for field := range fields {
		if smbForbiddenField(field) || !containsSMBString(template.AllowedFields, field) {
			return SMBTemplatePreview{}, fmt.Errorf("%w: field %q is not previewable", ErrSMBNoticeForbidden, field)
		}
	}
	return SMBTemplatePreview{Ref: templateRef, Tokens: append([]string(nil), template.AllowedFields...)}, nil
}

func smbNoticeIntentID(tenant, kind, event string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("smb-notice\x00"+tenant+"\x00"+kind+"\x00"+event)).String()
}

func smbNoticeEventKey(tenant, kind, event string) string {
	return tenant + "\x00" + kind + "\x00" + event
}

// TriggerEvent converges one domain event on one logical message. The same
// idempotency key replays the recorded intent; reuse on another event
// conflicts before any effect.
func (l *SMBNoticeLedger) TriggerEvent(ctx context.Context, trigger SMBNoticeTrigger, idempotencyKey string) (SMBNoticeMessage, error) {
	if strings.TrimSpace(trigger.TenantID) == "" || strings.TrimSpace(trigger.EventKind) == "" || strings.TrimSpace(trigger.EventID) == "" || strings.TrimSpace(trigger.RecipientRef) == "" || strings.TrimSpace(idempotencyKey) == "" || trigger.At.IsZero() {
		return SMBNoticeMessage{}, fmt.Errorf("%w: tenant, kind, event, recipient, key and time are required", ErrSMBNoticeInvalid)
	}
	eventKey := smbNoticeEventKey(trigger.TenantID, trigger.EventKind, trigger.EventID)
	l.mu.Lock()
	defer l.mu.Unlock()
	if prior, ok := l.effects[idempotencyKey]; ok {
		if prior.EventKey != eventKey {
			return SMBNoticeMessage{}, fmt.Errorf("%w: key was reused for another event", ErrSMBNoticeConflict)
		}
		record, ok := l.messages[prior.IntentID]
		if !ok || record.Message.TenantID != trigger.TenantID {
			return SMBNoticeMessage{}, ErrSMBNoticeNotFound
		}
		return record.Message, nil
	}
	binding, ok := l.triggers[trigger.EventKind]
	if !ok {
		return SMBNoticeMessage{}, fmt.Errorf("%w: event kind %q is not registered", ErrSMBNoticeInvalid, trigger.EventKind)
	}
	template, ok := l.templates[binding.TemplateRef]
	if !ok {
		return SMBNoticeMessage{}, fmt.Errorf("%w: template %q is not published", ErrSMBNoticeInvalid, binding.TemplateRef)
	}
	intentID := smbNoticeIntentID(trigger.TenantID, trigger.EventKind, trigger.EventID)
	available := trigger.At.UTC()
	intent, err := Accept(MessageIntent{
		IntentID: intentID, TenantID: trigger.TenantID, OrganizationScope: trigger.TenantID,
		Purpose: binding.Purpose, AudienceExpression: "recipient:" + trigger.RecipientRef, AudienceResolutionPolicy: "exact-recipient",
		TemplateRef: template.TemplateRef, ParametersRef: "params:" + trigger.EventID,
		Classification: "OPERATIONAL", Urgency: "NORMAL", DeliveryRequirement: binding.Requirement, ReplyMode: ReplyNotAllowed,
		BusinessTransactionID: trigger.AggregateRef, CorrelationID: trigger.EventID,
		AvailableAt: &available, ExpiresAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		return SMBNoticeMessage{}, err
	}
	record := &smbNoticeRecord{
		Message: SMBNoticeMessage{
			IntentID: intentID, TenantID: trigger.TenantID, EventKind: trigger.EventKind, EventID: trigger.EventID,
			RecipientRef: trigger.RecipientRef, TemplateRef: template.TemplateRef, State: SMBNoticeQueued, Intent: intent,
		},
		Intent: intent,
	}
	if l.directory != nil {
		if recipient, err := l.directory.Resolve(ctx, trigger.TenantID, trigger.RecipientRef); err == nil {
			record.Recipient = recipient
		}
	}
	l.messages[intentID] = record
	l.byEvent[eventKey] = intentID
	l.effects[idempotencyKey] = smbNoticeEffect{IntentID: intentID, EventKey: eventKey}
	return record.Message, nil
}

// MessageCount counts logical messages for one tenant.
func (l *SMBNoticeLedger) MessageCount(tenantID string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	count := 0
	for _, record := range l.messages {
		if record.Message.TenantID == tenantID {
			count++
		}
	}
	return count
}

// Send re-evaluates recipient authorization, contact validity and channel
// consent at send time, then delivers through the existing runner. A sent
// message replays without a second provider effect.
func (l *SMBNoticeLedger) Send(ctx context.Context, tenantID, intentID, recipientRef string, channel delivery.Channel, at time.Time) (SMBNoticeMessage, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(intentID) == "" || strings.TrimSpace(recipientRef) == "" || at.IsZero() {
		return SMBNoticeMessage{}, fmt.Errorf("%w: tenant, intent, recipient and time are required", ErrSMBNoticeInvalid)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	record, ok := l.messages[intentID]
	if !ok || record.Message.TenantID != tenantID {
		return SMBNoticeMessage{}, ErrSMBNoticeNotFound
	}
	if record.Message.State == SMBNoticeSent {
		return record.Message, nil
	}
	if l.directory == nil {
		return SMBNoticeMessage{}, fmt.Errorf("%w: no recipient directory is configured", ErrSMBNoticeInvalid)
	}
	recipient, err := l.directory.Resolve(ctx, tenantID, recipientRef)
	if err != nil {
		return SMBNoticeMessage{}, ErrSMBNoticeNotFound
	}
	template, ok := l.templates[record.Message.TemplateRef]
	if !ok {
		return SMBNoticeMessage{}, fmt.Errorf("%w: template %q is not published", ErrSMBNoticeInvalid, record.Message.TemplateRef)
	}
	admitted := false
	for _, allowed := range template.Channels {
		if allowed == channel {
			admitted = true
		}
	}
	if !admitted {
		l.blockLocked(record, recipient, "channel is not admitted for this template")
		return record.Message, fmt.Errorf("%w: channel %q is not admitted for template %q", ErrSMBNoticeForbidden, channel, template.TemplateRef)
	}
	record.Recipient = recipient
	record.Message.Channel = channel
	if recipient.AccessRevoked {
		l.blockLocked(record, recipient, "recipient access was revoked; verify representation before resending")
		return record.Message, fmt.Errorf("%w: recipient access was revoked", ErrSMBNoticeForbidden)
	}
	if !recipient.ContactValid {
		l.blockLocked(record, recipient, "recipient contact is invalid; verify the address before resending")
		return record.Message, fmt.Errorf("%w: recipient contact is invalid", ErrSMBNoticeForbidden)
	}
	if channel == delivery.ChannelSMS && recipient.SMSOptedOut {
		l.blockLocked(record, recipient, "SMS consent is not present; ask the customer through an opted-in channel to re-confirm")
		return record.Message, fmt.Errorf("%w: SMS consent is not present", ErrSMBNoticeConsent)
	}
	if channel == delivery.ChannelEmail && recipient.EmailOptedOut {
		l.blockLocked(record, recipient, "email consent is not present; ask the customer through an opted-in channel to re-confirm")
		return record.Message, fmt.Errorf("%w: email consent is not present", ErrSMBNoticeConsent)
	}
	digest := sha256.Sum256([]byte(intentID + "\x00" + recipientRef + "\x00" + string(channel)))
	envelope := delivery.Envelope{
		TenantID: tenantID, IntentID: intentID, RecipientRef: recipientRef, EndpointRef: recipient.EndpointRef,
		TemplateRef: template.TemplateRef, ParametersRef: "params:" + record.Message.EventID,
		Purpose: string(record.Intent.Purpose), Classification: record.Intent.Classification, Channel: channel,
		IdempotencyKey: "send:" + intentID + ":" + recipientRef + ":" + string(channel),
		CorrelationID:  record.Message.EventID, ContentDigest: hex.EncodeToString(digest[:]),
		AvailableAt: at.UTC(), ExpiresAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	observation, err := l.runner.Deliver(ctx, envelope)
	if err != nil {
		if observation.State == delivery.StateReviewRequired {
			record.Message.State = SMBNoticeReview
			record.Message.NextAction = "Provider response was ambiguous; wait for the provider receipt instead of resending."
			return record.Message, fmt.Errorf("messaging: provider response ambiguous: %w", err)
		}
		record.Message.State = SMBNoticeFailed
		record.Message.NextAction = "Review the failure and retry explicitly; the draft is retained."
		return record.Message, err
	}
	if observation.State == delivery.StateAlreadyObserved && observation.ProviderRef != "" {
		record.Message.ProviderRef = observation.ProviderRef
	}
	if observation.ProviderRef != "" {
		record.Message.ProviderRef = observation.ProviderRef
	}
	record.Message.State = SMBNoticeSent
	return record.Message, nil
}

func (l *SMBNoticeLedger) blockLocked(record *smbNoticeRecord, recipient SMBNoticeRecipient, action string) {
	record.Recipient = recipient
	record.Message.State = SMBNoticeBlocked
	record.Message.NextAction = action
}

// ObserveProvider records one provider-side signal. Acceptance proves
// submission — the message is Sent, never Read. Bounce and opt-out are
// visible failures with a safe manual next action.
func (l *SMBNoticeLedger) ObserveProvider(_ context.Context, tenantID, intentID string, observation SMBProviderObservation) (SMBNoticeMessage, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(intentID) == "" || observation.At.IsZero() {
		return SMBNoticeMessage{}, fmt.Errorf("%w: tenant, intent and time are required", ErrSMBNoticeInvalid)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	record, ok := l.messages[intentID]
	if !ok || record.Message.TenantID != tenantID {
		return SMBNoticeMessage{}, ErrSMBNoticeNotFound
	}
	switch observation.Kind {
	case SMBObservationAccepted:
		if observation.ProviderRef != "" {
			record.Message.ProviderRef = observation.ProviderRef
		}
		record.Message.State = SMBNoticeSent
	case SMBObservationBounced:
		record.Message.State = SMBNoticeFailed
		record.Message.NextAction = "The address bounced; verify it with the customer through another channel and retry explicitly."
	case SMBObservationOptedOut:
		record.Message.State = SMBNoticeBlocked
		record.Message.NextAction = "Consent was withdrawn; ask the customer through an opted-in channel to re-confirm."
	default:
		return SMBNoticeMessage{}, fmt.Errorf("%w: unknown observation kind %q", ErrSMBNoticeInvalid, observation.Kind)
	}
	return record.Message, nil
}

// View serves the message state. The recipient summary never carries
// identity, attachments or links a denied observer could reuse.
func (l *SMBNoticeLedger) View(tenantID, intentID string) (SMBNoticeView, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	record, ok := l.messages[intentID]
	if !ok || record.Message.TenantID != tenantID {
		return SMBNoticeView{}, false
	}
	view := SMBNoticeView{
		IntentID: record.Message.IntentID, State: record.Message.State,
		Language: record.Recipient.Locale, NextAction: record.Message.NextAction,
	}
	switch record.Message.State {
	case SMBNoticeQueued:
		view.Actions = []string{"Send now"}
		view.RecipientSafe = "queued for delivery"
	case SMBNoticeSent:
		view.Actions = []string{"View delivery"}
		view.AttachmentRef = "attach:" + record.Message.IntentID
		view.ActionLink = "link:" + record.Message.IntentID
		view.RecipientSafe = "sent; provider acceptance is submission, not readership"
	case SMBNoticeFailed:
		view.Actions = []string{"Retry explicitly"}
		view.RecipientSafe = "delivery failed"
	case SMBNoticeBlocked:
		view.Actions = []string{"Review again"}
		view.RecipientSafe = "delivery blocked"
	case SMBNoticeReview:
		view.Actions = []string{"Resolve receipt"}
		view.RecipientSafe = "provider response ambiguous"
	}
	return view, true
}

type smbNoticeSnapshot struct {
	Version   int                          `json:"version"`
	Triggers  map[string]smbTriggerBinding `json:"triggers"`
	Templates map[string]SMBNoticeTemplate `json:"templates"`
	Messages  map[string]smbNoticeRecord   `json:"messages"`
	ByEvent   map[string]string            `json:"by_event"`
	Effects   map[string]smbNoticeEffect   `json:"effects"`
}

// Export captures the ledger for crash recovery.
func (l *SMBNoticeLedger) Export() ([]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	snapshot := smbNoticeSnapshot{
		Version:   1,
		Triggers:  make(map[string]smbTriggerBinding, len(l.triggers)),
		Templates: make(map[string]SMBNoticeTemplate, len(l.templates)),
		Messages:  make(map[string]smbNoticeRecord, len(l.messages)),
		ByEvent:   make(map[string]string, len(l.byEvent)),
		Effects:   make(map[string]smbNoticeEffect, len(l.effects)),
	}
	for k, v := range l.triggers {
		snapshot.Triggers[k] = v
	}
	for k, v := range l.templates {
		v.Channels = append([]delivery.Channel(nil), v.Channels...)
		v.AllowedFields = append([]string(nil), v.AllowedFields...)
		snapshot.Templates[k] = v
	}
	for k, v := range l.messages {
		snapshot.Messages[k] = *v
	}
	for k, v := range l.byEvent {
		snapshot.ByEvent[k] = v
	}
	for k, v := range l.effects {
		snapshot.Effects[k] = v
	}
	return json.Marshal(snapshot)
}

// Import restores ledger state. Corrupt snapshots are refused without
// touching live state.
func (l *SMBNoticeLedger) Import(data []byte) error {
	var decoded smbNoticeSnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("%w: %v", ErrSMBNoticeSnapshot, err)
	}
	if decoded.Version != 1 {
		return fmt.Errorf("%w: unsupported version %d", ErrSMBNoticeSnapshot, decoded.Version)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.triggers = make(map[string]smbTriggerBinding, len(decoded.Triggers))
	for k, v := range decoded.Triggers {
		l.triggers[k] = v
	}
	l.templates = make(map[string]SMBNoticeTemplate, len(decoded.Templates))
	for k, v := range decoded.Templates {
		l.templates[k] = v
	}
	l.messages = make(map[string]*smbNoticeRecord, len(decoded.Messages))
	for k, v := range decoded.Messages {
		kept := v
		l.messages[k] = &kept
	}
	l.byEvent = make(map[string]string, len(decoded.ByEvent))
	for k, v := range decoded.ByEvent {
		l.byEvent[k] = v
	}
	l.effects = make(map[string]smbNoticeEffect, len(decoded.Effects))
	for k, v := range decoded.Effects {
		l.effects[k] = v
	}
	return nil
}

func smbForbiddenField(field string) bool {
	for _, prefix := range smbForbiddenFieldPrefixes {
		if field == prefix || strings.HasPrefix(field, prefix) {
			return true
		}
	}
	return false
}

func containsSMBString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
