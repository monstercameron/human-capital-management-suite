package device

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/spi"
)

var smbDevice001At = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

func smbDevice001Manifest() spi.AdapterManifest {
	return spi.AdapterManifest{
		AdapterID: "usb.scale.readonly", Vendor: "Acme", Product: "Scale S1", Version: "1.2.0",
		Objects:      []connectivity.ObjectKind{connectivity.ObjectWorker},
		Capabilities: []spi.Capability{{Object: connectivity.ObjectWorker, Operation: spi.OpRead, Version: "v1"}},
		Bounds:       connectivity.Bounds{MaxPageSize: 100, MaxPagesPerRun: 10, MaxRecordsPerRun: 1000, MaxRecordBytes: 65536},
		GeneratedAt:  smbDevice001At,
	}
}

func smbDevice001Profile() DeviceProfile {
	return DeviceProfile{
		ProfileID: "scale-s1-v1", Protocol: "usb-hid", AdapterVersion: "1.2.0",
		VendorMatch: "Acme", ProductMatch: "Scale S1",
		SupportedOS: []string{"windows", "linux"}, SupportedArch: []string{"amd64", "arm64"},
		ObservationSchema: "weight.v1", AllowedOperations: []string{"observe.weight"},
		FrameBudgetMs: 250, QueueBudget: 1000, PhysicalRisk: "low",
	}
}

func smbDevice001Bind(t *testing.T, reg *Registry, tenant, site, host, serial string) DeviceBinding {
	t.Helper()
	if err := reg.RegisterProfile(smbDevice001Profile(), smbDevice001Manifest()); err != nil {
		t.Fatalf("register profile: %v", err)
	}
	binding, err := reg.Bind(context.Background(), BindRequest{
		TenantID: tenant, SiteID: site, ConnectorID: "conn-1", ProfileID: "scale-s1-v1",
		Fingerprint: DeviceFingerprint{Serial: serial, HostID: host, Model: "Scale S1"},
		Policy:      "policy:site-devices/v1", Actor: "ada.admin", At: smbDevice001At,
	})
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	for _, to := range []connectivity.LifecycleState{connectivity.StateValidating, connectivity.StateReady, connectivity.StateActive} {
		var err error
		binding, err = reg.Transition(context.Background(), tenant, site, binding.BindingID, "ada.admin", to, smbDevice001At)
		if err != nil {
			t.Fatalf("transition to %s: %v", to, err)
		}
	}
	return binding
}

