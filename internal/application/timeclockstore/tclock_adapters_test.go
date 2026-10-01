package timeclockstore

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	clock "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/pressly/goose/v3"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func adapterFixture(t *testing.T) (*timestore.Store, string) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(timestore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := timestore.New(context.Background(), timestore.Config{DSN: db.URL, Schema: db.Schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, "tenant-adapter"
}

func TestProductionPorts_MapAllDurableClockSeams(t *testing.T) {
	store, tenant := adapterFixture(t)
	ports, err := NewPorts(store)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// at sits just after the DB-stamped issue time: a code is not redeemable before it was issued.
	at := time.Now().UTC().Truncate(time.Second).Add(30 * time.Minute)

	if err := ports.Enrollments.CreateEnrollmentCode(ctx, tenant, "pair-code", "site-1", "profile-1", "UTC", "admin", at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	device, err := ports.Enrollments.RedeemEnrollmentCode(ctx, tenant, "pair-code", "device-1", []byte("public-key"), "admin", at)
	if err != nil || device.SiteID != "site-1" {
		t.Fatalf("device=%+v err=%v", device, err)
	}
	gotDevice, err := ports.Devices.GetDevice(ctx, tenant, "device-1")
	if err != nil || gotDevice.ID != "device-1" {
		t.Fatalf("get device=%+v err=%v", gotDevice, err)
	}
	gotDevice, err = ports.Devices.SuspendDevice(ctx, tenant, "device-1", "admin", "maintenance", gotDevice.Revision)
	if err != nil || gotDevice.State != timestore.DeviceStateSuspended {
		t.Fatalf("suspend=%+v err=%v", gotDevice, err)
	}
	if devices, err := ports.Devices.DevicesBySite(ctx, tenant, "site-1", 10); err != nil || len(devices) != 1 {
		t.Fatalf("devices=%+v err=%v", devices, err)
	}
	gotDevice, err = ports.Devices.ResumeDevice(ctx, tenant, "device-1", "admin", "resume", gotDevice.Revision)
	if err != nil {
		t.Fatal(err)
	}
	gotDevice, err = ports.Devices.ReassignDeviceSite(ctx, tenant, "device-1", "site-2", "America/New_York", "admin", "move", gotDevice.Revision)
	if err != nil || gotDevice.SiteID != "site-2" {
		t.Fatalf("reassign=%+v err=%v", gotDevice, err)
	}
	gotDevice, err = ports.Devices.RotateDeviceKey(ctx, tenant, "device-1", []byte("new-key"), "admin", "rotate", gotDevice.Revision)
	if err != nil {
		t.Fatal(err)
	}

	session, err := ports.Sessions.OpenSession(ctx, tenant, clockservice.SessionRecord{ID: "session-1", TenantID: tenant, WorkerRef: "worker-1", AssignmentRef: "assignment-1", Status: "OPEN", Source: "KIOSK", OpenedAt: at, Payload: []byte(`{}`)}, clockservice.SessionEvent{Kind: "OPENED", ActorRef: "device-1", IdempotencyKey: "open-1", Digest: "digest-1"})
	if err != nil {
		t.Fatal(err)
	}
	if session.Revision != 1 {
		t.Fatalf("session revision=%d", session.Revision)
	}
	session, err = ports.Sessions.ApplyTransition(ctx, tenant, session.ID, session.Revision, clockservice.SessionRecord{ID: session.ID, TenantID: tenant, WorkerRef: "worker-1", AssignmentRef: "assignment-1", Status: "CLOSED", Source: "KIOSK", ClosedAt: at.Add(time.Hour), Payload: []byte(`{}`)}, []clockservice.SessionEvent{{Kind: "CLOSED", ActorRef: "device-1", IdempotencyKey: "close-1", Digest: "close-digest"}})
	if err != nil || session.Status != "CLOSED" {
		t.Fatalf("transition=%+v err=%v", session, err)
	}
	if _, err := ports.Sessions.CurrentSession(ctx, tenant, "worker-1", "assignment-1"); !errors.Is(err, timestore.ErrNotFound) {
		t.Fatalf("closed current session err=%v, want ErrNotFound", err)
	}

	obs, duplicate, err := ports.Observations.AppendObservation(ctx, tenant, clockservice.ObservationRecord{ID: "obs-1", TenantID: tenant, WorkerRef: "worker-1", AssignmentRef: "assignment-1", DeviceRef: "device-1", Source: "KIOSK", EventType: "IN", IdempotencyKey: "obs-key", Digest: "obs-digest", OccurredAt: at, ReceivedAt: at.Add(time.Second), Payload: json.RawMessage(`{}`)})
	if err != nil || duplicate || obs.ID != "obs-1" {
		t.Fatalf("observation=%+v duplicate=%v err=%v", obs, duplicate, err)
	}
	if _, _, err := ports.Observations.ListObservations(ctx, tenant, "worker-1", time.Time{}, time.Time{}, "", 10); err != nil {
		t.Fatal(err)
	}

	receipts, cursor, err := ports.Receipts.RecordBatch(ctx, tenant, "device-1", []clockservice.ReceiptRecord{{DeviceSequence: 1, Status: "ACCEPTED", ObservationID: obs.ID}})
	if err != nil || cursor != 1 || len(receipts) != 1 {
		t.Fatalf("receipts=%+v cursor=%d err=%v", receipts, cursor, err)
	}
	if got, err := ports.Receipts.DeviceCursor(ctx, tenant, "device-1"); err != nil || got != 1 {
		t.Fatalf("cursor=%d err=%v", got, err)
	}
	legacy := Adapter{Store: store}
	if receipt, found, err := legacy.LookupPunchReceipt(ctx, tenant, "device-1", 1); err != nil || !found || receipt.ObservationID != obs.ID {
		t.Fatalf("lookup receipt=%+v found=%v err=%v", receipt, found, err)
	}
	if events, err := legacy.ListEvents(ctx, tenant, 0, 20); err != nil || len(events) == 0 {
		t.Fatalf("events=%+v err=%v", events, err)
	}

	cred, err := ports.Credentials.IssuePINCredential(ctx, tenant, "worker-1", "1234", "admin")
	if err != nil || cred.Kind != timestore.CredentialPIN {
		t.Fatalf("credential=%+v err=%v", cred, err)
	}
	if ok, err := ports.Credentials.VerifyPIN(ctx, tenant, "worker-1", "1234"); err != nil || !ok {
		t.Fatalf("verify ok=%v err=%v", ok, err)
	}
	if _, err := ports.Credentials.RecordFailedWorkerAttempt(ctx, tenant, "worker-1", 3, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if state, err := ports.Credentials.WorkerLockout(ctx, tenant, "worker-1"); err != nil || state.FailedCount != 1 {
		t.Fatalf("lockout=%+v err=%v", state, err)
	}
	if _, err := ports.Credentials.IssueBadgeCredential(ctx, tenant, "worker-1", "badge-1", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := ports.Credentials.IssueQRCredential(ctx, tenant, "worker-1", "qr-1", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := ports.Credentials.RecordFailedDeviceAttempt(ctx, tenant, "device-1", 3, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := ports.Credentials.DeviceLockout(ctx, tenant, "device-1"); err != nil {
		t.Fatal(err)
	}
	if err := ports.Credentials.ResetDeviceAttempts(ctx, tenant, "device-1"); err != nil {
		t.Fatal(err)
	}
	if err := ports.Credentials.ResetWorkerAttempts(ctx, tenant, "worker-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := ports.Credentials.RecordSupervisorOverride(ctx, tenant, "device-1", "worker-1", "supervisor-cred", "approved"); err != nil {
		t.Fatal(err)
	}
	if _, err := ports.Credentials.RevokeCredential(ctx, tenant, "worker-1", timestore.CredentialBadge, "admin", 1); err != nil {
		t.Fatal(err)
	}

	if err := ports.Heartbeats.RecordHeartbeat(ctx, tenant, clockservice.HeartbeatRecord{DeviceID: "device-1", AppVersion: "1.0", QueueDepth: 0, BatteryPercent: 87, HasBatteryPercent: true, ObservedAt: at}); err != nil {
		t.Fatal(err)
	}
	hb, err := ports.Heartbeats.LatestHeartbeat(ctx, tenant, "device-1")
	if err != nil || hb.AppVersion != "1.0" {
		t.Fatalf("heartbeat=%+v err=%v", hb, err)
	}
	if !hb.HasBatteryPercent || hb.BatteryPercent != 87 {
		t.Fatalf("battery=%+v", hb)
	}

	// Proof-bound enrollment validates the injected registry before atomic use.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := clock.NewProfileRegistry([]clock.IntegrationProfile{{Class: clock.SourceManagedKiosk, Transport: "https", Authentication: "ed25519", TrustCeiling: clock.TrustCeilingMedium, PermittedMethods: []clock.IdentificationMethod{clock.MethodPIN}, FirstPartner: "test", Version: "v1"}})
	if err != nil {
		t.Fatal(err)
	}
	reg := staticRegistry{registry: clock.VerifiedProfileRegistry{Registry: registry, ValidFrom: at.Add(-time.Hour), ValidUntil: at.Add(time.Hour)}}
	proofCode := "proof-code"
	if err := ports.Enrollments.CreateEnrollmentCode(ctx, tenant, proofCode, "site-1", string(clock.SourceManagedKiosk), "UTC", "admin", at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	proof := clock.KeyPossessionProof{PublicKey: pub, Challenge: []byte(proofCode), Signature: ed25519.Sign(priv, []byte(proofCode))}
	proofAdapter := EnrollmentAdapter{Store: store, Registry: reg, Clock: func() time.Time { return at }}
	if _, err := proofAdapter.RedeemEnrollment(ctx, tenant, proofCode, "proof-device", proof, "admin", at); err != nil {
		t.Fatal(err)
	}
	if _, err := proofAdapter.RedeemEnrollment(ctx, tenant, proofCode, "proof-device-2", proof, "admin", at); err == nil {
		t.Fatal("reused enrollment code accepted")
	}
	badCode := "bad-proof-code"
	if err := ports.Enrollments.CreateEnrollmentCode(ctx, tenant, badCode, "site-1", string(clock.SourceManagedKiosk), "UTC", "admin", at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	wrong := proof
	wrong.Challenge = []byte(badCode)
	if _, err := proofAdapter.RedeemEnrollment(ctx, tenant, badCode, "bad-device", wrong, "admin", at); err == nil {
		t.Fatal("invalid proof accepted")
	}
	expired := "expired-code"
	if err := ports.Enrollments.CreateEnrollmentCode(ctx, tenant, expired, "site-1", string(clock.SourceManagedKiosk), "UTC", "admin", at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := proofAdapter.RedeemEnrollment(ctx, tenant, expired, "expired-device", proof, "admin", at.Add(2*time.Minute)); err == nil {
		t.Fatal("expired code accepted")
	}
}

type staticRegistry struct{ registry clock.VerifiedProfileRegistry }

func (s staticRegistry) Resolve(context.Context, string, time.Time) (clock.VerifiedProfileRegistry, error) {
	return s.registry, nil
}

func TestNewPorts_RejectsNilStore(t *testing.T) {
	if _, err := NewPorts(nil); err != ErrNilStore {
		t.Fatalf("err=%v, want ErrNilStore", err)
	}
}

func TestAdapters_FailClosedWithoutStore(t *testing.T) {
	ctx := context.Background()
	if _, err := (SessionAdapter{}).OpenSession(ctx, "t", clockservice.SessionRecord{}, clockservice.SessionEvent{}); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (SessionAdapter{}).ApplyTransition(ctx, "t", "s", 1, clockservice.SessionRecord{}, nil); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (SessionAdapter{}).CurrentSession(ctx, "t", "w", "a"); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, _, err := (ObservationAdapter{}).AppendObservation(ctx, "t", clockservice.ObservationRecord{}); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, _, err := (ObservationAdapter{}).ListObservations(ctx, "t", "w", time.Time{}, time.Time{}, "", 1); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, _, err := (ReceiptAdapter{}).RecordBatch(ctx, "t", "d", nil); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (ReceiptAdapter{}).DeviceCursor(ctx, "t", "d"); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (DeviceAdapter{}).GetDevice(ctx, "t", "d"); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (DeviceAdapter{}).DevicesBySite(ctx, "t", "s", 1); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (DeviceAdapter{}).RotateDeviceKey(ctx, "t", "d", []byte("k"), "a", "r", 1); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (DeviceAdapter{}).SuspendDevice(ctx, "t", "d", "a", "r", 1); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (DeviceAdapter{}).ResumeDevice(ctx, "t", "d", "a", "r", 1); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (DeviceAdapter{}).RevokeDevice(ctx, "t", "d", "a", "r", 1); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (DeviceAdapter{}).ReassignDeviceSite(ctx, "t", "d", "s", "UTC", "a", "r", 1); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if err := (EnrollmentAdapter{}).CreateEnrollmentCode(ctx, "t", "c", "s", "p", "UTC", "a", time.Now()); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (EnrollmentAdapter{}).RedeemEnrollmentCode(ctx, "t", "c", "d", []byte("k"), "a", time.Now()); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, _, err := (EnrollmentAdapter{}).LoadEnrollment(ctx, "t", "c"); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, _, err := (EnrollmentAdapter{}).LoadEnrollmentAt(ctx, "t", "c", time.Now()); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (EnrollmentAdapter{}).RedeemEnrollment(ctx, "t", "c", "d", clock.KeyPossessionProof{}, "a", time.Now()); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (CredentialAdapter{}).IssuePINCredential(ctx, "t", "w", "p", "a"); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (CredentialAdapter{}).IssueBadgeCredential(ctx, "t", "w", "b", "a"); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (CredentialAdapter{}).IssueQRCredential(ctx, "t", "w", "q", "a"); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (CredentialAdapter{}).RevokeCredential(ctx, "t", "w", "PIN", "a", 1); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (CredentialAdapter{}).VerifyPIN(ctx, "t", "w", "p"); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (CredentialAdapter{}).RecordFailedDeviceAttempt(ctx, "t", "d", 1, time.Now()); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (CredentialAdapter{}).RecordFailedWorkerAttempt(ctx, "t", "w", 1, time.Now()); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if err := (CredentialAdapter{}).ResetDeviceAttempts(ctx, "t", "d"); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if err := (CredentialAdapter{}).ResetWorkerAttempts(ctx, "t", "w"); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (CredentialAdapter{}).DeviceLockout(ctx, "t", "d"); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (CredentialAdapter{}).WorkerLockout(ctx, "t", "w"); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (CredentialAdapter{}).RecordSupervisorOverride(ctx, "t", "d", "w", "c", "r"); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if err := (HeartbeatAdapter{}).RecordHeartbeat(ctx, "t", clockservice.HeartbeatRecord{}); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
	if _, err := (HeartbeatAdapter{}).LatestHeartbeat(ctx, "t", "d"); err != clockservice.ErrUnavailable {
		t.Fatal(err)
	}
}

func TestMappingHelpers_CopyDurableValues(t *testing.T) {
	at := time.Unix(10, 0).UTC()
	s := clockservice.SessionRecord{ID: "s", TenantID: "t", WorkerRef: "w", AssignmentRef: "a", Status: "OPEN", Source: "KIOSK", Revision: 2, OpenedAt: at, Payload: []byte("payload")}
	row := sessionRow(s)
	s.Payload[0] = 'x'
	if row.ID != s.ID || string(row.Payload) != "payload" {
		t.Fatalf("session row=%+v", row)
	}
	e := eventRow("s", clockservice.SessionEvent{Kind: "OPENED", ActorRef: "d", Payload: []byte("event")})
	if e.SessionID != "s" || string(e.Payload) != "event" {
		t.Fatalf("event row=%+v", e)
	}
	d := deviceRecord(timestore.Device{TenantID: "t", ID: "d", PublicKey: []byte("key"), Revision: 1})
	if d.ID != "d" || string(d.PublicKey) != "key" {
		t.Fatalf("device=%+v", d)
	}
	c := credentialRecord(timestore.Credential{ID: "c", WorkerID: "w", Kind: "PIN", State: "ACTIVE", Revision: 1})
	if c.ID != "c" || c.WorkerID != "w" {
		t.Fatalf("credential=%+v", c)
	}
}
