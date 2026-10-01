package timeclockstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/punchpolicy"
)

func TestTodo_FTIME_003_PolicyAdapterProjectsPublishedPolicyAndFailsClosed(t *testing.T) {
	store, tenant := adapterFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 14, 0, 0, 0, time.UTC)
	policy := timestore.SitePolicy{
		PunchPolicy:          punchpolicy.Policy{ID: "punch", Version: 1, Jurisdiction: "US-CA"},
		AttestationQuestions: punchpolicy.QuestionSet{ID: "questions", Version: 1, Jurisdiction: "US-CA", Questions: []punchpolicy.Question{{ID: "break", Kind: punchpolicy.QuestionBreakProvided, TextKey: "clock.break", Required: true}}},
		IdentificationPolicy: clockdomain.RateLimitPolicy{MaxAttempts: 5, Window: 15 * time.Minute, LockoutDuration: 15 * time.Minute},
		FleetHealthPolicy:    clockdomain.FleetHealthPolicy{MaxDrift: time.Minute, MinimumVersion: "1.0.0", MissingHeartbeatLimit: 10 * time.Minute},
		MaxOfflineAge:        2 * time.Hour, SnapshotRevision: "policy-snapshot-1",
		JobCodes: []timestore.SiteJobCode{{Code: "JOB-1", Name: "Floor"}}, Tips: timestore.SiteTipRules{Enabled: true, Currency: "USD"}, LocalizedStrings: map[string]string{"clock.break": "Break"},
	}
	payload, err := json.Marshal(struct {
		PunchPolicy          punchpolicy.Policy            `json:"punch_policy"`
		AttestationQuestions punchpolicy.QuestionSet       `json:"attestation_questions"`
		IdentificationPolicy clockdomain.RateLimitPolicy   `json:"identification_policy"`
		FleetHealthPolicy    clockdomain.FleetHealthPolicy `json:"fleet_health_policy"`
		MaxOfflineAgeSeconds int64                         `json:"max_offline_age_seconds"`
		JobCodes             []timestore.SiteJobCode       `json:"job_codes"`
		Tips                 timestore.SiteTipRules        `json:"tips"`
		LocalizedStrings     map[string]string             `json:"localized_strings"`
		SnapshotRevision     string                        `json:"snapshot_revision"`
	}{policy.PunchPolicy, policy.AttestationQuestions, policy.IdentificationPolicy, policy.FleetHealthPolicy, int64(policy.MaxOfflineAge.Seconds()), policy.JobCodes, policy.Tips, policy.LocalizedStrings, policy.SnapshotRevision})
	if err != nil {
		t.Fatal(err)
	}
	var normalized any
	if err := json.Unmarshal(payload, &normalized); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	for _, profile := range []string{"", "profile-1"} {
		version := timestore.SitePolicyVersion{TenantID: tenant, SiteID: "site-1", ProfileID: profile, PolicyID: "published", Version: 1, EffectiveFrom: now.Add(-time.Hour), Policy: policy, Digest: "sha256:" + hex.EncodeToString(digest[:]), PublishedBy: "admin"}
		if err := store.PublishSitePolicy(ctx, tenant, version); err != nil {
			t.Fatal(err)
		}
	}
	adapter := NewSitePolicyAdapter(store, func() time.Time { return now })
	punch, err := adapter.PunchPolicy(ctx, tenant, "site-1")
	if err != nil || punch.ID != policy.PunchPolicy.ID {
		t.Fatalf("punch=%+v err=%v", punch, err)
	}
	questions, err := adapter.AttestationQuestions(ctx, tenant, "site-1")
	if err != nil || questions.ID != policy.AttestationQuestions.ID {
		t.Fatalf("questions=%+v err=%v", questions, err)
	}
	identification, err := adapter.IdentificationPolicy(ctx, tenant, "site-1", "ignored-device")
	if err != nil || identification.MaxAttempts != 5 {
		t.Fatalf("identification=%+v err=%v", identification, err)
	}
	fleet, err := adapter.FleetHealthPolicy(ctx, tenant, "site-1", "profile-1")
	if err != nil || fleet.MinimumVersion != "1.0.0" {
		t.Fatalf("fleet=%+v err=%v", fleet, err)
	}
	metadata, err := adapter.Metadata(ctx, tenant, "site-1", "profile-1")
	if err != nil || metadata.SnapshotRevision != policy.SnapshotRevision || metadata.JobCodes[0].Code != "JOB-1" || metadata.Tips.Currency != "USD" || metadata.LocalizedStrings["clock.break"] != "Break" {
		t.Fatalf("metadata=%+v err=%v", metadata, err)
	}
	alias, err := adapter.RosterMetadata(ctx, tenant, "site-1", "profile-1")
	if err != nil || alias.SnapshotRevision != metadata.SnapshotRevision {
		t.Fatalf("alias=%+v err=%v", alias, err)
	}
	if _, err := adapter.PunchPolicy(ctx, "other-tenant", "site-1"); err == nil {
		t.Fatal("cross-tenant policy read succeeded")
	}
	if _, err := adapter.FleetHealthPolicy(ctx, tenant, "site-1", "missing-profile"); err == nil {
		t.Fatal("missing profile fell back to another profile")
	}
	if _, err := (PolicyAdapter{}).PunchPolicy(ctx, tenant, "site-1"); err != clockservice.ErrUnavailable {
		t.Fatalf("missing adapter dependency error=%v", err)
	}
	if _, err := (PolicyAdapter{Store: store}).Metadata(ctx, tenant, "site-1", "profile-1"); err != clockservice.ErrUnavailable {
		t.Fatalf("missing clock error=%v", err)
	}
}
