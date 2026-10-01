package application

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/application/timeclockstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/buildinfo"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/clockpunch"
)

const (
	// localDevTimeRole owns the local-development time schema. The time store
	// refuses the core credential, so the schema gets a role of its own even
	// on a development database.
	localDevTimeRole = "hcmnext_time_dev"
	// localDevTimeSeedAuthor is recorded as the publisher of the demo profiles.
	localDevTimeSeedAuthor = "internal/application:local-development"
)

// localDevSelfClockInput is everything the local-development self clock
// borrows from the serve root; nothing here is a new authority.
type localDevSelfClockInput struct {
	Config    ServeConfig
	Pool      *pgxadapter.Pool
	Logger    bootstrap.Logger
	Options   Options
	Telemetry *hcmotel.Provider
}

// composeLocalDevSelfClock serves the worker self clock on a local-development
// cell: the time schema is provisioned and migrated, the demo time profiles are
// seeded, and clock-in and clock-out run through the published clock workflow
// on the durable engine. Any other profile keeps the explicit clock runtime
// configuration, and a failure here leaves the clock off with the reason
// logged rather than stopping a development server that does not need it.
func composeLocalDevSelfClock(ctx context.Context, in localDevSelfClockInput) (*ClockRuntime, error) {
	cfg := in.Config
	if cfg.Profile != ServeProfileLocalDev || !cfg.DevBrowserLogin || !cfg.DevWorkforceBootstrap || in.Pool == nil {
		return nil, nil
	}
	if in.Options.MigrateTime == nil {
		return nil, nil
	}
	now := in.Options.Now
	if now == nil {
		now = time.Now
	}
	logger := in.Logger
	if logger == nil {
		logger = discardLogger{}
	}
	dsn, password, err := localDevTimeDSN(cfg.DatabaseURL, cfg.DevHMACKey)
	if err != nil {
		logger.Info("hcmnext.local_dev_time_clock_disabled", "reason", err.Error())
		return nil, nil
	}
	if err := provisionLocalDevTimeSchema(ctx, in.Pool, password); err != nil {
		logger.Info("hcmnext.local_dev_time_clock_disabled", "reason", err.Error())
		return nil, nil
	}
	if err := in.Options.MigrateTime(ctx, dsn, cfg.DatabaseURL, timestore.SchemaName, logger); err != nil {
		return nil, fmt.Errorf("application: migrate the local development time schema: %w", err)
	}
	store, err := timestore.New(ctx, timestore.Config{DSN: dsn, CoreDSN: cfg.DatabaseURL, Schema: timestore.SchemaName, MaxConns: 8, MinConns: 1})
	if err != nil {
		return nil, fmt.Errorf("application: open the local development time store: %w", err)
	}
	for _, tenant := range cfg.ServedTenants() {
		seeded, seedErr := seedLocalDevTimeProfiles(ctx, in.Pool, store, tenant, now())
		if seedErr != nil {
			store.Close()
			return nil, seedErr
		}
		if seeded.Pinned > 0 || seeded.Published > 0 {
			logger.Info("hcmnext.local_dev_time_profiles_ready", "tenant", tenant, "profiles_published", seeded.Published, "assignments_pinned", seeded.Pinned)
		}
	}
	telemetryProvider := in.Telemetry
	var ownedProvider *hcmotel.Provider
	if telemetryProvider == nil {
		ownedProvider, err = newDiscardTelemetryProvider(ctx, cfg.CellID)
		if err != nil {
			store.Close()
			return nil, err
		}
		telemetryProvider = ownedProvider
	}
	worker, err := newLocalDevWorkerSelf(in, store, telemetryProvider, now)
	if err != nil {
		if ownedProvider != nil {
			_ = ownedProvider.Shutdown(context.Background())
		}
		store.Close()
		return nil, err
	}
	logger.Info("hcmnext.local_dev_time_clock_ready", "schema", timestore.SchemaName, "tenants", strings.Join(cfg.ServedTenants(), ","))
	return &ClockRuntime{Worker: &worker, Store: store, Close: func() {
		store.Close()
		if ownedProvider != nil {
			_ = ownedProvider.Shutdown(context.Background())
		}
	}}, nil
}

