// SURVEY-007: close and archive survey campaign.
//
// CloseCampaign locks a campaign to new responses, freezes its result
// and evidence digests, and applies the retention, hold and destruction
// schedule. The archive preserves a minimum anonymous chronology —
// counts per period, never identities. A privacy-cohort or retention
// failure never archives: it returns SURVEY_007_REJECTED with field,
// state and version and persists nothing. The function is kernel-pure.
package survey

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// CloseVersion is the rejection version for campaign closure.
const CloseVersion = "survey-close/v1"

var (
	// ErrCloseRejected is the SURVEY-007 sentinel. A privacy-cohort or
	// retention failure that still archived would be the seeded defect;
	// instead it fails with this error carrying field, state and version.
	ErrCloseRejected = errors.New("SURVEY_007_REJECTED")
)

// CloseRejection is the stable SURVEY-007 failure shape.
type CloseRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *CloseRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrCloseRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the SURVEY_007_REJECTED sentinel to errors.Is.
func (r *CloseRejection) Unwrap() error { return ErrCloseRejected }

func closeReject(field, state, reason string) error {
	return &CloseRejection{Field: field, State: state, Version: CloseVersion, Reason: reason}
}

// RetentionPolicy is the archive schedule for one campaign.
type RetentionPolicy struct {
	RetainUntil    time.Time
	LegalHold      bool
	DestructionDue time.Time
	ChronologyOnly bool
}

// CloseInput is one campaign closure request. ResultDigest and
// EvidenceDigest are the frozen SURVEY-006 outputs being archived.
type CloseInput struct {
	Tenant         string
	CampaignRef    string
	CohortCount    int
	MinimumCohort  int
	ResultDigest   string
	EvidenceDigest string
	Retention      RetentionPolicy
	ClosedAt       time.Time
	AuthorityRef   string
}

// ChronologyBucket is one anonymous period count preserved in the
// archive. It carries no respondent identity.
type ChronologyBucket struct {
	Period        string
	ResponseCount int
}

// ClosedCampaign is the sealed archive record.
type ClosedCampaign struct {
	Tenant         string
	CampaignRef    string
	LockedAt       time.Time
	ResultDigest   string
	EvidenceDigest string
	RetainUntil    time.Time
	LegalHold      bool
	DestructionDue time.Time
	Chronology     []ChronologyBucket
	Digest         string
}

func (c ClosedCampaign) computedDigest() string {
	buckets := make([]string, 0, len(c.Chronology))
	for _, b := range c.Chronology {
		buckets = append(buckets, fmt.Sprintf("%s:%d", b.Period, b.ResponseCount))
	}
	sort.Strings(buckets)
	w := canonicalbytes.New("hcmnext.domains.survey.ClosedCampaign", 1).
		String("tenant", c.Tenant).
		String("campaign", c.CampaignRef).
		String("locked_at", c.LockedAt.UTC().Format(time.RFC3339)).
		String("result", c.ResultDigest).
		String("evidence", c.EvidenceDigest).
		String("retain_until", c.RetainUntil.UTC().Format(time.RFC3339)).
		Bool("legal_hold", c.LegalHold).
		String("destruction_due", c.DestructionDue.UTC().Format(time.RFC3339)).
		SortedStrings("chronology", buckets)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// CloseCampaign locks responses, freezes result and evidence, and applies
// the retention schedule for one campaign.
func CloseCampaign(in CloseInput, chronology []ChronologyBucket) (ClosedCampaign, error) {
	if strings.TrimSpace(in.Tenant) == "" {
		return ClosedCampaign{}, closeReject("close.tenant", "MISSING", "tenant is required")
	}
	if strings.TrimSpace(in.CampaignRef) == "" {
		return ClosedCampaign{}, closeReject("close.campaign_ref", "MISSING", "campaign ref is required")
	}
	minimum := in.MinimumCohort
	if minimum <= 0 {
		minimum = MinReleaseCohort
	}
	if in.CohortCount < minimum {
		return ClosedCampaign{}, closeReject("close.cohort", "BELOW_MINIMUM", fmt.Sprintf("cohort %d below minimum %d", in.CohortCount, minimum))
	}
	if strings.TrimSpace(in.ResultDigest) == "" || strings.TrimSpace(in.EvidenceDigest) == "" {
		return ClosedCampaign{}, closeReject("close.frozen", "MISSING", "result and evidence digests are required")
	}
	if in.ClosedAt.IsZero() {
		return ClosedCampaign{}, closeReject("close.closed_at", "MISSING", "closure instant is required")
	}
	if strings.TrimSpace(in.AuthorityRef) == "" {
		return ClosedCampaign{}, closeReject("close.authority_ref", "MISSING", "authority ref is required")
	}
	if in.Retention.RetainUntil.IsZero() {
		return ClosedCampaign{}, closeReject("close.retention.retain_until", "MISSING", "retention horizon is required")
	}
	if !in.Retention.RetainUntil.After(in.ClosedAt) {
		return ClosedCampaign{}, closeReject("close.retention.retain_until", "EXPIRED", "retention horizon must follow closure")
	}
	if !in.Retention.LegalHold && in.Retention.DestructionDue.IsZero() {
		return ClosedCampaign{}, closeReject("close.retention.destruction_due", "MISSING", "destruction is scheduled unless a legal hold applies")
	}
	if in.Retention.LegalHold && !in.Retention.DestructionDue.IsZero() {
		return ClosedCampaign{}, closeReject("close.retention.destruction_due", "HELD", "held records carry no destruction date")
	}
	total := 0
	for i, b := range chronology {
		if strings.TrimSpace(b.Period) == "" || b.ResponseCount < 0 {
			return ClosedCampaign{}, closeReject(fmt.Sprintf("close.chronology[%d]", i), "INVALID", "chronology buckets carry period and non-negative count only")
		}
		total += b.ResponseCount
	}
	if total != in.CohortCount {
		return ClosedCampaign{}, closeReject("close.chronology", "MISMATCH", fmt.Sprintf("chronology sums to %d, cohort is %d", total, in.CohortCount))
	}
	closed := ClosedCampaign{
		Tenant: in.Tenant, CampaignRef: in.CampaignRef, LockedAt: in.ClosedAt.UTC(),
		ResultDigest: in.ResultDigest, EvidenceDigest: in.EvidenceDigest,
		RetainUntil: in.Retention.RetainUntil.UTC(), LegalHold: in.Retention.LegalHold,
		DestructionDue: in.Retention.DestructionDue.UTC(),
		Chronology:     append([]ChronologyBucket(nil), chronology...),
	}
	closed.Digest = closed.computedDigest()
	return closed, nil
}
