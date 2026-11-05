// TestTodo_SMB_CORE_002_Integration proves SMB-CORE-002 adoption against real
// PostgreSQL: 20 concurrent retries converge on one durable operation through
// the store-backed registry, a one-field change conflicts with the domain
// revision untouched, and a restarted runtime replays the same operation.
package platformidempotencystore

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/idempotency"
)

func TestTodo_SMB_CORE_002_Integration(t *testing.T) {
	db, tenant := fixture(t)
	registry := idempotency.NewRegistryWithStore(newAppStore(t, db))
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	party := idempotency.SMBAPIAdoptionDescriptor{
		Name: "fixture-party-note", SchemaVersion: "fixture-party-note/v1",
		Capability: "hcmnext.smb.fixture.note@1", TenantID: tenant.String(),
		ResourceScope: "fixture:party-notes", IdempotencyScope: "fixture:party-note:write",
		FilterAllowlist: []string{"status"}, OrderAllowlist: []string{"updated_at"},
		MaxBatchItems: 10, MaxPayloadBytes: 4096, PageSize: 5,
		EventFamily: "smb.fixture.note", EventSchema: "smb.fixture.note/v1",
		RetryHorizon: time.Hour, ControlEpoch: 7,
	}
	stock := party
	stock.Name = "fixture-stock-count"
	stock.SchemaVersion = "fixture-stock-count/v1"
	stock.Capability = "hcmnext.smb.fixture.count@1"
	stock.ResourceScope = "fixture:stock-counts"
	stock.IdempotencyScope = "fixture:stock-count:write"
	stock.EventFamily = "smb.fixture.count"
	stock.EventSchema = "smb.fixture.count/v1"
	stock.RetryHorizon = 30 * time.Minute
	harness := idempotency.NewSMBConformanceHarness(registry, now, 7)
	emptyBase, err := idempotency.NewSMBConformanceHarness(idempotency.NewRegistry(), now, 7).Export()
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.Import(emptyBase); err != nil {
		t.Fatal(err)
	}
	if err := harness.RegisterFixture(party); err != nil {
		t.Fatal(err)
	}
	if err := harness.RegisterFixture(stock); err != nil {
		t.Fatal(err)
	}
	harness.IssueMachineToken("machine-token-1")

	command := func(key, payload string, rev int64) idempotency.SMBOperationCommand {
		return idempotency.SMBOperationCommand{
			Descriptor: party, Key: key, Canonical: []byte(payload), ExpectedRevision: rev,
			MachineToken: "machine-token-1", DelegatedActor: "employee-1", ControlEpoch: 7, Now: now,
		}
	}

	const retries = 20
	refs := make([]string, retries)
	errs := make([]error, retries)
	var wg sync.WaitGroup
	for i := 0; i < retries; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := harness.Execute(command("op-note-race", `{"note":"race","priority":"high"}`, 0))
			if err == nil {
				refs[i] = result.OperationRef
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent retry %d err=%v, want convergence", i, err)
		}
		if refs[i] == "" || refs[i] != refs[0] {
			t.Fatalf("concurrent retry %d ref=%q, want the single operation %q", i, refs[i], refs[0])
		}
	}
	if got := harness.FixtureMutations(party.ResourceScope); got != 1 {
		t.Fatalf("fixture mutations = %d, want exactly one across 20 durable retries", got)
	}
	if events := harness.Events(); len(events) != 1 || events[0].AggregateRevision != 1 {
		t.Fatalf("logical outbox events = %+v, want one event for one operation", events)
	}

	changed, err := harness.Execute(command("op-note-race", `{"note":"race","priority":"low"}`, 0))
	if !errors.Is(err, idempotency.ErrConflict) || changed.OperationRef != "" {
		t.Fatalf("one-field change = %+v, err=%v; want conflict before effects", changed, err)
	}
	if rev := harness.CurrentRevision(party.ResourceScope); rev != 1 {
		t.Fatalf("conflicted retry moved durable revision to %d, want 1", rev)
	}

	restarted := idempotency.NewSMBConformanceHarness(idempotency.NewRegistryWithStore(newAppStore(t, db)), now, 7)
	if err := restarted.RegisterFixture(party); err != nil {
		t.Fatal(err)
	}
	if err := restarted.RegisterFixture(stock); err != nil {
		t.Fatal(err)
	}
	restarted.IssueMachineToken("machine-token-1")
	if _, err := restarted.Execute(command("op-note-unrestored", `{"note":"race"}`, 0)); !errors.Is(err, idempotency.ErrSMBBaseNotRestored) {
		t.Fatalf("unrestored durable new work err=%v, want base-not-restored refusal", err)
	}
	replay, err := restarted.Execute(command("op-note-race", `{"note":"race","priority":"high"}`, 0))
	if err != nil || replay.Decision != idempotency.Replay || replay.OperationRef != refs[0] || replay.Revision != 1 {
		t.Fatalf("post-restart retry = %+v, err=%v; want the same durable operation", replay, err)
	}
	if got := restarted.FixtureMutations(party.ResourceScope); got != 0 {
		t.Fatalf("post-restart replay applied %d mutations, want zero", got)
	}
	harnessSnap, err := harness.Export()
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Import(harnessSnap); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Execute(command("op-note-stale", `{"note":"race"}`, 0)); !errors.Is(err, idempotency.ErrSMBRevisionConflict) {
		t.Fatalf("stale durable new work err=%v, want revision-conflict refusal", err)
	}
	stored, err := newAppStore(t, db).Lookup(idempotency.Identity{Tenant: tenant.String(), Capability: party.Capability, EffectScope: party.IdempotencyScope, Key: "op-note-race", Principal: "machine-token-1"})
	if err != nil || stored.State != idempotency.Completed || stored.ResultRef != refs[0] {
		t.Fatalf("durable record = %+v, err=%v; want the completed operation", stored, err)
	}
}
