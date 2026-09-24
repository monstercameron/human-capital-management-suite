// Command worker is a thin composition root built on
// internal/platform/bootstrap.Run: it resolves this role's typed
// configuration, opens the database pool, and hands bootstrap one Workload
// (the outbox sweep loop in sweep.go). It owns no dispatch semantics of its
// own beyond that wiring.
//
// The Workload runs the transactional outbox consumer (internal/data/outbox)
// across every active tenant: each sweep claims due messages (PENDING, or
// IN_FLIGHT past their lease) and dispatches them. A message failing
// dispatch returns to PENDING for retry; the target database's own restart
// safety is Consumer.Poll's lease, not anything this process remembers, so
// killing and restarting worker loses nothing and never double-applies a
// delivered message (DATA-008).
//
// SVC-010 hosts semantic messaging delivery as one mode of the cmd/worker
// outbox role. The provider-neutral runner and durable observation adapter
// live below this composition root; this command only selects the role.
//
// SVC-008 hosts governed connector-operation execution as a second,
// independent role (connector_role.go): it drains
// internal/connectivity/operation's own journal rather than the outbox, so
// it runs as its own Workload alongside (not instead of) the outbox
// consumer above.
//
// The target server is HCMNEXT_DATABASE_URL, overridable with -database-url.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

// EnvDatabaseURL names the server this command connects to, matching
// cmd/migrate's and cmd/projector's convention.
const EnvDatabaseURL = "HCMNEXT_DATABASE_URL"

// EnvHealthAddr, if set, is the loopback host:port the health/readiness
// endpoint is served on; empty (the default) disables it.
const EnvHealthAddr = "HCMNEXT_WORKER_HEALTH_ADDR"

const EnvMessagingRole = "HCMNEXT_WORKER_MESSAGING_ROLE"

// EnvConnectorRole gates SVC-008's governed connector-operation execution
// role. It defaults off: enabling it today is safe (connectorRoleFor wires
// fail-closed credential/provider stand-ins) but inert, since no production
// destination-credential authority or connector provider adapter exists yet.
const EnvConnectorRole = "HCMNEXT_WORKER_CONNECTOR_ROLE"

// EnvWorkerRoles selects the independently authorized roles hosted by this
// worker process. The process identity remains "worker"; these are narrower
// in-process capabilities and are never interchangeable.
const EnvWorkerRoles = "HCMNEXT_WORKER_ROLES"

func main() {
	os.Exit(bootstrap.Run(context.Background(), spec(os.Args[1:])))
}

// workerConfigFields declares every flag/env-backed value this role
// accepts. It is a function rather than a package variable so callers never
// share (and risk mutating) one backing array.
func workerConfigFields() []bootstrap.Field {
	fields := []bootstrap.Field{
		{
			Name:   "database-url",
			Env:    EnvDatabaseURL,
			Usage:  "PostgreSQL connection URL (" + EnvDatabaseURL + " if unset)",
			Kind:   bootstrap.KindString,
			Secret: true,
		},
		{
			Name:    "poll-interval",
			Env:     "HCMNEXT_WORKER_POLL_INTERVAL",
			Usage:   "how long to sleep between sweeps that found no due work",
			Default: "2s",
			Kind:    bootstrap.KindDuration,
		},
		{
			Name:    "lease",
			Env:     "HCMNEXT_WORKER_LEASE",
			Usage:   "how long a claimed message stays IN_FLIGHT before another sweep may reclaim it",
			Default: outbox.DefaultLease.String(),
			Kind:    bootstrap.KindDuration,
		},
		{
			Name:    "batch-size",
			Env:     "HCMNEXT_WORKER_BATCH_SIZE",
			Usage:   "maximum messages one sweep claims per tenant",
			Default: fmt.Sprintf("%d", outbox.DefaultBatchSize),
			Kind:    bootstrap.KindInt,
		},
		{
			Name:  "health-addr",
			Env:   EnvHealthAddr,
			Usage: "loopback host:port (127.0.0.1, localhost or ::1) to serve the health/readiness endpoint on; empty disables it",
			Kind:  bootstrap.KindString,
		},
		{Name: "messaging-role", Env: EnvMessagingRole, Usage: "enable the semantic messaging delivery role", Default: "true", Kind: bootstrap.KindBool},
		{Name: "messaging-max-attempts", Env: "HCMNEXT_WORKER_MESSAGING_MAX_ATTEMPTS", Usage: "maximum provider attempts for one messaging delivery", Default: "3", Kind: bootstrap.KindInt},
		{Name: "connector-role", Env: EnvConnectorRole, Usage: "enable the governed connector-operation execution role (SVC-008; inert until a provider adapter and credential authority are wired)", Default: "false", Kind: bootstrap.KindBool},
		{Name: "roles", Env: EnvWorkerRoles, Usage: "comma-separated capability-activity, reconciliation and repair roles", Default: string(WorkerRoleCapabilityActivity), Kind: bootstrap.KindString},
		{Name: "intelligence-metric-job", Env: "HCMNEXT_WORKER_INTELLIGENCE_METRIC_JOB", Usage: "optional JSON request with tenant_id, metric definition and an existing intent decision_id", Kind: bootstrap.KindString},
	}
	return append(fields, workerTelemetryFields()...)
}

