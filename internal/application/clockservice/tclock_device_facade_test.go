package clockservice

import (
	"context"
	"errors"
	"testing"
	"time"

	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
)

type facadeTokenVerifier struct {
	claims DeviceWorkerTokenClaims
	err    error
}

func (f facadeTokenVerifier) VerifyDeviceWorkerToken(context.Context, string) (DeviceWorkerTokenClaims, error) {
	return f.claims, f.err
}

type facadeStatusSource struct {
	claims DeviceWorkerTokenClaims
	value  WorkerStatusResult
}

type facadePunchContext struct{ calls int }

func (f *facadePunchContext) ResolvePunchContext(context.Context, string, string, string, time.Time) (string, string, error) {
	f.calls++
	return "w1", "authoritative-assignment", nil
}

type facadeCredentialResolver struct{ calls int }

func (f *facadeCredentialResolver) ResolveDeviceCredential(context.Context, string, string, clockdomain.IdentificationMethod, string) (string, error) {
	f.calls++
	return "worker-1", nil
}

func (f *facadeStatusSource) ResolveWorkerStatus(_ context.Context, claims DeviceWorkerTokenClaims) (WorkerStatusResult, error) {
	f.claims = claims
	return f.value, nil
}

func TestDeviceFacade_GetWorkerStatus_checksTokenScope(t *testing.T) {
	now := time.Unix(50, 0).UTC()
	claims := DeviceWorkerTokenClaims{TenantID: "tenant", DeviceID: "dev-1", WorkerID: "worker", IssuedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute)}
	source := &facadeStatusSource{value: WorkerStatusResult{WorkerID: "worker", SessionStatus: "OPEN"}}
	facade := DeviceFacade{Service: Service{Devices: batchDeviceFake{d: DeviceRecord{TenantID: "tenant", ID: "dev-1", SiteID: "site", ProfileID: "profile", Timezone: "UTC", State: "ACTIVE", Revision: 1, PublicKey: make([]byte, 32)}}, Clock: func() time.Time { return now }}, Tokens: facadeTokenVerifier{claims: claims}, WorkerStatus: source, Clock: func() time.Time { return now }}
	p := batchPrincipal(t)

	got, err := facade.GetWorkerStatus(context.Background(), p, WorkerStatusRequest{DeviceID: "dev-1", PunchToken: "opaque"})
	if err != nil || got.WorkerID != "worker" || source.claims.DeviceID != "dev-1" {
		t.Fatalf("status = %#v, err=%v, claims=%#v", got, err, source.claims)
	}
	if _, err := facade.GetWorkerStatus(context.Background(), p, WorkerStatusRequest{DeviceID: "other", PunchToken: "opaque"}); !errors.Is(err, ErrDeviceNotEligible) {
		t.Fatalf("wrong device err = %v, want ErrDeviceNotEligible", err)
	}
}

func TestDeviceFacade_GetWorkerStatus_rejectsExpiredToken(t *testing.T) {
	now := time.Unix(50, 0).UTC()
	claims := DeviceWorkerTokenClaims{TenantID: "tenant", DeviceID: "dev-1", WorkerID: "worker", IssuedAt: now.Add(-time.Minute), ExpiresAt: now}
	facade := DeviceFacade{Service: Service{Devices: batchDeviceFake{d: DeviceRecord{TenantID: "tenant", ID: "dev-1", SiteID: "site", ProfileID: "profile", Timezone: "UTC", State: "ACTIVE", Revision: 1, PublicKey: make([]byte, 32)}}, Clock: func() time.Time { return now }}, Tokens: facadeTokenVerifier{claims: claims}, WorkerStatus: &facadeStatusSource{}, Clock: func() time.Time { return now }}
	if _, err := facade.GetWorkerStatus(context.Background(), batchPrincipal(t), WorkerStatusRequest{DeviceID: "dev-1", PunchToken: "opaque"}); !errors.Is(err, ErrInvalidPrincipal) {
		t.Fatalf("expired token err = %v, want ErrInvalidPrincipal", err)
	}
}

func TestIdentificationMethod_acceptsApplicationVocabulary(t *testing.T) {
	for _, method := range []string{"PIN", "badge", "Qr", "SUPERVISOR_OVERRIDE"} {
		if _, err := identificationMethod(method); err != nil {
			t.Errorf("method %q: %v", method, err)
		}
	}
	if _, err := identificationMethod("face"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unsupported method err = %v, want ErrInvalidRequest", err)
	}
}

func TestDeviceFacade_SubmitPunches_resolvesContextWithoutMutatingWireInput(t *testing.T) {
	store := &batchCommitFake{}
	ctx := &facadePunchContext{}
	facade := DeviceFacade{Service: batchService(store), PunchContext: ctx, Clock: func() time.Time { return time.Unix(20, 0) }}
	input := BatchPunch{DeviceSequence: 1, EventType: timesession.PunchIn, WorkerCredentialRef: "opaque-token", AssignmentRef: "client-assignment", Source: "KIOSK", Method: timesession.IdentBadge, DeviceOccurredAt: time.Unix(15, 0), IdempotencyKey: "p1", Scheduled: true}
	if _, err := facade.SubmitPunches(context.Background(), batchPrincipal(t), BatchRequest{DeviceID: "dev-1", Punches: []BatchPunch{input}}); err != nil {
		t.Fatal(err)
	}
	if ctx.calls != 1 || len(store.entries) != 1 || store.entries[0].Work == nil {
		t.Fatalf("resolver calls=%d entries=%d", ctx.calls, len(store.entries))
	}
	work := store.entries[0].Work
	if work.Session.WorkerRef != "w1" || work.Session.AssignmentRef != "authoritative-assignment" {
		t.Fatalf("resolved work = worker %q assignment %q", work.Session.WorkerRef, work.Session.AssignmentRef)
	}
	if input.WorkerCredentialRef != "opaque-token" || input.AssignmentRef != "client-assignment" {
		t.Fatal("facade mutated caller wire input")
	}
}