func newLocalDevWorkerSelf(in localDevSelfClockInput, store *timestore.Store, provider *hcmotel.Provider, now func() time.Time) (clockservice.WorkerSelfService, error) {
	cfg := in.Config
	plan, err := clockpunch.Compile()
	if err != nil {
		return clockservice.WorkerSelfService{}, fmt.Errorf("application: compile the clock workflow: %w", err)
	}
	served := make(map[uuid.UUID]string)
	for _, tenant := range cfg.ServedTenants() {
		served[pgstore.TenantID(tenant)] = tenant
	}
	resolveTenant := func(key string) (uuid.UUID, error) {
		id := pgstore.TenantID(strings.TrimSpace(key))
		if _, ok := served[id]; !ok {
			return uuid.Nil, fmt.Errorf("clock workflow: tenant %q is not served here", key)
		}
		return id, nil
	}
	tenantKey := func(id uuid.UUID) (string, error) {
		key, ok := served[id]
		if !ok {
			return "", errors.New("clock workflow: tenant is not served here")
		}
		return key, nil
	}
	telemetryAdapter, err := clockservice.NewClockWorkflowTelemetry(provider, slog.New(slog.NewTextHandler(io.Discard, nil)), now)
	if err != nil {
		return clockservice.WorkerSelfService{}, fmt.Errorf("application: clock workflow telemetry: %w", err)
	}
	terminal, err := NewClockWorkflowTerminalWriter("hcmnext:local-dev:clock-workflow", now)
	if err != nil {
		return clockservice.WorkerSelfService{}, fmt.Errorf("application: clock workflow terminal writer: %w", err)
	}
	signalAdapter := platformexecution.SignalSubscriptions{Store: signals.Store{}}
	committer := timeclockstore.Adapter{Store: store}
	factory, err := NewClockWorkflowDriverFactory(ClockWorkflowDriverFactoryOptions{
		DB: in.Pool, BaseSteps: clockPlanSteps{}, Commit: committer, Telemetry: telemetryAdapter,
		Terminal: terminal, Guard: idempotency.PostgresStore{},
		Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
		Signals:   signalAdapter, SignalReader: signalAdapter, SignalTimeoutReader: signalAdapter,
		ResolveTenant: resolveTenant, Clock: now,
	})
	if err != nil {
		return clockservice.WorkerSelfService{}, fmt.Errorf("application: clock workflow driver factory: %w", err)
	}
	bindings := timeclockstore.WorkflowBindingAdapter{Store: store, ResolveTenant: tenantKey}
	adapter := clockservice.TimeClockRuntimeAdapter{
		Resolver: clockPlanResolver{plan: plan}, Versions: workflowversionstore.Store{DB: in.Pool},
		Bindings: bindings, CellID: cfg.CellID, Clock: now, ResolveTenant: resolveTenant,
		Signals:        ClockSignalReceiver{DB: in.Pool, Verify: clockAttestedSignalVerifier{}, Now: now},
		VersionsReader: PostgresWorkflowInstanceVersionReader{Pool: in.Pool},
		SchemaRef:      clockpunch.ClockOutSignalSchemaRef(), Source: clockSignalSource,
	}
	ports, err := timeclockstore.NewPorts(store)
	if err != nil {
		return clockservice.WorkerSelfService{}, fmt.Errorf("application: clock persistence ports: %w", err)
	}
	people := SelfClockWorkforce{Pool: in.Pool}
	service := clockservice.Service{
		Sessions: ports.Sessions, Observations: ports.Observations, Receipts: ports.Receipts,
		Profiles: timeclockstore.TimestoreProfileResolver{Store: store},
		Workers:  people, Auth: SelfClockAuthority{Workers: people},
		Workflow: adapter,
		PunchWorkflow: clockservice.WorkflowPunchDriver{
			Factory: factory, Adapter: &adapter, Resolver: resolveTenant, Bindings: bindings,
			Receipts: committer, Evidence: PostgresClockWorkflowEvidenceReader{DB: in.Pool}, Telemetry: telemetryAdapter,
		},
		Outbox: committer, Work: committer, IDs: clockIDs{}, Clock: now,
	}
	labels := timeclockstore.CatalogClockLabels{
		Locale:        workspace.LocaleContext{Requested: "en-US", Resolved: "en-US"},
		WorkerLabel:   func(string) string { return "" },
		ScheduleLabel: func(string) string { return "" },
	}
	return (SelfClockComposition{
		Service: service, Workers: people,
		Profiles: timeclockstore.PublishedProfileSource{Store: store},
		Store:    timeclockstore.SelfProjectionStore{Store: store, Labels: labels},
		Clock:    now,
	}).WorkerSelfService()
}

