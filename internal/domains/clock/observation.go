package clock

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// observationSkew is the maximum tolerance for device clock drift ahead of
// the server clock. Past occurred times are accepted: offline buffering is
// owned by CLOCK-004, not punished here.
const observationSkew = 5 * time.Minute

var (
	// ErrObservationRejected is the CLOCK-003 seeded-defect sentinel. A test
	// that probes an unsigned, unbound or cross-tenant observation must see
	// this error with the offending field, state and version attached.
	ErrObservationRejected = errors.New("CLOCK_003_REJECTED")
	// ErrObservationEvidence identifies an invalid observation result that
	// cannot be used as time evidence.
	ErrObservationEvidence = errors.New("clock: observation evidence is invalid")
)

// EventType is the closed vocabulary of observable time events.
type EventType string

const (
	EventClockIn    EventType = "CLOCK_IN"
	EventClockOut   EventType = "CLOCK_OUT"
	EventMealStart  EventType = "MEAL_START"
	EventMealEnd    EventType = "MEAL_END"
	EventBreakStart EventType = "BREAK_START"
	EventBreakEnd   EventType = "BREAK_END"
)

func (e EventType) Valid() bool {
	switch e {
	case EventClockIn, EventClockOut, EventMealStart, EventMealEnd, EventBreakStart, EventBreakEnd:
		return true
	default:
		return false
	}
}

// DeviceSignature is the reference-only device signature over the bound
// observation payload. Raw signatures and keys never enter this contract:
// PayloadDigest is the digest the device claims to have signed, recomputed
// and compared by the server before acceptance.
type DeviceSignature struct {
	KeyRef        string
	Algorithm     string
	PayloadDigest string
}

func (s DeviceSignature) Validate() error {
	if strings.TrimSpace(s.KeyRef) == "" || strings.TrimSpace(s.Algorithm) == "" || strings.TrimSpace(s.PayloadDigest) == "" {
		return fmt.Errorf("%w: signature is incomplete", ErrObservationEvidence)
	}
	return nil
}

// ObservationRequest is the side-effect-free signing question. ClaimedRecordedAt
// is accepted on the wire for transport compatibility but is never trusted:
// CaptureObservation stamps RecordedAt from Now, the server clock.
type ObservationRequest struct {
	Tenant            values.TenantId
	Capture           CaptureEvidence
	EventType         EventType
	OccurredAt        time.Time
	ClaimedRecordedAt time.Time
	Timezone          string
	Location          LocationEvidence
	Signature         DeviceSignature
	Now               time.Time
}

// TimeObservation is the immutable signed evidence for one observed event.
// RecordedAt is always the server clock; the caller cannot supply it.
type TimeObservation struct {
	Accepted   bool
	Tenant     values.TenantId
	EventType  EventType
	DeviceRef  string
	WorkerRef  string
	OccurredAt time.Time
	RecordedAt time.Time
	Timezone   string
	Location   LocationEvidence
	Digest     string
}

// ObservationRejection is the stable CLOCK-003 failure shape.
type ObservationRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *ObservationRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrObservationRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the CLOCK_003_REJECTED sentinel to errors.Is.
func (r *ObservationRejection) Unwrap() error { return ErrObservationRejected }

func obsReject(field, state, version, reason string) error {
	return &ObservationRejection{Field: field, State: state, Version: version, Reason: reason}
}

// observationPayloadDigest is the server-side recomputation of what the
// device must have signed: capture digest, tenant, event, device/worker,
// occurred instant and timezone/location evidence.
func observationPayloadDigest(req ObservationRequest) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		string(req.Tenant),
		req.Capture.Digest,
		string(req.EventType),
		req.Capture.Device.DeviceRef,
		req.Capture.Worker.ResolvedWorkerRef,
		req.OccurredAt.UTC().Format(time.RFC3339Nano),
		req.Timezone,
		req.Location.LocationRef,
		req.Location.PolicyVersion,
	}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validTimezone(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_' || r == '-' || r == '+' || r == '/':
		default:
			return false
		}
	}
	return true
}

// CaptureObservation binds a CLOCK-002 capture to a signed time event. It is
// pure: it persists nothing and emits no domain effect.
func CaptureObservation(req ObservationRequest) (TimeObservation, error) {
	const version = "clock-observation/v1"
	if strings.TrimSpace(string(req.Tenant)) == "" {
		return TimeObservation{}, obsReject("tenant", "MISSING", version, "tenant is required")
	}
	if !req.EventType.Valid() {
		return TimeObservation{}, obsReject("event_type", "MISSING", version, "event type is not declared")
	}
	if req.OccurredAt.IsZero() {
		return TimeObservation{}, obsReject("occurred_at", "MISSING", version, "occurred time is required")
	}
	if req.Now.IsZero() {
		return TimeObservation{}, obsReject("now", "MISSING", version, "server clock is required")
	}
	if !validTimezone(req.Timezone) {
		return TimeObservation{}, obsReject("timezone", "INVALID", version, "timezone evidence is required")
	}
	if strings.TrimSpace(req.Capture.Digest) == "" {
		return TimeObservation{}, obsReject("capture.digest", "MISSING", version, "observation requires an authenticated capture")
	}
	if strings.TrimSpace(req.Capture.Device.DeviceRef) == "" {
		return TimeObservation{}, obsReject("capture.device", "MISSING", version, "observation requires device evidence")
	}
	if strings.TrimSpace(req.Capture.Worker.ResolvedWorkerRef) == "" {
		return TimeObservation{}, obsReject("capture.worker", "MISSING", version, "observation requires resolved worker evidence")
	}
	if !req.Location.Allowed || strings.TrimSpace(req.Location.LocationRef) == "" {
		return TimeObservation{}, obsReject("location", "DENIED", version, "observation requires allowed location evidence")
	}
	if string(req.Capture.Principal.Tenant) != string(req.Tenant) {
		return TimeObservation{}, obsReject("tenant", "MISMATCH", version, "capture principal tenant does not match request tenant")
	}
	if err := req.Signature.Validate(); err != nil {
		return TimeObservation{}, obsReject("signature", "MISSING", version, "device signature is required")
	}
	if req.OccurredAt.After(req.Now.Add(observationSkew)) {
		return TimeObservation{}, obsReject("occurred_at", "FUTURE", version, "occurred time is beyond clock skew")
	}
	if req.Signature.PayloadDigest != observationPayloadDigest(req) {
		return TimeObservation{}, obsReject("signature.payload_digest", "MISMATCH", version, "device signature does not bind the observation")
	}
	recorded := req.Now.UTC()
	occurred := req.OccurredAt.UTC()
	body := strings.Join([]string{
		string(req.Tenant),
		req.Signature.PayloadDigest,
		recorded.Format(time.RFC3339Nano),
	}, "\x00")
	sum := sha256.Sum256([]byte(body))
	return TimeObservation{
		Accepted:   true,
		Tenant:     req.Tenant,
		EventType:  req.EventType,
		DeviceRef:  req.Capture.Device.DeviceRef,
		WorkerRef:  req.Capture.Worker.ResolvedWorkerRef,
		OccurredAt: occurred,
		RecordedAt: recorded,
		Timezone:   req.Timezone,
		Location:   req.Location,
		Digest:     "sha256:" + hex.EncodeToString(sum[:]),
	}, nil
}
