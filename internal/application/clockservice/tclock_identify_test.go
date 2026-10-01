package clockservice

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type identifyCredentialsFake struct {
	mu             sync.Mutex
	clockDomainPIN bool
	deviceLock     LockoutState
	workerLock     LockoutState
	lookup         CredentialRecord
	lookupErr      error
	verified       *trust.Principal
	overrides      int
	deviceFailures int
	workerFailures int
	deviceResets   int
	workerResets   int
	pinCalls       int
	policy         clockdomain.RateLimitPolicy
}

func (f *identifyCredentialsFake) IssuePINCredential(context.Context, string, string, string, string) (CredentialRecord, error) {
	return CredentialRecord{}, nil
}
func (f *identifyCredentialsFake) IssueBadgeCredential(context.Context, string, string, string, string) (CredentialRecord, error) {
	return CredentialRecord{}, nil
}
func (f *identifyCredentialsFake) IssueQRCredential(context.Context, string, string, string, string) (CredentialRecord, error) {
	return CredentialRecord{}, nil
}
func (f *identifyCredentialsFake) RevokeCredential(context.Context, string, string, string, string, int64) (CredentialRecord, error) {
	return CredentialRecord{}, nil
}
func (f *identifyCredentialsFake) VerifyPIN(context.Context, string, string, string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pinCalls++
	return f.clockDomainPIN, nil
}
func (f *identifyCredentialsFake) RecordFailedDeviceAttempt(context.Context, string, string, int, time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deviceFailures++
	return f.deviceFailures, nil
}
func (f *identifyCredentialsFake) RecordFailedWorkerAttempt(context.Context, string, string, int, time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.workerFailures++
	return f.workerFailures, nil
}
func (f *identifyCredentialsFake) ResetDeviceAttempts(context.Context, string, string) error {
	f.mu.Lock()
	f.deviceResets++
	f.mu.Unlock()
	return nil
}
func (f *identifyCredentialsFake) ResetWorkerAttempts(context.Context, string, string) error {
	f.mu.Lock()
	f.workerResets++
	f.mu.Unlock()
	return nil
}
func (f *identifyCredentialsFake) DeviceLockout(context.Context, string, string) (LockoutState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deviceLock, nil
}
func (f *identifyCredentialsFake) WorkerLockout(context.Context, string, string) (LockoutState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.workerLock, nil
}
func (f *identifyCredentialsFake) RecordSupervisorOverride(context.Context, string, string, string, string, string) (SupervisorOverrideRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.overrides++
	return SupervisorOverrideRecord{ID: "override-1"}, nil
}
func (f *identifyCredentialsFake) LookupCredential(context.Context, string, string, string, string) (CredentialRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lookup, f.lookupErr
}
func (f *identifyCredentialsFake) VerifySupervisorCredential(context.Context, string, string, string) (*trust.Principal, error) {
	return f.verified, nil
}
func (f *identifyCredentialsFake) IssueDeviceWorkerToken(_ context.Context, claims DeviceWorkerTokenClaims) (DeviceWorkerToken, error) {
	return DeviceWorkerToken{Value: claims.TenantID + ":" + claims.DeviceID + ":" + claims.WorkerID, IssuedAt: claims.IssuedAt, ExpiresAt: claims.ExpiresAt}, nil
}
func (f *identifyCredentialsFake) IdentificationPolicy(context.Context, string, string, string) (clockdomain.RateLimitPolicy, error) {
	if f.policy.MaxAttempts > 0 {
		return f.policy, nil
	}
	return clockdomain.RateLimitPolicy{MaxAttempts: 3, Window: time.Minute, LockoutDuration: time.Hour}, nil
}

type identifyDeviceFake struct{ device DeviceRecord }

