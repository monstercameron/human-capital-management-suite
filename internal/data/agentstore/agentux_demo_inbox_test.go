package agentstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func agentuxDemoInboxFixture(t *testing.T) (*SupportInboxStore, *Store, uuid.UUID, uuid.UUID) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	a, b := uuid.New(), uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES($1),($2)`, a, b); err != nil {
		t.Fatal(err)
	}
	login, password := roleName("support_inbox"), uuid.NewString()
	if err := createAgentLogin(ctx, db.SQL, login, password); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.SQL.ExecContext(context.Background(), "DROP ROLE "+login) })
	store, err := New(ctx, Config{DSN: testDSN(t, db.URL, db.Schema, login, password, "postgres"), CoreDSN: "postgres://core:pw@127.0.0.1:5433/core", MaxConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	inbox, err := NewSupportInboxStore(store)
	if err != nil {
		t.Fatal(err)
	}
	return inbox, store, a, b
}

func TestAgentUXDemo_Inbox_Integration(t *testing.T) {
	s, _, tenant, other := agentuxDemoInboxFixture(t)
	ctx := context.Background()
	r := SupportInboxMessage{TenantID: tenant, MessageID: "customer-email-1", ContentRef: "object:support-email-1", ContentDigest: "sha256:" + strings.Repeat("a", 64), ClaimedSenderName: "Customer (claimed)", ClaimedSenderAddress: "customer@example.test", ReceivedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	if inserted, err := s.Receive(ctx, r); err != nil || !inserted {
		t.Fatalf("receive: %t %v", inserted, err)
	}
	if inserted, err := s.Receive(ctx, r); err != nil || inserted {
		t.Fatalf("replay: %t %v", inserted, err)
	}
	got, err := s.Get(ctx, tenant, r.MessageID)
	if err != nil || got.ContentRef != r.ContentRef || got.ClaimedSenderAddress != r.ClaimedSenderAddress {
		t.Fatalf("get: %+v %v", got, err)
	}
	if _, err := s.Get(ctx, other, r.MessageID); !errors.Is(err, ErrSupportInboxNotFound) {
		t.Fatalf("tenant leak: %v", err)
	}
	for _, change := range []func(*SupportInboxMessage){func(r *SupportInboxMessage) { r.ContentDigest = "sha256:" + strings.Repeat("b", 64) }, func(r *SupportInboxMessage) { r.ContentRef = "object:other" }, func(r *SupportInboxMessage) { r.ClaimedSenderAddress = "employee@example.test" }, func(r *SupportInboxMessage) { r.ReceivedAt = r.ReceivedAt.Add(time.Second) }} {
		bad := r
		change(&bad)
		if _, err := s.Receive(ctx, bad); !errors.Is(err, ErrSupportInboxReplay) {
			t.Fatalf("changed receipt accepted: %v", err)
		}
	}
	r.TenantID = other
	if inserted, err := s.Receive(ctx, r); err != nil || !inserted {
		t.Fatalf("independent tenant receipt: %t %v", inserted, err)
	}
}

func TestAgentUXDemo_Inbox_Security(t *testing.T) {
	s, store, tenant, other := agentuxDemoInboxFixture(t)
	ctx := context.Background()
	r := SupportInboxMessage{TenantID: tenant, MessageID: "spoofed-sender", ContentRef: "object:quarantined", ContentDigest: "sha256:" + strings.Repeat("a", 64), ClaimedSenderName: "Administrator", ClaimedSenderAddress: "admin@example.test", ReceivedAt: time.Now().UTC()}
	if _, err := s.Receive(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var verified bool
		err := tx.QueryRow(ctx, `SELECT sender_verified FROM support_inbox_message WHERE tenant_id=$1 AND message_id=$2`, tenant, r.MessageID).Scan(&verified)
		if err == nil && verified {
			t.Error("claimed employee trusted")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`UPDATE support_inbox_message SET claimed_sender_name='trusted' WHERE tenant_id=$1`, `DELETE FROM support_inbox_message WHERE tenant_id=$1`} {
		if err := store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error { _, err := tx.Exec(ctx, query, tenant); return err }); err == nil {
			t.Fatal("mutable receipt")
		}
	}
	if err := store.RunTenantTx(ctx, other, func(tx dbport.Tx) error {
		var n int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM support_inbox_message WHERE tenant_id=$1`, tenant).Scan(&n)
		if err == nil && n != 0 {
			t.Error("RLS exposed other tenant")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	bad := r
	bad.ContentDigest = "sha256:invalid"
	if _, err := s.Receive(ctx, bad); !errors.Is(err, ErrSupportInboxInvalid) {
		t.Fatalf("invalid digest: %v", err)
	}
	bad = r
	bad.ClaimedSenderAddress = "customer@example.test\r\nBcc:other@example.test"
	if _, err := s.Receive(ctx, bad); !errors.Is(err, ErrSupportInboxInvalid) {
		t.Fatalf("header injection: %v", err)
	}
}

func TestAgentUXDemo_BirthdayPreference_Integration(t *testing.T) {
	s, _, tenant, other := agentuxDemoInboxFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	p, err := s.BirthdayPreference(ctx, tenant, "worker-1", true)
	if err != nil || !p.Share || p.Revision != 0 {
		t.Fatalf("demo default: %+v %v", p, err)
	}
	p.Share = false
	p, err = s.SaveBirthdayPreference(ctx, tenant, "worker-1", p, now)
	if err != nil || p.Share || p.Revision != 1 {
		t.Fatalf("opt out: %+v %v", p, err)
	}
	got, err := s.BirthdayPreference(ctx, tenant, "worker-1", true)
	if err != nil || got.Share || got.Revision != 1 {
		t.Fatalf("lost opt out: %+v %v", got, err)
	}
	if _, err := s.SaveBirthdayPreference(ctx, tenant, "worker-1", BirthdayPreference{Share: true}, now); !errors.Is(err, ErrSupportInboxReplay) {
		t.Fatalf("stale edit: %v", err)
	}
	got.Share = true
	got, err = s.SaveBirthdayPreference(ctx, tenant, "worker-1", got, now)
	if err != nil || got.Revision != 2 {
		t.Fatalf("opt in: %+v %v", got, err)
	}
	got, err = s.BirthdayPreference(ctx, other, "worker-1", false)
	if err != nil || got.Share || got.Revision != 0 {
		t.Fatalf("preference tenant leak: %+v %v", got, err)
	}
}
