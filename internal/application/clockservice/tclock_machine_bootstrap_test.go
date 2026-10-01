package clockservice

import (
	"context"
	"crypto/ed25519"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	clock "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_TCLOCK_002_MachineBootstrapDerivesIdentityAndCredentialRef(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	store := newMachineBootstrapStore(t, now, "pair-code")
	service := MachineBootstrapService{Store: store, ScopePolicy: testMachineScopes, Clock: func() time.Time { return now }}
	got, err := service.EnrollDevice(context.Background(), MachineBootstrapRequest{
		Tenant: "tenant-a", EnrollmentCode: "pair-code", PublicKey: pub,
		Challenge: []byte("pair-code"), Signature: ed25519.Sign(priv, []byte("pair-code")),
		IdempotencyKey: "enroll-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceID == "" || got.DeviceID != got.MachineClientID || len(got.DeviceID) > maxBootstrapIDSize || got.CredentialRef != "credential-ref" {
		t.Fatalf("bootstrap result=%+v", got)
	}
	if store.commits != 1 || store.last.DeviceID != got.DeviceID || store.last.MachineClientID != got.DeviceID {
		t.Fatalf("commit=%d command=%+v result=%+v", store.commits, store.last, got)
	}
	if strings.Contains(got.DeviceID, "pair-code") || strings.Contains(got.CredentialRef, "pair-code") {
		t.Fatal("bootstrap identifiers contain enrollment secret")
	}
}

func TestTodo_TCLOCK_002_MachineBootstrapRequiresServerOwnedScopes(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := newMachineBootstrapStore(t, now, "pair-code")
	service := MachineBootstrapService{Store: store, Clock: func() time.Time { return now }}
	pub, priv, _ := ed25519.GenerateKey(nil)
	_, err := service.EnrollDevice(context.Background(), MachineBootstrapRequest{Tenant: "tenant-a", EnrollmentCode: "pair-code", PublicKey: pub, Challenge: []byte("pair-code"), Signature: ed25519.Sign(priv, []byte("pair-code")), IdempotencyKey: "enroll-1"})
	if !errors.Is(err, ErrUnavailable) || store.commits != 0 {
		t.Fatalf("missing policy err=%v commits=%d", err, store.commits)
	}
	service.ScopePolicy = func(context.Context, string, string, string) ([]string, error) { return []string{"*"}, nil }
	_, err = service.EnrollDevice(context.Background(), MachineBootstrapRequest{Tenant: "tenant-a", EnrollmentCode: "pair-code", PublicKey: pub, Challenge: []byte("pair-code"), Signature: ed25519.Sign(priv, []byte("pair-code")), IdempotencyKey: "enroll-1"})
	if !errors.Is(err, ErrInvalidRequest) || store.commits != 0 {
		t.Fatalf("broad policy err=%v commits=%d", err, store.commits)
	}
}

func TestTodo_TCLOCK_002_MachineBootstrapUsesCodeProofAndAtomicReplay(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := newMachineBootstrapStore(t, now, "pair-code")
	service := MachineBootstrapService{Store: store, Clock: func() time.Time { return now }}
	pub, priv, _ := ed25519.GenerateKey(nil)
	req := MachineBootstrapRequest{Tenant: "tenant-a", EnrollmentCode: "pair-code", PublicKey: pub, Challenge: []byte("pair-code"), Signature: ed25519.Sign(priv, []byte("pair-code")), IdempotencyKey: "enroll-1"}
	service.ScopePolicy = testMachineScopes
	if _, err := service.EnrollDevice(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if got, err := service.EnrollDevice(context.Background(), req); err != nil || got.CredentialRef != "credential-ref" || store.commits != 1 {
		t.Fatalf("recovery err=%v result=%+v commits=%d", err, got, store.commits)
	}
	store.pending.Enrollment.EnrollmentCodeHash = "tampered"
	if _, err := service.EnrollDevice(context.Background(), req); !errors.Is(err, ErrInvalidRequest) || store.commits != 1 {
		t.Fatalf("wrong persisted code binding err=%v commits=%d", err, store.commits)
	}
	bad := req
	bad.Challenge = []byte("other")
	bad.Signature = ed25519.Sign(priv, bad.Challenge)
	if _, err := service.EnrollDevice(context.Background(), bad); !errors.Is(err, ErrInvalidRequest) || store.commits != 1 {
		t.Fatalf("wrong challenge err=%v commits=%d", err, store.commits)
	}
}

func TestTodo_TCLOCK_002_RecoveryAcceptsStorageSafePendingCodeBinding(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := newMachineBootstrapStore(t, now, "pair-code")
	service := MachineBootstrapService{Store: store, ScopePolicy: testMachineScopes, Clock: func() time.Time { return now }}
	pub, priv, _ := ed25519.GenerateKey(nil)
	req := MachineBootstrapRequest{Tenant: "tenant-a", EnrollmentCode: "pair-code", PublicKey: pub, Challenge: []byte("pair-code"), Signature: ed25519.Sign(priv, []byte("pair-code")), IdempotencyKey: "enroll-1"}
	if _, err := service.EnrollDevice(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	store.pending.Enrollment.EnrollmentCodeHash = ""
	if _, err := service.EnrollDevice(context.Background(), req); err != nil {
		t.Fatalf("storage-safe recovery: %v", err)
	}
	store.pending.Enrollment.EnrollmentCodeHash = hashBootstrapEnrollmentCode("other-code")
	if _, err := service.EnrollDevice(context.Background(), req); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("mismatched persisted code binding error=%v", err)
	}
}

func TestTodo_TCLOCK_002_RecoveryRejectsTamperedProofOrExpiredPending(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := newMachineBootstrapStore(t, now, "pair-code")
	service := MachineBootstrapService{Store: store, ScopePolicy: testMachineScopes, Clock: func() time.Time { return now }}
	pub, priv, _ := ed25519.GenerateKey(nil)
	req := MachineBootstrapRequest{Tenant: "tenant-a", EnrollmentCode: "pair-code", PublicKey: pub, Challenge: []byte("pair-code"), Signature: ed25519.Sign(priv, []byte("pair-code")), IdempotencyKey: "enroll-1"}
	if _, err := service.EnrollDevice(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	store.pending.Enrollment.Proof.Signature = append([]byte(nil), store.pending.Enrollment.Proof.Signature...)
	store.pending.Enrollment.Proof.Signature[0] ^= 1
	if _, err := service.EnrollDevice(context.Background(), req); !errors.Is(err, clock.ErrEnrollmentRejected) || store.commits != 1 {
		t.Fatalf("tampered proof error=%v commits=%d", err, store.commits)
	}
	store.pending.Enrollment.Proof.Signature = req.Signature
	store.pending.Enrollment.ExpiresAt = now
	if _, err := service.EnrollDevice(context.Background(), req); !errors.Is(err, clock.ErrEnrollmentRejected) {
		t.Fatalf("expired pending error=%v", err)
	}
}

func TestTodo_TCLOCK_002_MachineBootstrapRequiresTrustedClockAndBoundActivation(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	store := newMachineBootstrapStore(t, now, "pair-code")
	pub, priv, _ := ed25519.GenerateKey(nil)
	req := MachineBootstrapRequest{Tenant: "tenant-a", EnrollmentCode: "pair-code", PublicKey: pub, Challenge: []byte("pair-code"), Signature: ed25519.Sign(priv, []byte("pair-code")), IdempotencyKey: "enroll-1"}
	service := MachineBootstrapService{Store: store, ScopePolicy: testMachineScopes}
	if _, err := service.EnrollDevice(context.Background(), req); !errors.Is(err, ErrUnavailable) || store.commits != 0 {
		t.Fatalf("missing trusted clock err=%v commits=%d", err, store.commits)
	}
	service.Clock = func() time.Time { return now }
	store.badActivation = true
	if _, err := service.EnrollDevice(context.Background(), req); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unbound activation err=%v, want unavailable", err)
	}
}

type machineBootstrapStore struct {
	code          clock.EnrollmentCode
	registry      clock.ProfileRegistry
	mu            sync.Mutex
	used          bool
	commits       int
	last          MachineClientEnrollment
	badActivation bool
	pending       MachineBootstrapPending
}

func newMachineBootstrapStore(t *testing.T, now time.Time, codeRef string) *machineBootstrapStore {
	t.Helper()
	registry, err := clock.NewProfileRegistry([]clock.IntegrationProfile{{Class: clock.SourceManagedKiosk, Transport: "https", Authentication: "ed25519", TrustCeiling: clock.TrustCeilingMedium, PermittedMethods: []clock.IdentificationMethod{clock.MethodPIN}, FirstPartner: "first-party", Version: "v1"}})
	if err != nil {
		t.Fatal(err)
	}
	return &machineBootstrapStore{code: clock.EnrollmentCode{CodeRef: codeRef, Tenant: values.TenantId("tenant-a"), SiteRef: "site-a", Profile: clock.SourceManagedKiosk, Timezone: "UTC", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}, registry: registry}
}

func testMachineScopes(context.Context, string, string, string) ([]string, error) {
	return []string{"time.clock.read", "time.clock.write"}, nil
}

func (s *machineBootstrapStore) LoadEnrollment(context.Context, string, string) (clock.EnrollmentCode, clock.ProfileRegistry, error) {
	return s.code, s.registry, nil
}

func (s *machineBootstrapStore) PrepareMachineEnrollment(_ context.Context, in MachineClientEnrollment) (MachineBootstrapPending, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commits++
	if s.used {
		return MachineBootstrapPending{}, clock.ErrEnrollmentRejected
	}
	s.used = true
	s.last = in
	s.pending = MachineBootstrapPending{Enrollment: in, Device: DeviceRecord{TenantID: in.Tenant, ID: in.DeviceID, SiteID: in.SiteID, ProfileID: in.ProfileID, Timezone: in.Timezone, PublicKey: append([]byte(nil), in.PublicKey...), State: "PENDING", Revision: 1}}
	return s.pending, nil
}

func (s *machineBootstrapStore) PendingMachineEnrollmentFor(context.Context, string, string) (MachineBootstrapPending, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pending
	// Recovery mirrors the durable adapter: the raw bearer code is never
	// returned after PrepareMachineEnrollment has consumed it.
	pending.Enrollment.EnrollmentCode = ""
	return pending, s.used, nil
}

func (s *machineBootstrapStore) EnsureMachineClient(context.Context, MachineBootstrapPending) (string, error) {
	return "credential-ref", nil
}

func (s *machineBootstrapStore) ActivateMachineEnrollment(_ context.Context, tenant, deviceID, credentialRef string) (DeviceRecord, error) {
	if s.badActivation {
		return DeviceRecord{TenantID: tenant, ID: "other-device", SiteID: s.last.SiteID, ProfileID: s.last.ProfileID, Timezone: s.last.Timezone, State: "ACTIVE", Revision: 2}, nil
	}
	return DeviceRecord{TenantID: tenant, ID: deviceID, SiteID: s.last.SiteID, ProfileID: s.last.ProfileID, Timezone: s.last.Timezone, PublicKey: append([]byte(nil), s.last.PublicKey...), State: "ACTIVE", Revision: 2}, nil
}