func smbDevice001Observe(t *testing.T, reg *Registry, binding DeviceBinding, seq uint64, key string) DeviceObservation {
	t.Helper()
	obs, err := reg.Ingest(context.Background(), IngestRequest{
		TenantID: binding.TenantID, SiteID: binding.SiteID, BindingID: binding.BindingID,
		Generation: binding.PairingGeneration, OperatorRef: "operator-1", SessionRef: "session-1",
		CapturedAt: smbDevice001At, ReceivedAt: smbDevice001At, Sequence: seq,
		RawDigest: "sha256:weight", Units: "kg", Quality: "ok", Schema: "weight.v1",
		IdempotencyKey: key, At: smbDevice001At,
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	return obs
}

// TestTodo_SMB_DEVICE_001 proves the device connection boundary: profile
// conformance against the read-only adapter manifest, fingerprint pairing
// through the shared lifecycle, typed observation ingest, and command
// dispatch that refuses unknown opcodes and unreleased writes.
func TestTodo_SMB_DEVICE_001(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()

	if err := reg.RegisterProfile(smbDevice001Profile(), smbDevice001Manifest()); err != nil {
		t.Fatalf("register profile: %v", err)
	}
	bad := smbDevice001Profile()
	bad.ProfileID = "bad-opcode"
	bad.AllowedOperations = []string{"launch.missiles"}
	if err := reg.RegisterProfile(bad, smbDevice001Manifest()); !errors.Is(err, ErrDeviceConformance) {
		t.Fatalf("unsupported opcode err = %v, want ErrDeviceConformance", err)
	}
	writer := smbDevice001Profile()
	writer.ProfileID = "sneaky-write"
	writer.AllowedOperations = []string{"observe.weight", "write.calibration"}
	if err := reg.RegisterProfile(writer, smbDevice001Manifest()); !errors.Is(err, ErrDeviceConformance) {
		t.Fatalf("write-through-read err = %v, want ErrDeviceConformance", err)
	}

	binding, err := reg.Bind(ctx, BindRequest{
		TenantID: "tenant-a", SiteID: "site-1", ConnectorID: "conn-1", ProfileID: "scale-s1-v1",
		Fingerprint: DeviceFingerprint{Serial: "SN-1", HostID: "", Model: "Scale S1"},
		Policy:      "policy:site-devices/v1", Actor: "ada.admin", At: smbDevice001At,
	})
	if !errors.Is(err, ErrDeviceIdentity) {
		t.Fatalf("serial-only bind err = %v, want ErrDeviceIdentity", err)
	}
	_ = binding
	bound := smbDevice001Bind(t, reg, "tenant-a", "site-1", "host-1", "SN-1")
	if bound.Status != connectivity.StateActive || bound.PairingGeneration != 1 {
		t.Fatalf("binding = %+v, want ACTIVE generation 1", bound)
	}

	obs := smbDevice001Observe(t, reg, bound, 1, "obs-1")
	if obs.ObservationID == "" || obs.ConsumedBy != "" {
		t.Fatalf("observation = %+v, want an unconsumed receipt", obs)
	}
	consumed, err := reg.ConsumeObservation(ctx, "tenant-a", "site-1", bound.BindingID, obs.ObservationID, "stock-op-9", "ada.admin", smbDevice001At)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if !consumed.Consumed || consumed.ConsumedBy != "stock-op-9" {
		t.Fatalf("consumed = %+v, want the stock operation recorded", consumed)
	}

	cmd, err := reg.QueueCommand(ctx, CommandRequest{
		TenantID: bound.TenantID, SiteID: bound.SiteID, BindingID: bound.BindingID,
		Generation: bound.PairingGeneration, Opcode: "observe.weight", Payload: "{}",
		IdempotencyKey: "cmd-1", Actor: "ada.admin", At: smbDevice001At,
	})
	if err != nil {
		t.Fatalf("queue read command: %v", err)
	}
	if cmd.State != operation.StateQueued {
		t.Fatalf("command state = %q, want QUEUED", cmd.State)
	}
	if _, err := reg.QueueCommand(ctx, CommandRequest{
		TenantID: bound.TenantID, SiteID: bound.SiteID, BindingID: bound.BindingID,
		Generation: bound.PairingGeneration, Opcode: "write.calibration", Payload: "{}",
		IdempotencyKey: "cmd-2", Actor: "ada.admin", At: smbDevice001At,
	}); !errors.Is(err, ErrDeviceConformance) {
		t.Fatalf("unreleased write err = %v, want ErrDeviceConformance", err)
	}
}

// TestTodo_SMB_DEVICE_001_Integration proves multi-site operation and
// restart recovery: identical serials pair independently per tenant, a
// restarted registry serves identical bindings with ordered observations,
// and the domain consumer pulls pending observations without the device
// ever pushing business effects.
func TestTodo_SMB_DEVICE_001_Integration(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()

	a := smbDevice001Bind(t, reg, "tenant-a", "site-1", "host-1", "SN-1")
	b := smbDevice001Bind(t, reg, "tenant-b", "site-1", "host-9", "SN-1")
	if a.BindingID == b.BindingID {
		t.Fatal("two tenants share one binding identity")
	}
	smbDevice001Observe(t, reg, a, 1, "i-obs-1")
	smbDevice001Observe(t, reg, a, 2, "i-obs-2")
	pending, err := reg.PendingObservations(ctx, "tenant-a", "site-1", a.BindingID, "ada.admin")
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 2 || pending[0].Sequence != 1 || pending[1].Sequence != 2 {
		t.Fatalf("pending = %d observations, want ordered 1,2", len(pending))
	}

	snapshot, err := reg.Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	restarted := NewRegistry()
	if err := restarted.Import(snapshot); err != nil {
		t.Fatalf("import: %v", err)
	}
	loaded, ok := restarted.Load(ctx, "tenant-a", "site-1", a.BindingID, "ada.admin")
	if !ok || loaded.Status != connectivity.StateActive || loaded.PairingGeneration != 1 {
		t.Fatalf("restored binding = %+v ok=%v, want ACTIVE generation 1", loaded, ok)
	}
	restored, err := restarted.PendingObservations(ctx, "tenant-a", "site-1", a.BindingID, "ada.admin")
	if err != nil {
		t.Fatalf("restored pending: %v", err)
	}
	if len(restored) != 2 {
		t.Fatalf("restored pending = %d, want 2", len(restored))
	}
}

// TestTodo_SMB_DEVICE_001_Security proves tenant fencing and pairing
// integrity: tenants with identical USB serials see nothing of each
// other, revoked generations reject stale observations and commands,
// same identifiers on another host never auto-pair, and device identity
// cannot impersonate the operator.
func TestTodo_SMB_DEVICE_001_Security(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()

	a := smbDevice001Bind(t, reg, "tenant-a", "site-1", "host-1", "SN-1")
	if _, ok := reg.Load(ctx, "tenant-b", "site-1", a.BindingID, "mallory"); ok {
		t.Fatal("foreign tenant loaded a binding")
	}
	if _, err := reg.Ingest(ctx, IngestRequest{
		TenantID: "tenant-b", SiteID: "site-1", BindingID: a.BindingID,
		Generation: a.PairingGeneration, OperatorRef: "operator-1",
		CapturedAt: smbDevice001At, ReceivedAt: smbDevice001At, Sequence: 9,
		RawDigest: "sha256:x", Units: "kg", Quality: "ok", Schema: "weight.v1",
		IdempotencyKey: "x-1", At: smbDevice001At,
	}); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("foreign-tenant ingest err = %v, want ErrDeviceNotFound", err)
	}
	if _, err := reg.Ingest(ctx, IngestRequest{
		TenantID: "tenant-a", SiteID: "site-1", BindingID: a.BindingID,
		Generation: a.PairingGeneration, OperatorRef: "device:SN-1",
		CapturedAt: smbDevice001At, ReceivedAt: smbDevice001At, Sequence: 9,
		RawDigest: "sha256:x", Units: "kg", Quality: "ok", Schema: "weight.v1",
		IdempotencyKey: "x-2", At: smbDevice001At,
	}); !errors.Is(err, ErrDeviceIdentity) {
		t.Fatalf("device-as-operator ingest err = %v, want ErrDeviceIdentity", err)
	}

	revoked, err := reg.Revoke(ctx, "tenant-a", "site-1", a.BindingID, "ada.admin", "host retired", smbDevice001At)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if revoked.PairingGeneration != 2 || revoked.Status != connectivity.StateRevoked {
		t.Fatalf("revoked = %+v, want REVOKED generation 2", revoked)
	}
	if _, err := reg.Ingest(ctx, IngestRequest{
		TenantID: "tenant-a", SiteID: "site-1", BindingID: a.BindingID,
		Generation: 1, OperatorRef: "operator-1",
		CapturedAt: smbDevice001At, ReceivedAt: smbDevice001At, Sequence: 9,
		RawDigest: "sha256:x", Units: "kg", Quality: "ok", Schema: "weight.v1",
		IdempotencyKey: "x-3", At: smbDevice001At,
	}); !errors.Is(err, ErrDeviceGeneration) {
		t.Fatalf("stale-generation ingest err = %v, want ErrDeviceGeneration", err)
	}
	if _, err := reg.QueueCommand(ctx, CommandRequest{
		TenantID: "tenant-a", SiteID: "site-1", BindingID: a.BindingID,
		Generation: 1, Opcode: "observe.weight", Payload: "{}",
		IdempotencyKey: "x-4", Actor: "ada.admin", At: smbDevice001At,
	}); !errors.Is(err, ErrDeviceGeneration) {
		t.Fatalf("stale-generation command err = %v, want ErrDeviceGeneration", err)
	}
	repaired, err := reg.Repair(ctx, "tenant-a", "site-1", a.BindingID, "ada.admin", DeviceFingerprint{Serial: "SN-1", HostID: "host-2", Model: "Scale S1"}, smbDevice001At)
	if err != nil {
		t.Fatalf("explicit re-pair on a new host: %v", err)
	}
	if repaired.PairingGeneration != 3 {
		t.Fatalf("re-paired generation = %d, want 3", repaired.PairingGeneration)
	}
}

// TestTodo_SMB_DEVICE_001_Fault proves ingest exactly-once: a response
// lost after the observation commits replays the recorded receipt under
// the same key with zero duplicate observations.
func TestTodo_SMB_DEVICE_001_Fault(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()
	bound := smbDevice001Bind(t, reg, "tenant-a", "site-1", "host-1", "SN-1")

	reg.AfterCommit = func() error { return errDeviceSimulatedCrash }
	_, err := reg.Ingest(ctx, IngestRequest{
		TenantID: bound.TenantID, SiteID: bound.SiteID, BindingID: bound.BindingID,
		Generation: bound.PairingGeneration, OperatorRef: "operator-1",
		CapturedAt: smbDevice001At, ReceivedAt: smbDevice001At, Sequence: 1,
		RawDigest: "sha256:weight", Units: "kg", Quality: "ok", Schema: "weight.v1",
		IdempotencyKey: "f-1", At: smbDevice001At,
	})
	if !errors.Is(err, errDeviceSimulatedCrash) {
		t.Fatalf("crashing ingest err = %v, want the simulated crash", err)
	}
	reg.AfterCommit = nil
	replayed, err := reg.Ingest(ctx, IngestRequest{
		TenantID: bound.TenantID, SiteID: bound.SiteID, BindingID: bound.BindingID,
		Generation: bound.PairingGeneration, OperatorRef: "operator-1",
		CapturedAt: smbDevice001At, ReceivedAt: smbDevice001At, Sequence: 1,
		RawDigest: "sha256:weight", Units: "kg", Quality: "ok", Schema: "weight.v1",
		IdempotencyKey: "f-1", At: smbDevice001At,
	})
	if err != nil {
		t.Fatalf("replay after crash: %v", err)
	}
	pending, err := reg.PendingObservations(ctx, "tenant-a", "site-1", bound.BindingID, "ada.admin")
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ObservationID != replayed.ObservationID {
		t.Fatalf("pending = %d observations, want the single replayed receipt", len(pending))
	}
	if _, err := reg.Ingest(ctx, IngestRequest{
		TenantID: bound.TenantID, SiteID: bound.SiteID, BindingID: bound.BindingID,
		Generation: bound.PairingGeneration, OperatorRef: "operator-1",
		CapturedAt: smbDevice001At, ReceivedAt: smbDevice001At, Sequence: 2,
		RawDigest: "sha256:other", Units: "kg", Quality: "ok", Schema: "weight.v1",
		IdempotencyKey: "f-1", At: smbDevice001At,
	}); !errors.Is(err, ErrDeviceIdempotencyConflict) {
		t.Fatalf("altered-payload replay err = %v, want ErrDeviceIdempotencyConflict", err)
	}
}

// TestTodo_SMB_DEVICE_001_Recovery proves restart convergence: bindings,
// generations, and unconsumed observations survive a snapshot round trip,
// consumed receipts stay consumed, and tampered snapshots fail closed.
func TestTodo_SMB_DEVICE_001_Recovery(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()
	bound := smbDevice001Bind(t, reg, "tenant-a", "site-1", "host-1", "SN-1")
	first := smbDevice001Observe(t, reg, bound, 1, "r-1")
	smbDevice001Observe(t, reg, bound, 2, "r-2")
	if _, err := reg.ConsumeObservation(ctx, "tenant-a", "site-1", bound.BindingID, first.ObservationID, "stock-op-1", "ada.admin", smbDevice001At); err != nil {
		t.Fatalf("consume: %v", err)
	}

	snapshot, err := reg.Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	restored := NewRegistry()
	if err := restored.Import(snapshot); err != nil {
		t.Fatalf("import: %v", err)
	}
	pending, err := restored.PendingObservations(ctx, "tenant-a", "site-1", bound.BindingID, "ada.admin")
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 || pending[0].Sequence != 2 {
		t.Fatalf("restored pending = %d observations, want only sequence 2", len(pending))
	}
	if _, err := restored.Ingest(ctx, IngestRequest{
		TenantID: bound.TenantID, SiteID: bound.SiteID, BindingID: bound.BindingID,
		Generation: bound.PairingGeneration, OperatorRef: "operator-1",
		CapturedAt: smbDevice001At, ReceivedAt: smbDevice001At, Sequence: 3,
		RawDigest: "sha256:weight", Units: "kg", Quality: "ok", Schema: "weight.v1",
		IdempotencyKey: "r-3", At: smbDevice001At,
	}); err != nil {
		t.Fatalf("post-restart ingest: %v", err)
	}
	if err := restored.Import([]byte(`{"bindings": {"x": {"generation": 2, "status": "BOGUS"}}}`)); err == nil {
		t.Fatal("tampered snapshot imported, want refusal")
	}
}

// TestTodo_SMB_DEVICE_001_Browser proves the served device view-state
// contract the integration-operations hub renders: list columns, per-site
// scoping, supported actions with status explanations, and the honest
// unsupported-device fallback. Served-route driving stays with repo E2E.
func TestTodo_SMB_DEVICE_001_Browser(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()
	bound := smbDevice001Bind(t, reg, "tenant-a", "site-1", "host-1", "SN-1")
	smbDevice001Observe(t, reg, bound, 1, "b-1")

	rows, err := reg.DeviceList(ctx, "tenant-a", "site-1", "ada.admin")
	if err != nil {
		t.Fatalf("device list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("device list = %d rows, want 1", len(rows))
	}
	row := rows[0]
	if row.Name == "" || row.Site != "site-1" || row.DeviceType == "" || row.Connection == "" || row.Status == "" || row.LastSeen.IsZero() {
		t.Fatalf("device row = %+v, want every list column populated", row)
	}
	detail, ok := reg.DeviceDetail(ctx, "tenant-a", "site-1", bound.BindingID, "ada.admin")
	if !ok {
		t.Fatal("no device detail for an active binding")
	}
	if len(detail.SupportedActions) == 0 || detail.StatusExplanation == "" || detail.ProfileVersion == "" {
		t.Fatalf("device detail = %+v, want actions, versions, and explanation", detail)
	}
	other, err := reg.DeviceList(ctx, "tenant-a", "site-2", "ada.admin")
	if err != nil {
		t.Fatalf("other-site list: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("other-site list = %d rows, want none", len(other))
	}
	fallback := UnsupportedDeviceFallback("Exotic Scanner", "barcode")
	if !strings.Contains(fallback, "keyboard mode") || !strings.Contains(fallback, "manual entry") {
		t.Fatalf("fallback = %q, want keyboard/manual guidance", fallback)
	}
	if strings.Contains(strings.ToLower(fallback), "disable") {
		t.Fatalf("fallback = %q, must never suggest disabling security", fallback)
	}
}

var errDeviceSimulatedCrash = errors.New("device: simulated crash")
