package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/jobs"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/hashchain"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// This command owns nothing but its flag set and its role. Everything below
// that -- the tick, the leases, the admission rule, the dispatch settlement --
// is internal/platform/execution/scheduler's, and is tested there. These tests
// are therefore deliberately narrow: they check that the right role is
// selected, that configuration is parsed and refused before anything starts,
// and that the one identifier this command parses becomes the claim the
// runtime package serves.

func noEnv(string) (string, bool) { return "", false }

func minimalArgs(tenantID uuid.UUID) []string {
	return []string{"-database-url=postgres://localhost/hcmnext", "-tenant-id=" + tenantID.String()}
}

// TestSpecSelectsTheSchedulerRole is the whole of this command's composition
// responsibility: name the role, hand bootstrap the runtime package's
// configuration surface, and let bootstrap.Run do the rest.
func TestSpecSelectsTheSchedulerRole(t *testing.T) {
	s := spec(minimalArgs(uuid.New()))
	if s.Role != bootstrap.RoleScheduler {
		t.Fatalf("role = %q, want %q", s.Role, bootstrap.RoleScheduler)
	}
	if err := s.Role.Validate(); err != nil {
		t.Fatalf("the selected role is not in the process-roles vocabulary: %v", err)
	}
	if s.DatabaseURLField != scheduler.FieldDatabaseURL {
		t.Fatalf("database URL field = %q, want %q", s.DatabaseURLField, scheduler.FieldDatabaseURL)
	}
	if s.Validate == nil || s.Build == nil || s.DBPoolFactory == nil {
		t.Fatal("the spec leaves validation, build or the pool factory unwired")
	}
	wantFields := len(scheduler.ConfigFields()) + len(scheduler.RoleConfigFields()) + 5
	if len(s.ConfigFields) != wantFields {
		t.Fatalf("the spec declares %d fields, the runtime package declares %d",
			len(s.ConfigFields), wantFields)
	}
	values, err := bootstrap.ParseConfig([]string{
		"-database-url=postgres://localhost/hcmnext",
		"-tenant-id=" + uuid.NewString(),
		"-timer-role=false",
		"-signal-role=true",
		"-signal-shard=signal-west",
	}, noEnv, s.ConfigFields)
	if err != nil {
		t.Fatalf("ParseConfig with role flags: %v", err)
	}
	roles, err := scheduler.RolesFrom(values)
	if err != nil {
		t.Fatalf("RolesFrom: %v", err)
	}
	if roles.TimerEnabled || !roles.SignalEnabled || roles.SignalShard != "signal-west" {
		t.Fatalf("resolved roles = %+v, want timer disabled and signal-west enabled", roles)
	}
}

