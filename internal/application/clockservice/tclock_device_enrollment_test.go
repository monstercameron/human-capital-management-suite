package clockservice

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	clock "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_TCLOCK_002_InvalidEnrollmentRequestFailsBeforeWrite(t *testing.T) {
	service := Service{Enrollments: enrollmentStoreStub{}, Auth: authStub{}, IDs: idsStub{}, Clock: func() time.Time { return time.Unix(10, 0) }}
	p := testPrincipal(t)
	_, err := service.CreateEnrollmentCode(context.Background(), p, EnrollmentRequest{SiteID: "site", ProfileID: "profile", Timezone: "UTC", TTL: MaxEnrollmentTTL + time.Second})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ttl error = %v, want ErrInvalidRequest", err)
	}
}

func TestTodo_TCLOCK_002_TimezonePolicyFailsClosed(t *testing.T) {
	service := Service{Enrollments: enrollmentStoreStub{}, Auth: authStub{}, IDs: idsStub{}, Clock: func() time.Time { return time.Unix(10, 0) }}
	_, err := service.CreateEnrollmentCode(context.Background(), testPrincipal(t), EnrollmentRequest{SiteID: "site", ProfileID: "profile", Timezone: "Mars/Olympus", TTL: time.Minute})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("timezone error = %v, want ErrInvalidRequest", err)
	}
}

func TestTodo_TCLOCK_002_EnrollmentRequiresProofBoundStore(t *testing.T) {
	service := Service{Enrollments: enrollmentStoreStub{}, Clock: func() time.Time { return time.Unix(10, 0) }}
	_, err := service.EnrollDevice(context.Background(), testPrincipal(t), DeviceEnrollmentRequest{Code: "code", DeviceID: "device", PublicKey: make([]byte, 32), Signature: make([]byte, 64)})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("legacy store error = %v, want ErrUnavailable", err)
	}
}

func TestTodo_TCLOCK_002_SecureEnrollmentBindsTenantAndChallenge(t *testing.T) {
	now := time.Unix(10, 0).UTC()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := clock.NewProfileRegistry([]clock.IntegrationProfile{{Class: clock.SourceManagedKiosk, Transport: "https", Authentication: "ed25519", TrustCeiling: clock.TrustCeilingMedium, PermittedMethods: []clock.IdentificationMethod{clock.MethodPIN}, FirstPartner: "first-party", Version: "v1"}})
	if err != nil {
		t.Fatal(err)
	}
	store := &secureEnrollmentStore{code: clock.EnrollmentCode{CodeRef: "pair", Tenant: "tenant-a", SiteRef: "site", Profile: clock.SourceManagedKiosk, Timezone: "UTC", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}, registry: registry}
	service := Service{Enrollments: store, Clock: func() time.Time { return now }}
	proof := []byte("pair")
	req := DeviceEnrollmentRequest{Code: "pair", DeviceID: "device", PublicKey: pub, Signature: ed25519.Sign(priv, proof)}
	got, err := service.EnrollDevice(context.Background(), testPrincipal(t), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "device" || !store.redeemed {
		t.Fatalf("enrollment result=%+v redeemed=%v", got, store.redeemed)
	}
	wrong := testPrincipalTenant(t, "tenant-b")
	if _, err := service.EnrollDevice(context.Background(), wrong, req); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("cross-tenant enrollment error = %v, want ErrInvalidRequest", err)
	}
}

func TestTodo_TCLOCK_002_SecureEnrollmentRejectsUnboundStoreResult(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := clock.NewProfileRegistry([]clock.IntegrationProfile{{Class: clock.SourceManagedKiosk, Transport: "https", Authentication: "ed25519", TrustCeiling: clock.TrustCeilingMedium, PermittedMethods: []clock.IdentificationMethod{clock.MethodPIN}, FirstPartner: "first-party", Version: "v1"}})
	if err != nil {
		t.Fatal(err)
	}
	store := &secureEnrollmentStore{code: clock.EnrollmentCode{CodeRef: "pair", Tenant: values.TenantId("tenant-a"), SiteRef: "site", Profile: clock.SourceManagedKiosk, Timezone: "UTC", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}, registry: registry, result: DeviceRecord{TenantID: "tenant-a", ID: "other-device", SiteID: "site", ProfileID: string(clock.SourceManagedKiosk), Timezone: "UTC", State: "ACTIVE", Revision: 1, PublicKey: pub}}
	service := Service{Enrollments: store, Clock: func() time.Time { return now }}
	_, err = service.EnrollDevice(context.Background(), testPrincipal(t), DeviceEnrollmentRequest{Code: "pair", DeviceID: "device", PublicKey: pub, Signature: ed25519.Sign(priv, []byte("pair"))})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unbound result error=%v", err)
	}
}

