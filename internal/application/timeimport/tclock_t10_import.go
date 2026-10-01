package timeimport

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/syncjob"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/timeapp"
)

// The application facade deliberately aliases the provider-neutral contracts
// owned by the connectivity adapter. This keeps the transport and webhook
// receipt implementations in their proper lower layer while giving the
// composition root one application import surface.
type IdentityLink = timeapp.IdentityLink
type IdentityLinker = timeapp.IdentityLinker
type SourcedObservation = timeapp.SourcedObservation
type SourcedCorrection = timeapp.SourcedCorrection
type ClockObservationPort = timeapp.ClockObservationPort
type StateRecord = timeapp.StateRecord
type ProviderStateStore = timeapp.ProviderStateStore
type Exception = timeapp.Exception
type Result = timeapp.Result
type WebhookRequest = timeapp.WebhookRequest
type Config = timeapp.Config
type Connector = timeapp.Connector

var (
	ErrInvalidConfig       = timeapp.ErrInvalidConfig
	ErrInvalidPayload      = timeapp.ErrInvalidPayload
	ErrIdentityUnresolved  = timeapp.ErrIdentityUnresolved
	ErrIdentityConflict    = timeapp.ErrIdentityConflict
	ErrFirstPartyConflict  = timeapp.ErrFirstPartyConflict
	ErrProviderUnavailable = timeapp.ErrProviderUnavailable
)

// NewDeputy constructs the selected third-party time-app connector. The
// connector verifies the provider receipt before parsing, resolves external
// users through the injected identity linker, and writes only through the
// sourced-observation port.
func NewDeputy(config Config, linker IdentityLinker, writer ClockObservationPort) (*Connector, error) {
	return timeapp.New(config, linker, writer)
}

// PlanSync exposes the shared resumable SyncJob classification for provider
// pulls. It is intentionally pure; callers persist the returned cursor only
// after their observation writes succeed.
func PlanSync(previous syncjob.Cursor, records []syncjob.SourceItem) (syncjob.Batch, error) {
	return timeapp.PlanSync(previous, records)
}

// VerifyAndImport is a convenience entry point for callers that keep the
// connector behind an interface. It does not add a second ingest path.
func VerifyAndImport(ctx context.Context, connector *Connector, req WebhookRequest, now time.Time) (Result, error) {
	if connector == nil {
		return Result{}, ErrInvalidConfig
	}
	return connector.VerifyAndImport(ctx, req, now)
}
