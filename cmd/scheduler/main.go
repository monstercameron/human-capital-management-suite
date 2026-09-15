// Command scheduler is a thin composition root built on
// internal/platform/bootstrap.Run: it resolves this role's typed
// configuration, parses the one identifier it owns (the tenant), opens the
// database pool, and hands bootstrap the single Workload
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

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
)

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
	return scheduler.BuildRuntimeWithSignalRoleAndRoles(deps, nil, nil, roles, claim)
}

// pgxDBPoolFactory opens a pgx pool against url and pings it once, so a bad
// connection string or unreachable server fails Run before any tick starts.
// bootstrap's own PgxPoolFactory returns a pool exposing only Ping/Close; the
// scheduler role needs Begin, and *pgxadapter.Pool has it.
func pgxDBPoolFactory(ctx context.Context, url string) (bootstrap.DBPool, error) {
	return pgxadapter.NewPool(ctx, url, nil)
}