func TestTodo_TCLOCK_002_RotationChallengeIsBound(t *testing.T) {
	first := RotationChallenge("tenant-a", "device-a", 4)
	if equalBytes(first, RotationChallenge("tenant-a", "device-a", 5)) || equalBytes(first, RotationChallenge("tenant-b", "device-a", 4)) {
		t.Fatal("rotation challenge is not bound to tenant and revision")
	}
}

func TestTodo_TCLOCK_002_AdminAuthorizationPrecedesLifecycleWrite(t *testing.T) {
	devices := &deviceStoreStub{record: DeviceRecord{TenantID: "tenant-a", ID: "device", SiteID: "site", Revision: 1}}
	service := Service{Devices: devices, Auth: denyingAuth{}, Clock: func() time.Time { return time.Unix(10, 0) }}
	_, err := service.RevokeDevice(context.Background(), testPrincipal(t), DeviceLifecycleRequest{DeviceID: "device", ExpectedRevision: 1, Reason: "retired"})
	if !errors.Is(err, ErrInvalidPrincipal) {
		t.Fatalf("authorization error = %v, want ErrInvalidPrincipal", err)
	}
	if devices.writes != 0 {
		t.Fatalf("writes = %d, want zero", devices.writes)
	}
}

func TestTodo_TCLOCK_002_CreateEnrollmentCodeSuccessAndAdminFailure(t *testing.T) {
	now := time.Unix(10, 0).UTC()
	store := &recordingEnrollmentStore{}
	auth := &authPolicy{}
	service := Service{Enrollments: store, Auth: auth, IDs: idsStub{}, Clock: func() time.Time { return now }}
	result, err := service.CreateEnrollmentCode(context.Background(), testPrincipal(t), EnrollmentRequest{SiteID: "site", ProfileID: "profile", Timezone: "UTC", TTL: time.Minute})
	if err != nil || result.Code != "code" || !result.ExpiresAt.Equal(now.Add(time.Minute)) || store.created != 1 {
		t.Fatalf("create result=%+v err=%v writes=%d", result, err, store.created)
	}
	auth.err = ErrInvalidPrincipal
	if _, err := service.CreateEnrollmentCode(context.Background(), testPrincipal(t), EnrollmentRequest{SiteID: "site", ProfileID: "profile", Timezone: "UTC", TTL: time.Minute}); !errors.Is(err, ErrInvalidPrincipal) || store.created != 1 {
		t.Fatalf("denied create err=%v writes=%d", err, store.created)
	}
}

func TestTodo_TCLOCK_002_LifecycleTransitionsAuditAndRevision(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*Service) (DeviceRecord, error)
		want string
	}{
		{"suspend", func(s *Service) (DeviceRecord, error) {
			return s.SuspendDevice(context.Background(), testPrincipal(t), DeviceLifecycleRequest{DeviceID: "device", ExpectedRevision: 1, Reason: "pause"})
		}, "pause"},
		{"resume", func(s *Service) (DeviceRecord, error) {
			return s.ResumeDevice(context.Background(), testPrincipal(t), DeviceLifecycleRequest{DeviceID: "device", ExpectedRevision: 1, Reason: "resume"})
		}, "resume"},
		{"revoke", func(s *Service) (DeviceRecord, error) {
			return s.RevokeDevice(context.Background(), testPrincipal(t), DeviceLifecycleRequest{DeviceID: "device", ExpectedRevision: 1, Reason: "retire"})
		}, "retire"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			devices := &deviceStoreStub{record: DeviceRecord{TenantID: "tenant-a", ID: "device", SiteID: "site", Revision: 1}}
			service := Service{Devices: devices, Auth: &authPolicy{}, Clock: func() time.Time { return time.Unix(10, 0) }}
			got, err := tc.call(&service)
			if err != nil || got.Revision != 2 || devices.writes != 1 || devices.actor != "admin" || devices.reason != tc.want {
				t.Fatalf("got=%+v err=%v writes=%d actor=%q reason=%q", got, err, devices.writes, devices.actor, devices.reason)
			}
		})
	}
}

