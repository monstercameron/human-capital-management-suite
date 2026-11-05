// TestTodo_SMB_CORE_002 covers SMB-CORE-002: expansion modules adopt the
// existing public API, idempotency and event contracts through typed
// test-only adoption descriptors and durable conformance fixtures. Retries
// converge through the shared idempotency kernel; conflicts, stale
// revisions, expired horizons, revoked tokens and fenced epochs fail before
// effects; reads use opaque tenant/filter-bound cursors over an allowlist.
package idempotency

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func smbCore002Fixture(now time.Time) (SMBAPIAdoptionDescriptor, SMBAPIAdoptionDescriptor) {
	party := SMBAPIAdoptionDescriptor{
		Name: "fixture-party-note", SchemaVersion: "fixture-party-note/v1",
		Capability: "hcmnext.smb.fixture.note@1", TenantID: "tenant-a",
		ResourceScope: "fixture:party-notes", IdempotencyScope: "fixture:party-note:write",
		FilterAllowlist: []string{"status", "owner"}, OrderAllowlist: []string{"updated_at", "created_at"},
		MaxBatchItems: 10, MaxPayloadBytes: 4096, PageSize: 5,
		EventFamily: "smb.fixture.note", EventSchema: "smb.fixture.note/v1",
		RetryHorizon: time.Hour, ControlEpoch: 7,
	}
	stock := SMBAPIAdoptionDescriptor{
		Name: "fixture-stock-count", SchemaVersion: "fixture-stock-count/v1",
		Capability: "hcmnext.smb.fixture.count@1", TenantID: "tenant-a",
		ResourceScope: "fixture:stock-counts", IdempotencyScope: "fixture:stock-count:write",
		FilterAllowlist: []string{"site", "item"}, OrderAllowlist: []string{"counted_at"},
		MaxBatchItems: 4, MaxPayloadBytes: 2048, PageSize: 5,
		EventFamily: "smb.fixture.count", EventSchema: "smb.fixture.count/v1",
		RetryHorizon: 30 * time.Minute, ControlEpoch: 7,
	}
	return party, stock
}

func smbCore002Harness(t *testing.T, now time.Time) (*SMBConformanceHarness, SMBAPIAdoptionDescriptor, SMBAPIAdoptionDescriptor) {
	t.Helper()
	party, stock := smbCore002Fixture(now)
	harness := NewSMBConformanceHarness(NewRegistry(), now, 7)
	if err := harness.RegisterFixture(party); err != nil {
		t.Fatal(err)
	}
	if err := harness.RegisterFixture(stock); err != nil {
		t.Fatal(err)
	}
	harness.IssueMachineToken("machine-token-1")
	return harness, party, stock
}

func smbCore002Command(d SMBAPIAdoptionDescriptor, key, payload string, rev int64, now time.Time) SMBOperationCommand {
	return SMBOperationCommand{
		Descriptor: d, Key: key, Canonical: []byte(payload), ExpectedRevision: rev,
		MachineToken: "machine-token-1", DelegatedActor: "employee-1", ControlEpoch: 7, Now: now,
	}
}