func TestTodo_REV_058_01(t *testing.T) {
	args := append(minimalArgs(uuid.New()), "-domain-dns-interval=4h")
	s := spec(args)
	values, err := bootstrap.ParseConfig(args, noEnv, s.ConfigFields)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateConfig(values); err != nil {
		t.Fatal(err)
	}
	if got, err := values.Duration(fieldDomainDNSInterval); err != nil || got != 4*time.Hour {
		t.Fatalf("DNS interval=%v err=%v", got, err)
	}
	runtime, err := build(context.Background(), bootstrap.Deps{Values: values, DB: buildTestDB{}, Identity: "role:scheduler:test", Logger: testLogger{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, workload := range runtime.Workloads {
		if workload.Name == "sending-domain-dns-verifier" && workload.Run != nil {
			return
		}
	}
	t.Fatalf("scheduler workloads omit DNS reverification: %+v", runtime.Workloads)
}

// TestTodo_REV_012_03 verifies the running command registers its recurring
// ledger verifier workload and exposes a validated cadence.
func TestTodo_REV_012_03(t *testing.T) {
	s := spec(append(minimalArgs(uuid.New()), "-invariant-scan-interval=17s"))
	values, err := bootstrap.ParseConfig(append(minimalArgs(uuid.New()), "-invariant-scan-interval=17s"), noEnv, s.ConfigFields)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := values.Duration(fieldInvariantScanInterval); err != nil || got.String() != "17s" {
		t.Fatalf("scan interval = %v, err=%v", got, err)
	}
	if got := values.String(fieldInvariantScanOwner); got != defaultInvariantScanOwner {
		t.Fatalf("default incident owner = %q, want established operations-on-call route", got)
	}
	if got := values.String(fieldInvariantScanRoute); got != defaultInvariantScanRoute {
		t.Fatalf("default escalation route = %q, want established platform on-call route", got)
	}
	if err := validateConfig(values); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	runtime, err := build(context.Background(), bootstrap.Deps{Values: values, DB: buildTestDB{}, Identity: "role:scheduler:pid:1", Logger: testLogger{}})
	if err != nil {
		t.Fatalf("build scheduler workloads: %v", err)
	}
	found := false
	for _, workload := range runtime.Workloads {
		if workload.Name == "ledger-invariant-scanner" && workload.Run != nil {
			found = true
		}
	}
	if !found {
		t.Fatalf("scheduler workloads = %+v, missing recurring ledger scanner", runtime.Workloads)
	}
	found = false
	for _, workload := range runtime.Workloads {
		if workload.Name == "batch-job-worker" && workload.Run != nil {
			found = true
		}
	}
	if !found {
		t.Fatalf("scheduler workloads = %+v, missing batch job worker", runtime.Workloads)
	}
	values, err = bootstrap.ParseConfig(append(minimalArgs(uuid.New()), "-invariant-scan-interval=0s"), noEnv, s.ConfigFields)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateConfig(values); err == nil {
		t.Fatal("accepted a non-positive invariant scan cadence")
	}
	values, err = bootstrap.ParseConfig(append(minimalArgs(uuid.New()), "-batch-job-interval=0s"), noEnv, s.ConfigFields)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateConfig(values); err == nil {
		t.Fatal("accepted a non-positive batch job cadence")
	}
	values, err = bootstrap.ParseConfig(append(minimalArgs(uuid.New()),
		"-invariant-scan-owner=tenant-data-ops", "-invariant-scan-secondary-route=tenant-platform-oncall"), noEnv, s.ConfigFields)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateConfig(values); err != nil {
		t.Fatalf("validate explicit incident routes: %v", err)
	}
	values, err = bootstrap.ParseConfig(append(minimalArgs(uuid.New()),
		"-invariant-scan-owner=same", "-invariant-scan-secondary-route=same"), noEnv, s.ConfigFields)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateConfig(values); err == nil {
		t.Fatal("accepted duplicate ledger invariant incident routes")
	}
}

// TestTodo_REV_012_03_Integration proves the registered scanner reaches the
// real tenant-scoped ledger snapshot and emits a completed scan receipt.
func TestTodo_REV_012_03_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'scanner integration', 'ACTIVE', now())`, tenant, "rev012-03-"+tenant.String())
	db.Exec(t, `INSERT INTO ledger_stream (tenant_id, stream_key, stream_kind, subject_ref)
		VALUES ($1, 'scanner-integration-stream', 'TRANSACTION', 'scanner-subject')`, tenant)
	db.Exec(t, `INSERT INTO ledger_hash_chain_link
		(tenant_id, stream_key, sequence, event_id, prev_hash, chain_hash, chain_algorithm)
		VALUES ($1, 'scanner-integration-stream', 1, $2, $3, $4, 'sha256')`,
		tenant, uuid.New(), hashchain.GenesisHash, strings.Repeat("a", 64))

	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema, "role": tenancy.AppRole})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	t.Cleanup(pool.Close)
	logger := newScannerCaptureLogger()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runInvariantScanner(ctx, pool, tenant, time.Hour, logger) }()

	select {
	case entry := <-logger.errors:
		if entry.message != "ledger.invariant_scan.action_required" || !strings.Contains(entry.attributes, "ORPHAN_EVENT") || !strings.Contains(entry.attributes, "incident_ref") || !strings.Contains(entry.attributes, "ledger-invariant-repair:") {
			cancel()
			<-done
			t.Fatalf("initial scan log = %+v, want findings routed with incident and repair references", entry)
		}
	case <-time.After(15 * time.Second):
		cancel()
		<-done
		t.Fatal("registered scanner did not scan the real ledger on startup")
	}
	tx, err := pool.Begin(context.Background())
	if err != nil {
		cancel()
		<-done
		t.Fatalf("open tenant verification transaction: %v", err)
	}
	defer tx.Rollback(context.Background())
	if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
		cancel()
		<-done
		t.Fatalf("set verification tenant: %v", err)
	}
	var incidents, observations, repairs int
	if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM operational_incident WHERE tenant_id=$1 AND incident_key LIKE 'ledger-invariant:%' AND status='OPEN'`, tenant).Scan(&incidents); err != nil {
		cancel()
		<-done
		t.Fatalf("read routed incident: %v", err)
	}
	if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM ledger_invariant_scan_observation WHERE tenant_id=$1 AND quality='DEGRADED'`, tenant).Scan(&observations); err != nil {
		cancel()
		<-done
		t.Fatalf("read durable scan observation: %v", err)
	}
	if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM ledger_invariant_repair_work WHERE tenant_id=$1 AND status='OPEN'`, tenant).Scan(&repairs); err != nil {
		cancel()
		<-done
		t.Fatalf("read durable repair handoff: %v", err)
	}
	if incidents != 1 || observations != 1 || repairs != 1 {
		cancel()
		<-done
		t.Fatalf("durable scan route incidents=%d observations=%d repairs=%d, want 1/1/1", incidents, observations, repairs)
	}
	if err := tx.Commit(context.Background()); err != nil {
		cancel()
		<-done
		t.Fatalf("finish verification transaction: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("scanner shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("scanner did not stop after cancellation")
	}
}

