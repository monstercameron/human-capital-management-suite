// Package geofence owns versioned jobsite boundaries and the location
// evidence evaluation that turns a device's fix into inside, outside or
// unknown for time capture. FTIME-010 reuses Clock's location evidence
// shape and Project's site authority; it does not build general employee
// tracking, and it retains only the minimum proof a time decision needs.
// The package is pure: no database, no clock, no network.
package geofence

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this package's contract version.
func Version() int { return schemaVersion }

var (
	// ErrBoundaryRejected is the FTIME-010 seeded-defect sentinel. A test
	// that probes a malformed boundary, an out-of-range coordinate or a
	// non-effective revision must see this error.
	ErrBoundaryRejected = errors.New("FTIME_010_REJECTED")
	// ErrEvidenceInvalid identifies location evidence that cannot be
	// evaluated.
	ErrEvidenceInvalid = errors.New("geofence: location evidence is invalid")
)

// BoundaryRejection is the stable FTIME-010 failure shape.
type BoundaryRejection struct {
	Field   string
	Version string
	Reason  string
}

func (r *BoundaryRejection) Error() string {
	return fmt.Sprintf("%s: field=%s version=%s: %s", ErrBoundaryRejected, r.Field, r.Version, r.Reason)
}

// Unwrap exposes the FTIME_010_REJECTED sentinel to errors.Is.
func (r *BoundaryRejection) Unwrap() error { return ErrBoundaryRejected }

func reject(field, version, reason string) error {
	return &BoundaryRejection{Field: field, Version: version, Reason: reason}
}

// Coordinate is a WGS84 point in decimal degrees.
type Coordinate struct {
	Latitude  float64
	Longitude float64
}

func (c Coordinate) Validate() error {
	if c.Latitude < -90 || c.Latitude > 90 {
		return reject("latitude", "", "must be within -90..90")
	}
	if c.Longitude < -180 || c.Longitude > 180 {
		return reject("longitude", "", "must be within -180..180")
	}
	return nil
}

// Boundary is one versioned jobsite boundary revision: a circular geofence
// (center plus radius) with the accuracy, staleness, exit-grace and
// hysteresis parameters that govern how evidence is evaluated against it,
// and the effective interval and retention it is authorized under.
type Boundary struct {
	SiteRef                 string
	Revision                uint64
	Center                  Coordinate
	RadiusMeters            float64
	AccuracyThresholdMeters float64
	MaxEvidenceAge          time.Duration
	ExitGrace               time.Duration
	HysteresisMeters        float64
	Effective               values.EffectiveInterval
	RetentionPeriod         time.Duration
	AuthorizedBy            string
	Version                 string
}

func (b Boundary) Validate() error {
	if strings.TrimSpace(b.SiteRef) == "" {
		return reject("site_ref", b.Version, "is required")
	}
	if b.Revision == 0 {
		return reject("revision", b.Version, "must be positive")
	}
	if err := b.Center.Validate(); err != nil {
		return err
	}
	if b.RadiusMeters <= 0 {
		return reject("radius_meters", b.Version, "must be positive")
	}
	if b.AccuracyThresholdMeters <= 0 {
		return reject("accuracy_threshold_meters", b.Version, "must be positive")
	}
	if b.MaxEvidenceAge <= 0 {
		return reject("max_evidence_age", b.Version, "must be positive")
	}
	if b.ExitGrace < 0 {
		return reject("exit_grace", b.Version, "cannot be negative")
	}
	if b.HysteresisMeters < 0 {
		return reject("hysteresis_meters", b.Version, "cannot be negative")
	}
	if err := b.Effective.Validate(); err != nil {
		return reject("effective", b.Version, err.Error())
	}
	if b.RetentionPeriod <= 0 {
		return reject("retention_period", b.Version, "must be positive")
	}
	if strings.TrimSpace(b.AuthorizedBy) == "" {
		return reject("authorized_by", b.Version, "is required")
	}
	if strings.TrimSpace(b.Version) == "" {
		return reject("version", b.Version, "is required")
	}
	return nil
}

func (b Boundary) digestBody() string {
	return strings.Join([]string{
		b.SiteRef, fmt.Sprint(b.Revision),
		fmt.Sprintf("%.6f", b.Center.Latitude), fmt.Sprintf("%.6f", b.Center.Longitude),
		fmt.Sprintf("%.3f", b.RadiusMeters), fmt.Sprintf("%.3f", b.AccuracyThresholdMeters),
		b.MaxEvidenceAge.String(), b.ExitGrace.String(), fmt.Sprintf("%.3f", b.HysteresisMeters),
		b.AuthorizedBy, b.Version,
	}, "\x00")
}

