// Command scheduler is a thin composition root built on
// internal/platform/bootstrap.Run: it resolves this role's typed
// configuration, parses the one identifier it owns (the tenant), opens the
// database pool, and hands bootstrap the scheduler Workload and ledger
// invariant scanner.
// internal/platform/execution/scheduler builds. It contains no workflow
// semantics and no dispatch mechanics of its own -- every function it calls
// below the flag set is exercised by that package's own tests.
//
// The role is SVC-004's: advance the durable workflow frontier and the
// scheduled triggers behind it. One tick takes or renews this replica's queue
// lease (the epoch fence), returns work abandoned by a dead replica to the
// pool, settles every due workflow_timer under that fence, and -- once a
// Dispatcher is wired -- claims the admissible ready work under a
// WORKFLOW_INSTANCE lease and a compare-and-swap on the row's own version, so
// two replicas over one database never publish the same logical execution and
// a restart loses nothing.
//
// This release runs the role publish-only: it fires durable timers into
// workflow_ready_work and keeps the queue leased, which is exactly the
// timer-firing responsibility definitions/architecture/process-roles.yaml
// assigns the scheduler command, and it claims nothing. The dispatch half is
// fully implemented and tested in
// internal/platform/execution/scheduler (its integration test drives a real
// internal/workflow/execute Driver through two replicas over one database);
// wiring a production Dispatcher needs the pinned-plan resolution SVC-005 and
// WF-RUN-006 bring, and a scheduler that claimed work it could not run would
// hold instance leases for nothing.
//
// The target server is HCMNEXT_DATABASE_URL, overridable with -database-url.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/opsmeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/sendingdomainstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
)

const fieldInvariantScanInterval = "invariant-scan-interval"
const fieldBatchJobInterval = "batch-job-interval"
const fieldDomainDNSInterval = "domain-dns-interval"
const fieldInvariantScanOwner = "invariant-scan-owner"
const fieldInvariantScanRoute = "invariant-scan-secondary-route"

const defaultInvariantScanOwner = "operations-on-call"
const defaultInvariantScanRoute = "team:platform-oncall"

func main() {
	os.Exit(bootstrap.Run(context.Background(), spec(os.Args[1:])))
}

// spec builds the full scheduler Spec for args. It pre-resolves health-addr
// with the same precedence bootstrap.Run itself applies (flag > env >
// default) because Spec.HealthAddr, unlike every other setting, is a plain
// field Run reads before it ever parses Spec.ConfigFields; the pre-parse below
// is a pure, side-effect-free rerun of exactly the parse Run performs moments
// later, so a bad flag here is simply reported again (correctly, with the
// banner and full error) by Run's own parse. cmd/worker resolves it the same
// way, for the same reason.
func spec(args []string) bootstrap.Spec {
	// Role settings are part of this command's configuration surface. Keeping
	// them in the same bootstrap parse gives flags the same precedence as the
	// existing scheduler settings (flag > environment > default), while the
	// runtime package still owns their validation and meaning.
	fields := append(scheduler.ConfigFields(), scheduler.RoleConfigFields()...)
	fields = append(fields, bootstrap.Field{Name: fieldInvariantScanInterval, Env: "HCMNEXT_SCHEDULER_INVARIANT_SCAN_INTERVAL", Usage: "ledger invariant scan cadence", Default: defaultInvariantScanInterval.String(), Kind: bootstrap.KindDuration})
	fields = append(fields, bootstrap.Field{Name: fieldBatchJobInterval, Env: "HCMNEXT_SCHEDULER_BATCH_JOB_INTERVAL", Usage: "batch job execution cadence", Default: defaultInvariantScanInterval.String(), Kind: bootstrap.KindDuration})
	fields = append(fields, bootstrap.Field{Name: fieldDomainDNSInterval, Env: "HCMNEXT_SCHEDULER_DOMAIN_DNS_INTERVAL", Usage: "sending-domain DNS authentication re-verification cadence", Default: defaultDomainDNSInterval.String(), Kind: bootstrap.KindDuration})
	fields = append(fields,
		bootstrap.Field{Name: fieldInvariantScanOwner, Env: "HCMNEXT_SCHEDULER_INVARIANT_SCAN_OWNER", Usage: "ledger invariant incident primary owner", Default: defaultInvariantScanOwner, Kind: bootstrap.KindString},
		bootstrap.Field{Name: fieldInvariantScanRoute, Env: "HCMNEXT_SCHEDULER_INVARIANT_SCAN_SECONDARY_ROUTE", Usage: "ledger invariant incident escalation route", Default: defaultInvariantScanRoute, Kind: bootstrap.KindString},
	)
	healthAddr := ""
	if values, err := bootstrap.ParseConfig(args, nil, fields); err == nil {
		healthAddr = values.String(scheduler.FieldHealthAddr)
	}

	return bootstrap.Spec{
		Role:             bootstrap.RoleScheduler,
		Args:             args,
		ConfigFields:     fields,
		Validate:         validateConfig,
		DatabaseURLField: scheduler.FieldDatabaseURL,
		DBPoolFactory:    pgxDBPoolFactory,
		HealthAddr:       healthAddr,
		Build:            build,
	}
}