func (f identifyDeviceFake) GetDevice(context.Context, string, string) (DeviceRecord, error) {
	return f.device, nil
}
func (identifyDeviceFake) DevicesBySite(context.Context, string, string, int) ([]DeviceRecord, error) {
	return nil, nil
}
func (identifyDeviceFake) RotateDeviceKey(context.Context, string, string, []byte, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, nil
}
func (identifyDeviceFake) SuspendDevice(context.Context, string, string, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, nil
}
func (identifyDeviceFake) ResumeDevice(context.Context, string, string, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, nil
}
func (identifyDeviceFake) RevokeDevice(context.Context, string, string, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, nil
}
func (identifyDeviceFake) ReassignDeviceSite(context.Context, string, string, string, string, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, nil
}

type identifyRosterFake struct{}

func (identifyRosterFake) Delta(context.Context, string, string, string, string, time.Time) (RosterDelta, error) {
	return RosterDelta{SnapshotRevision: "snapshot-1", MaxOfflineAge: time.Hour, PunchPolicyVersion: 1, Workers: []RosterWorker{{WorkerRef: "worker-1", BadgeID: "badge-1"}}}, nil
}

// credentialStoreNoIssuer deliberately implements only CredentialStore and
// the policy source, proving missing signer wiring fails before resets.
type credentialStoreNoIssuer struct{ base *identifyCredentialsFake }

func (f credentialStoreNoIssuer) IssuePINCredential(c context.Context, a, b, d, e string) (CredentialRecord, error) {
	return f.base.IssuePINCredential(c, a, b, d, e)
}
func (f credentialStoreNoIssuer) IssueBadgeCredential(c context.Context, a, b, d, e string) (CredentialRecord, error) {
	return f.base.IssueBadgeCredential(c, a, b, d, e)
}
func (f credentialStoreNoIssuer) IssueQRCredential(c context.Context, a, b, d, e string) (CredentialRecord, error) {
	return f.base.IssueQRCredential(c, a, b, d, e)
}
func (f credentialStoreNoIssuer) RevokeCredential(c context.Context, a, b, d, e string, n int64) (CredentialRecord, error) {
	return f.base.RevokeCredential(c, a, b, d, e, n)
}
func (f credentialStoreNoIssuer) VerifyPIN(c context.Context, a, b, d string) (bool, error) {
	return f.base.VerifyPIN(c, a, b, d)
}
func (f credentialStoreNoIssuer) RecordFailedDeviceAttempt(c context.Context, a, b string, n int, at time.Time) (int, error) {
	return f.base.RecordFailedDeviceAttempt(c, a, b, n, at)
}
func (f credentialStoreNoIssuer) RecordFailedWorkerAttempt(c context.Context, a, b string, n int, at time.Time) (int, error) {
	return f.base.RecordFailedWorkerAttempt(c, a, b, n, at)
}
func (f credentialStoreNoIssuer) ResetDeviceAttempts(c context.Context, a, b string) error {
	return f.base.ResetDeviceAttempts(c, a, b)
}
func (f credentialStoreNoIssuer) ResetWorkerAttempts(c context.Context, a, b string) error {
	return f.base.ResetWorkerAttempts(c, a, b)
}
func (f credentialStoreNoIssuer) DeviceLockout(c context.Context, a, b string) (LockoutState, error) {
	return f.base.DeviceLockout(c, a, b)
}
func (f credentialStoreNoIssuer) WorkerLockout(c context.Context, a, b string) (LockoutState, error) {
	return f.base.WorkerLockout(c, a, b)
}
func (f credentialStoreNoIssuer) RecordSupervisorOverride(c context.Context, a, b, d, e, g string) (SupervisorOverrideRecord, error) {
	return f.base.RecordSupervisorOverride(c, a, b, d, e, g)
}
func (f credentialStoreNoIssuer) IdentificationPolicy(c context.Context, a, b, d string) (clockdomain.RateLimitPolicy, error) {
	return f.base.IdentificationPolicy(c, a, b, d)
}

type identifyAuthorizerFake struct{}