func newDiscardTelemetryProvider(ctx context.Context, cellID string) (*hcmotel.Provider, error) {
	allowlist, err := telemetry.DefaultAllowlist()
	if err != nil {
		return nil, fmt.Errorf("application: compile telemetry allowlist: %w", err)
	}
	provider, err := hcmotel.NewProvider(ctx, hcmotel.Config{
		Resource:        telemetry.NewResourceFromBuild(buildinfo.Current(), "hcmnext", "local-dev-clock", "development", cellID, "", telemetry.ProcessRoleAPI, telemetry.TenantClassStandard),
		Evaluator:       telemetry.NewEvaluator(allowlist, telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion), telemetry.DefaultSamplingPolicy()),
		ShutdownTimeout: TelemetryShutdownGrace,
		Trace:           hcmotel.TraceConfig{StdoutWriter: io.Discard},
		Metric:          hcmotel.MetricConfig{StdoutWriter: io.Discard},
	})
	if err != nil {
		return nil, fmt.Errorf("application: build the clock workflow telemetry: %w", err)
	}
	return provider, nil
}

// localDevTimeDSN derives the connection URL of the time schema's role from
// the core URL. The password is a keyed digest of the development HMAC key, so
// nothing secret is written down and a changed key rotates the role's password
// on the next start.
func localDevTimeDSN(coreURL, devKey string) (dsn, password string, err error) {
	if strings.TrimSpace(devKey) == "" {
		return "", "", errors.New("the development key is not set")
	}
	u, parseErr := url.Parse(strings.TrimSpace(coreURL))
	if parseErr != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return "", "", errors.New("the core database URL is not a postgres URL")
	}
	mac := hmac.New(sha256.New, []byte(devKey))
	_, _ = mac.Write([]byte("hcmnext-local-dev-time-role"))
	password = hex.EncodeToString(mac.Sum(nil))[:32]
	u.User = url.UserPassword(localDevTimeRole, password)
	return u.String(), password, nil
}

