package timestore

import (
	"context"
	"errors"
	"testing"
	"time"

	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/punchpolicy"
)

func validSitePolicy() SitePolicy {
	return SitePolicy{
		PunchPolicy:          punchpolicy.Policy{ID: "punch", Version: 1, Jurisdiction: "US-CA"},
		AttestationQuestions: punchpolicy.QuestionSet{ID: "questions", Version: 1, Jurisdiction: "US-CA", Questions: []punchpolicy.Question{{ID: "break", Kind: punchpolicy.QuestionBreakProvided, TextKey: "clock.break", Required: true}}},
		IdentificationPolicy: clockdomain.RateLimitPolicy{MaxAttempts: 5, Window: 15 * time.Minute, LockoutDuration: 15 * time.Minute},
		FleetHealthPolicy:    clockdomain.FleetHealthPolicy{MaxDrift: time.Minute, MinimumVersion: "1.0.0", MissingHeartbeatLimit: 10 * time.Minute},
		MaxOfflineAge:        2 * time.Hour, SnapshotRevision: "snapshot-1",
		JobCodes: []SiteJobCode{{Code: "JOB-1", Name: "Floor"}}, Tips: SiteTipRules{Enabled: true, Currency: "USD"}, LocalizedStrings: map[string]string{"clock.break": "Was your break provided?"},
	}
}

func TestTodo_TCLOCK_009_SitePolicyPayloadIsStrictAndValidated(t *testing.T) {
	b, err := encodeSitePolicy(validSitePolicy())
	if err != nil {
		t.Fatalf("encode = %v", err)
	}
	got, err := decodeSitePolicy(b)
	if err != nil || got.SnapshotRevision != "snapshot-1" || got.PunchPolicy.ID != "punch" || got.MaxOfflineAge != 2*time.Hour {
		t.Fatalf("decode = %+v, %v", got, err)
	}
	bad := append(append([]byte(nil), b[:len(b)-1]...), []byte(`,"unknown":true}`)...)
	if _, err := decodeSitePolicy(bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown field error = %v", err)
	}
}

func TestTodo_TCLOCK_010_SitePolicyRejectsMissingDomainPolicy(t *testing.T) {
	p := validSitePolicy()
	p.AttestationQuestions.ID = ""
	if _, err := encodeSitePolicy(p); !errors.Is(err, punchpolicy.ErrInvalidQuestionSet) {
		t.Fatalf("error = %v", err)
	}
	p = validSitePolicy()
	p.MaxOfflineAge = 0
	if _, err := encodeSitePolicy(p); !errors.Is(err, ErrInvalid) {
		t.Fatalf("offline age error = %v", err)
	}
}

func TestTodo_TCLOCK_004_SitePolicyStoreIntegration(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	from := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	policy := validSitePolicy()
	payload, err := encodeSitePolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	version := SitePolicyVersion{TenantID: "tenant-a", SiteID: "site-a", ProfileID: "profile-a", PolicyID: "published", Version: 1, EffectiveFrom: from, Policy: policy, Digest: canonicalSitePolicyDigest(payload), PublishedBy: "root-principal"}
	if err := s.PublishSitePolicy(ctx, "tenant-a", version); err != nil {
		t.Fatalf("publish: %v", err)
	}
	got, err := s.CurrentSitePolicyAt(ctx, "tenant-a", "site-a", "profile-a", from.Add(time.Minute))
	if err != nil || got.Version != 1 || got.Policy.SnapshotRevision != "snapshot-1" {
		t.Fatalf("read = %+v err=%v", got, err)
	}
	if _, err := s.CurrentSitePolicyAt(ctx, "tenant-b", "site-a", "profile-a", from.Add(time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross tenant read = %v", err)
	}
	version.Version = 3
	if err := s.PublishSitePolicy(ctx, "tenant-a", version); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("out of order publish = %v", err)
	}
}