func (identifyAuthorizerFake) AuthorizePunch(context.Context, *trust.Principal, string, string, string) (bool, error) {
	return false, nil
}
func (identifyAuthorizerFake) AuthorizeDeviceAdmin(context.Context, *trust.Principal, string, string) error {
	return nil
}
func (identifyAuthorizerFake) AuthorizeSupervisorOverride(context.Context, *trust.Principal, string, string) error {
	return nil
}

func identifyPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "tenant-a", Subject: "device-client", ClientID: "device-client",
		SubjectKind: trust.SubjectKindService, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: time.Unix(100, 0),
		ExpiresAt: time.Unix(200, 0), CredentialDigest: "digest",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTodo_TCLOCK_005_IdentifyCredentialPIN(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	fake := &identifyCredentialsFake{clockDomainPIN: true}
	s := Service{Credentials: fake, Clock: func() time.Time { return now }}
	evidence, verified, err := s.identifyCredential(context.Background(), identifyPrincipal(t), DeviceRecord{ID: "device-1"}, IdentifyRequest{Method: clockdomain.MethodPIN, DeviceID: "device-1", WorkerID: "worker-1", PIN: "4821"}, now)
	if err != nil || !verified || evidence.WorkerRef != "worker-1" || evidence.DeviceRef != "device-1" {
		t.Fatalf("PIN = evidence=%+v verified=%v err=%v", evidence, verified, err)
	}
}

func TestTodo_TCLOCK_005_IdentifyCredentialRequiresRegistryForBadge(t *testing.T) {
	fake := &identifyCredentialsFake{lookupErr: ErrUnavailable}
	s := Service{Credentials: fake}
	_, _, err := s.identifyCredential(context.Background(), identifyPrincipal(t), DeviceRecord{ID: "device-1"}, IdentifyRequest{Method: clockdomain.MethodBadge, DeviceID: "device-1", WorkerID: "worker-1", BadgeID: "badge-1"}, time.Unix(100, 0))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("badge without registry = %v, want ErrUnavailable", err)
	}
}

func TestTodo_TCLOCK_005_EnsureUnlockedRejectsEitherScope(t *testing.T) {
	future := time.Unix(101, 0)
	fake := &identifyCredentialsFake{deviceLock: LockoutState{LockedUntil: future}}
	s := Service{Credentials: fake, Clock: func() time.Time { return time.Unix(100, 0) }}
	if err := s.ensureUnlocked(context.Background(), "tenant-a", "device-1", "worker-1"); !errors.Is(err, ErrLockedOut) {
		t.Fatalf("locked device = %v, want ErrLockedOut", err)
	}
}

func TestTodo_TCLOCK_005_IdentifyCredentialRejectsSupervisorWithoutVerifier(t *testing.T) {
	fake := &identifyCredentialsFake{}
	s := Service{Credentials: fake}
	_, _, err := s.identifyCredential(context.Background(), identifyPrincipal(t), DeviceRecord{ID: "device-1", SiteID: "site-1"}, IdentifyRequest{Method: clockdomain.MethodSupervisorOverride, WorkerID: "worker-1", SupervisorCredentialRef: "opaque", SupervisorReason: "forgot badge"}, time.Unix(100, 0))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("supervisor without verifier = %v, want ErrUnavailable", err)
	}
}

