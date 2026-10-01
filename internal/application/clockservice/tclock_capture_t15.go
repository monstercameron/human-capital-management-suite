package clockservice

import (
	"context"
	"strconv"
	"strings"
	"time"

	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// CaptureAuthenticationRequest is the application input for one authenticated
// capture. The device and principal are resolved from the trusted request
// context; the caller supplies only the worker claim and its capture instant.
type CaptureAuthenticationRequest struct {
	DeviceID         string
	ClaimedWorkerRef string
	CapturedAt       time.Time
}

// CaptureAuthenticationResult preserves the separated device, principal and
// worker evidence produced by the CLOCK-002 domain contract.
type CaptureAuthenticationResult struct {
	Device DeviceRecord
	Result clockdomain.CaptureResult
}

// CaptureLocationResolver is an optional policy seam. A production policy
// source may provide an explicit location decision; when it does not, the
// enrolled device site remains the bounded location authority for the device
// call. The resolver never receives a client-supplied location.
type CaptureLocationResolver interface {
	ResolveCaptureLocation(context.Context, string, string, time.Time) (clockdomain.LocationEvidence, error)
}

// AuthenticateCapture performs the application-side admission that precedes
// an observation. It rechecks the enrolled machine principal, resolves the
// claimed worker from the authoritative directory, and then delegates the
// separated proof validation to the clock domain.
func (s Service) AuthenticateCapture(ctx context.Context, p *trust.Principal, req CaptureAuthenticationRequest) (CaptureAuthenticationResult, error) {
	device, err := s.authenticatedRosterDevice(ctx, p, req.DeviceID)
	if err != nil {
		return CaptureAuthenticationResult{}, err
	}
	if s.Workers == nil {
		return CaptureAuthenticationResult{}, ErrUnavailable
	}
	worker, active, err := s.Workers.ResolveWorker(ctx, device.TenantID, strings.TrimSpace(req.ClaimedWorkerRef))
	if err != nil {
		return CaptureAuthenticationResult{}, err
	}
	now := req.CapturedAt
	if now.IsZero() {
		now = s.now()
	}
	location, err := s.captureLocation(ctx, device, now)
	if err != nil {
		return CaptureAuthenticationResult{}, err
	}
	result, err := clockdomain.AuthenticateCapture(clockdomain.CaptureRequest{
		Tenant:             values.TenantId(device.TenantID),
		CapturedAt:         now,
		DeviceRegistration: batchDeviceRegistration(device),
		DeviceProof: clockdomain.DeviceProof{
			DeviceID:            device.ID,
			RegistrationVersion: formatRevision(device.Revision),
			ProofRef:            "key:" + device.ID,
			Fingerprint:         batchDeviceFingerprint(device.PublicKey),
			IssuedAt:            p.IssuedAt(),
			ExpiresAt:           p.ExpiresAt(),
		},
		Principal:        p,
		ClaimedWorkerRef: strings.TrimSpace(req.ClaimedWorkerRef),
		Worker: clockdomain.WorkerResolution{
			WorkerRef:     worker,
			Tenant:        values.TenantId(device.TenantID),
			ResolutionRef: "worker-resolution:" + worker,
			EvidenceRef:   "worker-claim:" + strings.TrimSpace(req.ClaimedWorkerRef),
			Resolved:      true,
			Active:        active,
		},
		Location: location,
	})
	if err != nil {
		return CaptureAuthenticationResult{}, err
	}
	return CaptureAuthenticationResult{Device: device, Result: result}, nil
}

func (s Service) captureLocation(ctx context.Context, device DeviceRecord, at time.Time) (clockdomain.LocationEvidence, error) {
	if source, ok := s.Policies.(CaptureLocationResolver); ok {
		return source.ResolveCaptureLocation(ctx, device.TenantID, device.ID, at)
	}
	return clockdomain.LocationEvidence{
		LocationRef:   device.SiteID,
		PolicyVersion: "device-location/v1",
		EvidenceRef:   "device-location:" + device.ID,
		Allowed:       true,
	}, nil
}

func formatRevision(revision int64) string {
	if revision < 1 {
		return ""
	}
	return strconv.FormatInt(revision, 10)
}

// OfflineForwardEntry is the already-authenticated, signed evidence retained
// by a device while disconnected. PayloadDigest is required because the
// offline domain must never manufacture a signature after the fact.
type OfflineForwardEntry struct {
	Sequence      uint64
	EventType     clockdomain.EventType
	DeviceRef     string
	WorkerRef     string
	OccurredAt    time.Time
	PayloadDigest string
}

// OfflineForwardRequest is one local device log forwarded after reconnect.
type OfflineForwardRequest struct {
	DeviceID string
	Entries  []OfflineForwardEntry
}

// OfflineForwardResult is the confidence- and skew-marked result of forwarding
// an offline log. Entries remain in device sequence order.
type OfflineForwardResult struct {
	DeviceRef     string
	Entries       []clockdomain.SyncedEntry
	ReceiptDigest string
}

// ForwardOffline authenticates the reconnecting device and worker claims,
// validates the local sequence through CLOCK-004, and syncs using the trusted
// server clock. Receipt order is never rewritten and occurred time is never
// replaced with receipt time.
func (s Service) ForwardOffline(ctx context.Context, p *trust.Principal, req OfflineForwardRequest) (OfflineForwardResult, error) {
	device, err := s.authenticatedRosterDevice(ctx, p, req.DeviceID)
	if err != nil {
		return OfflineForwardResult{}, err
	}
	if s.Workers == nil {
		return OfflineForwardResult{}, ErrUnavailable
	}
	entries := make([]clockdomain.OfflineEntry, 0, len(req.Entries))
	for _, entry := range req.Entries {
		if strings.TrimSpace(entry.DeviceRef) != "" && entry.DeviceRef != device.ID {
			return OfflineForwardResult{}, ErrDeviceNotEligible
		}
		worker, active, resolveErr := s.Workers.ResolveWorker(ctx, device.TenantID, strings.TrimSpace(entry.WorkerRef))
		if resolveErr != nil {
			return OfflineForwardResult{}, resolveErr
		}
		if !active || strings.TrimSpace(worker) == "" || worker != strings.TrimSpace(entry.WorkerRef) {
			return OfflineForwardResult{}, ErrWorkerNotEligible
		}
		entries = append(entries, clockdomain.OfflineEntry{
			Sequence: entry.Sequence, EventType: entry.EventType, DeviceRef: device.ID,
			WorkerRef: worker, OccurredAt: entry.OccurredAt, PayloadDigest: entry.PayloadDigest,
		})
	}
	buffer, err := clockdomain.BufferOffline(device.ID, entries)
	if err != nil {
		return OfflineForwardResult{}, err
	}
	synced, err := clockdomain.SyncOffline(buffer, s.now())
	if err != nil {
		return OfflineForwardResult{}, err
	}
	return OfflineForwardResult{DeviceRef: synced.DeviceRef, Entries: synced.Entries, ReceiptDigest: synced.ReceiptDigest}, nil
}
