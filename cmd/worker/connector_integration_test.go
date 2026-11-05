package main

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

// connectorIntegrationPlanRequest is connectorPlanRequest with a caller-
// supplied tenant and a real wall-clock deadline: the bootstrap-driven
// dispatch loop below runs on the real clock (deps.Clock, not a fixed test
// clock), so the operation's deadline has to be in that same real "now" or
// MemoryJournal.Lease refuses it as expired before the role ever gets to
// run.
func connectorIntegrationPlanRequest(id uuid.UUID, tenant, destination string) operation.PlanRequest {
	req := connectorPlanRequest(id, destination)
	req.TenantID = tenant
	req.CreatedAt = time.Now().UTC()
	req.DeadlineAt = req.CreatedAt.Add(time.Hour)
	return req
}

// testConnectorBuild mirrors exactly the connector-role branch of
// production build() in main.go (same connectorRole type, same
// runConnectorLoop), but takes its journal/ledger/credential
// source/writer/authorizer as parameters instead of constructing
// production's fail-closed stand-ins. That is the same substitution the
// existing TestWorkerIntegrationDispatchesOutboxViaBootstrap makes for
// DBPoolFactory: one dependency is swapped for a test double; flag parsing,
// workload scheduling and shutdown all still run for real through
// bootstrap.Run. The substitution is unavoidable here because SVC-008's
// journal is process-internal by design (operation.MemoryJournal; no
// migration is in scope for this todo) - production's build() constructs
// one privately, with no seam to pre-seed it with fixture operations before
// Run starts.
func testConnectorBuild(journal connectorJournal, ledger connectorLedger, credentials connectorCredentialSource, writer operation.CredentialWriter, authorizer operation.MachineLeaseAuthorizer, tenants tenantLister, privacy connectorPrivacyAuthority) func(context.Context, bootstrap.Deps) (bootstrap.Runtime, error) {
	return func(_ context.Context, deps bootstrap.Deps) (bootstrap.Runtime, error) {
		enabled, err := deps.Values.Bool("connector-role")
		if err != nil {
			return bootstrap.Runtime{}, err
		}
		pollInterval, err := deps.Values.Duration("poll-interval")
		if err != nil {
			return bootstrap.Runtime{}, err
		}
		if !enabled {
			// Flag off: no connector workload is registered at all, not a
			// registered-but-inert one. bootstrap.Run tolerates an empty
			// Runtime (it starts, becomes ready and waits for shutdown).
			return bootstrap.Runtime{}, nil
		}
		role := connectorRole{
			logger: deps.Logger, journal: journal, ledger: ledger, credentials: credentials,
			privacy: privacy, revalidate: stubConnectorRevalidator{fn: confirmedConnectorRevalidation}, writer: writer,
			authorizer: authorizer, credentialOperation: custody.Encrypt, workerID: deps.Identity, leaseFor: time.Minute,
		}
		return bootstrap.Runtime{Workloads: []bootstrap.Workload{{
			Name: "connector-role",
			Run: func(ctx context.Context) error {
				return runConnectorLoop(ctx, deps.Logger, tenants, role, pollInterval)
			},
		}}}, nil
	}
}