func TestTodo_TCLOCK_002_ReassignAndRotateRequireBoundProofAndDestinationAuth(t *testing.T) {
	devices := &deviceStoreStub{record: DeviceRecord{TenantID: "tenant-a", ID: "device", SiteID: "source", Revision: 1}}
	auth := &authPolicy{denySite: "destination"}
	service := Service{Devices: devices, Auth: auth, Clock: func() time.Time { return time.Unix(10, 0) }}
	if _, err := service.ReassignDeviceSite(context.Background(), testPrincipal(t), DeviceSiteReassignmentRequest{DeviceID: "device", ExpectedRevision: 1, SiteID: "destination", Timezone: "UTC", Reason: "move"}); !errors.Is(err, ErrInvalidPrincipal) || devices.writes != 0 {
		t.Fatalf("destination denial err=%v writes=%d", err, devices.writes)
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	challenge := RotationChallenge("tenant-a", "device", 1)
	valid := DeviceKeyRotationRequest{DeviceID: "device", ExpectedRevision: 1, PublicKey: pub, Challenge: challenge, Signature: ed25519.Sign(priv, challenge), Reason: "rotate"}
	if _, err := service.RotateDeviceKey(context.Background(), testPrincipal(t), valid); err != nil || devices.writes != 1 {
		t.Fatalf("valid rotation err=%v writes=%d", err, devices.writes)
	}
	valid.Challenge = []byte("arbitrary")
	valid.Signature = ed25519.Sign(priv, valid.Challenge)
	if _, err := service.RotateDeviceKey(context.Background(), testPrincipal(t), valid); !errors.Is(err, ErrInvalidRequest) || devices.writes != 1 {
		t.Fatalf("unbound rotation err=%v writes=%d", err, devices.writes)
	}
}

func TestTodo_TCLOCK_002_ConcurrentEnrollmentConsumesOnce(t *testing.T) {
	now := time.Unix(10, 0).UTC()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := clock.NewProfileRegistry([]clock.IntegrationProfile{{Class: clock.SourceManagedKiosk, Transport: "https", Authentication: "ed25519", TrustCeiling: clock.TrustCeilingMedium, PermittedMethods: []clock.IdentificationMethod{clock.MethodPIN}, FirstPartner: "first-party", Version: "v1"}})
	if err != nil {
		t.Fatal(err)
	}
	store := &secureEnrollmentStore{code: clock.EnrollmentCode{CodeRef: "pair", Tenant: "tenant-a", SiteRef: "site", Profile: clock.SourceManagedKiosk, Timezone: "UTC", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}, registry: registry}
	service := Service{Enrollments: store, Clock: func() time.Time { return now }}
	req := DeviceEnrollmentRequest{Code: "pair", DeviceID: "device", PublicKey: pub, Signature: ed25519.Sign(priv, []byte("pair"))}
	principal := testPrincipal(t)
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, e := service.EnrollDevice(context.Background(), principal, req); results <- e }()
	}
	var successes int
	for i := 0; i < 2; i++ {
		if <-results == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successes=%d, want one", successes)
	}
}

func testPrincipal(t *testing.T) *trust.Principal {
	return testPrincipalTenant(t, "tenant-a")
}

