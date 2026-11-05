package delivery_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/delivery"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/messaging"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/idempotency"
)

type smbCore005Transport struct {
	mu    sync.Mutex
	calls int
	fail  bool
}

func (t *smbCore005Transport) Deliver(_ context.Context, _ delivery.Envelope) (delivery.ProviderResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls++
	if t.fail {
		return delivery.ProviderResult{}, errors.New("provider: mailbox unavailable")
	}
	return delivery.ProviderResult{ProviderRef: "prov-9", ObservationRef: "obs-9", Code: "ACCEPTED"}, nil
}

func (t *smbCore005Transport) invoked() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

type smbCore005Directory struct {
	mu         sync.Mutex
	recipients map[string]messaging.SMBNoticeRecipient
}

func (d *smbCore005Directory) Resolve(_ context.Context, tenantID, recipientRef string) (messaging.SMBNoticeRecipient, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	recipient, ok := d.recipients[tenantID+"\x00"+recipientRef]
	if !ok {
		return messaging.SMBNoticeRecipient{}, messaging.ErrSMBNoticeNotFound
	}
	return recipient, nil
}

func smbCore005Seed(t *testing.T, db *pgtest.DB, tenant, intent, endpoint uuid.UUID, at time.Time) {
	t.Helper()
	db.Exec(t, "INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',$4) ON CONFLICT DO NOTHING", tenant, "notice-"+tenant.String(), "notice", at.Add(-time.Hour))
	db.Exec(t, "INSERT INTO delivery_endpoint (tenant_id, endpoint_id, principal_ref, channel, address_digest, ownership, verification_state, effective_from) VALUES ($1,$2,'cust-1','EMAIL',$3,'BUSINESS','VERIFIED',$4) ON CONFLICT DO NOTHING", tenant, endpoint, strings.Repeat("a", 64), at.Add(-time.Hour))
	db.Exec(t, "INSERT INTO message_intent (tenant_id, message_intent_id, purpose, audience_expression, template_key, template_version, classification, urgency, delivery_requirement, workflow_ref, correlation_key, created_at) VALUES ($1,$2,'NOTICE','{}'::jsonb,'invoice-issued-email',1,'OPERATIONAL','NORMAL','BEST_EFFORT','invoice:inv-1',$3,$4)", tenant, intent, "evt-1", at)
	db.Exec(t, "INSERT INTO recipient_message (tenant_id, recipient_message_id, message_intent_id, recipient_ref, endpoint_id, rendered_digest, classification, correlation_key, created_at, updated_at) VALUES ($1,$2,$3,'cust-1',$4,$5,'OPERATIONAL',$6,$7,$7)", tenant, uuid.New(), intent, endpoint, strings.Repeat("b", 64), "evt-1", at)
}

func smbCore005Ledger(at time.Time, db *pgtest.DB, t *testing.T, transport *smbCore005Transport, directory *smbCore005Directory) *messaging.SMBNoticeLedger {
	t.Helper()
	return messaging.NewSMBNoticeLedger(delivery.Runner{
		Store:       delivery.PostgresAttemptStore{DB: postgresDeliveryAppConn(t, db), Clock: func() time.Time { return at }},
		Transport:   transport,
		Idempotency: idempotency.NewRegistry(),
		Clock:       func() time.Time { return at },
		MaxAttempts: 1,
	}, directory)
}