// TestTodo_REV_012_03_Recovery proves a transient database outage is surfaced
// and the recurring scanner recovers on its next cadence without restarting.
func TestTodo_REV_012_03_Recovery(t *testing.T) {
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'scanner recovery', 'ACTIVE', now())`, tenant, "rev012-03-recovery-"+tenant.String())
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema, "role": tenancy.AppRole})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	t.Cleanup(pool.Close)
	logger := newScannerCaptureLogger()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runInvariantScanner(ctx, &scannerFailOnceDB{db: pool}, tenant, 25*time.Millisecond, logger)
	}()

	select {
	case entry := <-logger.errors:
		if entry.message != "ledger.invariant_scan.failed" {
			cancel()
			<-done
			t.Fatalf("first scan error log = %+v, want failed scan", entry)
		}
	case <-time.After(15 * time.Second):
		cancel()
		<-done
		t.Fatal("scanner did not surface the injected initial database failure")
	}
	select {
	case entry := <-logger.infos:
		if entry.message != "ledger.invariant_scan.completed" || !strings.Contains(entry.attributes, "HEALTHY") {
			cancel()
			<-done
			t.Fatalf("recovery scan log = %+v, want healthy receipt", entry)
		}
	case <-time.After(15 * time.Second):
		cancel()
		<-done
		t.Fatal("scanner did not recover on a later cadence")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("scanner shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("scanner did not stop after cancellation")
	}
}

type scannerFailOnceDB struct {
	db     invariantScanQuerier
	failed atomic.Bool
}

func (db *scannerFailOnceDB) Begin(ctx context.Context) (dbport.Tx, error) {
	if db.failed.CompareAndSwap(false, true) {
		return nil, errors.New("simulated transient scanner database failure")
	}
	return db.db.Begin(ctx)
}

type scannerLogEntry struct{ message, attributes string }

type scannerCaptureLogger struct {
	infos  chan scannerLogEntry
	errors chan scannerLogEntry
}

func newScannerCaptureLogger() *scannerCaptureLogger {
	return &scannerCaptureLogger{infos: make(chan scannerLogEntry, 4), errors: make(chan scannerLogEntry, 4)}
}

func (l *scannerCaptureLogger) Info(message string, args ...any) {
	l.infos <- scannerLogEntry{message: message, attributes: fmt.Sprint(args...)}
}

func (l *scannerCaptureLogger) Error(message string, args ...any) {
	l.errors <- scannerLogEntry{message: message, attributes: fmt.Sprint(args...)}
}

func TestTodo_REV_035_02_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'batch worker integration', 'ACTIVE', now())`, tenant, "rev035-02-"+tenant.String())
	db.Exec(t, `INSERT INTO ledger_stream (tenant_id, stream_key, stream_kind, subject_ref)
		VALUES ($1, 'served-ledger-stream', 'TRANSACTION', 'served-ledger-subject')`, tenant)
	db.Exec(t, `CREATE TABLE rev035_job_effect (
		tenant_id uuid NOT NULL,
		item_key text NOT NULL,
		digest text NOT NULL,
		PRIMARY KEY (tenant_id, item_key)
	)`)
	db.Exec(t, `ALTER TABLE rev035_job_effect ENABLE ROW LEVEL SECURITY`)
	db.Exec(t, `ALTER TABLE rev035_job_effect FORCE ROW LEVEL SECURITY`)
	db.Exec(t, `CREATE POLICY rev035_job_effect_tenant ON rev035_job_effect
		USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
		WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid)`)
	db.Exec(t, `GRANT SELECT, INSERT ON rev035_job_effect TO `+tenancy.AppRole)

	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema, "role": tenancy.AppRole})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	t.Cleanup(pool.Close)
	args := append(minimalArgs(tenant), "-batch-job-interval=1h", "-invariant-scan-interval=1h")
	s := spec(args)
	values, err := bootstrap.ParseConfig(args, noEnv, s.ConfigFields)
	if err != nil {
		t.Fatal(err)
	}
	firstNow := time.Now().UTC()
	var firstEffectCalls atomic.Int32
	firstProcessor := func(ctx context.Context, ex jobs.Executor, tenantID uuid.UUID, item jobs.WorkItem) (string, error) {
		digest, err := processLedgerInvariantItem(ctx, ex, tenantID, item)
		if err != nil {
			return "", err
		}
		if _, err := ex.Exec(ctx, `INSERT INTO rev035_job_effect (tenant_id, item_key, digest) VALUES ($1, $2, $3)`, tenantID, item.Key, digest); err != nil {
			return "", err
		}
		if firstEffectCalls.Add(1) == 1 {
			return "", errors.New("simulated process crash before checkpoint")
		}
		return digest, nil
	}
	firstLogger := &captureLogger{}
	runtime, err := buildWithBatchProcessor(bootstrap.Deps{
		Values: values, DB: pool, Identity: "integration-process", Logger: firstLogger, Clock: func() time.Time { return firstNow },
	}, firstProcessor)
	if err != nil {
		t.Fatalf("compose scheduler workloads: %v", err)
	}
	var batch bootstrap.Workload
	for _, workload := range runtime.Workloads {
		if workload.Name == "batch-job-worker" {
			batch = workload
		}
	}
	if batch.Run == nil {
		t.Fatal("composed scheduler has no batch job worker")
	}
	firstCtx, stopFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() { firstDone <- batch.Run(firstCtx) }()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && len(firstLogger.Errors()) == 0 {
		time.Sleep(25 * time.Millisecond)
	}
	if len(firstLogger.Errors()) == 0 || firstEffectCalls.Load() != 1 {
		stopFirst()
		t.Fatalf("first worker did not reach the simulated pre-checkpoint crash: processor calls=%d logs=%v", firstEffectCalls.Load(), firstLogger.Errors())
	}
	stopFirst()
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first worker shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first worker did not stop after cancellation")
	}
	assertEffectAndCheckpointCounts(t, pool, tenant, 0, 0)
	// The first processor inserted its effect and then failed before the
	// checkpoint. Both writes must have rolled back. A new served workload
	// reclaims the expired claim and attempts the same item again.
	secondNow := firstNow.Add(ledgerJobLease + time.Minute)
	secondLogger := &captureLogger{}
	secondPool := &faultingPool{Pool: pool, failAt: 8}
	secondProcessor := func(ctx context.Context, ex jobs.Executor, tenantID uuid.UUID, item jobs.WorkItem) (string, error) {
		digest, err := processLedgerInvariantItem(ctx, ex, tenantID, item)
		if err != nil {
			return "", err
		}
		_, err = ex.Exec(ctx, `INSERT INTO rev035_job_effect (tenant_id, item_key, digest) VALUES ($1, $2, $3)`, tenantID, item.Key, digest)
		return digest, err
	}
	secondRuntime, err := buildWithBatchProcessor(bootstrap.Deps{
		Values: values, DB: secondPool, Identity: "second-process", Logger: secondLogger, Clock: func() time.Time { return secondNow },
	}, secondProcessor)
	if err != nil {
		t.Fatalf("compose recovery scheduler workload: %v", err)
	}
	for _, workload := range secondRuntime.Workloads {
		if workload.Name == "batch-job-worker" {
			batch = workload
		}
	}
	secondCtx, stopSecond := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() { secondDone <- batch.Run(secondCtx) }()
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && len(secondLogger.Errors()) == 0 {
		time.Sleep(25 * time.Millisecond)
	}
	if len(secondLogger.Errors()) == 0 || secondPool.calls.Load() != secondPool.failAt {
		stopSecond()
		t.Fatalf("recovery worker did not reach simulated post-checkpoint crash: calls=%d logs=%v", secondPool.calls.Load(), secondLogger.Errors())
	}
	stopSecond()
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("recovery worker shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("recovery worker did not stop after cancellation")
	}
	assertEffectAndCheckpointCounts(t, pool, tenant, 1, 1)

	// The second process committed effect and checkpoint together, then failed
	// when opening the partition-completion transaction. A third process must resume after the
	// checkpoint and complete without invoking the effect processor again.
	restartClock := func() time.Time { return secondNow.Add(ledgerJobLease + time.Minute) }
	var finalProcessorCalls atomic.Int32
	finalProcessor := func(ctx context.Context, ex jobs.Executor, tenantID uuid.UUID, item jobs.WorkItem) (string, error) {
		finalProcessorCalls.Add(1)
		return secondProcessor(ctx, ex, tenantID, item)
	}
	restarted, err := buildWithBatchProcessor(bootstrap.Deps{
		Values: values, DB: pool, Identity: "restarted-process", Logger: &captureLogger{}, Clock: restartClock,
	}, finalProcessor)
	if err != nil {
		t.Fatalf("compose restarted scheduler workload: %v", err)
	}
	for _, workload := range restarted.Workloads {
		if workload.Name == "batch-job-worker" {
			batch = workload
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workerDone := make(chan error, 1)
	go func() { workerDone <- batch.Run(ctx) }()

	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var runs, completedRuns, partitions, completedPartitions, checkpoints, effects int
		tx, err := pool.Begin(context.Background())
		if err != nil {
			t.Fatalf("begin observe transaction: %v", err)
		}
		if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatal(err)
		}
		err = tx.QueryRow(context.Background(), `SELECT
			(SELECT count(*) FROM job_run WHERE tenant_id=$1),
			(SELECT count(*) FROM job_run WHERE tenant_id=$1 AND run_state='COMPLETED'),
			(SELECT count(*) FROM job_partition WHERE tenant_id=$1),
			(SELECT count(*) FROM job_partition WHERE tenant_id=$1 AND partition_state='COMPLETED'),
			(SELECT count(*) FROM job_checkpoint WHERE tenant_id=$1),
			(SELECT count(*) FROM rev035_job_effect WHERE tenant_id=$1)`, tenant).Scan(&runs, &completedRuns, &partitions, &completedPartitions, &checkpoints, &effects)
		if err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatalf("observe job completion: %v", err)
		}
		if err := tx.Commit(context.Background()); err != nil {
			t.Fatal(err)
		}
		if runs > 0 && completedRuns == runs && partitions > 0 && completedPartitions == partitions && checkpoints > 0 && effects == 1 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	var definitions, runs, completedRuns, partitions, completedPartitions, checkpoints int
	var partitionAttempt int
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	err = tx.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM job_definition WHERE tenant_id=$1 AND job_id=$2),
		(SELECT count(*) FROM job_run WHERE tenant_id=$1),
		(SELECT count(*) FROM job_run WHERE tenant_id=$1 AND run_state='COMPLETED'),
		(SELECT count(*) FROM job_partition WHERE tenant_id=$1),
		(SELECT count(*) FROM job_partition WHERE tenant_id=$1 AND partition_state='COMPLETED'),
		(SELECT count(*) FROM job_checkpoint WHERE tenant_id=$1),
		(SELECT COALESCE(max(attempt), 0) FROM job_partition WHERE tenant_id=$1)`, tenant, ledgerJobID).Scan(&definitions, &runs, &completedRuns, &partitions, &completedPartitions, &checkpoints, &partitionAttempt)
	if err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if definitions != 1 || runs != 1 || completedRuns != 1 || partitions != 1 || completedPartitions != 1 || checkpoints != 1 || partitionAttempt != 3 || finalProcessorCalls.Load() != 0 {
		t.Fatalf("served job outcome = definitions/runs/completed/partitions/complete/checkpoints/partition-attempt/final-processor-calls %d/%d/%d/%d/%d/%d/%d/%d, want 1/1/1/1/1/1/3/0; first worker errors: %v", definitions, runs, completedRuns, partitions, completedPartitions, checkpoints, partitionAttempt, finalProcessorCalls.Load(), firstLogger.Errors())
	}
	cancel()
	select {
	case err := <-workerDone:
		if err != nil {
			t.Fatalf("batch worker shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("batch worker did not stop after cancellation")
	}
}

func assertEffectAndCheckpointCounts(t *testing.T, pool dbport.Beginner, tenant uuid.UUID, wantEffects, wantCheckpoints int) {
	t.Helper()
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	var effects, checkpoints int
	err = tx.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM rev035_job_effect WHERE tenant_id=$1),
		(SELECT count(*) FROM job_checkpoint WHERE tenant_id=$1)`, tenant).Scan(&effects, &checkpoints)
	if err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if effects != wantEffects || checkpoints != wantCheckpoints {
		t.Fatalf("durable effect/checkpoint counts = %d/%d, want %d/%d", effects, checkpoints, wantEffects, wantCheckpoints)
	}
}