// TestTodo_SVC_008_Integration proves the connector role is wired through
// worker's real bootstrap.Run pipeline the same way
// TestWorkerIntegrationDispatchesOutboxViaBootstrap proves the outbox role
// is: real config resolution (flags/env), the real workload run-group and
// real ordered shutdown. With connector-role=true, one pre-seeded QUEUED
// operation is claimed, dispatched and its attempt persisted in the
// journal. With connector-role=false, the workload never runs at all: the
// same operation is left exactly as journaled.
func TestTodo_SVC_008_Integration(t *testing.T) {
	t.Run("flag_on_dispatches_and_persists", func(t *testing.T) {
		journal := operation.NewMemoryJournal(nil)
		tenant := uuid.New()
		id := uuid.New()
		if _, err := journal.Plan(context.Background(), connectorIntegrationPlanRequest(id, tenant.String(), "connector.example")); err != nil {
			t.Fatalf("Plan: %v", err)
		}
		if _, err := journal.Queue(context.Background(), tenant.String(), id); err != nil {
			t.Fatalf("Queue: %v", err)
		}

		manager := newConnectorMachineManager(t, time.Now().UTC())
		credSource := &recordingCredentialSource{manager: manager, ttl: time.Minute}
		writer := &recordingCredentialWriter{}

		s := spec([]string{"-database-url=postgres://ignored/db", "-poll-interval=25ms", "-connector-role=true"})
		s.Getenv = func(string) (string, bool) { return "", false }
		s.DBPoolFactory = bootstrap.NewFakeDBPoolFactory(bootstrap.NewFakeDBPool())
		s.Logger = discardLogger()
		s.Stdout = io.Discard
		s.Stderr = io.Discard
		s.Build = testConnectorBuild(journal, ampleLedger(), credSource, writer, manager, fakeTenantLister{tenants: []uuid.UUID{tenant}}, connectorPrivacyAuthorityFor(t, "connector.example", false))

		runCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan int, 1)
		go func() { done <- bootstrap.Run(runCtx, s) }()

		deadline := time.Now().Add(20 * time.Second)
		var got operation.Operation
		for time.Now().Before(deadline) {
			var getErr error
			got, getErr = journal.Get(context.Background(), tenant.String(), id)
			if getErr != nil {
				t.Fatalf("read journal operation: %v", getErr)
			}
			if got.State == operation.StateProviderAccepted {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		if got.State != operation.StateProviderAccepted || len(got.Attempts) != 1 {
			t.Fatalf("operation after bootstrap-driven dispatch = %+v, want PROVIDER_ACCEPTED with one persisted attempt", got)
		}
		if writer.calls() != 1 {
			t.Fatalf("writer calls = %d, want exactly 1", writer.calls())
		}

		cancel()
		select {
		case code := <-done:
			if code != bootstrap.ExitOK {
				t.Fatalf("bootstrap.Run exit code = %d, want ExitOK", code)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("bootstrap.Run did not return after its context was canceled")
		}
	})

	t.Run("flag_off_role_never_runs", func(t *testing.T) {
		journal := operation.NewMemoryJournal(nil)
		tenant := uuid.New()
		id := uuid.New()
		if _, err := journal.Plan(context.Background(), connectorIntegrationPlanRequest(id, tenant.String(), "connector.example")); err != nil {
			t.Fatalf("Plan: %v", err)
		}
		if _, err := journal.Queue(context.Background(), tenant.String(), id); err != nil {
			t.Fatalf("Queue: %v", err)
		}

		manager := newConnectorMachineManager(t, time.Now().UTC())
		credSource := &recordingCredentialSource{manager: manager, ttl: time.Minute}
		writer := &recordingCredentialWriter{}

		s := spec([]string{"-database-url=postgres://ignored/db", "-poll-interval=25ms"}) // connector-role omitted: defaults false
		s.Getenv = func(string) (string, bool) { return "", false }
		s.DBPoolFactory = bootstrap.NewFakeDBPoolFactory(bootstrap.NewFakeDBPool())
		s.Logger = discardLogger()
		s.Stdout = io.Discard
		s.Stderr = io.Discard
		s.Build = testConnectorBuild(journal, ampleLedger(), credSource, writer, manager, fakeTenantLister{tenants: []uuid.UUID{tenant}}, connectorPrivacyAuthorityFor(t, "connector.example", false))

		runCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan int, 1)
		go func() { done <- bootstrap.Run(runCtx, s) }()

		// Give the (absent) workload every chance to have run several sweep
		// intervals, then prove it never touched the operation at all.
		time.Sleep(300 * time.Millisecond)
		got, err := journal.Get(context.Background(), tenant.String(), id)
		if err != nil {
			t.Fatalf("read journal operation: %v", err)
		}
		if got.State != operation.StateQueued || len(got.Attempts) != 0 {
			t.Fatalf("operation with connector-role=false = %+v, want unchanged QUEUED with zero attempts", got)
		}
		if writer.calls() != 0 || credSource.callCount() != 0 {
			t.Fatalf("provider or credential source was called with connector-role=false: writer=%d credentialMints=%d", writer.calls(), credSource.callCount())
		}

		cancel()
		select {
		case code := <-done:
			if code != bootstrap.ExitOK {
				t.Fatalf("bootstrap.Run exit code = %d, want ExitOK", code)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("bootstrap.Run did not return after its context was canceled")
		}
	})
}
