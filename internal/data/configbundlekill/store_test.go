package configbundlekill_test

import (
	"context"
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/configbundlekill"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/configbundle"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_REV_031_01_Recovery(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-kill',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantID, "kill-recovery-"+tenantID.String(), "kill recovery")
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	private := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	signer, err := configbundle.NewEd25519ReceiptSigner("recovery", "v1", private)
	if err != nil {
		t.Fatal(err)
	}
	public := private.Public().(ed25519.PublicKey)
	issued := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	kills := configbundle.NewKillSwitchStore(signer, public)
	kills.SetClock(func() time.Time { return issued })
	switchValue, err := kills.Publish(configbundle.KillSwitchRequest{
		SwitchID: "incident-recovery", Target: configbundle.KillSwitchTarget{TenantID: tenantID.String(), Capability: "rollout.expand"},
		Priority: 3, Reason: "contain incident", IncidentRef: "incident/43", Operator: "operator-a", Approver: "operator-b",
		EvidenceRef: "evidence/43", IssuedAt: issued, ExpiresAt: issued.Add(time.Hour), PropagationSLO: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := kills.Apply(switchValue)
	if err != nil {
		t.Fatal(err)
	}
	store := configbundlekill.New(conn, public)
	if err := store.PutApplied(context.Background(), switchValue, receipt); err != nil {
		t.Fatal(err)
	}
	recovered, err := configbundlekill.New(conn, public).ListApplied(context.Background(), tenantID.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 1 || recovered[0].Switch.Digest != switchValue.Digest || recovered[0].Receipt.Digest != receipt.Digest {
		t.Fatalf("reloaded applied evidence = %+v", recovered)
	}
	if err := recovered[0].Switch.Verify(public); err != nil {
		t.Fatalf("restored switch signature: %v", err)
	}
	if err := recovered[0].Receipt.Verify(public); err != nil {
		t.Fatalf("restored receipt signature: %v", err)
	}
	restarted := configbundle.NewKillSwitchStore(signer, public)
	if err := restarted.RestoreApplied(recovered[0].Switch, recovered[0].Receipt); err != nil {
		t.Fatalf("restore after process restart: %v", err)
	}
	decision := restarted.Evaluate(configbundle.KillSwitchTarget{TenantID: tenantID.String(), Capability: "rollout.expand"}, issued.Add(time.Second))
	if !decision.Disabled || decision.SwitchID != "incident-recovery" {
		t.Fatalf("recovered kill decision = %+v", decision)
	}
	if err := store.PutApplied(context.Background(), switchValue, receipt); err != nil {
		t.Fatalf("idempotent persistence retry: %v", err)
	}
	foreign, err := configbundlekill.New(conn, public).ListApplied(context.Background(), uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if len(foreign) != 0 {
		t.Fatalf("foreign tenant recovered %d records", len(foreign))
	}
}

func TestTodo_REV_043_03_Recovery(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-kill',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantID, "rollout-kill-"+tenantID.String(), "rollout kill recovery")
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	private := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	signer, err := configbundle.NewEd25519ReceiptSigner("recovery", "v1", private)
	if err != nil {
		t.Fatal(err)
	}
	public := private.Public().(ed25519.PublicKey)
	issued := time.Now().UTC().Truncate(time.Second)
	store := configbundle.NewKillSwitchStore(signer, public)
	switchValue, err := store.Issue(configbundle.KillSwitchRequest{SwitchID: "rollout-kill", Target: configbundle.KillSwitchTarget{TenantID: tenantID.String(), Capability: "rollout.activate"}, Priority: 1, Reason: "contain", IncidentRef: "incident/rollout", Operator: "operator-a", Approver: "operator-b", EvidenceRef: "evidence/rollout", IssuedAt: issued, ExpiresAt: issued.Add(time.Hour), PropagationSLO: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := store.Apply(switchValue)
	if err != nil {
		t.Fatal(err)
	}
	persistent := configbundlekill.New(conn, public)
	if err := persistent.PutApplied(context.Background(), switchValue, receipt); err != nil {
		t.Fatal(err)
	}
	items, err := configbundlekill.New(conn, public).ListApplied(context.Background(), tenantID.String())
	if err != nil || len(items) != 1 {
		t.Fatalf("durable applied state=%+v err=%v", items, err)
	}
	recovered := configbundle.NewKillSwitchStore(signer, public)
	if err := recovered.RestoreApplied(items[0].Switch, items[0].Receipt); err != nil {
		t.Fatal(err)
	}
	guardErr := recovered.Guard(configbundle.KillSwitchTarget{TenantID: tenantID.String(), Capability: "rollout.activate"}, func(decision configbundle.KillDecision) error {
		if !decision.Disabled {
			t.Fatalf("restarted rollout guard decision=%+v", decision)
		}
		return nil
	})
	if guardErr != nil {
		t.Fatal(guardErr)
	}
}

func TestTodo_REV_043_03_ApplyVsAdvance(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-kill-race',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantID, "rollout-kill-race-"+tenantID.String(), "rollout kill race")
	connA, connB := db.NewConn(t), db.NewConn(t)
	for _, conn := range []interface {
		Exec(context.Context, string, ...any) (int64, error)
	}{connA, connB} {
		if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
			t.Fatal(err)
		}
	}
	private := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	signer, err := configbundle.NewEd25519ReceiptSigner("race", "v1", private)
	if err != nil {
		t.Fatal(err)
	}
	public := private.Public().(ed25519.PublicKey)
	target := configbundle.KillSwitchTarget{TenantID: tenantID.String(), Capability: "rollout.expand"}
	guard := configbundlekill.New(connA, public)
	guard.SetClock(func() time.Time { return time.Now().UTC() })

	entered, release := make(chan struct{}), make(chan struct{})
	guardDone := make(chan error, 1)
	go func() {
		guardDone <- guard.Guard(target, func(decision configbundle.KillDecision) error {
			if decision.Disabled {
				return errors.New("unexpected pre-apply kill")
			}
			close(entered)
			<-release
			return nil // represents the guarded rollout stage commit
		})
	}()
	<-entered

	applyStore := configbundle.NewKillSwitchStore(signer, public)
	persistentWriter := configbundlekill.New(connB, public)
	applyStore.SetAppliedWriter(func(switchValue configbundle.SignedKillSwitch, receipt configbundle.AppliedKillSwitchReceipt) error {
		return persistentWriter.PutApplied(context.Background(), switchValue, receipt)
	})
	issued := time.Now().UTC().Truncate(time.Second)
	switchValue, err := applyStore.Issue(configbundle.KillSwitchRequest{SwitchID: "race-stop", Target: target, Priority: 1, Reason: "race stop",
		IncidentRef: "incident/race", Operator: "operator-a", Approver: "operator-b", EvidenceRef: "evidence/race",
		IssuedAt: issued, ExpiresAt: issued.Add(time.Hour), PropagationSLO: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	applyDone := make(chan error, 1)
	go func() { _, err := applyStore.Apply(switchValue); applyDone <- err }()
	select {
	case err := <-applyDone:
		t.Fatalf("durable CP-008 apply raced through guarded transition: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-guardDone; err != nil {
		t.Fatal(err)
	}
	if err := <-applyDone; err != nil {
		t.Fatal(err)
	}
	var blocked bool
	if err := guard.Guard(target, func(decision configbundle.KillDecision) error { blocked = decision.Disabled; return nil }); err != nil {
		t.Fatal(err)
	}
	if !blocked {
		t.Fatal("next guarded rollout transition did not see the cross-process applied kill")
	}
}
