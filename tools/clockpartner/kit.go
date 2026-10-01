package clockpartner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
)

var (
	ErrInvalidOptions = errors.New("clockpartner: invalid options")
	ErrConformance    = errors.New("clockpartner: conformance failed")
)

// Adapter is the callable service shape without variadic transport options.
// It is convenient for HTTP/protojson clients and for httptest integration.
type Adapter interface {
	CreateEnrollmentCode(context.Context, *timev1.CreateEnrollmentCodeRequest) (*timev1.CreateEnrollmentCodeResponse, error)
	EnrollDevice(context.Context, *timev1.EnrollDeviceRequest) (*timev1.EnrollDeviceResponse, error)
	RevokeDevice(context.Context, *timev1.RevokeDeviceRequest) (*timev1.RevokeDeviceResponse, error)
	SyncRoster(context.Context, *timev1.SyncRosterRequest) (*timev1.SyncRosterResponse, error)
	IdentifyWorker(context.Context, *timev1.IdentifyWorkerRequest) (*timev1.IdentifyWorkerResponse, error)
	SubmitPunches(context.Context, *timev1.SubmitPunchesRequest) (*timev1.SubmitPunchesResponse, error)
	Heartbeat(context.Context, *timev1.HeartbeatRequest) (*timev1.HeartbeatResponse, error)
}

// Options declares one isolated partner sandbox and the test inputs needed to
// exercise it. TenantID and SiteID are checked against every returned device.
type Options struct {
	TenantID      string
	SiteID        string
	ProfileRef    string
	WorkerID      string
	EnrollmentTTL time.Duration
	Now           time.Time
	Webhook       WebhookReceiver
}

// WebhookReceiver is an observed receipt sink supplied by the partner test
// endpoint. A nil receiver makes the webhook criterion fail.
type WebhookReceiver interface {
	Receive(context.Context, WebhookEvent) error
}

// WebhookEvent is the minimum receipt the kit requires from a partner webhook.
type WebhookEvent struct{ TenantID, Type, ReceiptID string }

// Observation is one criterion's actual outcome.
type Observation struct {
	Name   string
	Passed bool
	Detail string
}

// Result contains all observations and the raw IDs needed to audit a run.
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

// Validate checks options before any external call is made.
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