// provisionLocalDevTimeSchema creates the role and empty schema the time store
// runs in. It needs a core credential able to create roles, which a local
// development database has; anything less reports why and leaves the clock off.
func provisionLocalDevTimeSchema(ctx context.Context, pool *pgxadapter.Pool, password string) error {
	statements := []string{
		fmt.Sprintf(`DO $do$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%[1]s') THEN
				CREATE ROLE %[1]s LOGIN PASSWORD '%[2]s';
			ELSE
				ALTER ROLE %[1]s LOGIN PASSWORD '%[2]s';
			END IF;
			EXECUTE format('GRANT CONNECT, CREATE ON DATABASE %%I TO %[1]s', current_database());
		END $do$`, localDevTimeRole, password),
		fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s AUTHORIZATION %s`, timestore.SchemaName, localDevTimeRole),
		fmt.Sprintf(`ALTER SCHEMA %s OWNER TO %s`, timestore.SchemaName, localDevTimeRole),
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("provision the time schema: %w", err)
		}
	}
	return nil
}

// timeProfileSeedSummary counts what one seeding pass added.
type timeProfileSeedSummary struct{ Published, Pinned int }

// seedLocalDevTimeProfiles gives a demo company's workers their time profiles:
// hourly workers clock by punch, everyone else is an exempt salaried employee
// who records no time by clock. A company with no hourly worker is left alone,
// so its workers meet the honest "no time profile" answer. It adds and never
// rewrites: a worker with a pinned assignment keeps that pin.
func seedLocalDevTimeProfiles(ctx context.Context, pool *pgxadapter.Pool, store *timestore.Store, tenant string, at time.Time) (timeProfileSeedSummary, error) {
	var summary timeProfileSeedSummary
	if _, isDemo := demoworkforce.PackFor(tenant); !isDemo || pool == nil || store == nil {
		return summary, nil
	}
	rows, err := listTenantWorkers(ctx, pool, tenant)
	if err != nil {
		return summary, err
	}
	hourly := false
	for _, row := range rows {
		hourly = hourly || row.PayBasis == demoworkforce.PayBasisHourly
	}
	if !hourly {
		return summary, nil
	}
	resolvedAt := at.UTC()
	for _, row := range rows {
		if !strings.EqualFold(row.LifecycleStatus, "active") || row.AssignmentID == "" {
			continue
		}
		if _, pinErr := store.AssignmentProfilePinFor(ctx, tenant, row.AssignmentID); pinErr == nil {
			continue
		} else if !errors.Is(pinErr, timestore.ErrNotFound) {
			return summary, fmt.Errorf("application: read a time profile pin: %w", pinErr)
		}
		profile := demoTimeProfile(tenant, row)
		payload, marshalErr := timeclockstore.EncodeProfilePayload(profile)
		digest, digestErr := profile.Digest()
		if marshalErr != nil || digestErr != nil {
			return summary, fmt.Errorf("application: encode the demo time profile for %s: %w", row.WorkerKey, errors.Join(marshalErr, digestErr))
		}
		err := store.PublishProfileVersion(ctx, tenant, timestore.ProfileVersion{
			TenantID: tenant, ProfileID: profile.ID, Version: int64(profile.Version), EffectiveFrom: profile.EffectiveFrom.Time(),
			Payload: payload, Digest: digest, PublishedBy: localDevTimeSeedAuthor,
		})
		if err == nil {
			summary.Published++
		} else if !errors.Is(err, timestore.ErrRevisionConflict) {
			return summary, fmt.Errorf("application: publish the demo time profile for %s: %w", row.WorkerKey, err)
		}
		if _, err := store.PinAssignmentProfile(ctx, tenant, row.AssignmentID, profile.ID, int64(profile.Version), 0, resolvedAt); err != nil {
			return summary, fmt.Errorf("application: pin the demo time profile for %s: %w", row.WorkerKey, err)
		}
		summary.Pinned++
	}
	return summary, nil
}

func listTenantWorkers(ctx context.Context, pool *pgxadapter.Pool, tenant string) ([]workforce.WorkerRow, error) {
	tenantID := pgstore.TenantID(tenant)
	tx, err := pool.BeginReadOnly(ctx)
	if err != nil {
		return nil, fmt.Errorf("application: begin the workforce read for time profiles: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	rows, err := (workforce.Store{}).List(ctx, tx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("application: list workers for time profiles: %w", err)
	}
	return rows, nil
}

// demoTimeProfile is the profile a demo worker's recorded pay basis calls for.
func demoTimeProfile(tenant string, row workforce.WorkerRow) timeprofile.TimeProfile {
	profile := timeprofile.TimeProfile{
		ID: "tp-" + row.WorkerKey, Version: 1, TenantRef: values.TenantId(tenant),
		EffectiveFrom:  values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		Category:       timeprofile.CategoryEmployee,
		AggregationKey: "worker:" + row.WorkerKey,
		Destination:    timeprofile.DestinationPayroll,
	}
	if row.PayBasis == demoworkforce.PayBasisHourly {
		profile.Capture, profile.PayBasis, profile.Exemption = timeprofile.CapturePunch, timeprofile.PayHourly, timeprofile.NonExempt
		profile.OvertimeMethod = timeprofile.OvertimeSingleRate
		profile.OvertimeJurisdictions = []string{"US-FED", "US-CO"}
		return profile
	}
	profile.Capture, profile.PayBasis, profile.Exemption = timeprofile.CaptureNone, timeprofile.PaySalary, timeprofile.Exempt
	profile.OvertimeMethod = timeprofile.OvertimeNone
	profile.OvertimeJurisdictions = []string{"US-FED"}
	return profile
}
