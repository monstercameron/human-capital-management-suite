// APP-006: certify marketplace applications.
//
// Certification is repeatable evidence, not a badge: CERTIFIED status holds
// only while all nine dimensions carry fresh, third-party evidence bound to
// the exact version digest. Missing or expired evidence returns a
// DECERTIFIED certification — the marketplace must show that status, never
// the stale certified one. This package stays pure: it judges evidence, it
// does not publish listings or call providers.
package partnerapp

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalidCertification  = errors.New("partnerapp: invalid certification evidence")
	ErrCertificationEvidence = errors.New("partnerapp: certification evidence missing or expired")
)

// CertificationStatus is the marketplace certification state of one version.
type CertificationStatus string

// Certification states.
const (
	StatusCertified   CertificationStatus = "CERTIFIED"
	StatusDecertified CertificationStatus = "DECERTIFIED"
)

// CertificationDimension names one of the nine repeatable evidence
// dimensions a certification requires.
type CertificationDimension string

// Certification dimensions.
const (
	DimensionSecurity      CertificationDimension = "SECURITY"
	DimensionScope         CertificationDimension = "SCOPE"
	DimensionCompatibility CertificationDimension = "COMPATIBILITY"
	DimensionTenancy       CertificationDimension = "TENANCY"
	DimensionScale         CertificationDimension = "SCALE"
	DimensionOutage        CertificationDimension = "OUTAGE"
	DimensionRevocation    CertificationDimension = "REVOCATION"
	DimensionUpgrade       CertificationDimension = "UPGRADE"
	DimensionExit          CertificationDimension = "EXIT"
)

// AllCertificationDimensions returns the nine dimensions in canonical order.
func AllCertificationDimensions() []CertificationDimension {
	return []CertificationDimension{
		DimensionSecurity, DimensionScope, DimensionCompatibility,
		DimensionTenancy, DimensionScale, DimensionOutage,
		DimensionRevocation, DimensionUpgrade, DimensionExit,
	}
}

// DimensionEvidence is one dimension's assessment pin: who assessed what
// evidence, when, and for how long the assessment stands.
type DimensionEvidence struct {
	Dimension   CertificationDimension
	EvidenceRef string
	Assessor    string
	AssessedAt  time.Time
	ValidFor    time.Duration
}

func (e DimensionEvidence) valid(requester string, at time.Time) error {
	known := false
	for _, d := range AllCertificationDimensions() {
		if d == e.Dimension {
			known = true
		}
	}
	if !known {
		return fmt.Errorf("%w: unknown dimension %q", ErrInvalidCertification, e.Dimension)
	}
	if strings.TrimSpace(e.EvidenceRef) == "" || strings.TrimSpace(e.EvidenceRef) != e.EvidenceRef {
		return fmt.Errorf("%w: dimension %s evidence ref is required", ErrInvalidCertification, e.Dimension)
	}
	if strings.TrimSpace(e.Assessor) == "" {
		return fmt.Errorf("%w: dimension %s assessor is required", ErrInvalidCertification, e.Dimension)
	}
	if e.Assessor == requester {
		return fmt.Errorf("%w: dimension %s assessor must differ from the requester", ErrInvalidCertification, e.Dimension)
	}
	if e.ValidFor <= 0 {
		return fmt.Errorf("%w: dimension %s validity window is required", ErrInvalidCertification, e.Dimension)
	}
	if e.AssessedAt.IsZero() || e.AssessedAt.After(at) {
		return fmt.Errorf("%w: dimension %s assessment is not yet made at certification time", ErrInvalidCertification, e.Dimension)
	}
	return nil
}

func (e DimensionEvidence) expiresAt() time.Time { return e.AssessedAt.Add(e.ValidFor) }

// Certification is the judged marketplace state of one application version.
type Certification struct {
	ApplicationID string
	Version       string
	Revision      uint64
	VersionDigest string
	Status        CertificationStatus
	ExpiresAt     time.Time
	Dimensions    []CertificationDimension
	Digest        string
}

// Explain renders the certification without exposing evidence contents.
func (c Certification) Explain() string {
	return fmt.Sprintf("partnerapp certification application=%s version=%s status=%s dimensions=%d expires=%s digest=%s",
		c.ApplicationID, c.Version, c.Status, len(c.Dimensions), c.ExpiresAt.UTC().Format(time.RFC3339), c.Digest)
}

// ValidAt reports whether the certification still stands at t.
func (c Certification) ValidAt(t time.Time) error {
	if c.Status != StatusCertified {
		return fmt.Errorf("%w: status is %s", ErrCertificationEvidence, c.Status)
	}
	if !t.Before(c.ExpiresAt) {
		return fmt.Errorf("%w: certification lapsed at %s", ErrCertificationEvidence, c.ExpiresAt.UTC().Format(time.RFC3339))
	}
	return nil
}

// Certify judges version against evidence at time at. A complete, fresh,
// third-party evidence set yields CERTIFIED; any missing or expired
// dimension yields DECERTIFIED with ErrCertificationEvidence so the caller
// replaces the displayed status. Malformed evidence refuses with
// ErrInvalidCertification and no status at all.
func Certify(version ApplicationVersion, evidence []DimensionEvidence, at time.Time) (Certification, error) {
	if version.Digest == "" || version.ApplicationID == "" || version.Version == "" {
		return Certification{}, fmt.Errorf("%w: certification requires a published version", ErrInvalidCertification)
	}
	if at.IsZero() {
		return Certification{}, fmt.Errorf("%w: certification time is required", ErrInvalidCertification)
	}
	seen := make(map[CertificationDimension]DimensionEvidence, len(evidence))
	for i, e := range evidence {
		if err := e.valid(version.Requester, at); err != nil {
			return Certification{}, fmt.Errorf("%w: evidence %d: %v", ErrInvalidCertification, i, err)
		}
		if _, dup := seen[e.Dimension]; dup {
			return Certification{}, fmt.Errorf("%w: duplicate dimension %s", ErrInvalidCertification, e.Dimension)
		}
		seen[e.Dimension] = e
	}
	base := Certification{ApplicationID: version.ApplicationID, Version: version.Version, Revision: version.Revision, VersionDigest: version.Digest, Dimensions: AllCertificationDimensions()}
	for _, d := range base.Dimensions {
		e, ok := seen[d]
		if !ok {
			base.Status = StatusDecertified
			base.Digest = certificationDigest(base, evidence)
			return base, fmt.Errorf("%w: dimension %s is missing", ErrCertificationEvidence, d)
		}
		if !at.Before(e.expiresAt()) {
			base.Status = StatusDecertified
			base.ExpiresAt = e.expiresAt()
			base.Digest = certificationDigest(base, evidence)
			return base, fmt.Errorf("%w: dimension %s lapsed at %s", ErrCertificationEvidence, d, e.expiresAt().UTC().Format(time.RFC3339))
		}
		if base.ExpiresAt.IsZero() || e.expiresAt().Before(base.ExpiresAt) {
			base.ExpiresAt = e.expiresAt()
		}
	}
	base.Status = StatusCertified
	base.Digest = certificationDigest(base, evidence)
	return base, nil
}

func certificationDigest(c Certification, evidence []DimensionEvidence) string {
	ordered := append([]DimensionEvidence(nil), evidence...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Dimension < ordered[j].Dimension })
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%d|%s|%s", c.ApplicationID, c.Version, c.Revision, c.VersionDigest, c.Status)
	for _, e := range ordered {
		fmt.Fprintf(h, "|%s=%s@%s+%d", e.Dimension, e.EvidenceRef, e.AssessedAt.UTC().Format(time.RFC3339), int64(e.ValidFor))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