func TestTodo_TCLOCK_005_Identify_EndToEndScopedToken(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	device := DeviceRecord{TenantID: "tenant-a", ID: "device-client", PublicKey: make([]byte, 32), SiteID: "site-1", ProfileID: "profile-1", Timezone: "UTC", State: "ACTIVE", Revision: 1}
	fake := &identifyCredentialsFake{clockDomainPIN: true}
	s := Service{Credentials: fake, Devices: identifyDeviceFake{device: device}, Roster: identifyRosterFake{}, Clock: func() time.Time { return now }}
	result, err := s.Identify(context.Background(), identifyPrincipal(t), IdentifyRequest{DeviceID: device.ID, WorkerID: "worker-1", Method: clockdomain.MethodPIN, PIN: "4821"})
	if err != nil {
		t.Fatalf("Identify = %v", err)
	}
	if result.Evidence.WorkerRef != "worker-1" || result.Evidence.DeviceRef != device.ID {
		t.Fatalf("evidence = %+v", result.Evidence)
	}
	if result.Token.Value != "tenant-a:device-client:worker-1" || result.Token.ExpiresAt.Sub(now) != 5*time.Minute {
		t.Fatalf("token = %+v", result.Token)
	}
}

func TestTodo_TCLOCK_005_Identify_RejectsWrongMachineBinding(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	device := DeviceRecord{TenantID: "tenant-a", ID: "device-client", PublicKey: make([]byte, 32), SiteID: "site-1", ProfileID: "profile-1", Timezone: "UTC", State: "ACTIVE", Revision: 1}
	s := Service{Credentials: &identifyCredentialsFake{clockDomainPIN: true}, Devices: identifyDeviceFake{device: device}, Roster: identifyRosterFake{}, Clock: func() time.Time { return now }}
	_, err := s.Identify(context.Background(), identifyPrincipal(t), IdentifyRequest{DeviceID: "other-device", WorkerID: "worker-1", Method: clockdomain.MethodPIN, PIN: "4821"})
	if !errors.Is(err, ErrDeviceNotEligible) {
		t.Fatalf("cross-device identify = %v, want ErrDeviceNotEligible", err)
	}
}

func TestTodo_TCLOCK_005_Identify_ConcurrentAuthorizedAttempts(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	device := DeviceRecord{TenantID: "tenant-a", ID: "device-client", PublicKey: make([]byte, 32), SiteID: "site-1", ProfileID: "profile-1", Timezone: "UTC", State: "ACTIVE", Revision: 1}
	s := Service{Credentials: &identifyCredentialsFake{clockDomainPIN: true}, Devices: identifyDeviceFake{device: device}, Roster: identifyRosterFake{}, Clock: func() time.Time { return now }}
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func() {
			_, err := s.Identify(context.Background(), identifyPrincipal(t), IdentifyRequest{DeviceID: device.ID, WorkerID: "worker-1", Method: clockdomain.MethodPIN, PIN: "4821"})
			errs <- err
		}()
	}
	for i := 0; i < 16; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent identify = %v", err)
		}
	}
}

func TestTodo_TCLOCK_005_Identify_FailedPINLocksBothScopes(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	device := DeviceRecord{TenantID: "tenant-a", ID: "device-client", PublicKey: make([]byte, 32), SiteID: "site-1", ProfileID: "profile-1", Timezone: "UTC", State: "ACTIVE", Revision: 1}
	fake := &identifyCredentialsFake{policy: clockdomain.RateLimitPolicy{MaxAttempts: 2, Window: time.Minute, LockoutDuration: time.Hour}}
	s := Service{Credentials: fake, Devices: identifyDeviceFake{device: device}, Roster: identifyRosterFake{}, Clock: func() time.Time { return now }}
	req := IdentifyRequest{DeviceID: device.ID, WorkerID: "worker-1", Method: clockdomain.MethodPIN, PIN: "wrong"}
	if _, err := s.Identify(context.Background(), identifyPrincipal(t), req); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("first failed PIN = %v, want ErrInvalidRequest", err)
	}
	if _, err := s.Identify(context.Background(), identifyPrincipal(t), req); !errors.Is(err, ErrLockedOut) {
		t.Fatalf("threshold failed PIN = %v, want ErrLockedOut", err)
	}
	fake.mu.Lock()
	deviceFailures, workerFailures := fake.deviceFailures, fake.workerFailures
	fake.mu.Unlock()
	if deviceFailures != 2 || workerFailures != 2 {
		t.Fatalf("failure counts device=%d worker=%d, want both 2", deviceFailures, workerFailures)
	}
}

