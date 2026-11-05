// TestTodo_SMB_CORE_005 covers SMB-CORE-005: operational notices and
// customer communications bind domain triggers to purpose-bound message
// intents, signed field-restricted templates and consent-aware delivery.
// Duplicate triggers converge on one logical message, recipient
// authorization and contact validity are re-evaluated at send, provider
// acceptance is never labeled Read, and bounce/opt-out produce visible
// failure with a safe manual next action.
package messaging

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/delivery"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/idempotency"
)

type smbNoticeAttemptStore struct {
	mu      sync.Mutex
	attempt int
	claims  map[string]delivery.Claim
	replays map[string]int
}

func (s *smbNoticeAttemptStore) Claim(_ context.Context, envelope delivery.Envelope, attempt int) (delivery.Claim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claims == nil {
		s.claims = make(map[string]delivery.Claim)
		s.replays = make(map[string]int)
	}
	if prior, ok := s.claims[envelope.IdempotencyKey]; ok {
		s.replays[envelope.IdempotencyKey]++
		return delivery.Claim{AttemptID: prior.AttemptID, Attempt: prior.Attempt, AlreadyObserved: true}, nil
	}
	s.attempt++
	claim := delivery.Claim{AttemptID: "attempt-" + string(rune('0'+s.attempt)), Attempt: attempt}
	s.claims[envelope.IdempotencyKey] = claim
	return claim, nil
}

func (s *smbNoticeAttemptStore) Observe(_ context.Context, _ delivery.Envelope, _ delivery.Claim, _ delivery.Observation) error {
	return nil
}

type smbNoticeTransport struct {
	mu       sync.Mutex
	calls    int
	behavior func(delivery.Envelope) (delivery.ProviderResult, error)
	sent     []delivery.Envelope
}

func (t *smbNoticeTransport) Deliver(_ context.Context, envelope delivery.Envelope) (delivery.ProviderResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls++
	t.sent = append(t.sent, envelope)
	if t.behavior != nil {
		return t.behavior(envelope)
	}
	return delivery.ProviderResult{ProviderRef: "prov-1", ObservationRef: "obs-1", Code: "ACCEPTED"}, nil
}

func (t *smbNoticeTransport) invoked() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

type smbNoticeDirectory struct {
	mu         sync.Mutex
	recipients map[string]SMBNoticeRecipient
}

func (d *smbNoticeDirectory) Resolve(_ context.Context, tenantID, recipientRef string) (SMBNoticeRecipient, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	recipient, ok := d.recipients[tenantID+"\x00"+recipientRef]
	if !ok {
		return SMBNoticeRecipient{}, ErrSMBNoticeNotFound
	}
	return recipient, nil
}

func (d *smbNoticeDirectory) put(tenantID string, recipient SMBNoticeRecipient) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.recipients == nil {
		d.recipients = make(map[string]SMBNoticeRecipient)
	}
	d.recipients[tenantID+"\x00"+recipient.RecipientRef] = recipient
}

func smbNoticeFixture(at time.Time) (*SMBNoticeLedger, *smbNoticeTransport, *smbNoticeDirectory) {
	transport := &smbNoticeTransport{}
	directory := &smbNoticeDirectory{}
	ledger := NewSMBNoticeLedger(delivery.Runner{
		Store: &smbNoticeAttemptStore{}, Transport: transport,
		Idempotency: idempotency.NewRegistry(), Clock: func() time.Time { return at }, MaxAttempts: 1,
	}, directory)
	return ledger, transport, directory
}