func testPrincipalTenant(t *testing.T, tenant string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: "admin", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(1000, 0), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type idsStub struct{}

func (idsStub) Deterministic(parts ...string) string { return "det" }
func (idsStub) Random() string                       { return "code" }

type authStub struct{}

func (authStub) AuthorizePunch(context.Context, *trust.Principal, string, string, string) (bool, error) {
	return false, nil
}
func (authStub) AuthorizeDeviceAdmin(context.Context, *trust.Principal, string, string) error {
	return nil
}
func (authStub) AuthorizeSupervisorOverride(context.Context, *trust.Principal, string, string) error {
	return nil
}

type denyingAuth struct{ authStub }

func (denyingAuth) AuthorizeDeviceAdmin(context.Context, *trust.Principal, string, string) error {
	return ErrInvalidPrincipal
}

type authPolicy struct {
	err      error
	denySite string
}

func (a *authPolicy) AuthorizePunch(context.Context, *trust.Principal, string, string, string) (bool, error) {
	return false, nil
}
func (a *authPolicy) AuthorizeDeviceAdmin(_ context.Context, _ *trust.Principal, _ string, site string) error {
	if a.err != nil {
		return a.err
	}
	if a.denySite == site {
		return ErrInvalidPrincipal
	}
	return nil
}
func (a *authPolicy) AuthorizeSupervisorOverride(context.Context, *trust.Principal, string, string) error {
	return nil
}

type deviceStoreStub struct {
	record DeviceRecord
	writes int
	actor  string
	reason string
}

func (s *deviceStoreStub) GetDevice(context.Context, string, string) (DeviceRecord, error) {
	return s.record, nil
}
func (s *deviceStoreStub) DevicesBySite(context.Context, string, string, int) ([]DeviceRecord, error) {
	return nil, nil
}
func (s *deviceStoreStub) RotateDeviceKey(context.Context, string, string, []byte, string, string, int64) (DeviceRecord, error) {
	s.writes++
	s.actor, s.reason = "admin", "rotate"
	return s.next(), nil
}
func (s *deviceStoreStub) SuspendDevice(_ context.Context, _ string, _ string, actor, reason string, _ int64) (DeviceRecord, error) {
	s.writes++
	s.actor, s.reason = actor, reason
	return s.next(), nil
}
func (s *deviceStoreStub) ResumeDevice(_ context.Context, _ string, _ string, actor, reason string, _ int64) (DeviceRecord, error) {
	s.writes++
	s.actor, s.reason = actor, reason
	return s.next(), nil
}
func (s *deviceStoreStub) RevokeDevice(_ context.Context, _ string, _ string, actor, reason string, _ int64) (DeviceRecord, error) {
	s.writes++
	s.actor, s.reason = actor, reason
	return s.next(), nil
}
func (s *deviceStoreStub) ReassignDeviceSite(context.Context, string, string, string, string, string, string, int64) (DeviceRecord, error) {
	s.writes++
	s.actor, s.reason = "admin", "move"
	return s.next(), nil
}

func (s *deviceStoreStub) RotateDeviceKeyProof(context.Context, string, string, clock.KeyPossessionProof, string, string, int64) (DeviceRecord, error) {
	s.writes++
	s.actor, s.reason = "admin", "rotate"
	return s.next(), nil
}

func (s *deviceStoreStub) next() DeviceRecord {
	next := s.record
	next.Revision++
	return next
}

type recordingEnrollmentStore struct {
	enrollmentStoreStub
	created int
}

func (s *recordingEnrollmentStore) CreateEnrollmentCode(context.Context, string, string, string, string, string, string, time.Time) error {
	s.created++
	return nil
}

type enrollmentStoreStub struct{}

func (enrollmentStoreStub) CreateEnrollmentCode(context.Context, string, string, string, string, string, string, time.Time) error {
	return nil
}
func (enrollmentStoreStub) RedeemEnrollmentCode(context.Context, string, string, string, []byte, string, time.Time) (DeviceRecord, error) {
	return DeviceRecord{}, nil
}

type secureEnrollmentStore struct {
	enrollmentStoreStub
	code     clock.EnrollmentCode
	registry clock.ProfileRegistry
	result   DeviceRecord
	redeemed bool
	mu       sync.Mutex
}

func (s *secureEnrollmentStore) LoadEnrollment(context.Context, string, string) (clock.EnrollmentCode, clock.ProfileRegistry, error) {
	return s.code, s.registry, nil
}

func (s *secureEnrollmentStore) RedeemEnrollment(_ context.Context, _ string, _ string, _ string, proof clock.KeyPossessionProof, _ string, _ time.Time) (DeviceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.redeemed {
		return DeviceRecord{}, fmt.Errorf("already redeemed")
	}
	s.redeemed = true
	if s.result.ID != "" {
		return s.result, nil
	}
	return DeviceRecord{TenantID: "tenant-a", ID: "device", SiteID: "site", ProfileID: string(clock.SourceManagedKiosk), Timezone: "UTC", PublicKey: append(ed25519.PublicKey(nil), proof.PublicKey...), State: "ACTIVE", Revision: 1}, nil
}
