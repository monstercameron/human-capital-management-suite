package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/application/timeclockstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
)

// ClockDependencies are the application-owned ports which have no safe
// production default. In particular, roster and authorization are supplied by
// the caller so clock composition cannot silently grant access to a fixture or
// an invented supervisor policy.
type ClockDependencies struct {
	Roster        clockservice.RosterSource
	Authorizer    clockservice.Authorizer
	Workforce     timeclockstore.AuthoritativeWorkerDirectory
	Lookup        timeclockstore.CredentialLookup
	Supervisor    timeclockstore.SupervisorCredentialVerifier
	IDs           clockservice.IDs
	Workflow      clockservice.SessionWorkflow
	PunchWorkflow clockservice.WorkflowPunchExecutor
	BatchWorkflow clockservice.WorkflowBatchExecutor
	PremiumInputs clockservice.PremiumInputs
	CaseTasks     clockservice.CaseTasks
	FleetAlerts   clockservice.FleetAlertSink
	// SelfLabels supplies localized worker and schedule presentation from
	// authoritative upstream facts. Nil leaves the optional worker API disabled.
	SelfLabels timeclockstore.SelfClockLabels

	// Store and OpenStore are test and embedding seams. At most one may be
	// supplied. The normal production path uses ClockRuntimeConfig.NewTimeStore.
	Store     *timestore.Store
	OpenStore func(context.Context, ClockRuntimeConfig) (*timestore.Store, error)
	Registry  timeclockstore.VerifiedRegistrySource
}

// ClockRuntime is the mounted application surface and its owned lifetime.
// Close is idempotent and must be called by the serve root during shutdown.
type ClockRuntime struct {
	API    clockservice.DeviceAPI
	Worker *clockservice.WorkerSelfService
	Store  *timestore.Store
	Close  func()
}