func smbNoticeRegister(t *testing.T, ledger *SMBNoticeLedger) {
	t.Helper()
	if err := ledger.PublishTemplate(SMBNoticeTemplate{
		TemplateRef: "invoice-issued-email", Purpose: PurposeNotice,
		Channels: []delivery.Channel{delivery.ChannelEmail}, AllowedFields: []string{"invoice.number", "invoice.total", "invoice.due_date"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.RegisterTrigger("invoice-issued", "invoice-issued-email", PurposeNotice, RequirementSubmitted); err != nil {
		t.Fatal(err)
	}
}

func smbNoticeTrigger(tenant, kind, event string, at time.Time) SMBNoticeTrigger {
	return SMBNoticeTrigger{
		TenantID: tenant, EventKind: kind, EventID: event, RecipientRef: "cust-1",
		AggregateRef: "invoice:inv-1", AggregateRevision: 3, Actor: "billing", At: at,
	}
}

func TestTodo_SMB_CORE_005(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	ctx := context.Background()
	ledger, transport, directory := smbNoticeFixture(at)
	smbNoticeRegister(t, ledger)
	directory.put("tenant-a", SMBNoticeRecipient{RecipientRef: "cust-1", EndpointRef: "ep-1", ContactValid: true, Locale: "en-US"})

	first, err := ledger.TriggerEvent(ctx, smbNoticeTrigger("tenant-a", "invoice-issued", "evt-1", at), "trigger:evt-1")
	if err != nil || first.State != SMBNoticeQueued {
		t.Fatalf("first trigger = %+v, err=%v", first, err)
	}
	replay, err := ledger.TriggerEvent(ctx, smbNoticeTrigger("tenant-a", "invoice-issued", "evt-1", at.Add(time.Minute)), "trigger:evt-1")
	if err != nil || replay.IntentID != first.IntentID {
		t.Fatalf("duplicate trigger = %+v, err=%v; want the one logical message", replay, err)
	}
	if got := ledger.MessageCount("tenant-a"); got != 1 {
		t.Fatalf("message count = %d, want one logical message for duplicate events", got)
	}
	clash := smbNoticeTrigger("tenant-a", "invoice-issued", "evt-2", at)
	if _, err := ledger.TriggerEvent(ctx, clash, "trigger:evt-1"); !errors.Is(err, ErrSMBNoticeConflict) {
		t.Fatalf("reused trigger key err=%v, want conflict", err)
	}

	sent, err := ledger.Send(ctx, "tenant-a", first.IntentID, "cust-1", delivery.ChannelEmail, at.Add(2*time.Minute))
	if err != nil || sent.State != SMBNoticeSent || transport.invoked() != 1 {
		t.Fatalf("send = %+v, err=%v, transport calls=%d", sent, err, transport.invoked())
	}
	accepted, err := ledger.ObserveProvider(ctx, "tenant-a", first.IntentID, SMBProviderObservation{Kind: SMBObservationAccepted, ProviderRef: "prov-1", At: at.Add(3 * time.Minute)})
	if err != nil || accepted.State != SMBNoticeSent {
		t.Fatalf("provider acceptance = %+v, err=%v; want Sent, never Read", accepted, err)
	}
	if strings.Contains(string(accepted.State), "read") || strings.Contains(string(accepted.State), "Read") {
		t.Fatalf("provider acceptance labeled %q, want Sent", accepted.State)
	}

	if err := ledger.PublishTemplate(SMBNoticeTemplate{
		TemplateRef: "payroll-sneak", Purpose: PurposeNotice,
		Channels: []delivery.Channel{delivery.ChannelEmail}, AllowedFields: []string{"payroll.net_pay", "invoice.number"},
	}); !errors.Is(err, ErrSMBNoticeForbidden) {
		t.Fatalf("payroll-field template err=%v, want forbidden-field refusal", err)
	}
	if err := ledger.PublishTemplate(SMBNoticeTemplate{
		TemplateRef: "note-sneak", Purpose: PurposeNotice,
		Channels: []delivery.Channel{delivery.ChannelEmail}, AllowedFields: []string{"private-note.body"},
	}); !errors.Is(err, ErrSMBNoticeForbidden) {
		t.Fatalf("private-note template err=%v, want forbidden-field refusal", err)
	}
}

func TestTodo_SMB_CORE_005_Security(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	ctx := context.Background()
	ledger, transport, directory := smbNoticeFixture(at)
	smbNoticeRegister(t, ledger)
	directory.put("tenant-a", SMBNoticeRecipient{RecipientRef: "cust-1", EndpointRef: "ep-1", ContactValid: true, Locale: "en-US"})

	first, err := ledger.TriggerEvent(ctx, smbNoticeTrigger("tenant-a", "invoice-issued", "evt-1", at), "trigger:evt-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, visible := ledger.View("tenant-b", first.IntentID); visible {
		t.Fatal("cross-tenant message view is visible, want not-found")
	}
	if _, err := ledger.Send(ctx, "tenant-b", first.IntentID, "cust-1", delivery.ChannelEmail, at); !errors.Is(err, ErrSMBNoticeNotFound) {
		t.Fatalf("cross-tenant send err=%v, want neutral not-found", err)
	}
	if _, err := ledger.Send(ctx, "tenant-a", first.IntentID, "ghost", delivery.ChannelEmail, at); !errors.Is(err, ErrSMBNoticeNotFound) {
		t.Fatalf("forged recipient send err=%v, want neutral not-found", err)
	}

	// Access revoked after queueing: send is blocked, private attachment and
	// link are omitted, and no contact identity leaks to a denied observer.
	directory.put("tenant-a", SMBNoticeRecipient{RecipientRef: "cust-1", EndpointRef: "ep-1", ContactValid: true, AccessRevoked: true, Locale: "en-US"})
	blocked, err := ledger.Send(ctx, "tenant-a", first.IntentID, "cust-1", delivery.ChannelEmail, at.Add(time.Minute))
	if !errors.Is(err, ErrSMBNoticeForbidden) || blocked.State != SMBNoticeBlocked {
		t.Fatalf("revoked-access send = %+v, err=%v; want blocked", blocked, err)
	}
	if transport.invoked() != 0 {
		t.Fatalf("revoked-access send reached the provider %d times, want zero", transport.invoked())
	}
	view, _ := ledger.View("tenant-a", first.IntentID)
	if view.AttachmentRef != "" || view.ActionLink != "" {
		t.Fatalf("blocked view exposes attachment/link: %+v", view)
	}
	if strings.Contains(view.RecipientSafe, "cust-1") {
		t.Fatalf("blocked view leaks recipient identity: %q", view.RecipientSafe)
	}

	directory.put("tenant-a", SMBNoticeRecipient{RecipientRef: "cust-1", EndpointRef: "ep-1", ContactValid: false, Locale: "en-US"})
	if _, err := ledger.Send(ctx, "tenant-a", first.IntentID, "cust-1", delivery.ChannelEmail, at.Add(2*time.Minute)); !errors.Is(err, ErrSMBNoticeForbidden) {
		t.Fatalf("invalid-contact send err=%v, want forbidden", err)
	}
	if transport.invoked() != 0 {
		t.Fatalf("invalid-contact send reached the provider %d times, want zero", transport.invoked())
	}
}

func TestTodo_SMB_CORE_005_Fault(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	ctx := context.Background()
	ledger, transport, directory := smbNoticeFixture(at)
	smbNoticeRegister(t, ledger)
	directory.put("tenant-a", SMBNoticeRecipient{RecipientRef: "cust-1", EndpointRef: "ep-1", ContactValid: true, Locale: "en-US"})
	first, err := ledger.TriggerEvent(ctx, smbNoticeTrigger("tenant-a", "invoice-issued", "evt-1", at), "trigger:evt-1")
	if err != nil {
		t.Fatal(err)
	}

	// Provider timeout is ambiguous: the message needs review, and the late
	// receipt converges without a second provider send.
	transport.behavior = func(delivery.Envelope) (delivery.ProviderResult, error) {
		return delivery.ProviderResult{}, &delivery.ProviderError{Err: errors.New("provider timeout"), Ambiguous: true}
	}
	timedOut, err := ledger.Send(ctx, "tenant-a", first.IntentID, "cust-1", delivery.ChannelEmail, at.Add(time.Minute))
	if err == nil || timedOut.State != SMBNoticeReview {
		t.Fatalf("timeout send = %+v, err=%v; want review-required", timedOut, err)
	}
	transport.behavior = nil
	converged, err := ledger.ObserveProvider(ctx, "tenant-a", first.IntentID, SMBProviderObservation{Kind: SMBObservationAccepted, ProviderRef: "prov-1", At: at.Add(2 * time.Minute)})
	if err != nil || converged.State != SMBNoticeSent {
		t.Fatalf("late receipt = %+v, err=%v; want Sent", converged, err)
	}
	if transport.invoked() != 1 {
		t.Fatalf("late receipt caused %d provider sends, want exactly one", transport.invoked())
	}
	retry, err := ledger.Send(ctx, "tenant-a", first.IntentID, "cust-1", delivery.ChannelEmail, at.Add(3*time.Minute))
	if err != nil || retry.State != SMBNoticeSent || transport.invoked() != 1 {
		t.Fatalf("post-receipt retry = %+v, err=%v, calls=%d; want Sent with no second send", retry, err, transport.invoked())
	}
}

func TestTodo_SMB_CORE_005_Recovery(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	ctx := context.Background()
	ledger, _, directory := smbNoticeFixture(at)
	smbNoticeRegister(t, ledger)
	directory.put("tenant-a", SMBNoticeRecipient{RecipientRef: "cust-1", EndpointRef: "ep-1", ContactValid: true, Locale: "en-US"})
	first, err := ledger.TriggerEvent(ctx, smbNoticeTrigger("tenant-a", "invoice-issued", "evt-1", at), "trigger:evt-1")
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := ledger.Export()
	if err != nil {
		t.Fatal(err)
	}
	restored, _, _ := smbNoticeFixture(at)
	if err := restored.Import(snapshot); err != nil {
		t.Fatal(err)
	}
	replay, err := restored.TriggerEvent(ctx, smbNoticeTrigger("tenant-a", "invoice-issued", "evt-1", at.Add(time.Minute)), "trigger:evt-1")
	if err != nil || replay.IntentID != first.IntentID {
		t.Fatalf("post-restore replay = %+v, err=%v; want the one logical message", replay, err)
	}
	if got := restored.MessageCount("tenant-a"); got != 1 {
		t.Fatalf("post-restore message count = %d, want one", got)
	}
	if err := restored.Import([]byte("{not-json")); err == nil {
		t.Fatal("corrupt snapshot import succeeded, want refusal")
	}
}

func TestTodo_SMB_CORE_005_Browser(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	ctx := context.Background()
	ledger, _, directory := smbNoticeFixture(at)
	if err := ledger.PublishTemplate(SMBNoticeTemplate{
		TemplateRef: "invoice-issued-sms", Purpose: PurposeNotice,
		Channels: []delivery.Channel{delivery.ChannelSMS}, AllowedFields: []string{"invoice.number", "invoice.total"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.RegisterTrigger("invoice-issued", "invoice-issued-sms", PurposeNotice, RequirementSubmitted); err != nil {
		t.Fatal(err)
	}
	directory.put("tenant-a", SMBNoticeRecipient{RecipientRef: "cust-1", EndpointRef: "ep-1", ContactValid: true, SMSOptedOut: true, Locale: "de-DE"})

	first, err := ledger.TriggerEvent(ctx, smbNoticeTrigger("tenant-a", "invoice-issued", "evt-1", at), "trigger:evt-1")
	if err != nil {
		t.Fatal(err)
	}
	queued, _ := ledger.View("tenant-a", first.IntentID)
	if queued.State != SMBNoticeQueued || !containsSMBString(queued.Actions, "Send now") || queued.Language != "de-DE" {
		t.Fatalf("queued view = %+v, want queued with Send now in the recipient locale", queued)
	}
	optedOut, err := ledger.Send(ctx, "tenant-a", first.IntentID, "cust-1", delivery.ChannelSMS, at.Add(time.Minute))
	if !errors.Is(err, ErrSMBNoticeConsent) || optedOut.State != SMBNoticeBlocked {
		t.Fatalf("opted-out send = %+v, err=%v; want consent-blocked", optedOut, err)
	}
	blocked, _ := ledger.View("tenant-a", first.IntentID)
	if blocked.NextAction == "" || strings.Contains(blocked.NextAction, "cust-1") {
		t.Fatalf("blocked view = %+v, want a safe manual next action without identity", blocked)
	}

	preview, err := ledger.PreviewTemplate("invoice-issued-sms", map[string]string{"invoice.number": "INV-9", "payroll.net_pay": "1"})
	if err == nil || preview.Ref != "" {
		t.Fatalf("forbidden-field preview = %+v, err=%v; want refusal", preview, err)
	}
	preview, err = ledger.PreviewTemplate("invoice-issued-sms", map[string]string{"invoice.number": "INV-9"})
	if err != nil || preview.Ref != "invoice-issued-sms" || len(preview.Tokens) != 2 {
		t.Fatalf("safe preview = %+v, err=%v; want the allowed tokens", preview, err)
	}

	bounced, err := ledger.ObserveProvider(ctx, "tenant-a", first.IntentID, SMBProviderObservation{Kind: SMBObservationBounced, ProviderRef: "prov-1", At: at.Add(2 * time.Minute)})
	if err != nil || bounced.State != SMBNoticeFailed || bounced.NextAction == "" {
		t.Fatalf("bounce observation = %+v, err=%v; want failed with a manual next action", bounced, err)
	}
}