// spec builds the full worker Spec for args. It pre-resolves health-addr
// with the same precedence bootstrap.Run itself applies (flag > env >
// default) because Spec.HealthAddr, unlike every other worker setting, is a
// plain field bootstrap.Run reads before it ever parses Spec.ConfigFields;
// the pre-parse below is a pure, side-effect-free rerun of exactly the parse
// Run performs moments later, so a bad flag here is simply reported again
// (correctly, with the banner and full error) by Run's own parse.
func spec(args []string) bootstrap.Spec {
	fields := workerConfigFields()
	healthAddr := ""
	if values, err := bootstrap.ParseConfig(args, nil, fields); err == nil {
		healthAddr = values.String("health-addr")
	}

	return bootstrap.Spec{
		Role:             bootstrap.RoleWorker,
		Args:             args,
		ConfigFields:     fields,
		Validate:         validateConfig,
		DatabaseURLField: "database-url",
		DBPoolFactory:    pgxDBPoolFactory,
		HealthAddr:       healthAddr,
		Build:            build,
	}
}

// validateConfig fails config resolution (before any listener or workload
// starts) on a missing database URL or an unparsable typed field.
func validateConfig(v *bootstrap.Values) error {
	if v.String("database-url") == "" {
		return fmt.Errorf("%s is not set; pass -database-url or set the environment variable", EnvDatabaseURL)
	}
	if _, err := v.Duration("poll-interval"); err != nil {
		return err
	}
	if _, err := v.Duration("lease"); err != nil {
		return err
	}
	if _, err := v.Int("batch-size"); err != nil {
		return err
	}
	if _, err := v.Bool("messaging-role"); err != nil {
		return err
	}
	if _, err := v.Bool("connector-role"); err != nil {
		return err
	}
	maxAttempts, err := v.Int("messaging-max-attempts")
	if err != nil {
		return err
	}
	if maxAttempts < 1 {
		return fmt.Errorf("messaging-max-attempts must be positive")
	}
	if _, err := ParseWorkerRoles(v.String("roles")); err != nil {
		return err
	}
	if raw := v.String("intelligence-metric-job"); raw != "" {
		if _, err := parseIntelligenceMetricJob(raw); err != nil {
			return err
		}
	}
	switch v.String("otel-exporter") {
	case "none", "stdout":
	case "otlphttp":
		if v.String("otel-endpoint") == "" {
			return fmt.Errorf("otel-endpoint is required when otel-exporter=otlphttp")
		}
	default:
		return fmt.Errorf("otel-exporter must be one of none, stdout, or otlphttp")
	}
	return nil
}