// validateConfig fails config resolution (before any listener or tick starts)
// on anything the runtime package can judge, plus the one thing it cannot: the
// tenant identifier's syntax. internal/platform may not import
// github.com/google/uuid (definitions/architecture/dependency-roles.yaml
// reserves it to the roots it lists, cmd among them), so parsing the tenant is
// this command's own job -- and doing it here means an unparsable tenant is a
// startup failure rather than a first-tick one.
func validateConfig(v *bootstrap.Values) error {
	if err := scheduler.ValidateConfig(v); err != nil {
		return err
	}
	if v.Has(fieldInvariantScanInterval) {
		if d, err := v.Duration(fieldInvariantScanInterval); err != nil || d <= 0 {
			if err != nil {
				return err
			}
			return fmt.Errorf("-%s must be positive", fieldInvariantScanInterval)
		}
	}
	if v.Has(fieldBatchJobInterval) {
		if d, err := v.Duration(fieldBatchJobInterval); err != nil || d <= 0 {
			if err != nil {
				return err
			}
			return fmt.Errorf("-%s must be positive", fieldBatchJobInterval)
		}
	}
	if v.Has(fieldDomainDNSInterval) {
		if d, err := v.Duration(fieldDomainDNSInterval); err != nil || d <= 0 {
			if err != nil {
				return err
			}
			return fmt.Errorf("-%s must be positive", fieldDomainDNSInterval)
		}
	}
	if v.Has(fieldInvariantScanOwner) && v.Has(fieldInvariantScanRoute) {
		owner, route := v.String(fieldInvariantScanOwner), v.String(fieldInvariantScanRoute)
		if strings.TrimSpace(owner) == "" || strings.TrimSpace(route) == "" || owner == route ||
			len(owner) > 256 || len(route) > 256 || strings.ContainsAny(owner+route, "\r\n{}[]") {
			return fmt.Errorf("-%s and -%s must name distinct non-empty routes", fieldInvariantScanOwner, fieldInvariantScanRoute)
		}
	}
	_, err := tenantOf(v)
	return err
}

// tenantOf parses the configured tenant identifier.
func tenantOf(v *bootstrap.Values) (uuid.UUID, error) {
	raw := v.String(scheduler.FieldTenantID)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("-%s %q is not a UUID: %w", scheduler.FieldTenantID, raw, err)
	}
	if id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("-%s must not be the nil UUID", scheduler.FieldTenantID)
	}
	return id, nil
}

// claimOf renders the resolved configuration as the one lease claim this
// replica serves: its tenant, its queue resource and its holder identity.
// Carrying the tenant inside the acquire request is what lets the runtime
// package handle it without naming the identifier type.
func claimOf(deps bootstrap.Deps) (lease.AcquireRequest, error) {
	tenantID, err := tenantOf(deps.Values)
	if err != nil {
		return lease.AcquireRequest{}, err
	}
	return lease.AcquireRequest{
		TenantID: tenantID,
		Resource: lease.Resource{Kind: lease.ResourceQueue, ID: deps.Values.String(scheduler.FieldQueueKey)},
		Holder:   scheduler.Identity(deps),
	}, nil
}

