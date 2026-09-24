package application

// This file holds the composition seams: the ports the application root
// constructs a production adapter for when nobody supplied one, and the
// explicit way a caller supplies one instead.
//
// The ARCH-GO-020 rule these types exist to satisfy is "test composition
// swaps adapters, clocks and providers explicitly without production-only
// branches". Every seam below is one field with one meaning: nil means "this
// root builds the production adapter", non-nil means "the caller supplied
// this one". There is no environment sniffing, no `if testing.Testing()`, no
// build tag and no second constructor that only tests call - the binary and
// the suite run the same ComposeServe over the same Options type, and differ
// only in which fields they filled.

import (
	"context"
	"net"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionsteps"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
)

// Migrator applies the pending schema before the listeners start.
//
// It is a port rather than a call, because the concrete migration runner is
// Goose over the embedded migration tree and definitions/architecture/
// library-firewall.yaml confines github.com/pressly/goose/v3 to the
// `migrations` and `cmd` roots. The command therefore supplies the adapter
// (cmd/hcmnext's migrateUp) and this package states only that the serve role
// depends on "something that brings the schema to the target version". A
// configuration with Migrate=true and no Migrator is refused rather than
// silently skipped.
type Migrator func(ctx context.Context, databaseURL string, logger bootstrap.Logger) error

// ListenFunc opens the TCP listeners the two surfaces are published on. Nil
// means net.Listen.
type ListenFunc func(network, address string) (net.Listener, error)

// StoreFactory builds the persistence adapter the cell reads and appends
// through. Nil means the PostgreSQL store over the composed pool.
type StoreFactory func(pool *pgxadapter.Pool, cfg ServeConfig) (app.Store, error)

// VerifierFactory builds the credential verifier the listener authenticates
// with. Nil means the deterministic HMAC development verifier.
type VerifierFactory func(cfg ServeConfig) (trust.Verifier, error)

// SIEMRingResolver loads destination-scoped SIEM signing credentials from the
// configured custody provider. The serve role has no implicit development-key fallback.
type SIEMRingResolver interface {
	ResolveSIEMRing(context.Context, *trust.Principal, string) (*subscription.CredentialRing, error)
}

// TelemetryFactory builds the process-local OTel provider. Nil means
// NewTelemetryProvider, which returns (nil, nil) for -otel-exporter=none.
type TelemetryFactory func(ctx context.Context, instanceID string, cfg ServeConfig) (*hcmotel.Provider, error)

// ExecutionComposer fills the execution-shaped fields of a CellConfig when
// the P1B execution authority gate is on. Nil means
// ComposeExecutionAuthority.
type ExecutionComposer func(cellConfig *app.CellConfig, pool *pgxadapter.Pool, evidence app.EvidenceStore, cfg ServeConfig) error

// Options are the explicit composition seams. The zero value is the
// production composition; a test fills only the fields it means to replace.
type Options struct {
	// HealthPoolPing replaces only the PostgreSQL Ping call, allowing an
	// adapter or integration fixture to observe connection failure and recovery
	// while the production schema check still runs against the composed pool.
	HealthPoolPing func(context.Context) error
	// CurrentHealthConfigFingerprint reads the effective config source again
	// during each readiness refresh. ServeSpec supplies a bootstrap-backed
	// implementation that resolves the original flags against current env.
	CurrentHealthConfigFingerprint func() (string, error)
	HealthCheckInterval            time.Duration
	HealthCheckTimeout             time.Duration
	// ChatSessionRevocation checks each chat authorization against the current
	// session lifecycle. Standard-profile chat remains fail closed without it.
	ChatSessionRevocation session.RevocationChecker
	// ChatMedia configures the protected media route. Zero value keeps the
	// route explicitly unavailable; it never supplies an accepting scanner.
	ChatMedia ChatMediaConfig
	// Logger is the process logger the role's Spec hands bootstrap. Nil
	// means the redacting JSON handler over stdout that a deployed hcmnext
	// uses.
	Logger bootstrap.Logger

	// NewStore, NewVerifier, NewTelemetry and ComposeExecution replace one
	// constructed adapter each.
	NewStore         StoreFactory
	NewVerifier      VerifierFactory
	SIEMRingResolver SIEMRingResolver
	NewTelemetry     TelemetryFactory
	ComposeExecution ExecutionComposer

	// Migrate applies the schema. Required when ServeConfig.Migrate is true.
	Migrate Migrator
	// Listen opens the two surfaces' listeners.
	Listen ListenFunc

	// Now, Clock and IDs are the cell's time and identifier seams. Nil means
	// the trusted host clock and UUIDv7, which is what a production cell
	// composes.
	Now   func() time.Time
	Clock intent.Clock
	IDs   intent.IDSource

	// Evidence is the one sink the gateway, the gate and (when composed) the
	// execution driver all record on. Nil means the durable PostgreSQL store
	// over the composed pool (internal/data/evidencestore, WF-RUN-035); a
	// test that supplies the in-memory double does so explicitly here.
	Evidence app.EvidenceStore

	// Inputs, Workers and Bands are the governed read ports the capability
	// handlers answer from. Nil means the cell's own fixture corpus, which is
	// the P1A default.
	Inputs  app.DomainInputs
	Workers people.WorkerFacts
	Bands   rewards.PayBandCatalog

	// RepairEffect, RepairObservation and RepairReconciliation are
	// WF-RUN-016's external-system seams for a RepairPlan execution: the
	// adapter that redrives one failed effect, the one that reads the result
	// back, and the comparer that decides whether external consistency was
	// restored. Nil means the cell's refusing defaults -- no production
	// adapter exists yet, because the connectivity plane is read-only -- and a
	// repair submitted to such a cell is denied rather than reported as done.
	RepairEffect         app.RepairEffectPort
	RepairObservation    app.RepairObservationPort
	RepairReconciliation app.RepairReconciliationPort

	// ProviderReceipts reads the payroll and identity providers'
	// confirmations the promotion 1.1.0 observations judge after each
	// provider-confirmation wait. Nil means none is composed yet, and every
	// 1.1.0 payroll and access observation fails closed
	// (provider.reader=unconfigured) rather than passing unconfirmed.
	ProviderReceipts promotionsteps.ProviderReceiptReader
}