// Digest validates and returns the boundary's canonical digest.
func (b Boundary) Digest() (string, error) {
	if err := b.Validate(); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(b.digestBody()))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// EffectiveAt reports whether the boundary revision is authorized at t.
func (b Boundary) EffectiveAt(t time.Time) (bool, error) {
	if err := b.Validate(); err != nil {
		return false, err
	}
	if t.IsZero() {
		return false, reject("t", b.Version, "instant is required")
	}
	return b.Effective.ContainsInstant(values.NewInstant(t))
}

// Source is where a location fix came from.
type Source string

const (
	SourceDeviceGPS         Source = "DEVICE_GPS"
	SourceNetwork           Source = "NETWORK"
	SourceManualAttestation Source = "MANUAL_ATTESTATION"
)

func (s Source) Valid() bool {
	switch s {
	case SourceDeviceGPS, SourceNetwork, SourceManualAttestation:
		return true
	}
	return false
}

// DeviceObservation is the raw fix as the device reported it: its own
// observed instant and accuracy claim, kept separate from the server
// receipt so replay and clock skew stay visible.
type DeviceObservation struct {
	ObservedAt        time.Time
	Position          Coordinate
	AccuracyMeters    float64
	Source            Source
	PermissionGranted bool
	ConsentRef        string
}

// ServerEvidence binds a device observation to the server's own receipt and
// an evidence reference. It retains only what one time decision needs: no
// raw location history beyond this one evaluated fix.
type ServerEvidence struct {
	Observation DeviceObservation
	ReceivedAt  time.Time
	EvidenceRef string
}

// Status is the closed evaluation verdict.
type Status string

const (
	StatusInside  Status = "INSIDE"
	StatusOutside Status = "OUTSIDE"
	StatusUnknown Status = "UNKNOWN"
)

// UnknownReason names why a verdict could not be a confirmed inside or
// outside. It is empty for a confirmed verdict.
type UnknownReason string

const (
	ReasonNone                 UnknownReason = ""
	ReasonBoundaryNotEffective UnknownReason = "BOUNDARY_NOT_EFFECTIVE"
	ReasonInvalidSource        UnknownReason = "INVALID_SOURCE"
	ReasonMissingPermission    UnknownReason = "MISSING_PERMISSION"
	ReasonStaleEvidence        UnknownReason = "STALE_EVIDENCE"
	ReasonWeakAccuracy         UnknownReason = "WEAK_ACCURACY"
)

// Evaluation is the outcome of evaluating one piece of evidence against one
// boundary revision.
type Evaluation struct {
	SiteRef          string
	BoundaryRevision uint64
	BoundaryDigest   string
	Status           Status
	Reason           UnknownReason
	DistanceMeters   float64
	HasDistance      bool
	EvaluatedAt      time.Time
}

// Evaluate classifies evidence against a boundary at now as inside, outside
// or unknown. Missing permission, stale evidence, weak accuracy, an
// unauthorized boundary revision or an undeclared source all return unknown
// rather than defaulting to a confirmed fact that could wrongly open or
// close a shift.
func Evaluate(b Boundary, evidence ServerEvidence, now time.Time) (Evaluation, error) {
	digest, err := b.Digest()
	if err != nil {
		return Evaluation{}, err
	}
	if now.IsZero() {
		return Evaluation{}, fmt.Errorf("%w: now is required", ErrEvidenceInvalid)
	}
	if evidence.Observation.ObservedAt.IsZero() || evidence.ReceivedAt.IsZero() {
		return Evaluation{}, fmt.Errorf("%w: observation and receipt instants are required", ErrEvidenceInvalid)
	}
	if strings.TrimSpace(evidence.EvidenceRef) == "" {
		return Evaluation{}, fmt.Errorf("%w: evidence reference is required", ErrEvidenceInvalid)
	}
	base := Evaluation{SiteRef: b.SiteRef, BoundaryRevision: b.Revision, BoundaryDigest: digest, EvaluatedAt: now.UTC()}

	effective, err := b.EffectiveAt(now)
	if err != nil {
		return Evaluation{}, err
	}
	if !effective {
		base.Status, base.Reason = StatusUnknown, ReasonBoundaryNotEffective
		return base, nil
	}
	if !evidence.Observation.Source.Valid() {
		base.Status, base.Reason = StatusUnknown, ReasonInvalidSource
		return base, nil
	}
	if !evidence.Observation.PermissionGranted || strings.TrimSpace(evidence.Observation.ConsentRef) == "" {
		base.Status, base.Reason = StatusUnknown, ReasonMissingPermission
		return base, nil
	}
	if evidence.Observation.ObservedAt.After(now) || now.Sub(evidence.Observation.ObservedAt) > b.MaxEvidenceAge {
		base.Status, base.Reason = StatusUnknown, ReasonStaleEvidence
		return base, nil
	}
	if evidence.Observation.AccuracyMeters <= 0 || evidence.Observation.AccuracyMeters > b.AccuracyThresholdMeters {
		base.Status, base.Reason = StatusUnknown, ReasonWeakAccuracy
		return base, nil
	}
	if err := evidence.Observation.Position.Validate(); err != nil {
		return Evaluation{}, err
	}
	distance := haversineMeters(b.Center, evidence.Observation.Position)
	base.DistanceMeters, base.HasDistance = distance, true
	if distance <= b.RadiusMeters+b.HysteresisMeters {
		base.Status = StatusInside
		return base, nil
	}
	base.Status = StatusOutside
	return base, nil
}