func TestTodo_SMB_CORE_002(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	harness, party, stock := smbCore002Harness(t, now)

	bad := party
	bad.SchemaVersion = "not-a-version"
	if err := harness.RegisterFixture(bad); !errors.Is(err, ErrSMBDescriptorInvalid) {
		t.Fatalf("bad schema version err=%v, want descriptor refusal", err)
	}
	bad = party
	bad.MaxBatchItems = 0
	if err := harness.RegisterFixture(bad); !errors.Is(err, ErrSMBDescriptorInvalid) {
		t.Fatalf("zero batch limit err=%v, want descriptor refusal", err)
	}
	bad = party
	bad.RetryHorizon = 0
	if err := harness.RegisterFixture(bad); !errors.Is(err, ErrSMBDescriptorInvalid) {
		t.Fatalf("zero retry horizon err=%v, want descriptor refusal", err)
	}

	first, err := harness.Execute(smbCore002Command(party, "op-note-1", `{"note":"hello","priority":"high"}`, 0, now))
	if err != nil || first.Decision != Reserved || first.Revision != 1 || first.EventID == "" {
		t.Fatalf("first execute = %+v, err=%v", first, err)
	}
	if first.MachinePrincipal != "machine-token-1" || first.DelegatedActor != "employee-1" {
		t.Fatalf("actor binding = %+v, want separate machine and delegated actors", first)
	}
	stockFirst, err := harness.Execute(smbCore002Command(stock, "op-count-1", `{"site":"s1","qty":3}`, 0, now))
	if err != nil || stockFirst.Revision != 1 {
		t.Fatalf("second typed descriptor execute = %+v, err=%v", stockFirst, err)
	}
	if events := harness.Events(); len(events) != 2 || events[0].AggregateRevision != 1 || events[0].Family != "smb.fixture.note" {
		t.Fatalf("logical outbox events = %+v, want one typed event per operation", events)
	}

	replay, err := harness.Execute(smbCore002Command(party, "op-note-1", `{"note":"hello","priority":"high"}`, 0, now.Add(time.Minute)))
	if err != nil || replay.Decision != Replay || replay.OperationRef != first.OperationRef || replay.EventID != first.EventID {
		t.Fatalf("retry = %+v, err=%v; want the original logical operation", replay, err)
	}
	if got := harness.FixtureMutations(party.ResourceScope); got != 1 {
		t.Fatalf("fixture mutations = %d, want exactly one after retry", got)
	}
	if events := harness.Events(); len(events) != 2 {
		t.Fatalf("retry emitted %d events, want no second logical event", len(events))
	}

	changed, err := harness.Execute(smbCore002Command(party, "op-note-1", `{"note":"hello","priority":"low"}`, 0, now.Add(2*time.Minute)))
	if !errors.Is(err, ErrConflict) || changed.OperationRef != "" {
		t.Fatalf("key-payload mismatch = %+v, err=%v; want conflict before effects", changed, err)
	}
	if rev := harness.CurrentRevision(party.ResourceScope); rev != 1 {
		t.Fatalf("conflicted retry moved revision to %d, want 1", rev)
	}

	stale, err := harness.Execute(smbCore002Command(party, "op-note-2", `{"note":"new"}`, 0, now.Add(3*time.Minute)))
	if !errors.Is(err, ErrSMBRevisionConflict) || stale.OperationRef != "" {
		t.Fatalf("stale revision = %+v, err=%v; want revision-conflict refusal", stale, err)
	}

	batch := harness.ExecuteBatch([]SMBOperationCommand{
		smbCore002Command(stock, "op-count-2", `{"site":"s1","qty":5}`, 1, now.Add(4*time.Minute)),
		smbCore002Command(stock, "op-count-1", `{"site":"s1","qty":9}`, 0, now.Add(4*time.Minute)),
		smbCore002Command(stock, "op-count-3", `{"site":"s2","qty":1}`, 99, now.Add(4*time.Minute)),
	})
	if len(batch) != 3 || batch[0].Err != "" || batch[1].Err == "" || batch[2].Err == "" {
		t.Fatalf("batch items = %+v, want per-item status with partial completion", batch)
	}
	if !strings.Contains(batch[1].Err, "conflict") || !strings.Contains(batch[2].Err, "revision") {
		t.Fatalf("batch errors = %+v, want conflict distinguished from stale revision", batch)
	}
	if rev := harness.CurrentRevision(stock.ResourceScope); rev != 2 {
		t.Fatalf("batch revision = %d, want the one accepted item applied", rev)
	}

	// rev-smb-b FINDING 2: the caller names only the scope. A rewritten
	// capability, payload bound or batch limit in the supplied descriptor is
	// ignored in favor of the registered fixture.
	spoofed := stock
	spoofed.Capability = "hcmnext.evil@9"
	spoofReplay, err := harness.Execute(smbCore002Command(spoofed, "op-count-1", `{"site":"s1","qty":3}`, 0, now.Add(5*time.Minute)))
	if err != nil || spoofReplay.Decision != Replay || spoofReplay.OperationRef != stockFirst.OperationRef {
		t.Fatalf("spoofed-capability retry = %+v, err=%v; want the original operation", spoofReplay, err)
	}
	oversize := smbCore002Command(party, "op-note-big", strings.Repeat("x", 5000), 1, now.Add(5*time.Minute))
	oversize.Descriptor.MaxPayloadBytes = 1000000
	if _, err := harness.Execute(oversize); !errors.Is(err, ErrSMBCommandInvalid) {
		t.Fatalf("spoofed-bound payload err=%v, want the registered 4096-byte refusal", err)
	}
	var wide []SMBOperationCommand
	for i := 0; i < 5; i++ {
		cmd := smbCore002Command(stock, "op-wide", `{"site":"s1"}`, 2, now.Add(5*time.Minute))
		cmd.Key = "op-wide"
		cmd.Descriptor.MaxBatchItems = 100
		wide = append(wide, cmd)
	}
	wideBatch := harness.ExecuteBatch(wide)
	for i, item := range wideBatch {
		if !strings.Contains(item.Err, "registered limit 4") {
			t.Fatalf("wide batch item %d = %+v, want the registered limit-4 refusal", i, item)
		}
	}

	// Duplicate event families are refused at registration so delivery
	// attribution stays deterministic.
	dupe := party
	dupe.ResourceScope = "fixture:other"
	dupe.IdempotencyScope = "fixture:other:write"
	if err := harness.RegisterFixture(dupe); !errors.Is(err, ErrSMBDescriptorInvalid) {
		t.Fatalf("duplicate family err=%v, want descriptor refusal", err)
	}

	// Per-token namespaces carry distinct operation references.
	harness.IssueMachineToken("machine-token-2")
	refA, err := harness.Execute(smbCore002Command(party, "op-note-p3", `{"note":"p3"}`, 1, now.Add(6*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	tokenB := smbCore002Command(party, "op-note-p3", `{"note":"p3"}`, 2, now.Add(6*time.Minute))
	tokenB.MachineToken = "machine-token-2"
	refB, err := harness.Execute(tokenB)
	if err != nil || refA.OperationRef == refB.OperationRef {
		t.Fatalf("per-token refs = %q and %q, want distinct operation identities", refA.OperationRef, refB.OperationRef)
	}
}

func TestTodo_SMB_CORE_002_Security(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	harness, party, _ := smbCore002Harness(t, now)
	for i := 0; i < 6; i++ {
		key := "op-note-seed-" + string(rune('a'+i))
		cmd := smbCore002Command(party, key, `{"note":"seed"}`, int64(i), now.Add(time.Duration(i)*time.Second))
		if _, err := harness.Execute(cmd); err != nil {
			t.Fatal(err)
		}
	}

	otherTenant := party
	otherTenant.TenantID = "tenant-b"
	if page, err := harness.Read(SMBReadQuery{Descriptor: otherTenant, MachineToken: "machine-token-1", Now: now}); err == nil || len(page.Items) != 0 {
		t.Fatalf("cross-tenant read = %+v, err=%v; want refusal with no records", page, err)
	}
	first, err := harness.Read(SMBReadQuery{Descriptor: party, MachineToken: "machine-token-1", Now: now})
	if err != nil || len(first.Items) == 0 {
		t.Fatalf("authorized read = %+v, err=%v", first, err)
	}
	if first.NextCursor == "" {
		t.Fatalf("first page = %+v, want a continuation cursor over six seeded rows", first)
	}
	forged := SMBReadQuery{Descriptor: otherTenant, Cursor: first.NextCursor, MachineToken: "machine-token-1", Now: now}
	if page, err := harness.Read(forged); err == nil || len(page.Items) != 0 {
		t.Fatalf("cross-tenant cursor = %+v, err=%v; want refusal with no records", page, err)
	}

	arbitrary := SMBReadQuery{Descriptor: party, Filter: map[string]string{"password": "x"}, MachineToken: "machine-token-1", Now: now}
	if page, err := harness.Read(arbitrary); !errors.Is(err, ErrSMBFilterRejected) || len(page.Items) != 0 {
		t.Fatalf("arbitrary filter = %+v, err=%v; want filter refusal with no records", page, err)
	}
	badOrder := SMBReadQuery{Descriptor: party, Order: "password", MachineToken: "machine-token-1", Now: now}
	if _, err := harness.Read(badOrder); !errors.Is(err, ErrSMBFilterRejected) {
		t.Fatalf("arbitrary order err=%v, want filter refusal", err)
	}
	scopedChange := SMBReadQuery{Descriptor: party, Cursor: first.NextCursor, Filter: map[string]string{"status": "open"}, MachineToken: "machine-token-1", Now: now}
	if _, err := harness.Read(scopedChange); !errors.Is(err, ErrSMBCursorRejected) {
		t.Fatalf("cursor scope change err=%v, want cursor refusal", err)
	}

	pastHorizon := smbCore002Command(party, "op-note-seed-a", `{"note":"seed"}`, 0, now.Add(2*time.Hour))
	if _, err := harness.Execute(pastHorizon); !errors.Is(err, ErrExpired) {
		t.Fatalf("retry beyond retention err=%v, want protected-expiry refusal, never fresh work", err)
	}
	if got := harness.FixtureMutations(party.ResourceScope); got != 6 {
		t.Fatalf("expired retry left %d mutations, want exactly six", got)
	}

	harness.RevokeMachineToken("machine-token-1")
	revokedCmd := smbCore002Command(party, "op-note-2", `{"note":"after revoke"}`, 6, now.Add(time.Minute))
	if _, err := harness.Execute(revokedCmd); !errors.Is(err, ErrSMBTokenRevoked) {
		t.Fatalf("revoked token err=%v, want token refusal", err)
	}
	if _, err := harness.Read(SMBReadQuery{Descriptor: party, MachineToken: "machine-token-1", Now: now}); !errors.Is(err, ErrSMBTokenRevoked) {
		t.Fatalf("revoked token read err=%v, want token refusal", err)
	}
	if rev := harness.CurrentRevision(party.ResourceScope); rev != 6 {
		t.Fatalf("revoked-token attempt moved revision to %d, want 6", rev)
	}
}

func TestTodo_SMB_CORE_002_Fault(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	harness, party, _ := smbCore002Harness(t, now)
	if _, err := harness.Execute(smbCore002Command(party, "op-note-1", `{"note":"v1"}`, 0, now)); err != nil {
		t.Fatal(err)
	}

	harness.DisconnectStream()
	if _, err := harness.Execute(smbCore002Command(party, "op-note-2", `{"note":"v2"}`, 1, now.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.Execute(smbCore002Command(party, "op-note-3", `{"note":"v3"}`, 2, now.Add(2*time.Minute))); err != nil {
		t.Fatal(err)
	}
	events := harness.Events()
	if len(events) != 3 {
		t.Fatalf("disconnected commands emitted %d events, want 3 durable logical events", len(events))
	}

	reversed := []SMBLogicalEvent{events[2], events[2], events[1], events[0], events[2]}
	report := harness.DeliverEvents(reversed)
	if report.Accepted != 3 || report.Duplicates != 2 {
		t.Fatalf("delivery report = %+v, want 3 accepted and 2 duplicates", report)
	}
	harness.ReconnectStream()
	refetched := harness.ResolveOperationView(party.TenantID, party.ResourceScope, "op-note-3", now.Add(3*time.Minute))
	if refetched.Revision != 3 || refetched.Status != SMBViewCompleted {
		t.Fatalf("reconnect view = %+v, want completed at current revision 3", refetched)
	}
	stale := harness.DeliverEvents([]SMBLogicalEvent{events[0]})
	if stale.MaxRevision != 3 {
		t.Fatalf("stale redelivery report = %+v, want subscriber held at revision 3", stale)
	}
	if rev := harness.SubscriberRevision(party.ResourceScope); rev != 3 {
		t.Fatalf("subscriber revision rolled back to %d, want 3", rev)
	}
}

// sharedCore002Store is a durable-across-harnesses Store backed by one shared
// kernel: the store-backed restart analog without PostgreSQL.
type sharedCore002Store struct{ r *Registry }

func (s sharedCore002Store) Reserve(req Request) (Resolution, error) {
	return s.r.Reserve(req)
}

func (s sharedCore002Store) Complete(identity Identity, digest, resultRef, effectRef string, now time.Time) (Record, error) {
	return s.r.Complete(identity, digest, resultRef, effectRef, now)
}

func (s sharedCore002Store) Lookup(identity Identity) (Record, error) {
	return s.r.Lookup(identity)
}

func TestTodo_SMB_CORE_002_Recovery(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	harness, party, _ := smbCore002Harness(t, now)
	first, err := harness.Execute(smbCore002Command(party, "op-note-1", `{"note":"hello"}`, 0, now))
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := harness.Export()
	if err != nil {
		t.Fatal(err)
	}
	restored := NewSMBConformanceHarness(NewRegistry(), now, 7)
	restored.IssueMachineToken("machine-token-1")
	if err := restored.Import(snapshot); err != nil {
		t.Fatal(err)
	}
	replay, err := restored.Execute(smbCore002Command(party, "op-note-1", `{"note":"hello"}`, 0, now.Add(time.Minute)))
	if err != nil || replay.Decision != Replay || replay.OperationRef != first.OperationRef {
		t.Fatalf("post-restore retry = %+v, err=%v; want the original operation, no duplicate work", replay, err)
	}
	if got := restored.FixtureMutations(party.ResourceScope); got != 1 {
		t.Fatalf("post-restore mutations = %d, want exactly one", got)
	}
	if events := restored.Events(); len(events) != 1 || events[0].EventID != first.EventID {
		t.Fatalf("post-restore events = %+v, want the single original logical event", events)
	}
	if err := restored.Import([]byte("{not-json")); err == nil {
		t.Fatal("corrupt snapshot import succeeded, want refusal")
	}

	staleCmd := smbCore002Command(party, "op-note-2", `{"note":"restored stale"}`, 1, now.Add(2*time.Minute))
	staleCmd.ControlEpoch = 6
	if _, err := restored.Execute(staleCmd); !errors.Is(err, ErrSMBEpochFenced) {
		t.Fatalf("stale-epoch command err=%v, want fenced refusal, never fresh work", err)
	}
	if rev := restored.CurrentRevision(party.ResourceScope); rev != 1 {
		t.Fatalf("fenced command moved revision to %d, want 1", rev)
	}
	fenced := restored.ResolveOperationView(party.TenantID, party.ResourceScope, "op-note-2", now.Add(2*time.Minute))
	if fenced.Status != SMBViewFenced || fenced.OperationRef == "" {
		t.Fatalf("fenced view = %+v, want an observable fenced status with a safe reference", fenced)
	}

	// rev-smb-b FINDING 1: a store-backed restart must not lose the revision
	// base. Replay reports the durable revision; new work fails closed until a
	// snapshot restores the base.
	shared := NewRegistry()
	emptyBase, err := NewSMBConformanceHarness(NewRegistry(), now, 7).Export()
	if err != nil {
		t.Fatal(err)
	}
	h1 := NewSMBConformanceHarness(NewRegistryWithStore(sharedCore002Store{shared}), now, 7)
	if err := h1.Import(emptyBase); err != nil {
		t.Fatal(err)
	}
	if err := h1.RegisterFixture(party); err != nil {
		t.Fatal(err)
	}
	h1.IssueMachineToken("machine-token-1")
	h1cmd := smbCore002Command(party, "op-restart-1", `{"note":"v1"}`, 0, now.Add(3*time.Minute))
	if _, err := h1.Execute(h1cmd); err != nil {
		t.Fatal(err)
	}
	h2 := NewSMBConformanceHarness(NewRegistryWithStore(sharedCore002Store{shared}), now, 7)
	if err := h2.RegisterFixture(party); err != nil {
		t.Fatal(err)
	}
	h2.IssueMachineToken("machine-token-1")
	if rev := h2.CurrentRevision(party.ResourceScope); rev != 0 {
		t.Fatalf("fresh harness revision = %d, want the unrestored 0", rev)
	}
	h2replay, err := h2.Execute(smbCore002Command(party, "op-restart-1", `{"note":"v1"}`, 0, now.Add(4*time.Minute)))
	if err != nil || h2replay.Decision != Replay || h2replay.Revision != 1 {
		t.Fatalf("store-backed replay = %+v, err=%v; want the original revision 1", h2replay, err)
	}
	if _, err := h2.Execute(smbCore002Command(party, "op-restart-2", `{"note":"v2"}`, 0, now.Add(4*time.Minute))); !errors.Is(err, ErrSMBBaseNotRestored) {
		t.Fatalf("unrestored new work err=%v, want base-not-restored refusal", err)
	}
	h1snap, err := h1.Export()
	if err != nil {
		t.Fatal(err)
	}
	if err := h2.Import(h1snap); err != nil {
		t.Fatal(err)
	}
	if _, err := h2.Execute(smbCore002Command(party, "op-restart-2", `{"note":"v2"}`, 0, now.Add(5*time.Minute))); !errors.Is(err, ErrSMBRevisionConflict) {
		t.Fatalf("stale new work err=%v, want revision-conflict refusal", err)
	}
}

func TestTodo_SMB_CORE_002_Browser(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	harness, party, _ := smbCore002Harness(t, now)

	pending := harness.ResolveOperationView(party.TenantID, party.ResourceScope, "op-unknown", now)
	if pending.Status != SMBViewPending || pending.OperationRef == "" || !containsString(pending.Actions, "Refresh") {
		t.Fatalf("unknown-operation view = %+v, want pending with reference and Refresh", pending)
	}

	first, err := harness.Execute(smbCore002Command(party, "op-note-1", `{"note":"hello","priority":"high"}`, 0, now))
	if err != nil {
		t.Fatal(err)
	}
	completed := harness.ResolveOperationView(party.TenantID, party.ResourceScope, "op-note-1", now.Add(time.Minute))
	if completed.Status != SMBViewCompleted || completed.OperationRef != first.OperationRef || completed.Revision != 1 {
		t.Fatalf("completed view = %+v, want completed at revision 1 with the operation reference", completed)
	}

	if _, err := harness.Execute(smbCore002Command(party, "op-note-1", `{"note":"hello","priority":"low"}`, 0, now.Add(2*time.Minute))); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict setup err=%v, want key-payload conflict", err)
	}
	conflicted := harness.ResolveOperationView(party.TenantID, party.ResourceScope, "op-note-1", now.Add(2*time.Minute))
	if conflicted.Status != SMBViewConflict || conflicted.SafeReference == "" {
		t.Fatalf("conflict view = %+v, want conflict status with a safe reference", conflicted)
	}
	if strings.Contains(conflicted.SafeReference, "low") || strings.Contains(conflicted.Note, "low") {
		t.Fatalf("conflict view leaks the other payload: %+v", conflicted)
	}

	staleCmd := smbCore002Command(party, "op-note-2", `{"note":"stale"}`, 0, now.Add(3*time.Minute))
	if _, err := harness.Execute(staleCmd); !errors.Is(err, ErrSMBRevisionConflict) {
		t.Fatalf("stale setup err=%v, want revision-conflict refusal", err)
	}
	stale := harness.ResolveOperationView(party.TenantID, party.ResourceScope, "op-note-2", now.Add(3*time.Minute))
	if stale.Status != SMBViewStaleRevision || !containsString(stale.Actions, "Reload") || !containsString(stale.Actions, "Review again") {
		t.Fatalf("stale view = %+v, want stale-revision status with Reload and Review again", stale)
	}
	if len(stale.ChangedFields) == 0 {
		t.Fatalf("stale view = %+v, want the changed fields listed", stale)
	}

	harness.DisconnectStream()
	offline := harness.ResolveOperationView(party.TenantID, party.ResourceScope, "op-note-3", now.Add(4*time.Minute))
	if offline.Status != SMBViewDisconnected || !containsString(offline.Actions, "Refetch") || offline.Freshness.IsZero() {
		t.Fatalf("disconnected view = %+v, want freshness with explicit refetch", offline)
	}
}
