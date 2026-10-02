package agentstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestAgentUXDemo_EffectBudget_Integration(t *testing.T) {
	s, store, tenant, other := agentuxDemoInboxFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 26; i++ {
		id := fmt.Sprintf("email-%d", i)
		if _, err := s.Receive(ctx, SupportInboxMessage{TenantID: tenant, MessageID: id, ContentRef: "object:" + id, ContentDigest: "sha256:" + strings.Repeat("a", 64), ClaimedSenderName: "Customer", ClaimedSenderAddress: "customer@example.test", ReceivedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 25; i++ {
		if err := s.ReserveSupportEffect(ctx, tenant, fmt.Sprintf("email-%d", i), "support.create_ticket", now, 25); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ReserveSupportEffect(ctx, tenant, "email-25", "support.create_ticket", now, 25); !errors.Is(err, ErrSupportDailyLimit) {
		t.Fatalf("daily ceiling: %v", err)
	}
	if err := s.ReserveSupportEffect(ctx, tenant, "email-1", "support.create_ticket", now, 25); err != nil {
		t.Fatalf("replay consumed budget: %v", err)
	}
	if err := s.ReserveSupportEffect(ctx, tenant, "email-25", "support.alert_channel", now, 25); err != nil {
		t.Fatalf("independent effect budget: %v", err)
	}
	if err := s.ReserveSupportEffect(ctx, tenant, "email-25", "support.create_ticket", now.Add(24*time.Hour), 25); err != nil {
		t.Fatalf("next day: %v", err)
	}
	if err := s.ReserveSupportEffect(ctx, other, "email-1", "support.create_ticket", now, 25); err == nil {
		t.Fatal("foreign email admitted")
	}
	if err := store.RunTenantTx(ctx, other, func(tx dbport.Tx) error {
		var n int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM support_effect_reservation WHERE tenant_id=$1`, tenant).Scan(&n)
		if err == nil && n != 0 {
			t.Error("budget RLS leak")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAgentUXDemo_SupportPlan_Integration(t *testing.T) {
	s, store, tenant, other := agentuxDemoInboxFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := s.Receive(ctx, SupportInboxMessage{TenantID: tenant, MessageID: "email-plan", ContentRef: "object:email", ContentDigest: "sha256:" + strings.Repeat("a", 64), ClaimedSenderName: "Customer", ClaimedSenderAddress: "customer@example.test", ReceivedAt: now}); err != nil {
		t.Fatal(err)
	}
	r := SupportPlanReceipt{TenantID: tenant, MessageID: "email-plan", RunID: uuid.NewString(), PlanRef: "object:plan", PlanDigest: "sha256:" + strings.Repeat("b", 64), RecordedAt: now}
	if err := s.WithSupportRunFence(ctx, tenant, r.MessageID, func() error { return s.RecordSupportPlan(ctx, r) }); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSupportPlan(ctx, r); err != nil {
		t.Fatalf("replay: %v", err)
	}
	got, err := s.GetSupportPlan(ctx, tenant, r.MessageID)
	if err != nil || got != r {
		t.Fatalf("plan: %+v %v", got, err)
	}
	bad := r
	bad.PlanDigest = "sha256:" + strings.Repeat("c", 64)
	if err := s.RecordSupportPlan(ctx, bad); !errors.Is(err, ErrSupportInboxReplay) {
		t.Fatalf("changed model plan: %v", err)
	}
	if _, err := s.GetSupportPlan(ctx, other, r.MessageID); !errors.Is(err, ErrSupportInboxNotFound) {
		t.Fatalf("plan tenant leak: %v", err)
	}
	if err := store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE support_run_plan SET plan_ref='object:tampered' WHERE tenant_id=$1`, tenant)
		return err
	}); err == nil {
		t.Fatal("mutable plan")
	}
}

func TestAgentUXDemo_BirthdayFence_Integration(t *testing.T) {
	s, _, tenant, _ := agentuxDemoInboxFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	entered, read, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	fenced := make(chan error, 1)
	go func() {
		fenced <- s.WithBirthdayPreferenceFence(ctx, tenant, func() error {
			close(entered)
			<-read
			pref, err := s.BirthdayPreference(ctx, tenant, "worker", true)
			if err != nil {
				return err
			}
			if !pref.Share {
				return errors.New("opt-out overtook birthday delivery fence")
			}
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	writes := make(chan error, 3)
	attempts := make(chan struct{}, 3)
	for range 3 {
		go func() {
			attempts <- struct{}{}
			_, err := s.SaveBirthdayPreference(ctx, tenant, "worker", BirthdayPreference{Share: false}, time.Now())
			writes <- err
		}()
	}
	for range 3 {
		<-attempts
	}
	close(read)
	// The callback reads through the ordinary request pool while preference
	// writers wait in the separate fence pool. Even a two-connection serving
	// pool must remain usable here.
	close(release)
	if err := <-fenced; err != nil {
		t.Fatal(err)
	}
	success := 0
	for range 3 {
		err := <-writes
		if err == nil {
			success++
		} else if !errors.Is(err, ErrSupportInboxReplay) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("preference CAS successes=%d", success)
	}
	pref, err := s.BirthdayPreference(ctx, tenant, "worker", true)
	if err != nil || pref.Share || pref.Revision != 1 {
		t.Fatalf("opt-out after fence: %+v %v", pref, err)
	}
}