func TestTodo_TCLOCK_005_Identify_LockoutPrecedesCredential(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	device := DeviceRecord{TenantID: "tenant-a", ID: "device-client", PublicKey: make([]byte, 32), SiteID: "site-1", ProfileID: "profile-1", Timezone: "UTC", State: "ACTIVE", Revision: 1}
	fake := &identifyCredentialsFake{clockDomainPIN: true, deviceLock: LockoutState{LockedUntil: now.Add(time.Hour)}}
	s := Service{Credentials: fake, Devices: identifyDeviceFake{device: device}, Roster: identifyRosterFake{}, Clock: func() time.Time { return now }}
	_, err := s.Identify(context.Background(), identifyPrincipal(t), IdentifyRequest{DeviceID: device.ID, WorkerID: "worker-1", Method: clockdomain.MethodPIN, PIN: "4821"})
	if !errors.Is(err, ErrLockedOut) {
		t.Fatalf("locked identify = %v, want ErrLockedOut", err)
	}
	fake.mu.Lock()
	pinCalls := fake.pinCalls
	fake.mu.Unlock()
	if pinCalls != 0 {
		t.Fatalf("VerifyPIN calls = %d, want zero while locked", pinCalls)
	}
}

func TestTodo_TCLOCK_005_Identify_MissingSignerHasNoResetSideEffects(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	device := DeviceRecord{TenantID: "tenant-a", ID: "device-client", PublicKey: make([]byte, 32), SiteID: "site-1", ProfileID: "profile-1", Timezone: "UTC", State: "ACTIVE", Revision: 1}
	base := &identifyCredentialsFake{clockDomainPIN: true}
	s := Service{Credentials: credentialStoreNoIssuer{base: base}, Devices: identifyDeviceFake{device: device}, Roster: identifyRosterFake{}, Clock: func() time.Time { return now }}
	_, err := s.Identify(context.Background(), identifyPrincipal(t), IdentifyRequest{DeviceID: device.ID, WorkerID: "worker-1", Method: clockdomain.MethodPIN, PIN: "4821"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing signer = %v, want ErrUnavailable", err)
	}
	base.mu.Lock()
	deviceResets, workerResets := base.deviceResets, base.workerResets
	base.mu.Unlock()
	if deviceResets != 0 || workerResets != 0 {
		t.Fatalf("resets device=%d worker=%d, want zero", deviceResets, workerResets)
	}
}

func TestTodo_TCLOCK_005_Identify_RegistryRevocationAndRotation(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	for _, tc := range []struct {
		name   string
		method clockdomain.IdentificationMethod
		state  string
		ext    string
		badge  string
		qr     string
		want   bool
	}{
		{name: "badge revoked", method: clockdomain.MethodBadge, state: "REVOKED", ext: "badge-1", badge: "badge-1"},
		{name: "badge rotated", method: clockdomain.MethodBadge, state: "ACTIVE", ext: "badge-2", badge: "badge-1"},
		{name: "badge current", method: clockdomain.MethodBadge, state: "ACTIVE", ext: "badge-1", badge: "badge-1", want: true},
		{name: "qr revoked", method: clockdomain.MethodQR, state: "REVOKED", ext: "qr-1", qr: "qr-1"},
		{name: "qr rotated", method: clockdomain.MethodQR, state: "ACTIVE", ext: "qr-2", qr: "qr-1"},
		{name: "qr current", method: clockdomain.MethodQR, state: "ACTIVE", ext: "qr-1", qr: "qr-1", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &identifyCredentialsFake{lookup: CredentialRecord{WorkerID: "worker-1", ExternalID: tc.ext, State: tc.state}}
			s := Service{Credentials: fake}
			_, verified, err := s.identifyCredential(context.Background(), identifyPrincipal(t), DeviceRecord{ID: "device-1"}, IdentifyRequest{Method: tc.method, WorkerID: "worker-1", BadgeID: tc.badge, QRKeyID: tc.qr}, now)
			if err != nil || verified != tc.want {
				t.Fatalf("badge verified=%v err=%v, want %v", verified, err, tc.want)
			}
		})
	}
}

