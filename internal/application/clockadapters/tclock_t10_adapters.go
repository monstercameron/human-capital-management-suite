package clockadapters

import (
	"context"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	transport "github.com/monstercameron/human-capital-management-suite/internal/transport/clockadapter"
)

// Protocol contracts are aliases so application composition cannot bypass the
// transport adapter's authentication-before-parse and bounded-input rules.
type AuthenticatedDevice = transport.AuthenticatedDevice
type DeviceAuthenticator = transport.DeviceAuthenticator
type PunchSink = transport.PunchSink
type RosterSink = transport.RosterSink
type BatchClaimStore = transport.BatchClaimStore
type BatchLease = transport.BatchLease
type Unsupported = transport.Unsupported
type Translation = transport.Translation
type RosterTranslation = transport.RosterTranslation
type VendorWorker = transport.VendorWorker
type VendorJob = transport.VendorJob
type ADMSHandler = transport.ADMSHandler
type RESTPunch = transport.RESTPunch
type BatchFormat = transport.BatchFormat

const (
	FormatADP           = transport.FormatADP
	FormatPaychex       = transport.FormatPaychex
	FormatQuickBooksIIF = transport.FormatQuickBooksIIF
)

var (
	ErrMalformed             = transport.ErrMalformed
	ErrUnauthenticated       = transport.ErrUnauthenticated
	ErrMissingSequence       = transport.ErrMissingSequence
	ErrBatchAlreadyProcessed = transport.ErrBatchAlreadyProcessed
)

// ParseADMS translates ZKTeco ADMS-style push rows after the enrolled device
// identity has been established by the caller.
func ParseADMS(deviceID string, body []byte, location *time.Location) (Translation, error) {
	return transport.ParseADMS(deviceID, body, location)
}

// MapRESTPunches translates a vendor-cloud response without evaluating
// attendance policy.
func MapRESTPunches(deviceID string, payload []byte) (Translation, error) {
	return transport.MapRESTPunches(deviceID, payload)
}

// ParseBatch translates supported export layouts and reports unsupported
// fields instead of silently dropping them.
func ParseBatch(deviceID string, content []byte, format BatchFormat) (Translation, error) {
	return transport.ParseBatch(deviceID, content, format)
}

// ImportAuthenticatedBatch content-addresses and claims a file before it is
// submitted to the canonical ingest, making identical reprocessing safe.
func ImportAuthenticatedBatch(ctx context.Context, store BatchClaimStore, sink PunchSink, device AuthenticatedDevice, content []byte, format BatchFormat) (*timev1.SubmitPunchesResponse, string, []Unsupported, error) {
	return transport.ImportAuthenticatedBatch(ctx, store, sink, device, content, format)
}

// TranslateRoster maps the canonical verifier-only roster to the supported
// vendor subset and reports fields the device cannot represent.
func TranslateRoster(deviceID string, snapshot *timev1.RosterSnapshot) (RosterTranslation, error) {
	return transport.TranslateRoster(deviceID, snapshot)
}

// SyncRoster obtains the canonical roster first, then translates it for the
// target device. Cursor and revision semantics remain owned by the canonical
// roster service.
func SyncRoster(ctx context.Context, sink RosterSink, deviceID, cursor string, maxResults int32) (RosterTranslation, error) {
	return transport.SyncRoster(ctx, sink, deviceID, cursor, maxResults)
}
