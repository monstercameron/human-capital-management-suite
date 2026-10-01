package timeapp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/syncjob"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
)

const deputySchema = "deputy.timesheet/v1"

var (
	ErrInvalidConfig       = errors.New("timeapp: invalid configuration")
	ErrInvalidPayload      = errors.New("timeapp: invalid Deputy payload")
	ErrIdentityUnresolved  = errors.New("timeapp: external employee is not linked")
	ErrIdentityConflict    = errors.New("timeapp: external employee linkage conflicts")
	ErrFirstPartyConflict  = errors.New("timeapp: observation conflicts with first-party evidence")
	ErrProviderUnavailable = errors.New("timeapp: provider unavailable")
)

// IdentityLink is an authoritative identity-link result. The adapter never
// derives WorkerRef from an employee name or a vendor supplied worker id.
type IdentityLink struct {
	WorkerRef     string
	ResolutionRef string
	EvidenceRef   string
	Resolved      bool
	Conflict      bool
}

// IdentityLinker resolves a provider employee id within the tenant.
type IdentityLinker interface {
	Resolve(context.Context, string, string) (IdentityLink, error)
}

// SourcedObservation is the ordinary clock observation write requested by an
// imported app row. Source and trust are pinned by the registered profile.
type SourcedObservation struct {
	Tenant             string
	Provider           string
	ProviderRecordID   string
	ExternalEmployeeID string
	WorkerRef          string
	Event              clock.EventType
	OccurredAt         time.Time
	ObservedAt         time.Time
	SourceClass        clock.SourceClass
	TrustCeiling       clock.TrustCeiling
	ProfileVersion     string
	ResolutionRef      string
	EvidenceRef        string
	PayloadDigest      string
}

// SourcedCorrection appends a provider edit against the original observation.
type SourcedCorrection struct {
	Tenant           string
	Provider         string
	ProviderRecordID string
	OriginalDigest   string
	OriginalOccurred time.Time
	CorrectedAt      time.Time
	Reason           string
	PayloadDigest    string
}

// ClockObservationPort is the adapter's write boundary. Implementations
// should delegate to the clock observation service and reject first-party
// conflicts with ErrFirstPartyConflict.
type ClockObservationPort interface {
	Observe(context.Context, SourcedObservation) (string, error)
	Correct(context.Context, SourcedCorrection) error
}

// StateRecord is the durable provider identity needed to turn a later edit
// into corrections against the original observations.
type StateRecord struct {
	Tenant          string
	ProviderRecord  string
	PayloadDigest   string
	Start           time.Time
	End             time.Time
	StartReceiptRef string
	EndReceiptRef   string
	WorkerRef       string
}

// ProviderStateStore persists provider records. Implementations must make
// Save idempotent for the same tenant and provider record.
type ProviderStateStore interface {
	Load(context.Context, string, string, string) (StateRecord, bool, error)
	Save(context.Context, StateRecord) error
}

// Exception is a typed per-record refusal. No exception is guessed into a
// worker or silently converted into an observation.
type Exception struct {
	ProviderRecordID   string
	ExternalEmployeeID string
	Kind               string
	Reason             string
}

// Result describes one accepted receipt and its per-row effects.
type Result struct {
	ReceiptID  string
	Observed   int
	Corrected  int
	Replayed   bool
	Exceptions []Exception
}

// WebhookRequest is the verified transport envelope received from Deputy.
// Deputy signs the exact body with X-Deputy-Secret and supplies generation
// time in X-Deputy-Generation-Time; EventID is supplied by the receiver's
// endpoint routing and must remain stable across retries.
type WebhookRequest struct {
	EventID         string
	TenantID        string
	Topic           string
	GenerationTime  time.Time
	DeputySignature string
	Payload         []byte
}