// build resolves the sweep loop's dependencies from deps and returns it as
// this role's single Workload. It is the one place worker's Spec touches
// outbox-specific types.
func build(ctx context.Context, deps bootstrap.Deps) (bootstrap.Runtime, error) {
	pool, ok := deps.DB.(workerPool)
	if !ok {
		return bootstrap.Runtime{}, fmt.Errorf("worker: database pool %T does not support outbox operations (Begin/Query)", deps.DB)
	}

	pollInterval, err := deps.Values.Duration("poll-interval")
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	lease, err := deps.Values.Duration("lease")
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	batchSize, err := deps.Values.Int("batch-size")
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	messagingRole, err := deps.Values.Bool("messaging-role")
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	connectorRoleEnabled, err := deps.Values.Bool("connector-role")
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	roles, err := ParseWorkerRoles(deps.Values.String("roles"))
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	var metricJob *IntelligenceMetricJob
	if raw := deps.Values.String("intelligence-metric-job"); raw != "" {
		parsed, err := parseIntelligenceMetricJob(raw)
		if err != nil {
			return bootstrap.Runtime{}, err
		}
		metricJob = &parsed
	}

	consumer := outbox.NewConsumer(pool, outbox.WithLease(lease), outbox.WithBatchSize(batchSize))
	tenants := pgxTenantLister{pool: pool}
	logger := deps.Logger
	telemetryProvider, err := newWorkerTelemetryProvider(ctx, deps.Identity, deps.Values)
	if err != nil {
		return bootstrap.Runtime{}, err
	}

	logger.Info("worker.outbox_consumer_configured",
		"poll_interval", pollInterval.String(),
		"lease", lease.String(),
		"batch_size", batchSize,
		"messaging_role", messagingRole,
		"connector_role", connectorRoleEnabled,
	)
	maxAttempts := 3
	if deps.Values.Has("messaging-max-attempts") {
		maxAttempts, err = deps.Values.Int("messaging-max-attempts")
		if err != nil {
			return bootstrap.Runtime{}, err
		}
	}

	workloadName := "outbox-consumer"
	if messagingRole {
		workloadName = "messaging-delivery"
	}
	wl := bootstrap.Workload{
		Name: workloadName,
		Run: func(ctx context.Context) error {
			return runOutboxLoopWithTelemetry(ctx, logger, tenants, consumer, pollInterval, legacyMessageHandler(logger), telemetryProvider, deps.Clock)
		},
	}
	workloads := []bootstrap.Workload{wl}
	if messagingRole {
		messaging := messagingRoleFor(deps, pool, maxAttempts)
		workloads[0].Run = func(ctx context.Context) error {
			return runOutboxLoopWithTelemetry(ctx, logger, tenants, consumer, pollInterval, messaging.dispatch, telemetryProvider, deps.Clock)
		}
	}
	if connectorRoleEnabled {
		// REV-013-01: the journal is durable Postgres through
		// connectivityopstore whenever the pool is configured; memory
		// remains only the no-database dev fallback inside
		// connectorJournalForPool. The in-process ledger supplies fast local
		// admission; Store.Claim also checks durable queue leases and attempt
		// history against the conservative unknown-connector quota so restart
		// cannot reset its reservation or rate window.
		connectorJournalStore := connectorJournalForPool(pool)
		connectorLedgerStore := operation.NewConnectorLedger(nil)
		connector := connectorRoleFor(deps, connectorJournalStore, connectorLedgerStore, deps.Identity, lease)
		workloads = append(workloads, bootstrap.Workload{
			Name: "connector-role",
			Run: func(ctx context.Context) error {
				return runConnectorLoop(ctx, logger, tenants, connector, pollInterval)
			},
		})
	}
	if metricJob != nil {
		job := *metricJob
		workloads = append(workloads, bootstrap.Workload{
			Name: "intelligence-metric",
			Run: func(ctx context.Context) error {
				return runIntelligenceMetricLoop(ctx, pool, job, pollInterval, deps.Clock)
			},
		})
	}
	workloads = append(workloads, workerRoleWorkloads(deps.Logger, roles)...)
	// The cross-layer idempotency registry carries finite retention horizons;
	// reclaim it on a tenant-scoped schedule even when no messaging event is due.
	reclaimer := idempotencyReclaimerFor(pool)
	workloads = append(workloads, bootstrap.Workload{
		Name: "idempotency-reclaimer",
		Run: func(ctx context.Context) error {
			return runIdempotencyReclaimer(ctx, logger, tenants, reclaimer, pollInterval, deps.Clock)
		},
	})
	runtime := bootstrap.Runtime{Workloads: workloads}
	if telemetryProvider != nil {
		runtime.Shutdown = append(runtime.Shutdown, bootstrap.ShutdownStep{Name: "shutdown-telemetry", Run: func(stepCtx context.Context) error {
			report := telemetryProvider.Shutdown(stepCtx)
			if report.Err() != nil || report.DeadlineExceeded {
				logger.Error("worker.telemetry_shutdown_degraded", "error", report.Err(), "deadline_exceeded", report.DeadlineExceeded)
			}
			return nil
		}})
	}
	return runtime, nil
}

// workerPool is the database capability this role needs beyond bootstrap's own
// Ping/Close DBPool port: outbox.NewConsumer needs Begin, and activeTenants
// listing needs Query. bootstrap's default DBPoolFactory (PgxPoolFactory)
// returns an unexported type exposing only Ping/Close, so this role supplies
// pgxDBPoolFactory instead, whose *pgxadapter.Pool return value satisfies this
// interface too. Both added methods are stated in [dbport] terms; the driver
// itself is reached only through the adapter this composition root constructs.
type workerPool interface {
	bootstrap.DBPool
	dbport.Beginner
	Query(ctx context.Context, sql string, args ...any) (dbport.Rows, error)
}

// pgxDBPoolFactory opens a pgx pool against url and pings it once, so a bad
// connection string or unreachable server fails Run before any workload
// starts, matching bootstrap.PgxPoolFactory's own contract.
func pgxDBPoolFactory(ctx context.Context, url string) (bootstrap.DBPool, error) {
	return pgxadapter.NewPool(ctx, url, nil)
}

// pgxTenantLister lists active tenants over any pool that can Query.
type pgxTenantLister struct {
	pool interface {
		Query(ctx context.Context, sql string, args ...any) (dbport.Rows, error)
	}
}

func (l pgxTenantLister) ActiveTenants(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := l.pool.Query(ctx, `SELECT tenant_id FROM tenant WHERE status = 'ACTIVE'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