func TestTodo_TCLOCK_005_Identify_SupervisorCredentialBound(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	device := DeviceRecord{ID: "device-1", SiteID: "site-1"}
	base := func(t *testing.T, supervisor *trust.Principal, reason string) error {
		t.Helper()
		fake := &identifyCredentialsFake{verified: supervisor}
		s := Service{Credentials: fake, Auth: identifyAuthorizerFake{}}
		_, _, err := s.identifyCredential(context.Background(), identifyPrincipal(t), device, IdentifyRequest{Method: clockdomain.MethodSupervisorOverride, WorkerID: "worker-1", SupervisorCredentialRef: "credential-1", SupervisorReason: reason}, now)
		return err
	}
	human, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "supervisor-1", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: time.Unix(99, 0), ExpiresAt: time.Unix(200, 0), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-b", Subject: "supervisor-1", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: time.Unix(99, 0), ExpiresAt: time.Unix(200, 0), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		p      *trust.Principal
		reason string
		want   error
	}{
		{name: "foreign tenant", p: foreign, reason: "valid", want: ErrSelfApproval},
		{name: "self", p: mustSupervisorPrincipal(t, "tenant-a", "worker-1"), reason: "valid", want: ErrSelfApproval},
		{name: "missing reason", p: human, reason: "", want: clockdomain.ErrIdentificationRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := base(t, tc.p, tc.reason); !errors.Is(err, tc.want) {
				t.Fatalf("supervisor error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestTodo_TCLOCK_005(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	tests := []struct {
		name   string
		method clockdomain.IdentificationMethod
		fake   *identifyCredentialsFake
		req    IdentifyRequest
		want   clockdomain.AssuranceLevel
	}{
		{name: "pin", method: clockdomain.MethodPIN, fake: &identifyCredentialsFake{clockDomainPIN: true}, req: IdentifyRequest{Method: clockdomain.MethodPIN, WorkerID: "worker-1", PIN: "4821"}, want: clockdomain.AssuranceLow},
		{name: "badge", method: clockdomain.MethodBadge, fake: &identifyCredentialsFake{lookup: CredentialRecord{WorkerID: "worker-1", ExternalID: "badge-1", State: "ACTIVE"}}, req: IdentifyRequest{Method: clockdomain.MethodBadge, WorkerID: "worker-1", BadgeID: "badge-1"}, want: clockdomain.AssuranceMedium},
		{name: "qr", method: clockdomain.MethodQR, fake: &identifyCredentialsFake{lookup: CredentialRecord{WorkerID: "worker-1", ExternalID: "qr-1", State: "ACTIVE"}}, req: IdentifyRequest{Method: clockdomain.MethodQR, WorkerID: "worker-1", QRKeyID: "qr-1"}, want: clockdomain.AssuranceMedium},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			evidence, verified, err := (Service{Credentials: tc.fake}).identifyCredential(context.Background(), identifyPrincipal(t), DeviceRecord{ID: "device-1", SiteID: "site-1"}, tc.req, now)
			if err != nil || !verified {
				t.Fatalf("identify evidence=%+v verified=%v err=%v", evidence, verified, err)
			}
			if evidence.Method != tc.method || evidence.AssuranceLevel != tc.want || evidence.WorkerRef != "worker-1" || evidence.DeviceRef != "device-1" {
				t.Fatalf("evidence=%+v, want method=%s assurance=%s", evidence, tc.method, tc.want)
			}
		})
	}

	supervisor := mustSupervisorPrincipal(t, "tenant-a", "supervisor-1")
	fake := &identifyCredentialsFake{verified: supervisor}
	evidence, verified, err := (Service{Credentials: fake, Auth: identifyAuthorizerFake{}}).identifyCredential(context.Background(), identifyPrincipal(t), DeviceRecord{ID: "device-1", SiteID: "site-1"}, IdentifyRequest{Method: clockdomain.MethodSupervisorOverride, WorkerID: "worker-1", SupervisorCredentialRef: "credential-1", SupervisorReason: "badge unavailable"}, now)
	if err != nil || !verified || evidence.AssuranceLevel != clockdomain.AssuranceHigh || evidence.SupervisorRef != "supervisor-1" || fake.overrides != 1 {
		t.Fatalf("override evidence=%+v verified=%v overrides=%d err=%v", evidence, verified, fake.overrides, err)
	}
}

