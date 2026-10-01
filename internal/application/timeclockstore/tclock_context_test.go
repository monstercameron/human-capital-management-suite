package timeclockstore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
)

func TestPINMatchesUsesSaltedVerifier(t *testing.T) {
	salt := []byte("0123456789012345")
	want := testPINHash("2468", salt)
	if !pinMatches("2468", want, salt) {
		t.Fatal("correct pin did not verify")
	}
	if pinMatches("2469", want, salt) {
		t.Fatal("wrong pin verified")
	}
	if pinMatches("2468", want, []byte("short")) {
		t.Fatal("short salt verified")
	}
}

func TestMatchingWorkersRejectsAmbiguousPIN(t *testing.T) {
	workers := map[string]clockservice.RosterWorker{
		"a": {WorkerRef: "a", PINHash: testPINHash("1234", []byte("aaaaaaaaaaaaaaaa")), PINSalt: []byte("aaaaaaaaaaaaaaaa")},
		"b": {WorkerRef: "b", PINHash: testPINHash("1234", []byte("bbbbbbbbbbbbbbbb")), PINSalt: []byte("bbbbbbbbbbbbbbbb")},
	}
	if got := matchingWorkers(workers, clockdomain.MethodPIN, "1234"); len(got) != 2 {
		t.Fatalf("matches=%v, want two candidates for ambiguity rejection", got)
	}
	if got := matchingWorkers(workers, clockdomain.MethodPIN, "0000"); len(got) != 0 {
		t.Fatalf("wrong pin matches=%v", got)
	}
}