type faultingPool struct {
	*pgxadapter.Pool
	calls  atomic.Int32
	failAt int32
}

func (p *faultingPool) Begin(ctx context.Context) (dbport.Tx, error) {
	if p.calls.Add(1) == p.failAt {
		return nil, errors.New("simulated process crash after durable item checkpoint")
	}
	return p.Pool.Begin(ctx)
}

type buildTestDB struct{}

func (buildTestDB) Begin(context.Context) (dbport.Tx, error) { return nil, nil }
func (buildTestDB) Ping(context.Context) error               { return nil }
func (buildTestDB) Close()                                   {}

type testLogger struct{}

func (testLogger) Info(string, ...any)  {}
func (testLogger) Error(string, ...any) {}

type captureLogger struct {
	mu     sync.Mutex
	errors []string
}

func (l *captureLogger) Info(string, ...any) {}
func (l *captureLogger) Error(message string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errors = append(l.errors, message+": "+fmt.Sprint(args...))
}
func (l *captureLogger) Errors() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.errors...)
}

// TestSpecPreResolvesTheHealthAddress covers the one piece of parsing this
// command does ahead of bootstrap.Run, because Spec.HealthAddr is a plain
// field Run reads before it parses ConfigFields at all.
func TestSpecPreResolvesTheHealthAddress(t *testing.T) {
	args := append(minimalArgs(uuid.New()), "-health-addr=127.0.0.1:9464")
	if got := spec(args).HealthAddr; got != "127.0.0.1:9464" {
		t.Fatalf("health address = %q, want the flag's value", got)
	}
	if got := spec(minimalArgs(uuid.New())).HealthAddr; got != "" {
		t.Fatalf("health address = %q with no flag, want empty (the endpoint disabled)", got)
	}
	// A spec built from unparsable arguments still comes back so that
	// bootstrap.Run reports the parse failure itself, with the banner.
	if got := spec([]string{"-not-a-flag"}).HealthAddr; got != "" {
		t.Fatalf("health address = %q from unparsable arguments, want empty", got)
	}
}