func TestTodo_TCLOCK_005_Security(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	device := DeviceRecord{TenantID: "tenant-a", ID: "device-client", PublicKey: make([]byte, 32), SiteID: "site-1", ProfileID: "profile-1", Timezone: "UTC", State: "ACTIVE", Revision: 1}
	fake := &identifyCredentialsFake{policy: clockdomain.RateLimitPolicy{MaxAttempts: 1, Window: time.Minute, LockoutDuration: time.Hour}}
	s := Service{Credentials: fake, Devices: identifyDeviceFake{device: device}, Roster: identifyRosterFake{}, Clock: func() time.Time { return now }}
	_, err := s.Identify(context.Background(), identifyPrincipal(t), IdentifyRequest{DeviceID: device.ID, WorkerID: "worker-1", Method: clockdomain.MethodPIN, PIN: "wrong"})
	if !errors.Is(err, ErrLockedOut) {
		t.Fatalf("failed PIN err=%v, want lockout", err)
	}
	fake.mu.Lock()
	deviceFailures, workerFailures := fake.deviceFailures, fake.workerFailures
	fake.mu.Unlock()
	if deviceFailures != 1 || workerFailures != 1 {
		t.Fatalf("lockout counters device=%d worker=%d, want one each", deviceFailures, workerFailures)
	}
}

func TestTodo_TCLOCK_005_Property(t *testing.T) {
	methods := []struct {
		method clockdomain.IdentificationMethod
		want   clockdomain.AssuranceLevel
	}{
		{clockdomain.MethodPIN, clockdomain.AssuranceLow},
		{clockdomain.MethodBadge, clockdomain.AssuranceMedium},
		{clockdomain.MethodQR, clockdomain.AssuranceMedium},
		{clockdomain.MethodFace, clockdomain.AssuranceHigh},
		{clockdomain.MethodSupervisorOverride, clockdomain.AssuranceHigh},
	}
	for _, tc := range methods {
		if got := tc.method.AssuranceLevel(); got != tc.want {
			t.Fatalf("method=%s assurance=%s, want %s", tc.method, got, tc.want)
		}
	}
}

func TestTodo_TCLOCK_005_Race(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	device := DeviceRecord{TenantID: "tenant-a", ID: "device-client", PublicKey: make([]byte, 32), SiteID: "site-1", ProfileID: "profile-1", Timezone: "UTC", State: "ACTIVE", Revision: 1}
	s := Service{Credentials: &identifyCredentialsFake{clockDomainPIN: true}, Devices: identifyDeviceFake{device: device}, Roster: identifyRosterFake{}, Clock: func() time.Time { return now }}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Identify(context.Background(), identifyPrincipal(t), IdentifyRequest{DeviceID: device.ID, WorkerID: "worker-1", Method: clockdomain.MethodPIN, PIN: "4821"})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent identification err=%v", err)
		}
	}
}

func mustSupervisorPrincipal(t *testing.T, tenant, subject string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: time.Unix(99, 0), ExpiresAt: time.Unix(200, 0), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
