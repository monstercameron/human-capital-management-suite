package timestore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ProfileVersion is one published, immutable version of a WTIME-001 time
// profile. Publish only ever inserts a new row; time_forbid_mutation blocks
// every later UPDATE or DELETE, so a new version can never rewrite an old
// one's payload.
type ProfileVersion struct {
	TenantID      string
	ProfileID     string
	Version       int64
	EffectiveFrom time.Time
	EffectiveTo   time.Time // zero means open-ended
	Payload       json.RawMessage
	Digest        string
	PublishedBy   string
	CreatedAt     time.Time
}

// EligibilityRuleVersion is one published, immutable version of a WTIME-001
// eligibility rule.
type EligibilityRuleVersion struct {
	TenantID    string
	RuleID      string
	Version     int64
	Payload     json.RawMessage
	Digest      string
	PublishedBy string
	CreatedAt   time.Time
}

// AssignmentProfilePin is the per-assignment resolved profile pin (WTIME-002):
// the one profile version an assignment's current session or period run
// keeps for its lifetime. It is mutated in place under an optimistic
// revision guard, never replayed from history, because only the latest pin
// is ever meaningful to a caller resolving "now".
type AssignmentProfilePin struct {
	TenantID       string
	AssignmentRef  string
	Revision       int64
	ProfileID      string
	ProfileVersion int64
	ResolvedAt     time.Time
	UpdatedAt      time.Time
}

// PublishProfileVersion appends the next version of a time profile. version
// must be exactly one greater than the highest version already published for
// profileID (or 1 for the first publish); any other value is
// ErrRevisionConflict, which keeps a caller from publishing out of order or
// silently overwriting a version another actor just published.
func (s *Store) PublishProfileVersion(ctx context.Context, tenant string, v ProfileVersion) error {
	if tenant == "" || v.TenantID != tenant || v.ProfileID == "" || v.Version <= 0 || v.PublishedBy == "" || v.Digest == "" || v.EffectiveFrom.IsZero() {
		return ErrInvalid
	}
	if !v.EffectiveTo.IsZero() && !v.EffectiveTo.After(v.EffectiveFrom) {
		return ErrInvalid
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var maxVersion int64
		err := tx.QueryRow(ctx, `SELECT coalesce(max(version),0) FROM time_profile_version WHERE tenant_id=$1 AND profile_id=$2`, tenant, v.ProfileID).Scan(&maxVersion)
		if err != nil {
			return err
		}
		if v.Version != maxVersion+1 {
			return ErrRevisionConflict
		}
		var effectiveTo any
		if !v.EffectiveTo.IsZero() {
			effectiveTo = v.EffectiveTo
		}
		_, err = tx.Exec(ctx, `INSERT INTO time_profile_version(tenant_id,profile_id,version,effective_from,effective_to,payload,digest,published_by) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,$8)`,
			tenant, v.ProfileID, v.Version, v.EffectiveFrom, effectiveTo, []byte(v.Payload), v.Digest, v.PublishedBy)
		return err
	})
}

// ProfileVersionAt returns the profile version whose effective window covers
// at, or ErrNotFound when no published version does. An assignment with no
// resolvable profile cannot record time (WTIME-001 GREEN); returning
// ErrNotFound rather than a zero value is what keeps a caller from treating
// "no profile" as a default profile.
func (s *Store) ProfileVersionAt(ctx context.Context, tenant, profileID string, at time.Time) (ProfileVersion, error) {
	if tenant == "" || profileID == "" || at.IsZero() {
		return ProfileVersion{}, ErrInvalid
	}
	var v ProfileVersion
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var effectiveTo *time.Time
		var payload []byte
		row := tx.QueryRow(ctx, `SELECT tenant_id,profile_id,version,effective_from,effective_to,payload,digest,published_by,created_at
			FROM time_profile_version WHERE tenant_id=$1 AND profile_id=$2 AND effective_from<=$3 AND (effective_to IS NULL OR effective_to>$3)
			ORDER BY version DESC LIMIT 1`, tenant, profileID, at)
		if err := row.Scan(&v.TenantID, &v.ProfileID, &v.Version, &v.EffectiveFrom, &effectiveTo, &payload, &v.Digest, &v.PublishedBy, &v.CreatedAt); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if effectiveTo != nil {
			v.EffectiveTo = *effectiveTo
		}
		v.Payload = append(json.RawMessage(nil), payload...)
		return nil
	})
	if err != nil {
		return ProfileVersion{}, err
	}
	return v, nil
}