func TestProductionWorkerDirectoryDelegatesAuthoritativeQueries(t *testing.T) {
	d := ProductionWorkerDirectory{Source: contextWorkerSource{}}
	if id, active, err := d.ResolveWorker(context.Background(), "tenant", "worker-1"); err != nil || id != "worker-1" || !active {
		t.Fatalf("worker=%q active=%v err=%v", id, active, err)
	}
	if id, project, site, ok, err := d.CurrentAssignment(context.Background(), "tenant", "worker-1", time.Now()); err != nil || id != "assignment-1" || project != "project-1" || site != "site-1" || !ok {
		t.Fatalf("assignment=%q project=%q site=%q ok=%v err=%v", id, project, site, ok, err)
	}
	status, err := (ProductionWorkerStatusSource{Directory: contextWorkerSource{}}).ResolveWorkerStatus(context.Background(), clockservice.DeviceWorkerTokenClaims{TenantID: "tenant", WorkerID: "worker-1", DeviceID: "device"})
	if err != nil || status.WorkerID != "worker-1" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

type resolverRoster struct{}

func (resolverRoster) Delta(context.Context, string, string, string, string, time.Time) (clockservice.RosterDelta, error) {
	return clockservice.RosterDelta{Workers: []clockservice.RosterWorker{{WorkerRef: "worker-1", BadgeID: "badge-1"}}, HasMore: false}, nil
}

type resolverDevice struct{ device clockservice.DeviceRecord }

func (d resolverDevice) GetDevice(context.Context, string, string) (clockservice.DeviceRecord, error) {
	return d.device, nil
}

type resolverPolicy struct {
	gotSite, gotProfile string
	policy              clockdomain.RateLimitPolicy
}

func (p *resolverPolicy) IdentificationPolicy(_ context.Context, _, site, profile string) (clockdomain.RateLimitPolicy, error) {
	p.gotSite, p.gotProfile = site, profile
	return p.policy, nil
}

type resolverCredentials struct {
	clockservice.CredentialStore
	failedDevice, failedWorker int
}

func (c *resolverCredentials) DeviceLockout(context.Context, string, string) (clockservice.LockoutState, error) {
	return clockservice.LockoutState{}, nil
}
func (c *resolverCredentials) WorkerLockout(context.Context, string, string) (clockservice.LockoutState, error) {
	return clockservice.LockoutState{}, nil
}
func (c *resolverCredentials) RecordFailedDeviceAttempt(context.Context, string, string, int, time.Time) (int, error) {
	c.failedDevice++
	return c.failedDevice, nil
}
func (c *resolverCredentials) RecordFailedWorkerAttempt(context.Context, string, string, int, time.Time) (int, error) {
	c.failedWorker++
	return c.failedWorker, nil
}
func (c *resolverCredentials) ResetDeviceAttempts(context.Context, string, string) error { return nil }
func (c *resolverCredentials) ResetWorkerAttempts(context.Context, string, string) error { return nil }

func TestProductionCredentialResolverUsesPublishedPolicyAndScope(t *testing.T) {
	policy := &resolverPolicy{policy: clockdomain.RateLimitPolicy{MaxAttempts: 2, Window: time.Minute, LockoutDuration: 7 * time.Minute}}
	creds := &resolverCredentials{}
	resolver := ProductionCredentialResolver{
		Roster:      resolverRoster{},
		Devices:     resolverDevice{device: clockservice.DeviceRecord{TenantID: "tenant", ID: "device", SiteID: "site", ProfileID: "profile", State: "ACTIVE"}},
		Credentials: creds, Policy: policy, Clock: func() time.Time { return time.Unix(100, 0).UTC() },
	}
	if _, err := resolver.ResolveDeviceCredential(context.Background(), "tenant", "device", clockdomain.MethodBadge, "wrong"); !errors.Is(err, clockservice.ErrWorkerNotEligible) {
		t.Fatalf("wrong credential err=%v", err)
	}
	if policy.gotSite != "site" || policy.gotProfile != "profile" || creds.failedDevice != 1 {
		t.Fatalf("policy scope=%q/%q failures=%d", policy.gotSite, policy.gotProfile, creds.failedDevice)
	}
	resolver.Clock = nil
	if _, err := resolver.ResolveDeviceCredential(context.Background(), "tenant", "device", clockdomain.MethodBadge, "badge-1"); !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("nil clock err=%v", err)
	}
	resolver.Clock = func() time.Time { return time.Unix(100, 0).UTC() }
	resolver.Devices = resolverDevice{device: clockservice.DeviceRecord{TenantID: "other", ID: "device", SiteID: "site", ProfileID: "profile", State: "ACTIVE"}}
	if _, err := resolver.ResolveDeviceCredential(context.Background(), "tenant", "device", clockdomain.MethodBadge, "badge-1"); !errors.Is(err, clockservice.ErrDeviceNotEligible) {
		t.Fatalf("cross tenant device err=%v", err)
	}
}

func testPINHash(pin string, salt []byte) []byte {
	sum := []byte(pin)
	for i := 0; i < pinIterations; i++ {
		mac := hmac.New(sha256.New, salt)
		_, _ = mac.Write(sum)
		sum = mac.Sum(nil)
	}
	return sum
}

type contextWorkerSource struct{}

func (contextWorkerSource) ResolveWorker(context.Context, string, string) (string, bool, error) {
	return "worker-1", true, nil
}
func (contextWorkerSource) ResolveAssignment(context.Context, string, string, string) (string, string, bool, error) {
	return "project-1", "site-1", true, nil
}
func (contextWorkerSource) CurrentAssignment(context.Context, string, string, time.Time) (string, string, string, bool, error) {
	return "assignment-1", "project-1", "site-1", true, nil
}
func (contextWorkerSource) ResolveWorkerStatus(context.Context, string, string, string) (clockservice.WorkerStatusResult, error) {
	return clockservice.WorkerStatusResult{WorkerID: "worker-1", DisplayName: "Worker"}, nil
}

type contextProfiles struct{}

func (contextProfiles) Resolve(context.Context, string, string, string, time.Time) (timeprofile.TimeProfile, error) {
	return timeprofile.TimeProfile{}, nil
}
