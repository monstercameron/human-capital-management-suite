package timestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/punchpolicy"
)

// SitePolicy is the complete published read model consumed by clock devices.
// It contains no defaults: all policy and roster metadata needed offline is
// published in one versioned payload.
type SitePolicy struct {
	PunchPolicy          punchpolicy.Policy
	AttestationQuestions punchpolicy.QuestionSet
	IdentificationPolicy clockdomain.RateLimitPolicy
	FleetHealthPolicy    clockdomain.FleetHealthPolicy
	MaxOfflineAge        time.Duration
	JobCodes             []SiteJobCode
	Tips                 SiteTipRules
	LocalizedStrings     map[string]string
	SnapshotRevision     string
}

// SiteJobCode is a bounded job/cost code in a published device snapshot.
type SiteJobCode struct{ Code, Name string }

// SiteTipRules describes the tip declaration controls shown at a device.
type SiteTipRules struct {
	Enabled  bool
	Currency string
}

// SitePolicyVersion is one immutable effective-dated site policy row.
type SitePolicyVersion struct {
	TenantID, SiteID, ProfileID, PolicyID string
	Version                               int64
	EffectiveFrom, EffectiveTo            time.Time
	Policy                                SitePolicy
	Payload                               json.RawMessage
	Digest, PublishedBy                   string
}

// SitePolicyAt reads the one published policy effective at at. A non-empty
// profile selects that profile's policy; callers never receive a silent site
// default when no matching policy exists.
func (s *Store) SitePolicyAt(ctx context.Context, tenant, site, profile string, at time.Time) (SitePolicyVersion, error) {
	if s == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(site) == "" || at.IsZero() {
		return SitePolicyVersion{}, ErrInvalid
	}
	var out SitePolicyVersion
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var effectiveTo *time.Time
		var payload []byte
		row := tx.QueryRow(ctx, `SELECT tenant_id,site_id,profile_id,policy_id,version,effective_from,effective_to,snapshot_revision,max_offline_age_seconds,payload,digest,published_by
			FROM time_site_policy_version WHERE tenant_id=$1 AND site_id=$2 AND profile_id=$3 AND effective_from <= $4 AND (effective_to IS NULL OR effective_to > $4)
			ORDER BY version DESC LIMIT 1`, tenant, site, profile, at)
		var snapshot string
		var maxAge int64
		if err := row.Scan(&out.TenantID, &out.SiteID, &out.ProfileID, &out.PolicyID, &out.Version, &out.EffectiveFrom, &effectiveTo, &snapshot, &maxAge, &payload, &out.Digest, &out.PublishedBy); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if effectiveTo != nil {
			out.EffectiveTo = *effectiveTo
		}
		out.Payload = append(json.RawMessage(nil), payload...)
		policy, err := decodeSitePolicy(payload)
		if err != nil {
			return err
		}
		if policy.SnapshotRevision != snapshot || policy.MaxOfflineAge != time.Duration(maxAge)*time.Second || canonicalSitePolicyDigest(payload) != out.Digest {
			return ErrInvalid
		}
		out.Policy = policy
		return nil
	})
	if err != nil {
		return SitePolicyVersion{}, err
	}
	return out, nil
}

// CurrentSitePolicyAt returns the highest published policy version effective
// for a site/profile. It is the read-model entry point used by clock service
// ports, where the policy identifier is intentionally not client supplied.
func (s *Store) CurrentSitePolicyAt(ctx context.Context, tenant, site, profile string, at time.Time) (SitePolicyVersion, error) {
	if s == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(site) == "" || at.IsZero() {
		return SitePolicyVersion{}, ErrInvalid
	}
	var out SitePolicyVersion
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var effectiveTo *time.Time
		var payload []byte
		var snapshot string
		var maxAge int64
		row := tx.QueryRow(ctx, `SELECT tenant_id,site_id,profile_id,policy_id,version,effective_from,effective_to,snapshot_revision,max_offline_age_seconds,payload,digest,published_by
			FROM time_site_policy_version WHERE tenant_id=$1 AND site_id=$2 AND profile_id=$3 AND effective_from <= $4 AND (effective_to IS NULL OR effective_to > $4)
			ORDER BY version DESC, policy_id DESC LIMIT 1`, tenant, site, profile, at)
		if err := row.Scan(&out.TenantID, &out.SiteID, &out.ProfileID, &out.PolicyID, &out.Version, &out.EffectiveFrom, &effectiveTo, &snapshot, &maxAge, &payload, &out.Digest, &out.PublishedBy); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if effectiveTo != nil {
			out.EffectiveTo = *effectiveTo
		}
		out.Payload = append(json.RawMessage(nil), payload...)
		policy, err := decodeSitePolicy(payload)
		if err != nil {
			return err
		}
		if policy.SnapshotRevision != snapshot || int64(policy.MaxOfflineAge/time.Second) != maxAge || canonicalSitePolicyDigest(payload) != out.Digest {
			return ErrInvalid
		}
		out.Policy = policy
		return nil
	})
	if err != nil {
		return SitePolicyVersion{}, err
	}
	return out, nil
}