// ListProfileVersions returns every published version of profileID, oldest
// first, so a caller can prove a later publish never altered an earlier one.
func (s *Store) ListProfileVersions(ctx context.Context, tenant, profileID string) ([]ProfileVersion, error) {
	if tenant == "" || profileID == "" {
		return nil, ErrInvalid
	}
	out := make([]ProfileVersion, 0)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id,profile_id,version,effective_from,effective_to,payload,digest,published_by,created_at
			FROM time_profile_version WHERE tenant_id=$1 AND profile_id=$2 ORDER BY version`, tenant, profileID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v ProfileVersion
			var effectiveTo *time.Time
			var payload []byte
			if err := rows.Scan(&v.TenantID, &v.ProfileID, &v.Version, &v.EffectiveFrom, &effectiveTo, &payload, &v.Digest, &v.PublishedBy, &v.CreatedAt); err != nil {
				return err
			}
			if effectiveTo != nil {
				v.EffectiveTo = *effectiveTo
			}
			v.Payload = append(json.RawMessage(nil), payload...)
			out = append(out, v)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PublishEligibilityRuleVersion appends the next version of an eligibility
// rule, with the same strictly-sequential and append-only guarantee as
// PublishProfileVersion.
func (s *Store) PublishEligibilityRuleVersion(ctx context.Context, tenant string, v EligibilityRuleVersion) error {
	if tenant == "" || v.TenantID != tenant || v.RuleID == "" || v.Version <= 0 || v.PublishedBy == "" || v.Digest == "" {
		return ErrInvalid
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var maxVersion int64
		err := tx.QueryRow(ctx, `SELECT coalesce(max(version),0) FROM time_eligibility_rule_version WHERE tenant_id=$1 AND rule_id=$2`, tenant, v.RuleID).Scan(&maxVersion)
		if err != nil {
			return err
		}
		if v.Version != maxVersion+1 {
			return ErrRevisionConflict
		}
		_, err = tx.Exec(ctx, `INSERT INTO time_eligibility_rule_version(tenant_id,rule_id,version,payload,digest,published_by) VALUES($1,$2,$3,$4::jsonb,$5,$6)`,
			tenant, v.RuleID, v.Version, []byte(v.Payload), v.Digest, v.PublishedBy)
		return err
	})
}

// LatestEligibilityRuleVersion returns the highest published version of
// ruleID.
func (s *Store) LatestEligibilityRuleVersion(ctx context.Context, tenant, ruleID string) (EligibilityRuleVersion, error) {
	if tenant == "" || ruleID == "" {
		return EligibilityRuleVersion{}, ErrInvalid
	}
	var v EligibilityRuleVersion
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var payload []byte
		row := tx.QueryRow(ctx, `SELECT tenant_id,rule_id,version,payload,digest,published_by,created_at
			FROM time_eligibility_rule_version WHERE tenant_id=$1 AND rule_id=$2 ORDER BY version DESC LIMIT 1`, tenant, ruleID)
		if err := row.Scan(&v.TenantID, &v.RuleID, &v.Version, &payload, &v.Digest, &v.PublishedBy, &v.CreatedAt); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		v.Payload = append(json.RawMessage(nil), payload...)
		return nil
	})
	if err != nil {
		return EligibilityRuleVersion{}, err
	}
	return v, nil
}

// PinAssignmentProfile resolves-and-pins, or repins, the profile version an
// assignment's aggregate keeps. expectedRevision is 0 for the first pin (no
// row exists yet) and the last-observed revision for a repin; a mismatch is
// ErrRevisionConflict, which is what stops a repin from silently replacing a
// pin an open run is still relying on.
func (s *Store) PinAssignmentProfile(ctx context.Context, tenant, assignmentRef, profileID string, profileVersion, expectedRevision int64, resolvedAt time.Time) (AssignmentProfilePin, error) {
	if tenant == "" || assignmentRef == "" || profileID == "" || profileVersion <= 0 || expectedRevision < 0 || resolvedAt.IsZero() {
		return AssignmentProfilePin{}, ErrInvalid
	}
	var out AssignmentProfilePin
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if expectedRevision == 0 {
			_, err := tx.Exec(ctx, `INSERT INTO time_assignment_profile_pin(tenant_id,assignment_ref,revision,profile_id,profile_version,resolved_at) VALUES($1,$2,1,$3,$4,$5)`,
				tenant, assignmentRef, profileID, profileVersion, resolvedAt)
			if err != nil {
				return err
			}
			out = AssignmentProfilePin{TenantID: tenant, AssignmentRef: assignmentRef, Revision: 1, ProfileID: profileID, ProfileVersion: profileVersion, ResolvedAt: resolvedAt}
			return nil
		}
		tag, err := tx.Exec(ctx, `UPDATE time_assignment_profile_pin SET revision=revision+1,profile_id=$1,profile_version=$2,resolved_at=$3,updated_at=now()
			WHERE tenant_id=$4 AND assignment_ref=$5 AND revision=$6`, profileID, profileVersion, resolvedAt, tenant, assignmentRef, expectedRevision)
		if err != nil {
			return err
		}
		if tag != 1 {
			return ErrRevisionConflict
		}
		out = AssignmentProfilePin{TenantID: tenant, AssignmentRef: assignmentRef, Revision: expectedRevision + 1, ProfileID: profileID, ProfileVersion: profileVersion, ResolvedAt: resolvedAt}
		return nil
	})
	if err != nil {
		return AssignmentProfilePin{}, err
	}
	return out, nil
}

// AssignmentProfilePinFor returns the current pin for assignmentRef.
func (s *Store) AssignmentProfilePinFor(ctx context.Context, tenant, assignmentRef string) (AssignmentProfilePin, error) {
	if tenant == "" || assignmentRef == "" {
		return AssignmentProfilePin{}, ErrInvalid
	}
	var out AssignmentProfilePin
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		row := tx.QueryRow(ctx, `SELECT tenant_id,assignment_ref,revision,profile_id,profile_version,resolved_at,updated_at
			FROM time_assignment_profile_pin WHERE tenant_id=$1 AND assignment_ref=$2`, tenant, assignmentRef)
		if err := row.Scan(&out.TenantID, &out.AssignmentRef, &out.Revision, &out.ProfileID, &out.ProfileVersion, &out.ResolvedAt, &out.UpdatedAt); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		return nil
	})
	if err != nil {
		return AssignmentProfilePin{}, err
	}
	return out, nil
}