// Config binds one tenant endpoint to the immutable TCLOCK-001 profile.
type Config struct {
	EndpointID   string
	TenantID     string
	Secret       []byte
	Registry     clock.ProfileRegistry
	MaxBytes     int
	Window       time.Duration
	ReceiptStore *webhook.Store
	StateStore   ProviderStateStore
}

// Connector is a concurrency-safe Deputy timesheet importer.
type Connector struct {
	config  Config
	linker  IdentityLinker
	writer  ClockObservationPort
	receipt *webhook.Store
	state   ProviderStateStore
	secret  []byte
}

// New constructs a Deputy connector and registers its webhook endpoint with
// the shared receipt/replay machinery.
func New(config Config, linker IdentityLinker, writer ClockObservationPort) (*Connector, error) {
	if strings.TrimSpace(config.EndpointID) == "" || strings.TrimSpace(config.TenantID) == "" || len(config.Secret) < 32 || linker == nil || writer == nil || config.ReceiptStore == nil || config.StateStore == nil {
		return nil, ErrInvalidConfig
	}
	validated, err := clock.NewProfileRegistry(config.Registry.Profiles)
	if err != nil || validated.Version != config.Registry.Version || validated.Digest != config.Registry.Digest {
		return nil, fmt.Errorf("%w: registry is not a validated immutable snapshot", ErrInvalidConfig)
	}
	config.Registry = validated
	profile, ok := config.Registry.Profile(clock.SourceThirdPartyApp)
	if !ok || profile.FirstPartner != "Deputy webhooks" || profile.TrustCeiling == "" {
		return nil, fmt.Errorf("%w: registered Deputy profile is required", ErrInvalidConfig)
	}
	if config.MaxBytes == 0 {
		config.MaxBytes = 1 << 20
	}
	if config.Window == 0 {
		config.Window = 5 * time.Minute
	}
	if config.MaxBytes <= 0 || config.Window <= 0 {
		return nil, ErrInvalidConfig
	}
	relay := append([]byte(nil), config.Secret...)
	return &Connector{config: config, linker: linker, writer: writer, receipt: config.ReceiptStore, state: config.StateStore, secret: relay}, nil
}