func TestDeviceFacade_SubmitPunches_replaySkipsContextResolution(t *testing.T) {
	store := &batchLookupFake{batchCommitFake: &batchCommitFake{}, found: true}
	input := BatchPunch{DeviceSequence: 1, EventType: timesession.PunchIn, WorkerCredentialRef: "opaque-token", AssignmentRef: "client-assignment", Source: "KIOSK", Method: timesession.IdentBadge, DeviceOccurredAt: time.Unix(15, 0), IdempotencyKey: "p1", Scheduled: true}
	store.receipt = ReceiptRecord{DeviceSequence: 1, Status: string(BatchRejected)}
	store.receipt.Payload = []byte(`{"input_digest":"` + batchInputDigest("tenant", "dev-1", input) + `"}`)
	ctx := &facadePunchContext{}
	facade := DeviceFacade{Service: batchService(store.batchCommitFake), PunchContext: ctx, Clock: func() time.Time { return time.Unix(20, 0) }}
	facade.Work = store
	got, err := facade.SubmitPunches(context.Background(), batchPrincipal(t), BatchRequest{DeviceID: "dev-1", Punches: []BatchPunch{input}})
	if err != nil || len(got.Receipts) != 1 || got.Receipts[0].Status != BatchDuplicate || ctx.calls != 0 {
		t.Fatalf("response=%+v err=%v resolver calls=%d", got, err, ctx.calls)
	}
}

func TestDeviceFacade_SubmitPunches_boundsBeforeContextResolution(t *testing.T) {
	ctx := &facadePunchContext{}
	punches := make([]BatchPunch, maxPunchBatch+1)
	facade := DeviceFacade{Service: batchService(&batchCommitFake{}), PunchContext: ctx}
	_, err := facade.SubmitPunches(context.Background(), batchPrincipal(t), BatchRequest{DeviceID: "dev-1", Punches: punches})
	if !errors.Is(err, ErrInvalidRequest) || ctx.calls != 0 {
		t.Fatalf("err=%v resolver calls=%d, want bounds error and zero calls", err, ctx.calls)
	}
}

func TestDeviceFacade_IdentifyDevice_resolvesCredentialAndMintsToken(t *testing.T) {
	now := time.Unix(150, 0).UTC()
	device := DeviceRecord{TenantID: "tenant-a", ID: "device-client", SiteID: "site", ProfileID: "profile", Timezone: "UTC", State: "ACTIVE", Revision: 1, PublicKey: make([]byte, 32)}
	credentials := &identifyCredentialsFake{clockDomainPIN: true}
	base := Service{Credentials: credentials, Devices: identifyDeviceFake{device: device}, Roster: identifyRosterFake{}, Clock: func() time.Time { return now }}
	resolver := &facadeCredentialResolver{}
	facade := DeviceFacade{Service: base, Credentials: resolver, Clock: func() time.Time { return now }}
	principal := identifyPrincipal(t)
	got, err := facade.IdentifyDevice(context.Background(), principal, IdentifyDeviceRequest{DeviceID: device.ID, Method: "PIN", Credential: "4821"})
	if err != nil || got.Token.Value == "" || resolver.calls != 1 || credentials.pinCalls != 1 {
		t.Fatalf("result=%+v err=%v resolver=%d pin=%d", got, err, resolver.calls, credentials.pinCalls)
	}
}

func TestDeviceFacade_Heartbeat_usesServerClock(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	device := DeviceRecord{TenantID: "tenant-a", ID: "device-a", SiteID: "site-a", ProfileID: "profile-a", Timezone: "UTC", State: "ACTIVE", Revision: 1, PublicKey: make([]byte, 32)}
	store := &fleetHeartbeatFake{}
	base := Service{Devices: fleetDeviceFake{device: device}, Heartbeats: store, Clock: func() time.Time { return now }}
	facade := DeviceFacade{Service: base, Fleet: FleetService{Devices: base, Policies: fleetPolicyFake{policy: clockdomain.FleetHealthPolicy{MaxDrift: time.Minute, MinimumVersion: "1.0.0", MissingHeartbeatLimit: time.Hour}}}, Clock: func() time.Time { return now }}
	_, err := facade.Heartbeat(context.Background(), fleetPrincipal(t, "tenant-a", "device-a", now), HeartbeatRequest{DeviceID: "device-a", AppVersion: "1.0.0", PowerState: "MAINS", ObservedAt: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if !store.last.ObservedAt.Equal(now) {
		t.Fatalf("observed at = %v, want server clock %v", store.last.ObservedAt, now)
	}
}