func haversineMeters(a, b Coordinate) float64 {
	const earthRadiusMeters = 6371000.0
	lat1, lon1 := degToRad(a.Latitude), degToRad(a.Longitude)
	lat2, lon2 := degToRad(b.Latitude), degToRad(b.Longitude)
	dLat := lat2 - lat1
	dLon := lon2 - lon1
	sinDLat := math.Sin(dLat / 2)
	sinDLon := math.Sin(dLon / 2)
	h := sinDLat*sinDLat + math.Cos(lat1)*math.Cos(lat2)*sinDLon*sinDLon
	return 2 * earthRadiusMeters * math.Asin(math.Min(1, math.Sqrt(h)))
}

func degToRad(d float64) float64 { return d * math.Pi / 180 }

// RetainedProof is the minimum retained proof for one evaluated fix: no raw
// position, only the boundary digest, the outcome, the computed distance
// (when one exists) and when it must be purged.
type RetainedProof struct {
	SiteRef        string
	BoundaryDigest string
	Status         Status
	Reason         UnknownReason
	DistanceMeters float64
	HasDistance    bool
	EvidenceRef    string
	EvaluatedAt    time.Time
	RetainUntil    time.Time
}

// Retain trims an evaluation down to the minimum retained proof a later
// time decision or review might need.
func Retain(b Boundary, e Evaluation, evidenceRef string) (RetainedProof, error) {
	if err := b.Validate(); err != nil {
		return RetainedProof{}, err
	}
	if e.BoundaryDigest == "" || e.EvaluatedAt.IsZero() {
		return RetainedProof{}, fmt.Errorf("%w: evaluation is incomplete", ErrEvidenceInvalid)
	}
	if strings.TrimSpace(evidenceRef) == "" {
		return RetainedProof{}, fmt.Errorf("%w: evidence reference is required", ErrEvidenceInvalid)
	}
	return RetainedProof{
		SiteRef: b.SiteRef, BoundaryDigest: e.BoundaryDigest, Status: e.Status, Reason: e.Reason,
		DistanceMeters: e.DistanceMeters, HasDistance: e.HasDistance, EvidenceRef: evidenceRef,
		EvaluatedAt: e.EvaluatedAt, RetainUntil: e.EvaluatedAt.Add(b.RetentionPeriod),
	}, nil
}

// DueForPurge reports whether the retained proof's retention period has
// elapsed at now.
func (p RetainedProof) DueForPurge(now time.Time) bool { return !now.Before(p.RetainUntil) }

// Explanation is the audit-safe summary of one evaluation.
type Explanation struct {
	SiteRef  string
	Revision uint64
	Status   Status
	Reason   UnknownReason
	Digest   string
}

// Explain validates and summarizes an evaluation for operator display.
func Explain(e Evaluation) (Explanation, error) {
	if e.BoundaryDigest == "" || e.EvaluatedAt.IsZero() {
		return Explanation{}, ErrEvidenceInvalid
	}
	return Explanation{SiteRef: e.SiteRef, Revision: e.BoundaryRevision, Status: e.Status, Reason: e.Reason, Digest: e.BoundaryDigest}, nil
}