// VerifyAndImport authenticates, receipts, parses and imports a Deputy
// Timesheet.Insert, Timesheet.Save or Timesheet.Update payload.
func (c *Connector) VerifyAndImport(ctx context.Context, req WebhookRequest, now time.Time) (Result, error) {
	if c == nil || ctx == nil || req.TenantID != c.config.TenantID || req.Topic == "" || now.IsZero() {
		return Result{}, ErrInvalidPayload
	}
	if len(req.Payload) > c.config.MaxBytes {
		return Result{}, webhook.ErrPayloadTooLarge
	}
	if !verifyDeputy(c.secret, req.Payload, req.DeputySignature) {
		return Result{}, webhook.ErrInvalidSignature
	}
	eventID := payloadDigest(req.Topic, string(req.Payload))
	wreq := webhook.Request{EndpointID: c.config.EndpointID, TenantID: req.TenantID, EventID: eventID, EventType: req.Topic, Schema: deputySchema, Timestamp: now.UTC(), Signature: webhook.Sign(c.secret, webhook.Request{Timestamp: now.UTC(), EventID: eventID, EventType: req.Topic, Schema: deputySchema, Payload: req.Payload}), Payload: req.Payload}
	receipt, err := c.receipt.Receive(wreq, now)
	if err != nil {
		// The shared idempotency registry may report a reservation conflict on
		// a byte-identical retry after a process failed during writes. Because
		// eventID is itself the exact signed topic/body digest, this branch is
		// safe for that retry and cannot admit a different payload.
		if !errors.Is(err, webhook.ErrDuplicateDifferent) {
			return Result{}, err
		}
		result := Result{Replayed: true}
		rows, parseErr := parsePayload(req.Payload, req.Topic)
		if parseErr != nil {
			return Result{}, parseErr
		}
		for _, row := range rows {
			if importErr := c.importRow(ctx, req, row, now, &result); importErr != nil {
				return Result{}, importErr
			}
		}
		return result, nil
	}
	rows, err := parsePayload(req.Payload, req.Topic)
	if err != nil {
		return Result{}, err
	}
	result := Result{ReceiptID: receipt.ID, Replayed: receipt.ReceivedAt.Before(now.UTC())}
	for _, row := range rows {
		if err := c.importRow(ctx, req, row, now, &result); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

type deputyEnvelope struct {
	Topic string          `json:"topic"`
	Data  json.RawMessage `json:"data"`
}
type deputyTimesheet struct {
	ID         json.Number `json:"Id"`
	EmployeeID json.Number `json:"EmployeeId"`
	Start      string      `json:"Start"`
	End        string      `json:"End"`
}

func parsePayload(body []byte, expectedTopic string) ([]deputyTimesheet, error) {
	var env deputyEnvelope
	if err := json.Unmarshal(body, &env); err != nil || len(env.Data) == 0 {
		return nil, fmt.Errorf("%w: envelope: %v", ErrInvalidPayload, err)
	}
	if strings.TrimSpace(env.Topic) != strings.TrimSpace(expectedTopic) {
		return nil, fmt.Errorf("%w: topic does not match authenticated event", ErrInvalidPayload)
	}
	var rows []deputyTimesheet
	if len(env.Data) > 0 && env.Data[0] == '[' {
		if err := json.Unmarshal(env.Data, &rows); err != nil {
			return nil, fmt.Errorf("%w: data: %v", ErrInvalidPayload, err)
		}
	} else {
		var row deputyTimesheet
		if err := json.Unmarshal(env.Data, &row); err != nil {
			return nil, fmt.Errorf("%w: data: %v", ErrInvalidPayload, err)
		}
		rows = []deputyTimesheet{row}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: no timesheet rows", ErrInvalidPayload)
	}
	return rows, nil
}

func (c *Connector) importRow(ctx context.Context, req WebhookRequest, row deputyTimesheet, now time.Time, result *Result) error {
	id := strings.TrimSpace(row.ID.String())
	employee := strings.TrimSpace(row.EmployeeID.String())
	if id == "" || id == "0" || employee == "" || employee == "0" {
		result.Exceptions = append(result.Exceptions, Exception{ProviderRecordID: id, ExternalEmployeeID: employee, Kind: "MALFORMED_RECORD", Reason: "Deputy timesheet id and employee id are required"})
		return nil
	}
	start, err := time.Parse(time.RFC3339, row.Start)
	if err != nil {
		result.Exceptions = append(result.Exceptions, Exception{ProviderRecordID: id, ExternalEmployeeID: employee, Kind: "MALFORMED_RECORD", Reason: "invalid start time"})
		return nil
	}
	end, err := time.Parse(time.RFC3339, row.End)
	if err != nil {
		result.Exceptions = append(result.Exceptions, Exception{ProviderRecordID: id, ExternalEmployeeID: employee, Kind: "MALFORMED_RECORD", Reason: "invalid end time"})
		return nil
	}
	link, err := c.linker.Resolve(ctx, req.TenantID, employee)
	if err != nil {
		if errors.Is(err, ErrProviderUnavailable) {
			return err
		}
		result.Exceptions = append(result.Exceptions, Exception{ProviderRecordID: id, ExternalEmployeeID: employee, Kind: "IDENTITY_ERROR", Reason: err.Error()})
		return nil
	}
	if link.Conflict {
		result.Exceptions = append(result.Exceptions, Exception{ProviderRecordID: id, ExternalEmployeeID: employee, Kind: "IDENTITY_CONFLICT", Reason: ErrIdentityConflict.Error()})
		return nil
	}
	if !link.Resolved || link.WorkerRef == "" || link.ResolutionRef == "" || link.EvidenceRef == "" {
		result.Exceptions = append(result.Exceptions, Exception{ProviderRecordID: id, ExternalEmployeeID: employee, Kind: "UNMATCHED_EXTERNAL_USER", Reason: ErrIdentityUnresolved.Error()})
		return nil
	}
	digest := payloadDigest(id, employee, row.Start, row.End)
	profile, _ := c.config.Registry.Profile(clock.SourceThirdPartyApp)
	previous, exists, err := c.state.Load(ctx, req.TenantID, "Deputy", id)
	if err != nil {
		return err
	}
	if exists && previous.PayloadDigest == digest {
		result.Replayed = true
		return nil
	}
	if exists {
		for _, old := range []struct {
			at     time.Time
			digest string
		}{{previous.Start, previous.StartReceiptRef}, {previous.End, previous.EndReceiptRef}} {
			if err := c.writer.Correct(ctx, SourcedCorrection{Tenant: req.TenantID, Provider: "Deputy", ProviderRecordID: id, OriginalDigest: old.digest, OriginalOccurred: old.at, CorrectedAt: now.UTC(), Reason: "Deputy timesheet update", PayloadDigest: digest}); err != nil {
				if errors.Is(err, ErrFirstPartyConflict) {
					result.Exceptions = append(result.Exceptions, Exception{ProviderRecordID: id, ExternalEmployeeID: employee, Kind: "FIRST_PARTY_CONFLICT", Reason: err.Error()})
					return nil
				}
				return err
			}
		}
		result.Corrected++
	} else {
		var startReceipt, endReceipt string
		for _, eventTime := range []struct {
			event clock.EventType
			at    time.Time
		}{{clock.EventClockIn, start}, {clock.EventClockOut, end}} {
			receipt, err := c.writer.Observe(ctx, SourcedObservation{Tenant: req.TenantID, Provider: "Deputy", ProviderRecordID: id, ExternalEmployeeID: employee, WorkerRef: link.WorkerRef, Event: eventTime.event, OccurredAt: eventTime.at.UTC(), ObservedAt: now.UTC(), SourceClass: clock.SourceThirdPartyApp, TrustCeiling: profile.TrustCeiling, ProfileVersion: profile.Version, ResolutionRef: link.ResolutionRef, EvidenceRef: link.EvidenceRef, PayloadDigest: digest})
			if err != nil {
				if errors.Is(err, ErrFirstPartyConflict) {
					result.Exceptions = append(result.Exceptions, Exception{ProviderRecordID: id, ExternalEmployeeID: employee, Kind: "FIRST_PARTY_CONFLICT", Reason: err.Error()})
					return nil
				}
				return err
			}
			if eventTime.event == clock.EventClockIn {
				startReceipt = receipt
			} else {
				endReceipt = receipt
			}
			result.Observed++
		}
		if err := c.state.Save(ctx, StateRecord{Tenant: req.TenantID, ProviderRecord: id, PayloadDigest: digest, Start: start, End: end, StartReceiptRef: startReceipt, EndReceiptRef: endReceipt, WorkerRef: link.WorkerRef}); err != nil {
			return err
		}
		return nil
	}
	previous.PayloadDigest = digest
	previous.Start = start
	previous.End = end
	previous.WorkerRef = link.WorkerRef
	if err := c.state.Save(ctx, previous); err != nil {
		return err
	}
	return nil
}

func verifyDeputy(secret, body []byte, signature string) bool {
	got, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return false
	}
	sum := hmac.New(sha256.New, secret)
	_, _ = sum.Write(body)
	return hmac.Equal(got, sum.Sum(nil))
}
func payloadDigest(values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// PlanSync uses the shared SyncJob kernel for provider change classification.
func PlanSync(previous syncjob.Cursor, records []syncjob.SourceItem) (syncjob.Batch, error) {
	return syncjob.PlanBatch(syncjob.Job{ID: "deputy-timesheets", Mode: syncjob.ModeDelta, LocalSystem: "hcmnext"}, previous, records)
}