// Option is the functional form of one Options field. Options are values
// rather than package state on purpose: there is no global registry a package
// init could append to, so what a process composed is exactly what its caller
// passed and nothing an imported package added behind it.
type Option func(*Options)

// Apply folds every option into a copy of o and returns it.
func (o Options) Apply(opts ...Option) Options {
	out := o
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&out)
	}
	return out
}

// WithLogger supplies the process logger.
func WithLogger(logger bootstrap.Logger) Option {
	return func(o *Options) { o.Logger = logger }
}

// WithStore supplies the persistence adapter directly.
func WithStore(store app.Store) Option {
	return func(o *Options) {
		o.NewStore = func(*pgxadapter.Pool, ServeConfig) (app.Store, error) { return store, nil }
	}
}

// WithStoreFactory supplies the persistence adapter's constructor.
func WithStoreFactory(factory StoreFactory) Option {
	return func(o *Options) { o.NewStore = factory }
}

// WithVerifier supplies the credential verifier directly.
func WithVerifier(verifier trust.Verifier) Option {
	return func(o *Options) {
		o.NewVerifier = func(ServeConfig) (trust.Verifier, error) { return verifier, nil }
	}
}

// WithTelemetryProvider supplies the OTel provider directly. A nil provider
// is meaningful: it is telemetry off, the same state -otel-exporter=none
// composes.
func WithTelemetryProvider(provider *hcmotel.Provider) Option {
	return func(o *Options) {
		o.NewTelemetry = func(context.Context, string, ServeConfig) (*hcmotel.Provider, error) {
			return provider, nil
		}
	}
}

// WithMigrator supplies the schema migration adapter.
func WithMigrator(migrate Migrator) Option {
	return func(o *Options) { o.Migrate = migrate }
}

// WithListener supplies the listener factory both surfaces are opened with.
func WithListener(listen ListenFunc) Option {
	return func(o *Options) { o.Listen = listen }
}

// WithHealthPoolPing supplies an optional Ping delegate and the process
// readiness cache and timeout bounds. Schema verification always runs over
// the composed database pool.
func WithHealthPoolPing(ping func(context.Context) error, interval, timeout time.Duration) Option {
	return func(o *Options) {
		o.HealthPoolPing = ping
		o.HealthCheckInterval = interval
		o.HealthCheckTimeout = timeout
	}
}

// WithCurrentHealthConfigFingerprint supplies the independently observed
// configuration fingerprint source used by readiness checks.
func WithCurrentHealthConfigFingerprint(current func() (string, error)) Option {
	return func(o *Options) { o.CurrentHealthConfigFingerprint = current }
}

// WithClock supplies the cell's wall-clock reading.
func WithClock(now func() time.Time) Option {
	return func(o *Options) { o.Now = now }
}

// WithIntentClock supplies the intent-level clock seam.
func WithIntentClock(clock intent.Clock) Option {
	return func(o *Options) { o.Clock = clock }
}

// WithIDs supplies the identifier source seam.
func WithIDs(ids intent.IDSource) Option {
	return func(o *Options) { o.IDs = ids }
}

// WithEvidence supplies the shared evidence sink.
func WithEvidence(sink app.EvidenceStore) Option {
	return func(o *Options) { o.Evidence = sink }
}

// WithDomainInputs supplies the governed read ports the capability handlers
// answer from.
func WithDomainInputs(inputs app.DomainInputs, workers people.WorkerFacts, bands rewards.PayBandCatalog) Option {
	return func(o *Options) {
		o.Inputs = inputs
		o.Workers = workers
		o.Bands = bands
	}
}

// WithProviderReceipts supplies the provider-receipt reader the promotion
// 1.1.0 observations read through.
func WithProviderReceipts(reader promotionsteps.ProviderReceiptReader) Option {
	return func(o *Options) { o.ProviderReceipts = reader }
}

// WithExecutionComposer supplies the P1B execution-authority composer.
func WithExecutionComposer(compose ExecutionComposer) Option {
	return func(o *Options) { o.ComposeExecution = compose }
}

// WithRepairAdapters supplies the three external-system adapters a RepairPlan
// execution needs. Everything else about the governed repair -- the JIT
// authority, the dual control, the sealed simulation, the journaled receipt
// and the durable idempotency record -- is composed the same way with or
// without them.
func WithRepairAdapters(effect app.RepairEffectPort, observation app.RepairObservationPort, reconciliation app.RepairReconciliationPort) Option {
	return func(o *Options) {
		o.RepairEffect = effect
		o.RepairObservation = observation
		o.RepairReconciliation = reconciliation
	}
}

// listen resolves the listener factory, defaulting to net.Listen.
func (o Options) listen() ListenFunc {
	if o.Listen != nil {
		return o.Listen
	}
	return net.Listen
}
