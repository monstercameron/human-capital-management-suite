package admission

import (
	"sync"
	"testing"
	"time"
)

func credentialQuotaFixture() (time.Time, CredentialIdentity, CredentialPolicy) {
	return time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC),
		CredentialIdentity{TenantID: "tenant-a", ClientID: "partner-app"},
		CredentialPolicy{RateLimit: 2, RateWindow: time.Minute, QuotaLimit: 3, QuotaWindow: time.Hour}
}

func TestTodo_REV_100_02(t *testing.T) {
	now, identity, policy := credentialQuotaFixture()
	limiter := NewCredentialLimiter()
	for wantUsed := 1; wantUsed <= 2; wantUsed++ {
		decision, err := limiter.Admit(now, identity, policy)
		if err != nil || decision.Outcome != CredentialAdmit || decision.Usage.RateUsed != wantUsed || decision.Usage.QuotaUsed != wantUsed {
			t.Fatalf("admission %d = %+v, err=%v", wantUsed, decision, err)
		}
	}
	limited, err := limiter.Admit(now, identity, policy)
	if err != nil || limited.Outcome != CredentialRateLimited || limited.Reason != "CREDENTIAL_RATE_LIMIT" || limited.RetryAfter != 60 {
		t.Fatalf("rate decision = %+v, err=%v", limited, err)
	}
	quota, err := limiter.Admit(now.Add(time.Minute), identity, policy)
	if err != nil || quota.Outcome != CredentialAdmit || quota.Usage.QuotaUsed != 3 {
		t.Fatalf("new rate window admission = %+v, err=%v", quota, err)
	}
	quota, err = limiter.Admit(now.Add(time.Minute), identity, policy)
	if err != nil || quota.Outcome != CredentialQuotaExceeded || quota.Reason != "CREDENTIAL_QUOTA_EXCEEDED" || quota.RetryAfter != 3540 {
		t.Fatalf("quota decision = %+v, err=%v", quota, err)
	}
}

func TestTodo_REV_100_02_Security(t *testing.T) {
	now, identity, policy := credentialQuotaFixture()
	limiter := NewCredentialLimiter()
	for i := 0; i < policy.RateLimit; i++ {
		if _, err := limiter.Admit(now, identity, policy); err != nil {
			t.Fatal(err)
		}
	}
	otherScopes := []CredentialIdentity{
		{TenantID: "tenant-b", ClientID: identity.ClientID},
		{TenantID: identity.TenantID, ClientID: "other-app"},
	}
	for _, other := range otherScopes {
		decision, err := limiter.Admit(now, other, policy)
		if err != nil || decision.Outcome != CredentialAdmit || decision.Usage.RateUsed != 1 {
			t.Fatalf("independent identity %+v shared quota: decision=%+v err=%v", other, decision, err)
		}
	}
	usage, ok, err := limiter.Usage(now, otherScopes[0], policy)
	if err != nil || !ok || usage.Identity != otherScopes[0] || usage.RateUsed != 1 {
		t.Fatalf("tenant-scoped telemetry = %+v, found=%v err=%v", usage, ok, err)
	}
	if _, found, err := limiter.Usage(now, CredentialIdentity{TenantID: "tenant-c", ClientID: identity.ClientID}, policy); err != nil || found {
		t.Fatalf("foreign tenant telemetry found=%v err=%v", found, err)
	}
}

func TestTodo_REV_100_02_Race(t *testing.T) {
	now, identity, policy := credentialQuotaFixture()
	policy.RateLimit, policy.QuotaLimit = 7, 7
	limiter := NewCredentialLimiter()
	var wg sync.WaitGroup
	var mu sync.Mutex
	admitted, limited := 0, 0
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			decision, err := limiter.Admit(now, identity, policy)
			if err != nil {
				t.Errorf("Admit: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			switch decision.Outcome {
			case CredentialAdmit:
				admitted++
			case CredentialRateLimited:
				limited++
			default:
				t.Errorf("unexpected decision: %+v", decision)
			}
		}()
	}
	wg.Wait()
	if admitted != 7 || limited != 93 {
		t.Fatalf("admitted=%d rate-limited=%d, want 7 and 93", admitted, limited)
	}
}

func TestTodo_REV_100_02_Fault(t *testing.T) {
	now, identity, policy := credentialQuotaFixture()
	limiter := NewCredentialLimiter()
	if _, err := limiter.Admit(now, CredentialIdentity{}, policy); err != ErrInvalidCredentialQuota {
		t.Fatalf("invalid identity error = %v", err)
	}
	if _, err := limiter.Admit(now, identity, CredentialPolicy{}); err != ErrInvalidCredentialQuota {
		t.Fatalf("zero policy error = %v", err)
	}
	decision, err := limiter.Admit(now, identity, policy)
	if err != nil || decision.Outcome != CredentialAdmit {
		t.Fatalf("valid request after invalid inputs = %+v, err=%v", decision, err)
	}
	var nilLimiter *CredentialLimiter
	if _, err := nilLimiter.Admit(now, identity, policy); err != ErrInvalidCredentialQuota {
		t.Fatalf("nil limiter error = %v", err)
	}
}