// build hands the resolved claim to the runtime package, which owns every
// decision from here on. The nil Dispatcher is this release's publish-only
// posture, explained in the package comment.
func build(_ context.Context, deps bootstrap.Deps) (bootstrap.Runtime, error) {
	return buildWithBatchProcessor(deps, processLedgerInvariantItem)
}

func buildWithBatchProcessor(deps bootstrap.Deps, process batchJobProcessor) (bootstrap.Runtime, error) {
	claim, err := claimOf(deps)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	roles, err := scheduler.RolesFrom(deps.Values)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	// No signal role: resuming a matched signal continuation needs the
	// execution driver and the approved start it re-presents, which this
	// publish-only command does not compose. The served composition
	// (internal/application's scheduler workload) runs
	// scheduler.SignalDispatcher over the cell that owns both (WF-RUN-005); a
	// role here that claimed signal work it could not resume would hold
	// instance leases for nothing, and one that only logged would claim a
	// capability it does not have.
	runtime, err := scheduler.BuildRuntimeWithSignalRoleAndRoles(deps, nil, nil, roles, claim)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	interval, err := deps.Values.Duration(fieldInvariantScanInterval)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	if interval <= 0 {
		return bootstrap.Runtime{}, fmt.Errorf("-%s must be positive", fieldInvariantScanInterval)
	}
	batchInterval, err := deps.Values.Duration(fieldBatchJobInterval)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	if batchInterval <= 0 {
		return bootstrap.Runtime{}, fmt.Errorf("-%s must be positive", fieldBatchJobInterval)
	}
	dnsInterval, err := deps.Values.Duration(fieldDomainDNSInterval)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	if dnsInterval <= 0 {
		return bootstrap.Runtime{}, fmt.Errorf("-%s must be positive", fieldDomainDNSInterval)
	}
	db, ok := deps.DB.(invariantScanQuerier)
	if !ok {
		return bootstrap.Runtime{}, fmt.Errorf("scheduler database cannot open ledger scan transactions")
	}
	runtime.Workloads = append(runtime.Workloads, bootstrap.Workload{
		Name: "ledger-invariant-scanner",
		Run: func(ctx context.Context) error {
			return runInvariantScannerConfigured(ctx, db, claim.TenantID, interval, deps.Logger, deps.Clock,
				opsmeta.LedgerInvariantRoutes{PrimaryOwner: deps.Values.String(fieldInvariantScanOwner), SecondaryRoute: deps.Values.String(fieldInvariantScanRoute)})
		},
	})
	runtime.Workloads = append(runtime.Workloads, bootstrap.Workload{
		Name: "batch-job-worker",
		Run: func(ctx context.Context) error {
			return runBatchJobWorkloadWithProcessor(ctx, db, claim.TenantID, scheduler.Identity(deps).String(), batchInterval, deps.Clock, deps.Logger, process)
		},
	})
	domainStore, err := sendingdomainstore.New(db, claim.TenantID, dnsInterval)
	if err != nil {
		return bootstrap.Runtime{}, err
	}
	runtime.Workloads = append(runtime.Workloads, bootstrap.Workload{
		Name: "sending-domain-dns-verifier",
		Run: func(ctx context.Context) error {
			return runDomainDNSVerifier(ctx, domainStore, dnsInterval, deps.Clock, deps.Logger)
		},
	})
	return runtime, nil
}

// pgxDBPoolFactory opens a pgx pool against url and pings it once, so a bad
// connection string or unreachable server fails Run before any tick starts.
// bootstrap's own PgxPoolFactory returns a pool exposing only Ping/Close; the
// scheduler role needs Begin, and *pgxadapter.Pool has it.
func pgxDBPoolFactory(ctx context.Context, url string) (bootstrap.DBPool, error) {
	return pgxadapter.NewPool(ctx, url, nil)
}
