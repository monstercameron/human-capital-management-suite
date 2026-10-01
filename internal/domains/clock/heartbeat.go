// TCLOCK-008: monitor device fleet health, clock drift and app or
// firmware versions.
//
// A device's heartbeat reports its app or firmware version, queue depth,
// oldest unsent punch age, battery and power state and measured offset
// against the server clock. EvaluateHeartbeat derives the confidence grade
// later punches should carry (reusing offline.go's SyncConfidence: HIGH or
// DEGRADED) and whether the device clears the tenant's minimum supported
// version. MissingHeartbeatAlert reports silence beyond the policy's limit,
// restricted to the site's declared operating hours, which this package
// never infers from a timezone name. The package is pure: no clock, no
// storage, no network.
package clock

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrHeartbeatRejected is the TCLOCK-008 seeded-defect sentinel. A test
	// that probes a malformed heartbeat, an unparseable version string or a
	// zero clock must see this error.
	ErrHeartbeatRejected = errors.New("TCLOCK_008_REJECTED")
)

// HeartbeatRejection is the stable TCLOCK-008 failure shape.
type HeartbeatRejection struct {
	Field   string
	Version string
	Reason  string
}

func (r *HeartbeatRejection) Error() string {
	return fmt.Sprintf("%s: field=%s version=%s: %s", ErrHeartbeatRejected, r.Field, r.Version, r.Reason)
}

// Unwrap exposes the TCLOCK_008_REJECTED sentinel to errors.Is.
func (r *HeartbeatRejection) Unwrap() error { return ErrHeartbeatRejected }

func heartbeatReject(field, version, reason string) error {
	return &HeartbeatRejection{Field: field, Version: version, Reason: reason}
}

const heartbeatVersion = "tclock-heartbeat/v1"

// PowerState is the closed vocabulary of a device's power source at
// heartbeat time.
type PowerState string

const (
	PowerMains      PowerState = "MAINS"
	PowerBattery    PowerState = "BATTERY"
	PowerLowBattery PowerState = "LOW_BATTERY"
)

func (p PowerState) Valid() bool {
	switch p {
	case PowerMains, PowerBattery, PowerLowBattery:
		return true
	}
	return false
}

// Heartbeat is one device fleet-health report.
type Heartbeat struct {
	DeviceRef       string
	AppVersion      string
	QueueDepth      int
	OldestUnsentAge time.Duration
	BatteryPercent  int
	Power           PowerState
	MeasuredOffset  time.Duration
	SentAt          time.Time
}

func (h Heartbeat) Validate() error {
	if strings.TrimSpace(h.DeviceRef) == "" {
		return heartbeatReject("device_ref", "", "device reference is required")
	}
	if strings.TrimSpace(h.AppVersion) == "" {
		return heartbeatReject("app_version", "", "app or firmware version is required")
	}
	if h.QueueDepth < 0 {
		return heartbeatReject("queue_depth", "", "cannot be negative")
	}
	if h.OldestUnsentAge < 0 {
		return heartbeatReject("oldest_unsent_age", "", "cannot be negative")
	}
	if h.BatteryPercent < -1 || h.BatteryPercent > 100 {
		return heartbeatReject("battery_percent", "", "must be -1 (not applicable) or within 0..100")
	}
	if !h.Power.Valid() {
		return heartbeatReject("power", "", "power state is not declared")
	}
	if h.SentAt.IsZero() {
		return heartbeatReject("sent_at", "", "sent instant is required")
	}
	return nil
}

// FleetHealthPolicy is data: the maximum tolerated clock drift, the
// minimum supported app or firmware version and the maximum silence before
// a missing-heartbeat alert during a site's operating hours.
type FleetHealthPolicy struct {
	MaxDrift              time.Duration
	MinimumVersion        string
	MissingHeartbeatLimit time.Duration
}

func (p FleetHealthPolicy) Validate() error {
	if p.MaxDrift <= 0 {
		return heartbeatReject("policy.max_drift", "", "must be positive")
	}
	if strings.TrimSpace(p.MinimumVersion) == "" {
		return heartbeatReject("policy.minimum_version", "", "minimum supported version is required")
	}
	if p.MissingHeartbeatLimit <= 0 {
		return heartbeatReject("policy.missing_heartbeat_limit", "", "must be positive")
	}
	return nil
}