func TestTodo_SMB_CORE_005_Integration(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := uuid.New()
	endpoint := uuid.New()
	transport := &smbCore005Transport{}
	directory := &smbCore005Directory{recipients: map[string]messaging.SMBNoticeRecipient{
		tenant.String() + "\x00" + "cust-1": {RecipientRef: "cust-1", EndpointRef: endpoint.String(), ContactValid: true, Locale: "en-US"},
	}}
	ledger := smbCore005Ledger(at, db, t, transport, directory)
	if err := ledger.PublishTemplate(messaging.SMBNoticeTemplate{
		TemplateRef: "invoice-issued-email", Purpose: messaging.PurposeNotice,
		Channels: []delivery.Channel{delivery.ChannelEmail}, AllowedFields: []string{"invoice.number", "invoice.total"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.RegisterTrigger("invoice-issued", "invoice-issued-email", messaging.PurposeNotice, messaging.RequirementSubmitted); err != nil {
		t.Fatal(err)
	}

	trigger := messaging.SMBNoticeTrigger{
		TenantID: tenant.String(), EventKind: "invoice-issued", EventID: "evt-1", RecipientRef: "cust-1",
		AggregateRef: "invoice:inv-1", AggregateRevision: 3, Actor: "billing", At: at,
	}
	first, err := ledger.TriggerEvent(ctx, trigger, "trigger:evt-1")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := ledger.TriggerEvent(ctx, trigger, "trigger:evt-1")
	if err != nil || replay.IntentID != first.IntentID {
		t.Fatalf("duplicate trigger = %+v, err=%v; want the one logical message", replay, err)
	}
	intentID, err := uuid.Parse(first.IntentID)
	if err != nil {
		t.Fatalf("intent id %q is not a durable UUID: %v", first.IntentID, err)
	}
	smbCore005Seed(t, db, tenant, intentID, endpoint, at)

	sent, err := ledger.Send(ctx, tenant.String(), first.IntentID, "cust-1", delivery.ChannelEmail, at.Add(time.Minute))
	if err != nil || sent.State != messaging.SMBNoticeSent || transport.invoked() != 1 {
		t.Fatalf("durable send = %+v, err=%v, calls=%d", sent, err, transport.invoked())
	}

	restarted := smbCore005Ledger(at, db, t, transport, directory)
	if err := restarted.PublishTemplate(messaging.SMBNoticeTemplate{
		TemplateRef: "invoice-issued-email", Purpose: messaging.PurposeNotice,
		Channels: []delivery.Channel{delivery.ChannelEmail}, AllowedFields: []string{"invoice.number", "invoice.total"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterTrigger("invoice-issued", "invoice-issued-email", messaging.PurposeNotice, messaging.RequirementSubmitted); err != nil {
		t.Fatal(err)
	}
	replayed, err := restarted.TriggerEvent(ctx, trigger, "trigger:evt-1")
	if err != nil || replayed.IntentID != first.IntentID {
		t.Fatalf("post-restart replay = %+v, err=%v; want the same intent", replayed, err)
	}
	resent, err := restarted.Send(ctx, tenant.String(), first.IntentID, "cust-1", delivery.ChannelEmail, at.Add(2*time.Minute))
	if err != nil || resent.State != messaging.SMBNoticeSent || transport.invoked() != 1 {
		t.Fatalf("post-restart send = %+v, err=%v, calls=%d; want Sent with no second send", resent, err, transport.invoked())
	}
	if got := countDeliveryRows(t, db, "delivery_attempt", tenant); got != 1 {
		t.Fatalf("delivery_attempt rows = %d, want one durable attempt", got)
	}

	transport.fail = true
	bouncedTrigger := messaging.SMBNoticeTrigger{
		TenantID: tenant.String(), EventKind: "invoice-issued", EventID: "evt-2", RecipientRef: "cust-1",
		AggregateRef: "invoice:inv-2", AggregateRevision: 1, Actor: "billing", At: at,
	}
	second, err := restarted.TriggerEvent(ctx, bouncedTrigger, "trigger:evt-2")
	if err != nil {
		t.Fatal(err)
	}
	secondIntent, err := uuid.Parse(second.IntentID)
	if err != nil {
		t.Fatal(err)
	}
	smbCore005Seed(t, db, tenant, secondIntent, endpoint, at)
	failed, err := restarted.Send(ctx, tenant.String(), second.IntentID, "cust-1", delivery.ChannelEmail, at.Add(3*time.Minute))
	if err == nil || failed.State != messaging.SMBNoticeFailed || failed.NextAction == "" {
		t.Fatalf("bounced send = %+v, err=%v; want failed with a manual next action", failed, err)
	}

	if _, err := restarted.Send(ctx, uuid.NewString(), first.IntentID, "cust-1", delivery.ChannelEmail, at); !errors.Is(err, messaging.ErrSMBNoticeNotFound) {
		t.Fatalf("cross-tenant send err=%v, want neutral not-found", err)
	}
}