// TestValidateConfigRefusesABadTenantIdentifier is the one check this command
// adds to the runtime package's own: internal/platform may not import
// github.com/google/uuid, so parsing the tenant is this root's job, and doing
// it at validation time makes an unparsable tenant a startup failure rather
// than a first-tick one.
func TestValidateConfigRefusesABadTenantIdentifier(t *testing.T) {
	for name, tenant := range map[string]string{
		"not a uuid": "the-acme-corporation",
		"truncated":  "0f9d1a1e-1111-2222",
		"nil uuid":   uuid.Nil.String(),
	} {
		t.Run(name, func(t *testing.T) {
			values, err := bootstrap.ParseConfig(
				[]string{"-database-url=postgres://localhost/x", "-tenant-id=" + tenant}, noEnv, scheduler.ConfigFields())
			if err != nil {
				t.Fatalf("ParseConfig: %v", err)
			}
			err = validateConfig(values)
			if err == nil {
				t.Fatalf("validateConfig accepted tenant %q", tenant)
			}
			if !strings.Contains(err.Error(), scheduler.FieldTenantID) {
				t.Fatalf("refusal %q does not name the offending flag", err)
			}
		})
	}
}

func TestValidateConfigAcceptsTheMinimalConfiguration(t *testing.T) {
	values, err := bootstrap.ParseConfig(minimalArgs(uuid.New()), noEnv, scheduler.ConfigFields())
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if err := validateConfig(values); err != nil {
		t.Fatalf("validateConfig on the minimal configuration: %v", err)
	}
}