// VersionAtLeast compares dotted numeric versions ("1.4.2") left to right
// without a third-party semver dependency. A version with more segments
// than the minimum is at least as new once its shared prefix is not lower;
// a malformed segment fails closed.
func VersionAtLeast(version, minimum string) (bool, error) {
	v, err := parseVersion(version)
	if err != nil {
		return false, heartbeatReject("app_version", heartbeatVersion, err.Error())
	}
	m, err := parseVersion(minimum)
	if err != nil {
		return false, heartbeatReject("policy.minimum_version", heartbeatVersion, err.Error())
	}
	n := len(v)
	if len(m) > n {
		n = len(m)
	}
	for i := 0; i < n; i++ {
		var vi, mi int
		if i < len(v) {
			vi = v[i]
		}
		if i < len(m) {
			mi = m[i]
		}
		if vi != mi {
			return vi > mi, nil
		}
	}
	return true, nil
}

func parseVersion(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("version string is empty")
	}
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 {
			return nil, fmt.Errorf("version segment %q is not a non-negative integer", p)
		}
		out = append(out, n)
	}
	return out, nil
}

// HeartbeatEvaluation is the derived fleet-health verdict for one
// heartbeat.
type HeartbeatEvaluation struct {
	DeviceRef        string
	Confidence       SyncConfidence
	VersionSupported bool
	DriftExceeded    bool
	SentAt           time.Time
}

// EvaluateHeartbeat validates a heartbeat against policy at now and derives
// the confidence grade later punches should carry, plus whether the device
// clears the minimum supported version.
func EvaluateHeartbeat(policy FleetHealthPolicy, hb Heartbeat, now time.Time) (HeartbeatEvaluation, error) {
	if err := policy.Validate(); err != nil {
		return HeartbeatEvaluation{}, err
	}
	if err := hb.Validate(); err != nil {
		return HeartbeatEvaluation{}, err
	}
	if now.IsZero() {
		return HeartbeatEvaluation{}, heartbeatReject("now", heartbeatVersion, "server clock is required")
	}
	supported, err := VersionAtLeast(hb.AppVersion, policy.MinimumVersion)
	if err != nil {
		return HeartbeatEvaluation{}, err
	}
	drift := hb.MeasuredOffset
	if drift < 0 {
		drift = -drift
	}
	exceeded := drift > policy.MaxDrift
	confidence := SyncConfidenceHigh
	if exceeded {
		confidence = SyncConfidenceDegraded
	}
	return HeartbeatEvaluation{DeviceRef: hb.DeviceRef, Confidence: confidence, VersionSupported: supported, DriftExceeded: exceeded, SentAt: hb.SentAt.UTC()}, nil
}

// MissingHeartbeatAlert reports whether silence since lastSeen exceeds the
// policy's limit at now. withinOperatingHours is evidence the caller
// supplies from the site's own schedule; this package never guesses a
// site's operating hours from a timezone name, so silence outside them
// never alerts.
func MissingHeartbeatAlert(policy FleetHealthPolicy, lastSeen, now time.Time, withinOperatingHours bool) (bool, error) {
	if err := policy.Validate(); err != nil {
		return false, err
	}
	if lastSeen.IsZero() {
		return false, heartbeatReject("last_seen", heartbeatVersion, "last-seen instant is required")
	}
	if now.IsZero() {
		return false, heartbeatReject("now", heartbeatVersion, "server clock is required")
	}
	if now.Before(lastSeen) {
		return false, heartbeatReject("now", heartbeatVersion, "server clock cannot precede the last-seen instant")
	}
	if !withinOperatingHours {
		return false, nil
	}
	return now.Sub(lastSeen) > policy.MissingHeartbeatLimit, nil
}

// HeartbeatExplanation is the audit-safe summary of one heartbeat
// evaluation.
type HeartbeatExplanation struct {
	DeviceRef        string
	Confidence       SyncConfidence
	VersionSupported bool
	DriftExceeded    bool
}

// ExplainHeartbeat validates and summarizes an evaluation for operator
// display.
func ExplainHeartbeat(e HeartbeatEvaluation) (HeartbeatExplanation, error) {
	if e.DeviceRef == "" || e.SentAt.IsZero() {
		return HeartbeatExplanation{}, fmt.Errorf("%w: heartbeat evaluation is incomplete", ErrHeartbeatRejected)
	}
	return HeartbeatExplanation{DeviceRef: e.DeviceRef, Confidence: e.Confidence, VersionSupported: e.VersionSupported, DriftExceeded: e.DriftExceeded}, nil
}
