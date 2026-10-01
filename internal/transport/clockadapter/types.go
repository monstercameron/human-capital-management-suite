package clockadapter

import (
	"context"
	"errors"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
)

var (
	ErrMalformed             = errors.New("clockadapter: malformed input")
	ErrUnauthenticated       = errors.New("clockadapter: device authentication failed")
	ErrMissingSequence       = errors.New("clockadapter: device sequence is required")
	ErrBatchAlreadyProcessed = errors.New("clockadapter: batch already processed")
)

// AuthenticatedDevice is the enrolled identity established by the verifier.
// Serial is informational; callers must use DeviceID when submitting.
type AuthenticatedDevice struct {
	DeviceID, Serial string
	Tenant           string
}

// DeviceAuthenticator verifies the request credential and returns the enrolled
// identity. Implementations must not derive identity from an untrusted body.
type DeviceAuthenticator interface {
	Authenticate(context.Context, string) (AuthenticatedDevice, error)
}

// PunchSink is the canonical clock ingest service. It owns deduplication,
// sequence gaps, roster policy, and persistence.
type PunchSink interface {
	SubmitPunches(context.Context, AuthenticatedDevice, *timev1.SubmitPunchesRequest) (*timev1.SubmitPunchesResponse, error)
}

// RosterSink supplies the canonical roster snapshot for an enrolled device.
type RosterSink interface {
	SyncRoster(context.Context, *timev1.SyncRosterRequest) (*timev1.SyncRosterResponse, error)
}

// BatchClaimStore atomically claims content hashes so a file is processed once.
type BatchClaimStore interface {
	BeginBatch(context.Context, AuthenticatedDevice, string) (BatchLease, error)
	CompleteBatch(context.Context, BatchLease) error
	ReleaseBatch(context.Context, BatchLease) error
}

// BatchLease is an owner-bound reservation for a content hash.
type BatchLease struct{ ID, Digest string }

// Unsupported reports a source field that has no canonical representation.
type Unsupported struct{ Field, Value string }

// Translation is the result of a protocol translation, including fields that
// were deliberately reported instead of silently discarded.
type Translation struct {
	Request     *timev1.SubmitPunchesRequest
	Unsupported []Unsupported
}

// RosterTranslation is a vendor roster payload and its unsupported fields.
type RosterTranslation struct {
	DeviceID    string
	Workers     []VendorWorker
	Jobs        []VendorJob
	Unsupported []Unsupported
}

// VendorWorker is the safe subset sent to a hardware clock. VerifierRef is
// already a non-secret reference or salted verifier from the canonical model.
type VendorWorker struct {
	WorkerID, DisplayName, VerifierRef string
	Method                             timev1.IdentificationMethod
}

// VendorJob is a canonical job/cost-code pair.
type VendorJob struct{ JobID, CostCodeID, Label string }
