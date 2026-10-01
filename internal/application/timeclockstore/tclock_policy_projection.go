package timeclockstore

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/punchpolicy"
)

// PolicyAdapter exposes the published site-policy read model through every
// clock-service policy port. It never accepts policy values from a device.
type PolicyAdapter struct {
	Store *timestore.Store
	Now   func() time.Time
}

// MetadataPort is the narrow roster dependency for offline policy metadata.
// Worker and shift projection remain separate sources and are never inferred
// from this policy adapter.
type MetadataPort interface {
	Metadata(context.Context, string, string, string) (RosterPolicyMetadata, error)
}

var _ clockservice.PolicySource = PolicyAdapter{}
var _ clockservice.IdentificationPolicySource = PolicyAdapter{}
var _ clockservice.HeartbeatPolicySource = PolicyAdapter{}
var _ MetadataPort = PolicyAdapter{}

// NewPolicyAdapter constructs the production policy read-model adapter.
func NewPolicyAdapter(store *timestore.Store, now func() time.Time) PolicyAdapter {
	return PolicyAdapter{Store: store, Now: now}
}

// NewSitePolicyAdapter is the descriptive constructor alias used by clock
// composition code.
func NewSitePolicyAdapter(store *timestore.Store, now func() time.Time) PolicyAdapter {
	return NewPolicyAdapter(store, now)
}

// PunchPolicy returns the effective, validated site punch policy.
func (a PolicyAdapter) PunchPolicy(ctx context.Context, tenant, site string) (punchpolicy.Policy, error) {
	v, err := a.current(ctx, tenant, site, "")
	if err != nil {
		return punchpolicy.Policy{}, err
	}
	return v.Policy.PunchPolicy, nil
}

// AttestationQuestions returns the effective, validated site question set.
func (a PolicyAdapter) AttestationQuestions(ctx context.Context, tenant, site string) (punchpolicy.QuestionSet, error) {
	v, err := a.current(ctx, tenant, site, "")
	if err != nil {
		return punchpolicy.QuestionSet{}, err
	}
	return v.Policy.AttestationQuestions, nil
}

// IdentificationPolicy returns the server-side lockout policy. device is
// intentionally ignored: the authenticated service resolves its site before
// calling this port, so a client cannot select another site's policy.
func (a PolicyAdapter) IdentificationPolicy(ctx context.Context, tenant, site, device string) (clockdomain.RateLimitPolicy, error) {
	v, err := a.current(ctx, tenant, site, "")
	if err != nil {
		return clockdomain.RateLimitPolicy{}, err
	}
	return v.Policy.IdentificationPolicy, nil
}

// FleetHealthPolicy returns the profile-specific fleet policy, falling back
// only to an explicitly published site profile (empty profile is a stored
// site-wide policy, not an in-code default).
func (a PolicyAdapter) FleetHealthPolicy(ctx context.Context, tenant, site, profile string) (clockdomain.FleetHealthPolicy, error) {
	v, err := a.current(ctx, tenant, site, profile)
	if err != nil {
		return clockdomain.FleetHealthPolicy{}, err
	}
	return v.Policy.FleetHealthPolicy, nil
}

// RosterPolicyMetadata is the policy portion of a device roster snapshot.
type RosterPolicyMetadata struct {
	SnapshotRevision string
	MaxOfflineAge    time.Duration
	JobCodes         []clockservice.JobCode
	Questions        punchpolicy.QuestionSet
	Tips             clockservice.TipRules
	LocalizedStrings map[string]string
}

// Metadata resolves the policy metadata required for offline roster use.
func (a PolicyAdapter) Metadata(ctx context.Context, tenant, site, profile string) (RosterPolicyMetadata, error) {
	v, err := a.current(ctx, tenant, site, profile)
	if err != nil {
		return RosterPolicyMetadata{}, err
	}
	jobCodes := make([]clockservice.JobCode, len(v.Policy.JobCodes))
	for i, j := range v.Policy.JobCodes {
		jobCodes[i] = clockservice.JobCode{Code: j.Code, Name: j.Name}
	}
	stringsCopy := make(map[string]string, len(v.Policy.LocalizedStrings))
	for k, val := range v.Policy.LocalizedStrings {
		stringsCopy[k] = val
	}
	return RosterPolicyMetadata{SnapshotRevision: v.Policy.SnapshotRevision, MaxOfflineAge: v.Policy.MaxOfflineAge, JobCodes: jobCodes, Questions: v.Policy.AttestationQuestions, Tips: clockservice.TipRules{Enabled: v.Policy.Tips.Enabled, Currency: v.Policy.Tips.Currency}, LocalizedStrings: stringsCopy}, nil
}

// RosterMetadata is an alias used by roster composition code.
func (a PolicyAdapter) RosterMetadata(ctx context.Context, tenant, site, profile string) (RosterPolicyMetadata, error) {
	return a.Metadata(ctx, tenant, site, profile)
}

func (a PolicyAdapter) current(ctx context.Context, tenant, site, profile string) (timestore.SitePolicyVersion, error) {
	if a.Store == nil || a.Now == nil {
		return timestore.SitePolicyVersion{}, clockservice.ErrUnavailable
	}
	now := a.Now().UTC()
	return a.Store.CurrentSitePolicyAt(ctx, tenant, site, profile, now)
}