// TestValidateConfigDefersToTheRuntimePackage proves this command adds a check
// rather than replacing one: a configuration the runtime package refuses is
// still refused here.
func TestValidateConfigDefersToTheRuntimePackage(t *testing.T) {
	values, err := bootstrap.ParseConfig(
		[]string{"-tenant-id=" + uuid.New().String()}, noEnv, scheduler.ConfigFields())
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if err := validateConfig(values); err == nil {
		t.Fatal("validateConfig accepted a configuration with no database URL")
	}
}

// TestClaimOfCarriesTheParsedTenantIntoTheLeaseRequest is the seam between the
// two roots: the identifier this command parsed travels to the runtime package
// inside a lease.AcquireRequest, which is how that package handles a tenant
// without naming the identifier type.
func TestClaimOfCarriesTheParsedTenantIntoTheLeaseRequest(t *testing.T) {
	tenantID := uuid.New()
	args := append(minimalArgs(tenantID), "-queue-key=queue:workflow-runtime-shard-2")
	values, err := bootstrap.ParseConfig(args, noEnv, scheduler.ConfigFields())
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	deps := bootstrap.Deps{Values: values, Identity: "role:scheduler:pid:99"}

	claim, err := claimOf(deps)
	if err != nil {
		t.Fatalf("claimOf: %v", err)
	}
	if claim.TenantID != tenantID {
		t.Fatalf("claim tenant = %s, want %s", claim.TenantID, tenantID)
	}
	if claim.Resource.Kind != lease.ResourceQueue || claim.Resource.ID != "queue:workflow-runtime-shard-2" {
		t.Fatalf("claim resource = %+v, want the configured QUEUE", claim.Resource)
	}
	if claim.Holder != (lease.Identity{
		WorkloadRef: scheduler.DefaultWorkloadRef, InstanceRef: "role:scheduler:pid:99",
	}) {
		t.Fatalf("claim holder = %+v, want the configured workload and this process instance", claim.Holder)
	}
}

func TestClaimOfRefusesABadTenantIdentifier(t *testing.T) {
	values, err := bootstrap.ParseConfig(
		[]string{"-database-url=postgres://localhost/x", "-tenant-id=not-a-uuid"}, noEnv, scheduler.ConfigFields())
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if _, err := claimOf(bootstrap.Deps{Values: values, Identity: "role:scheduler:pid:1"}); err == nil {
		t.Fatal("claimOf accepted an unparsable tenant")
	}
}