// PublishSitePolicy appends the next version in one tenant transaction. The
// caller must supply an already-authorized principal identifier; this store
// never accepts role claims or performs authorization itself.
func (s *Store) PublishSitePolicy(ctx context.Context, tenant string, v SitePolicyVersion) error {
	if s == nil || strings.TrimSpace(tenant) == "" || v.TenantID != tenant || v.SiteID == "" || v.PolicyID == "" || v.Version <= 0 || v.PublishedBy == "" || v.EffectiveFrom.IsZero() {
		return ErrInvalid
	}
	if !v.EffectiveTo.IsZero() && !v.EffectiveTo.After(v.EffectiveFrom) {
		return ErrInvalid
	}
	payload, err := encodeSitePolicy(v.Policy)
	if err != nil {
		return err
	}
	if len(v.Payload) > 0 && !jsonEqualBytes(v.Payload, payload) {
		return ErrInvalid
	}
	if v.Digest != canonicalSitePolicyDigest(payload) {
		return ErrInvalid
	}
	maxAge := int64(v.Policy.MaxOfflineAge / time.Second)
	if maxAge <= 0 {
		return ErrInvalid
	}
	v.Payload = payload
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		// Serialize publishers for this tenant/site/profile/policy key before
		// reading the next version. The row may not exist yet, so a row lock
		// alone cannot protect two first publishers.
		lockKey := fmt.Sprintf("%s|%s|%s|%s", tenant, v.SiteID, v.ProfileID, v.PolicyID)
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
			return err
		}
		var maxVersion int64
		row := tx.QueryRow(ctx, `SELECT version FROM time_site_policy_version WHERE tenant_id=$1 AND site_id=$2 AND profile_id=$3 AND policy_id=$4 ORDER BY version DESC LIMIT 1`, tenant, v.SiteID, v.ProfileID, v.PolicyID)
		if err := row.Scan(&maxVersion); err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if v.Version != maxVersion+1 {
			return ErrRevisionConflict
		}
		_, err := tx.Exec(ctx, `INSERT INTO time_site_policy_version(tenant_id,site_id,profile_id,policy_id,version,effective_from,effective_to,snapshot_revision,max_offline_age_seconds,payload,digest,published_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12)`, tenant, v.SiteID, v.ProfileID, v.PolicyID, v.Version, v.EffectiveFrom, tclockSitePolicyNullableTime(v.EffectiveTo), v.Policy.SnapshotRevision, maxAge, []byte(payload), v.Digest, v.PublishedBy)
		return err
	})
}

func tclockSitePolicyNullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
func jsonEqualBytes(a, b []byte) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && string(mustJSON(x)) == string(mustJSON(y))
}

func canonicalSitePolicyDigest(payload []byte) string {
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return ""
	}
	payload = mustJSON(value)
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

type sitePolicyJSON struct {
	PunchPolicy          punchpolicy.Policy            `json:"punch_policy"`
	AttestationQuestions punchpolicy.QuestionSet       `json:"attestation_questions"`
	IdentificationPolicy clockdomain.RateLimitPolicy   `json:"identification_policy"`
	FleetHealthPolicy    clockdomain.FleetHealthPolicy `json:"fleet_health_policy"`
	MaxOfflineAgeSeconds int64                         `json:"max_offline_age_seconds"`
	JobCodes             []SiteJobCode                 `json:"job_codes"`
	Tips                 SiteTipRules                  `json:"tips"`
	LocalizedStrings     map[string]string             `json:"localized_strings"`
	SnapshotRevision     string                        `json:"snapshot_revision"`
}

func encodeSitePolicy(p SitePolicy) (json.RawMessage, error) {
	if err := p.PunchPolicy.Validate(); err != nil {
		return nil, err
	}
	if err := p.AttestationQuestions.Validate(); err != nil {
		return nil, err
	}
	if err := p.IdentificationPolicy.Validate(); err != nil {
		return nil, err
	}
	if err := p.FleetHealthPolicy.Validate(); err != nil {
		return nil, err
	}
	if p.MaxOfflineAge <= 0 || p.SnapshotRevision == "" {
		return nil, ErrInvalid
	}
	b, err := json.Marshal(sitePolicyJSON{p.PunchPolicy, p.AttestationQuestions, p.IdentificationPolicy, p.FleetHealthPolicy, int64(p.MaxOfflineAge / time.Second), p.JobCodes, p.Tips, p.LocalizedStrings, p.SnapshotRevision})
	if err != nil {
		return nil, err
	}
	return b, nil
}
func decodeSitePolicy(b []byte) (SitePolicy, error) {
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	var w sitePolicyJSON
	if err := dec.Decode(&w); err != nil {
		return SitePolicy{}, ErrInvalid
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return SitePolicy{}, ErrInvalid
	} else if !errors.Is(err, io.EOF) {
		return SitePolicy{}, ErrInvalid
	}
	p := SitePolicy{PunchPolicy: w.PunchPolicy, AttestationQuestions: w.AttestationQuestions, IdentificationPolicy: w.IdentificationPolicy, FleetHealthPolicy: w.FleetHealthPolicy, MaxOfflineAge: time.Duration(w.MaxOfflineAgeSeconds) * time.Second, JobCodes: w.JobCodes, Tips: w.Tips, LocalizedStrings: w.LocalizedStrings, SnapshotRevision: w.SnapshotRevision}
	if _, err := encodeSitePolicy(p); err != nil {
		return SitePolicy{}, ErrInvalid
	}
	return p, nil
}
