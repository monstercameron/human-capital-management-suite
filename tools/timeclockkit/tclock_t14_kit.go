// Package timeclockkit is the transport-neutral partner kit for TCLOCK-018.
//
// It deliberately consumes the generated device API shape instead of defining
// a second punch model. A partner can provide an Adapter backed by gRPC, HTTP,
// or a local test endpoint and run the same simulator and certification suite.
package timeclockkit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
)

var (
	ErrInvalidOptions = errors.New("timeclockkit: invalid options")
	ErrConformance    = errors.New("timeclockkit: conformance failed")
)

// Adapter is the device-facing subset exercised by certification. It matches
// the generated ClockDeviceService request/response types exactly.
type Adapter interface {
	CreateEnrollmentCode(context.Context, *timev1.CreateEnrollmentCodeRequest) (*timev1.CreateEnrollmentCodeResponse, error)
	EnrollDevice(context.Context, *timev1.EnrollDeviceRequest) (*timev1.EnrollDeviceResponse, error)
	RevokeDevice(context.Context, *timev1.RevokeDeviceRequest) (*timev1.RevokeDeviceResponse, error)
	SyncRoster(context.Context, *timev1.SyncRosterRequest) (*timev1.SyncRosterResponse, error)
	IdentifyWorker(context.Context, *timev1.IdentifyWorkerRequest) (*timev1.IdentifyWorkerResponse, error)
	SubmitPunches(context.Context, *timev1.SubmitPunchesRequest) (*timev1.SubmitPunchesResponse, error)
	Heartbeat(context.Context, *timev1.HeartbeatRequest) (*timev1.HeartbeatResponse, error)
}

// Options identifies the isolated sandbox used by a certification run.
// TenantID is sent in the enrollment scope and checked on the returned device;
// a production tenant may not be substituted for the sandbox tenant.
type Options struct {
	TenantID      string
	SiteID        string
	ProfileRef    string
	WorkerID      string
	EnrollmentTTL time.Duration
	Now           time.Time
	Webhook       WebhookReceiver
}

// WebhookReceiver observes the receipt emitted for the accepted punch.
type WebhookReceiver interface {
	Receive(context.Context, WebhookEvent) error
}

// WebhookEvent is intentionally payload-free. A partner proves receipt and
// scope without receiving worker or punch details in the certification log.
type WebhookEvent struct {
	TenantID  string
	Type      string
	ReceiptID string
}

// Observation is one independently reported certification criterion.
type Observation struct {
	Name   string
	Passed bool
	Detail string
}

// Result contains the evidence collected by Run. The provenance field is
// private so callers cannot fabricate a successful result for signing.
type Result struct {
	Profile      string
	TenantID     string
	DeviceID     string
	Observations []Observation
	Passed       bool
	StartedAt    time.Time
	FinishedAt   time.Time
	provenance   [32]byte
}

// Validate rejects an unsafe or non-deterministic certification setup before
// it can issue an enrollment code or make another external call.
func (o Options) Validate() error {
	if strings.TrimSpace(o.TenantID) == "" || strings.TrimSpace(o.SiteID) == "" || strings.TrimSpace(o.ProfileRef) == "" || strings.TrimSpace(o.WorkerID) == "" {
		return fmt.Errorf("%w: tenant, site, profile and worker are required", ErrInvalidOptions)
	}
	if o.EnrollmentTTL <= 0 || o.Now.IsZero() {
		return fmt.Errorf("%w: positive enrollment TTL and explicit clock are required", ErrInvalidOptions)
	}
	if o.Webhook == nil {
		return fmt.Errorf("%w: webhook receiver is required", ErrInvalidOptions)
	}
	return nil
}

var mandatoryObservations = [...]string{
	"enrollment-code", "tenant-isolation", "enrollment", "heartbeat",
	"roster-sync", "identify", "offline-replay", "sequence-gap",
	"duplicate-replay", "clock-drift", "webhook-receipt", "revocation",
}