// ComposeClock composes the production device API when cfg is enabled. A nil
// configuration means the optional clock surface is disabled and returns nil
// without inspecting any other dependency. Enabled composition validates all
// authority seams before opening the dedicated time store.
func ComposeClock(ctx context.Context, cfg *ClockRuntimeConfig, corePool *pgxadapter.Pool, serverNow func() time.Time, deps ClockDependencies) (*ClockRuntime, error) {
	if cfg == nil {
		return nil, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("compose clock: %w", err)
	}
	if ctx == nil {
		return nil, errors.New("compose clock: context is required")
	}
	if corePool == nil {
		return nil, errors.New("compose clock: core database pool is required")
	}
	if serverNow == nil || serverNow().IsZero() {
		return nil, errors.New("compose clock: trusted server clock is required")
	}
	if deps.Roster == nil {
		return nil, errors.New("compose clock: authoritative roster is required")
	}
	if deps.Authorizer == nil {
		return nil, errors.New("compose clock: punch and device authority is required")
	}
	if deps.Workforce == nil {
		return nil, errors.New("compose clock: authoritative workforce directory is required")
	}
	if deps.Lookup == nil {
		return nil, errors.New("compose clock: credential lookup authority is required")
	}
	if deps.Supervisor == nil {
		return nil, errors.New("compose clock: supervisor credential authority is required")
	}
	if deps.IDs == nil {
		return nil, errors.New("compose clock: id generator is required")
	}
	if deps.Workflow == nil {
		return nil, errors.New("compose clock: session workflow is required")
	}
	if deps.PunchWorkflow == nil {
		return nil, errors.New("compose clock: synchronous punch workflow is required")
	}
	if deps.BatchWorkflow == nil {
		return nil, errors.New("compose clock: synchronous batch workflow is required")
	}
	if deps.Store != nil && deps.OpenStore != nil {
		return nil, errors.New("compose clock: store and store factory are mutually exclusive")
	}

	store, err := openClockStore(ctx, *cfg, deps)
	if err != nil {
		return nil, fmt.Errorf("compose clock: open time store: %w", err)
	}
	if store == nil {
		return nil, errors.New("compose clock: store factory returned nil store")
	}
	closeOnce := sync.Once{}
	closeStore := func() { closeOnce.Do(store.Close) }

	registry := deps.Registry
	if registry == nil {
		verified, loadErr := cfg.LoadSignedRegistry(serverNow().UTC())
		if loadErr != nil {
			closeStore()
			return nil, fmt.Errorf("compose clock: load signed registry: %w", loadErr)
		}
		registry = fixedClockRegistry{registry: verified}
	}
	ports, err := timeclockstore.NewPortsWithRegistry(store, registry, serverNow)
	if err != nil {
		closeStore()
		return nil, fmt.Errorf("compose clock: build persistence ports: %w", err)
	}

	policy := timeclockstore.NewSitePolicyAdapter(store, serverNow)
	workerDirectory := timeclockstore.ProductionWorkerDirectory{Source: deps.Workforce}
	profiles := timeclockstore.TimestoreProfileResolver{Store: store}
	tokens := clockservice.HMACDeviceWorkerTokens{Key: append([]byte(nil), cfg.WorkerTokenKey...), Clock: serverNow}
	identification := timeclockstore.IdentificationAdapter{
		CredentialAdapter: timeclockstore.CredentialAdapter{Store: store},
		Policy:            policy, Token: tokens, Lookup: deps.Lookup, Supervisor: deps.Supervisor,
	}
	service := clockservice.Service{
		Sessions: ports.Sessions, Observations: ports.Observations, Receipts: ports.Receipts,
		Devices: ports.Devices, Enrollments: ports.Enrollments, Credentials: identification,
		Heartbeats: ports.Heartbeats, Roster: deps.Roster, Policies: policy,
		Profiles: profiles, Workers: workerDirectory, Auth: deps.Authorizer,
		Workflow: deps.Workflow, PunchWorkflow: deps.PunchWorkflow, BatchWorkflow: deps.BatchWorkflow,
		Outbox: timeclockstore.Adapter{Store: store},
		Work:   timeclockstore.Adapter{Store: store}, PremiumInputs: deps.PremiumInputs,
		CaseTasks: deps.CaseTasks, IDs: deps.IDs, Clock: serverNow,
	}
	api := clockservice.DeviceFacade{
		Service:      service,
		Fleet:        clockservice.FleetService{Devices: service, Policies: policy, Alerts: deps.FleetAlerts},
		Credentials:  timeclockstore.ProductionCredentialResolver{Roster: deps.Roster, Devices: ports.Devices, Credentials: identification, Policy: policy, Clock: serverNow},
		Tokens:       tokens,
		PunchContext: timeclockstore.ProductionPunchContextSource{Tokens: tokens, Workers: workerDirectory, Profiles: profiles},
		WorkerStatus: timeclockstore.ProductionWorkerStatusSource{Directory: deps.Workforce},
		Clock:        serverNow,
	}
	runtime := &ClockRuntime{API: api, Store: store, Close: closeStore}
	if deps.SelfLabels != nil {
		worker, composeErr := (SelfClockComposition{
			Service:  service,
			Workers:  timeclockstore.WorkforceSelfSource{Directory: deps.Workforce, Clock: serverNow},
			Profiles: timeclockstore.PublishedProfileSource{Store: store},
			Store:    timeclockstore.SelfProjectionStore{Store: store, Labels: deps.SelfLabels},
			Clock:    serverNow,
		}).WorkerSelfService()
		if composeErr != nil {
			closeStore()
			return nil, fmt.Errorf("compose clock: worker API: %w", composeErr)
		}
		runtime.Worker = &worker
	}
	return runtime, nil
}

func openClockStore(ctx context.Context, cfg ClockRuntimeConfig, deps ClockDependencies) (*timestore.Store, error) {
	if deps.Store != nil {
		return deps.Store, nil
	}
	if deps.OpenStore != nil {
		return deps.OpenStore(ctx, cfg)
	}
	return cfg.NewTimeStore(ctx)
}

// fixedClockRegistry keeps the already verified registry behind the source
// interface required by proof-bound device enrollment.
type fixedClockRegistry struct {
	registry clockdomain.VerifiedProfileRegistry
}

func (r fixedClockRegistry) Resolve(_ context.Context, tenant string, now time.Time) (clockdomain.VerifiedProfileRegistry, error) {
	if strings.TrimSpace(tenant) == "" || now.Before(r.registry.ValidFrom) || !now.Before(r.registry.ValidUntil) {
		return clockdomain.VerifiedProfileRegistry{}, errors.New("clock registry is unavailable")
	}
	registry, err := clockdomain.NewProfileRegistry(r.registry.Registry.Profiles)
	if err != nil || registry.Digest != r.registry.Registry.Digest || registry.Version != r.registry.Registry.Version {
		return clockdomain.VerifiedProfileRegistry{}, errors.New("clock registry is invalid")
	}
	return clockdomain.VerifiedProfileRegistry{Revision: r.registry.Revision, ValidFrom: r.registry.ValidFrom, ValidUntil: r.registry.ValidUntil, Registry: registry}, nil
}
