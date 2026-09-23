package application

// The application lifecycle.
//
// ARCH-GO-020's GREEN says cmd/* "only parses command/runtime configuration,
// selects role and invokes application lifecycle". This file is the third of
// those: one interface a command can start and stop, and one composed value
// that implements it over exactly the workloads and ordered shutdown steps
// the composition produced.
//
// [App.Runtime] hands the same two lists to internal/platform/bootstrap, so
// the deployed process and a test driving [App.Start]/[App.Stop] run the same
// workloads through the same shutdown order. There is deliberately no second
// list: a lifecycle that stopped different things than the process stops
// would prove nothing about the process.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

// Lifecycle is what a command invokes once it has selected a role.
type Lifecycle interface {
	// Start begins every workload. It returns once they are running, not
	// once they finish.
	Start(ctx context.Context) error
	// Stop runs the ordered shutdown sequence and waits for the workloads to
	// return. It is safe to call more than once and safe to call without a
	// preceding Start.
	Stop(ctx context.Context) error
}

// App is one composed application: its graph, its workloads, its ordered
// shutdown steps, and whatever role-specific values a caller legitimately
// needs to read back (the composed cell and the two bound addresses, for the
// serve role).
type App struct {
	role   Role
	graph  Graph
	logger bootstrap.Logger
	cell   *app.Cell
	// disposition is the serve cell's governed retention, legal-hold and
	// verified-deletion gate (REV-004-02). It is composed with the cell so
	// the libraries it fronts are reachable from the running process.
	disposition *DispositionGate
	grpcAddr    string
	httpAddr    string

	workloads []bootstrap.Workload
	shutdown  []bootstrap.ShutdownStep
	// listeners are closed by Stop after the ordered steps have run, so a
	// composition that was never started still releases its bound ports. In
	// the deployed process the steps close them first and this is a no-op.
	listeners []net.Listener

	mu      sync.Mutex
	started bool
	stopped bool
	wg      sync.WaitGroup
	runErrs []error
}

var _ Lifecycle = (*App)(nil)

// Role reports which role this application was composed for.
func (a *App) Role() Role { return a.role }

// Graph is the composed dependency graph.
func (a *App) Graph() Graph { return a.graph }

// Cell is the composed application cell, or nil for a role that has none.
func (a *App) Cell() *app.Cell { return a.cell }

// Disposition is the composed governed-disposition gate, or nil for a role
// that composes none.
func (a *App) Disposition() *DispositionGate { return a.disposition }

// GRPCAddr and HTTPAddr are the addresses the two surfaces actually bound.
// They are read back rather than read from configuration because ":0" is a
// legitimate configured address and the bound port is what a caller needs.
func (a *App) GRPCAddr() string { return a.grpcAddr }

// HTTPAddr is the bound HTTP edge address.
func (a *App) HTTPAddr() string { return a.httpAddr }

// Runtime is the bootstrap view of this application: the same workloads and
// the same ordered shutdown steps Start and Stop drive.
func (a *App) Runtime() bootstrap.Runtime {
	return bootstrap.Runtime{
		Workloads: append([]bootstrap.Workload(nil), a.workloads...),
		Shutdown:  append([]bootstrap.ShutdownStep(nil), a.shutdown...),
	}
}

// Start runs every workload concurrently.
func (a *App) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return fmt.Errorf("application: %s has already stopped", a.role)
	}
	if a.started {
		return fmt.Errorf("application: %s has already started", a.role)
	}
	a.started = true
	for _, workload := range a.workloads {
		workload := workload
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			if err := workload.Run(ctx); err != nil {
				a.mu.Lock()
				a.runErrs = append(a.runErrs, fmt.Errorf("%s: %w", workload.Name, err))
				a.mu.Unlock()
			}
		}()
	}
	return nil
}

// Stop runs the ordered shutdown sequence, releases any still-bound
// listener, and waits for the started workloads to return. Every step runs
// even when an earlier one failed: a shutdown that abandons the remaining
// steps on the first error leaves exactly the resources it was supposed to
// release.
func (a *App) Stop(ctx context.Context) error {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return nil
	}
	a.stopped = true
	started := a.started
	a.mu.Unlock()

	var errs []error
	for _, step := range a.shutdown {
		if step.Run == nil {
			continue
		}
		if err := step.Run(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", step.Name, err))
		}
	}
	for _, listener := range a.listeners {
		if listener == nil {
			continue
		}
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, fmt.Errorf("close listener: %w", err))
		}
	}
	if started {
		a.wg.Wait()
	}
	a.mu.Lock()
	errs = append(errs, a.runErrs...)
	a.mu.Unlock()
	return errors.Join(errs...)
}
