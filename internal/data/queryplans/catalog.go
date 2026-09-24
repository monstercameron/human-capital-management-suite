package queryplans

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/jobs"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/temporal"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
)

// DefaultRowThreshold is the total row count this package seeds each
// catalogue entry's table to before asking PostgreSQL for a plan: not the
// tenant under proof's own row count, but the table's, spread across
// [decoyTenantCount] other tenants by [seedWithDecoys] plus [targetRowCount]
// rows of the tenant's own.
//
// The split matters. A tenant whose own rows are the WHOLE of a table is a
// query the planner correctly answers with a sequential scan no matter which
// index exists -- filtering "tenant_id = $1" removes nothing when every row
// already satisfies it, so a scan-then-filter is cheaper than a scan-through-
// an-index for exactly the same reason it would be cheaper on any other
// unfiltered read. Proving an index is actually chosen requires the shape a
// real shared table has: the tenant a request names is a small, selective
// slice of a much larger table most of whose rows belong to someone else.
// 20,000 total rows -- split four ways by migrations/00005_ledger.sql's own
// hash partitioning of ledger_event where that table is under proof, so
// roughly 5,000 rows land in the one partition the tenant under proof's own
// rows hash to -- is comfortably past the row count at which a small table
// or a small per-partition slice is correctly scanned sequentially
// regardless of indexes. The margin past a bare top-level equality scan's
// own crossover matters here specifically because
// internal/data/ledger/temporal's delegated CORRECTION self-join
// (internal/data/bitemporal's own "target" alias in sql.go) is a nested-loop
// lookup run once per row the outer scan returns: at a few hundred rows per
// partition PostgreSQL sometimes prices sequentially scanning the whole
// partition, once per outer row, as cheaper than 25 index probes into it --
// a real cost trade-off, not a missing index, that only clears once the
// partition itself is large enough that a sequential pass costs more than
// an index probe even repeated. Seeding stays fast enough (one bulk INSERT
// ... SELECT ... FROM unnest(...) per tenant, then one ANALYZE) to run
// inside an ordinary `go test` at this size.
const DefaultRowThreshold = 20000

// decoyTenantCount is how many other tenants [seedWithDecoys] spreads the
// non-target rows of [DefaultRowThreshold] across.
const decoyTenantCount = 10

// targetRowCount is how many rows [seedWithDecoys] gives the tenant under
// proof -- small and fixed, rather than a share of the threshold, so growing
// [DefaultRowThreshold] makes the table bigger and the proof harder, never
// the target tenant's own slice less realistic.
const targetRowCount = 25

// seedTenantRow inserts one ACTIVE tenant row: the FK every table this
// catalogue seeds requires (migrations/00002_tenant_primitives.sql).
func seedTenantRow(ctx context.Context, ex dbport.Conn, tenant uuid.UUID, key string) error {
	if _, err := ex.Exec(ctx, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant, key, key); err != nil {
		return fmt.Errorf("queryplans: seed tenant %s: %w", key, err)
	}
	return nil
}

// seedWithDecoys calls seedRows once per freshly created decoy tenant with a
// share of total's non-target rows, then once more for target with
// [targetRowCount] rows -- always last, so a caller's seedRows closure can
// tell the tenant under proof from a decoy by simple equality and capture
// whatever identifier (a worker key, a run id, ...) its own run closure will
// need. See [DefaultRowThreshold] for why the split, not just the total,
// matters.
func seedWithDecoys(ctx context.Context, ex dbport.Conn, target uuid.UUID, total int,
	seedRows func(ctx context.Context, ex dbport.Conn, tenant uuid.UUID, n int) error,
) error {
	decoyRows := total - targetRowCount
	if decoyRows < 0 {
		decoyRows = 0
	}
	perDecoy := decoyRows / decoyTenantCount
	for i := 0; i < decoyTenantCount; i++ {
		decoy := uuid.New()
		if err := seedTenantRow(ctx, ex, decoy, fmt.Sprintf("queryplans-decoy-%02d-%s", i, decoy.String()[:8])); err != nil {
			return err
		}
		if perDecoy > 0 {
			if err := seedRows(ctx, ex, decoy, perDecoy); err != nil {
				return err
			}
		}
	}
	return seedRows(ctx, ex, target, targetRowCount)
}

// Entry is one declared critical query: the table it must not scan
// sequentially at [Entry.RowThreshold] rows, and a Prepare function that
// seeds that table and returns a run closure driving the real, exported
// store method under proof.
//
// Prepare receives ex -- an unrestricted connection the seeding statements
// run on -- and returns run, a closure the prover calls with a
// [dbport.Conn] it has wrapped in [Spy] and scoped to hcmnext_app under one
// tenant, exactly the way a production request-scoped connection reaches
// these tables. run's own job is only ever to call the one store method
// Entry exists to prove; the SQL that method sends is what [Spy] recovers,
// never a copy this package typed out separately.
type Entry struct {
	// Name identifies the entry in reports and the golden.
	Name string
	// Owner names the package and method this entry proves, for a reader who
	// wants to go look at the store code.
	Owner string
	// Table is the relation [Prove] must not find sequentially scanned. For
	// migrations/00005_ledger.sql's hash-partitioned ledger_event this is the
	// parent table name; belongsToTable (see plan.go) accounts for the
	// per-partition child relations a plan actually names.
	Table string
	// RowThreshold is how many rows Prepare seeds Table to.
	RowThreshold int
	// ExpectedIndexSubstrings is the set of index-name fragments; the proof
	// requires at least one index-based scan node whose index name contains
	// one of them (see [PlanNode.UsesIndexContaining]).
	ExpectedIndexSubstrings []string
	// Prepare seeds ex with RowThreshold rows scoped to tenant and returns a
	// closure that issues the exact store call this entry proves.
	Prepare func(ctx context.Context, ex dbport.Conn, tenant uuid.UUID, rowThreshold int) (run func(ctx context.Context, q dbport.Conn) error, err error)
}

// Catalog returns DB-020's declared critical queries.
//
// # A named gap
//
// planning/todos.md's DB-020 entry also names "workflow-ready" and similar
// scheduler-poll reads among the query shapes to prove. This lane surveyed
// internal/data/runtimestate in full (inventory.go, scheduling.go,
// workqueue.go): every store there is a single-row Enqueue / Transition /
// Load keyed by an id its own caller already holds (FrontierStore.Enter,
// ReadyWorkStore.Load, TimerStore.Load, QueueStore.LoadItem, and so on). No
// store method lists workflow_ready_work by (tenant, state, eligible_at),
// scans workflow_timer for what is due, or dispatches from work_queue_item
// by (queue, state, priority, eligible_at) -- migration 00026's own
// workflow_ready_work_eligible, workflow_timer_due and work_queue_item_queue
// indexes were built ahead of that code, for the WF-RUN scheduler
// definitions/runtime/durable-runtime-decision.yaml (WF-RUN-000) still gates
// (see inventory.go's GatedTables doc comment). This package's own contract
// (plan.go's doc comment, and Entry.Prepare's) is to prove the exact
// statement a real, exported store method sends; inventing a poll query no
// store issues would prove a guess about a future scheduler's SQL, not a
// fact about this codebase. Those three indexes are already tenant-leading
// (verified by reading migrations/00026_workflow_scheduling_state.sql
// directly) and need no migration; they simply have no entry here because
// they have no caller yet. When WF-RUN-002's scheduler lands its own store
// method, that method belongs in this catalogue.
func Catalog() []Entry {
	return []Entry{
		workforceListEntry(),
		workforceGetEntry(),
		ledgerStreamReplayEntry(),
		ledgerEffectiveAsOfEntry(),
		jobsPartitionListByRunEntry(),
		jobsLoadOpenRunEntry(),
		intentcontrolContextLoadEntry(),
	}
}

// hex64 returns a 64 lowercase hex character digest, the shape every
// content_digest domain column (migrations/00002_tenant_primitives.sql)
// requires, deterministically derived from seed so re-running Prepare twice
// in the same process produces the same fixture bytes.
func hex64(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// internal/data/workforce
// ---------------------------------------------------------------------------

// seedJourneyWorkers inserts n journey_worker rows for tenant in one
// statement and returns the last one's worker_key, for a caller that needs a
// specific row to look up.
func seedJourneyWorkers(ctx context.Context, ex dbport.Conn, tenant uuid.UUID, n int) (string, error) {
	ids := make([]string, n)
	keys := make([]string, n)
	lastKey := ""
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("queryplans-seed-worker-%06d", i)
		ids[i] = uuid.New().String()
		keys[i] = key
		lastKey = key
	}
	if _, err := ex.Exec(ctx, `
		INSERT INTO journey_worker (
			tenant_id, worker_id, worker_key,
			legal_name, preferred_name, worker_number,
			worker_type, lifecycle_status,
			employment_id, assignment_id,
			job_code, grade, org_unit, position_id, location, pay_zone,
			fte, manager_relationship_ref,
			hire_date, effective_from,
			base_pay, currency, pay_basis, bonus_target,
			revision_stream, revision_sequence, known_at, recorded_at,
			created_by, source)
		SELECT $1, w.id::uuid, w.key,
			'Queryplans Seed', 'Seed', w.key,
			'EMPLOYEE', 'ACTIVE',
			'EMP-SEED', 'ASG-SEED',
			'JC-SEED', 'G-SEED', 'OU-SEED', 'POS-SEED', 'LOC-SEED', 'PZ-SEED',
			1.0000, 'MGR-SEED',
			DATE '2020-01-01', DATE '2020-01-01',
			50000.00, 'USD', 'SALARIED', 0.1000,
			w.key, 1, now(), now(),
			'queryplans-seed', 'CREATED'
		FROM unnest($2::text[], $3::text[]) AS w(id, key)`,
		tenant, ids, keys); err != nil {
		return "", fmt.Errorf("queryplans: seed journey_worker: %w", err)
	}
	if _, err := ex.Exec(ctx, "ANALYZE journey_worker"); err != nil {
		return "", fmt.Errorf("queryplans: analyze journey_worker: %w", err)
	}
	return lastKey, nil
}

func workforceListEntry() Entry {
	return Entry{
		Name:                    "WORKFORCE_LIST_BY_TENANT",
		Owner:                   "internal/data/workforce.Store.List (internal/data/workforce/store.go)",
		Table:                   "journey_worker",
		RowThreshold:            DefaultRowThreshold,
		ExpectedIndexSubstrings: []string{"journey_worker_recorded"},
		Prepare: func(ctx context.Context, ex dbport.Conn, tenant uuid.UUID, n int) (func(context.Context, dbport.Conn) error, error) {
			seed := func(ctx context.Context, ex dbport.Conn, t uuid.UUID, rows int) error {
				_, err := seedJourneyWorkers(ctx, ex, t, rows)
				return err
			}
			if err := seedWithDecoys(ctx, ex, tenant, n, seed); err != nil {
				return nil, err
			}
			return func(ctx context.Context, q dbport.Conn) error {
				_, err := (workforce.Store{}).List(ctx, q, tenant)
				return err
			}, nil
		},
	}
}

func workforceGetEntry() Entry {
	return Entry{
		Name:  "WORKFORCE_GET_BY_KEY_OR_ID",
		Owner: "internal/data/workforce.Store.Get (internal/data/workforce/store.go)",
		Table: "journey_worker",
		// Store.Get's OR across worker_key and worker_id::text has no single
		// index of its own -- worker_id::text is not indexed at all -- but
		// EXPLAIN shows PostgreSQL never needs one: journey_worker_recorded
		// (tenant_id, recorded_at DESC) already bounds the Bitmap Heap Scan to
		// the requesting tenant's own rows before the OR is ever evaluated as
		// a filter, and journey_worker_key_unique (tenant_id, worker_key)
		// bounds it just as well when the planner picks that one instead. A
		// tenant-leading index needs to exist, not be the one shaped exactly
		// like this query's OR, for the scan to stay bounded by tenant size
		// rather than table size.
		ExpectedIndexSubstrings: []string{"journey_worker_recorded", "journey_worker_key_unique"},
		RowThreshold:            DefaultRowThreshold,
		Prepare: func(ctx context.Context, ex dbport.Conn, tenant uuid.UUID, n int) (func(context.Context, dbport.Conn) error, error) {
			var targetKey string
			seed := func(ctx context.Context, ex dbport.Conn, t uuid.UUID, rows int) error {
				key, err := seedJourneyWorkers(ctx, ex, t, rows)
				if err != nil {
					return err
				}
				if t == tenant {
					targetKey = key
				}
				return nil
			}
			if err := seedWithDecoys(ctx, ex, tenant, n, seed); err != nil {
				return nil, err
			}
			return func(ctx context.Context, q dbport.Conn) error {
				_, _, err := (workforce.Store{}).Get(ctx, q, tenant, targetKey)
				return err
			}, nil
		},
	}
}

// ---------------------------------------------------------------------------
// internal/data/ledger and internal/data/ledger/temporal
// ---------------------------------------------------------------------------

const seedSchemaRef = "queryplans-seed-schema"

// streamKeyForTenant derives one tenant's own seeded stream key.
//
// The key is per-tenant, not a literal shared by every seeded tenant,
// because a shared literal makes stream_key's own column statistics say it
// matches 100% of the table no matter how selective tenant_id is -- the same
// "this tenant owns the whole table" trap [DefaultRowThreshold]'s own doc
// comment describes for an ungated tenant_id predicate, reproduced one
// column over. A real ledger never shares one stream key across every
// tenant, so this fixture should not either.
func streamKeyForTenant(tenant uuid.UUID) string {
	return "queryplans-seed-stream-" + tenant.String()
}

// seedLedgerEvents registers one ledger_stream and one payload_schema, then
// inserts n TRANSACTION_FACT ledger_event rows on that stream, spread one
// minute apart starting 2026-01-01T00:00:00Z. TRANSACTION_FACT is the one
// assertion class migrations/00005_ledger.sql's own
// ledger_event_authority_required check does not require an authority_ref
// for, which keeps this fixture from also needing an authority_assignment
// row.
func seedLedgerEvents(ctx context.Context, ex dbport.Conn, tenant uuid.UUID, n int) error {
	streamKey := streamKeyForTenant(tenant)
	if _, err := ex.Exec(ctx, `
		INSERT INTO ledger_stream (tenant_id, stream_key, stream_kind, subject_ref)
		VALUES ($1, $2, 'TRANSACTION', 'queryplans-seed-subject')`,
		tenant, streamKey); err != nil {
		return fmt.Errorf("queryplans: seed ledger_stream: %w", err)
	}
	if _, err := ex.Exec(ctx, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, $2, 1, 'queryplans.Seed', 'PROTOBUF', 'LEDGER_EVENT')`,
		tenant, seedSchemaRef); err != nil {
		return fmt.Errorf("queryplans: seed payload_schema: %w", err)
	}

	sequences := make([]int64, n)
	eventIDs := make([]string, n)
	digests := make([]string, n)
	correlationIDs := make([]string, n)
	idempotencyKeys := make([]string, n)
	effectiveAt := make([]time.Time, n)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		sequences[i] = int64(i + 1)
		eventIDs[i] = uuid.New().String()
		correlationIDs[i] = uuid.New().String()
		digests[i] = hex64(fmt.Sprintf("queryplans-ledger-event-%d", i))
		idempotencyKeys[i] = fmt.Sprintf("queryplans-ledger-idem-%06d", i)
		effectiveAt[i] = base.Add(time.Duration(i) * time.Minute)
	}
	if _, err := ex.Exec(ctx, `
		INSERT INTO ledger_event (
			tenant_id, stream_key, sequence, event_id, assertion_class,
			source_ref, schema_ref, payload, canonical_length,
			digest, digest_algorithm,
			occurred_at, effective_at, recorded_at,
			correlation_id, idempotency_key)
		SELECT $1, $2, e.sequence, e.event_id::uuid, 'TRANSACTION_FACT',
			'queryplans-seed', $3, '\x00'::bytea, 1,
			e.digest, 'sha256',
			e.effective_at, e.effective_at, now(),
			e.correlation_id::uuid, e.idempotency_key
		FROM unnest($4::bigint[], $5::text[], $6::text[], $7::text[], $8::text[], $9::timestamptz[])
			AS e(sequence, event_id, digest, correlation_id, idempotency_key, effective_at)`,
		tenant, streamKey, seedSchemaRef,
		sequences, eventIDs, digests, correlationIDs, idempotencyKeys, effectiveAt); err != nil {
		return fmt.Errorf("queryplans: seed ledger_event: %w", err)
	}
	if _, err := ex.Exec(ctx, "ANALYZE ledger_event"); err != nil {
		return fmt.Errorf("queryplans: analyze ledger_event: %w", err)
	}
	return nil
}

func ledgerStreamReplayEntry() Entry {
	return Entry{
		Name:         "LEDGER_STREAM_REPLAY",
		Owner:        "internal/data/ledger.Reader.ReadStream (internal/data/ledger/reader.go)",
		Table:        "ledger_event",
		RowThreshold: DefaultRowThreshold,
		// ReadStream's WHERE clause (tenant_id, stream_key) is served exactly
		// by ledger_event's own primary key; EXPLAIN also shows PostgreSQL
		// sometimes preferring ledger_event_correlation (tenant_id,
		// correlation_id) or ledger_event_effective (tenant_id, stream_key,
		// effective_at) purely on the leading tenant_id column, which bounds
		// the Bitmap Heap Scan to the tenant's own rows just as well. Any of
		// the three is a tenant-leading index; which one the planner reaches
		// for is a cost-estimate detail, not a schema gap. ledger_event is
		// hash-partitioned (migrations/00005_ledger.sql), so a per-partition
		// index PostgreSQL builds for a parent index it did not itself name
		// gets its own generated name (ledger_event_p0_tenant_id_correlation_id_idx,
		// never ledger_event_correlation) -- the column-name fragments below
		// match that generated shape as well as the parent's own name.
		ExpectedIndexSubstrings: []string{
			"_pkey",
			"ledger_event_correlation", "tenant_id_correlation_id_idx",
			"ledger_event_effective", "tenant_id_stream_key_effective_at_idx",
		},
		Prepare: func(ctx context.Context, ex dbport.Conn, tenant uuid.UUID, n int) (func(context.Context, dbport.Conn) error, error) {
			if err := seedWithDecoys(ctx, ex, tenant, n, seedLedgerEvents); err != nil {
				return nil, err
			}
			return func(ctx context.Context, q dbport.Conn) error {
				_, err := ledger.NewReader().ReadStream(ctx, q, tenant, streamKeyForTenant(tenant))
				return err
			}, nil
		},
	}
}

func ledgerEffectiveAsOfEntry() Entry {
	return Entry{
		Name:         "LEDGER_EFFECTIVE_AS_OF",
		Owner:        "internal/data/ledger/temporal.AsOf (internal/data/ledger/temporal/query.go, delegated to internal/data/bitemporal.Query)",
		Table:        "ledger_event",
		RowThreshold: DefaultRowThreshold,
		// See LEDGER_STREAM_REPLAY's own comment: any tenant-leading index on
		// ledger_event bounds this query's scan to the tenant's own rows, and
		// EXPLAIN shows the planner choosing among the same three (by
		// whichever name -- parent or generated per-partition child -- it
		// actually built for the chosen index).
		ExpectedIndexSubstrings: []string{
			"_pkey",
			"ledger_event_correlation", "tenant_id_correlation_id_idx",
			"ledger_event_effective", "tenant_id_stream_key_effective_at_idx",
		},
		Prepare: func(ctx context.Context, ex dbport.Conn, tenant uuid.UUID, n int) (func(context.Context, dbport.Conn) error, error) {
			if err := seedWithDecoys(ctx, ex, tenant, n, seedLedgerEvents); err != nil {
				return nil, err
			}
			return func(ctx context.Context, q dbport.Conn) error {
				req := temporal.Request{
					Tenant:      tenant,
					Mode:        temporal.ModeEffectiveAsOf,
					Subject:     streamKeyForTenant(tenant),
					EffectiveAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
					KnownAt:     time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				}
				dec := temporal.Decision{Tenant: tenant}
				_, err := temporal.AsOf(ctx, q, req, dec)
				return err
			}, nil
		},
	}
}

// ---------------------------------------------------------------------------
// internal/data/jobs
// ---------------------------------------------------------------------------

// seedJobPartitions registers one job_definition and one job_run, then
// inserts n PENDING job_partition rows on that run, and returns the run id.
func seedJobPartitions(ctx context.Context, ex dbport.Conn, tenant uuid.UUID, n int) (uuid.UUID, error) {
	const jobID = "queryplans-seed-job"
	runID := uuid.New()

	if _, err := ex.Exec(ctx, `
		INSERT INTO job_definition (
			tenant_id, job_id, version, definition_digest, trigger_digest,
			target_definition_ref, target_definition_version, body,
			published_by, published_at)
		VALUES ($1, $2, 1, $3, $4, 'queryplans-seed-target', 1, '\x00'::bytea, 'queryplans-seed', now())`,
		tenant, jobID, hex64("queryplans-job-definition"), hex64("queryplans-job-trigger")); err != nil {
		return uuid.Nil, fmt.Errorf("queryplans: seed job_definition: %w", err)
	}
	if _, err := ex.Exec(ctx, `
		INSERT INTO job_run (tenant_id, run_id, job_id, job_version, run_state, declared_by, declared_at)
		VALUES ($1, $2, $3, 1, 'DECLARED', 'queryplans-seed', now())`,
		tenant, runID, jobID); err != nil {
		return uuid.Nil, fmt.Errorf("queryplans: seed job_run: %w", err)
	}

	partitionIDs := make([]string, n)
	partitionKeys := make([]string, n)
	for i := 0; i < n; i++ {
		partitionIDs[i] = uuid.New().String()
		partitionKeys[i] = fmt.Sprintf("queryplans-partition-%06d", i)
	}
	if _, err := ex.Exec(ctx, `
		INSERT INTO job_partition (tenant_id, partition_id, run_id, partition_key, partition_state, created_at)
		SELECT $1, p.id::uuid, $2, p.key, 'PENDING', now()
		FROM unnest($3::text[], $4::text[]) AS p(id, key)`,
		tenant, runID, partitionIDs, partitionKeys); err != nil {
		return uuid.Nil, fmt.Errorf("queryplans: seed job_partition: %w", err)
	}
	if _, err := ex.Exec(ctx, "ANALYZE job_partition"); err != nil {
		return uuid.Nil, fmt.Errorf("queryplans: analyze job_partition: %w", err)
	}
	return runID, nil
}

func jobsPartitionListByRunEntry() Entry {
	return Entry{
		Name:         "JOBS_PARTITION_LIST_BY_RUN",
		Owner:        "internal/data/jobs.PartitionStore.ListByRun (internal/data/jobs/jobs.go)",
		Table:        "job_partition",
		RowThreshold: DefaultRowThreshold,
		// ListByRun orders by partition_key, which job_partition_key_unique
		// (tenant_id, run_id, partition_key) serves for free -- EXPLAIN shows
		// the planner preferring it over job_partition_run (tenant_id,
		// run_id, partition_state) for exactly that reason, since the latter
		// would still need a separate sort. Both are tenant-leading, so
		// either is an acceptable answer to "is this bounded by an index".
		ExpectedIndexSubstrings: []string{"job_partition_key_unique", "job_partition_run"},
		Prepare: func(ctx context.Context, ex dbport.Conn, tenant uuid.UUID, n int) (func(context.Context, dbport.Conn) error, error) {
			var targetRun uuid.UUID
			seed := func(ctx context.Context, ex dbport.Conn, t uuid.UUID, rows int) error {
				runID, err := seedJobPartitions(ctx, ex, t, rows)
				if err != nil {
					return err
				}
				if t == tenant {
					targetRun = runID
				}
				return nil
			}
			if err := seedWithDecoys(ctx, ex, tenant, n, seed); err != nil {
				return nil, err
			}
			return func(ctx context.Context, q dbport.Conn) error {
				_, err := (jobs.PartitionStore{}).ListByRun(ctx, q, tenant, targetRun)
				return err
			}, nil
		},
	}
}

func seedOpenJobRuns(ctx context.Context, ex dbport.Conn, tenant uuid.UUID, n int) error {
	const jobID = "queryplans-seed-open-job"
	if _, err := ex.Exec(ctx, `
		INSERT INTO job_definition (
			tenant_id, job_id, version, definition_digest, trigger_digest,
			target_definition_ref, target_definition_version, body,
			published_by, published_at)
		VALUES ($1, $2, 1, $3, $4, 'queryplans-seed-target', 1, '\x00'::bytea, 'queryplans-seed', now())`,
		tenant, jobID, hex64("queryplans-open-job-definition"), hex64("queryplans-open-job-trigger")); err != nil {
		return fmt.Errorf("queryplans: seed open job definition: %w", err)
	}
	runIDs := make([]string, n)
	declaredAt := make([]time.Time, n)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range n {
		runIDs[i] = uuid.NewString()
		declaredAt[i] = base.Add(time.Duration(i) * time.Second)
	}
	if _, err := ex.Exec(ctx, `
		INSERT INTO job_run (tenant_id, run_id, job_id, job_version, run_state, declared_by, declared_at)
		SELECT $1, r.id::uuid, $2, 1, 'DECLARED', 'queryplans-seed', r.declared_at
		FROM unnest($3::text[], $4::timestamptz[]) AS r(id, declared_at)`, tenant, jobID, runIDs, declaredAt); err != nil {
		return fmt.Errorf("queryplans: seed open job runs: %w", err)
	}
	if _, err := ex.Exec(ctx, "ANALYZE job_run"); err != nil {
		return fmt.Errorf("queryplans: analyze job_run: %w", err)
	}
	return nil
}

func jobsLoadOpenRunEntry() Entry {
	return Entry{
		Name:         "JOBS_LOAD_OPEN_RUN_BY_JOB",
		Owner:        "internal/data/jobs.RunStore.LoadOpenByJob (internal/data/jobs/jobs.go)",
		Table:        "job_run",
		RowThreshold: DefaultRowThreshold,
		// The primary key's tenant_id prefix also bounds this lookup to the
		// requesting tenant's runs; the planner currently chooses it over the
		// job and state indexes when the seeded tenant slice is selective.
		ExpectedIndexSubstrings: []string{"job_run_pkey", "job_run_job", "job_run_state"},
		Prepare: func(ctx context.Context, ex dbport.Conn, tenant uuid.UUID, n int) (func(context.Context, dbport.Conn) error, error) {
			if err := seedWithDecoys(ctx, ex, tenant, n, seedOpenJobRuns); err != nil {
				return nil, err
			}
			return func(ctx context.Context, q dbport.Conn) error {
				_, err := (jobs.RunStore{}).LoadOpenByJob(ctx, q, tenant, "queryplans-seed-open-job")
				return err
			}, nil
		},
	}
}

// ---------------------------------------------------------------------------
// internal/data/intentcontrol
// ---------------------------------------------------------------------------

// seedIntentContexts inserts n intent_instance rows and their
// intent_instance_context rows, and returns the last intent's id.
func seedIntentContexts(ctx context.Context, ex dbport.Conn, tenant uuid.UUID, n int) (uuid.UUID, error) {
	intentIDs := make([]string, n)
	idempotencyKeys := make([]string, n)
	var lastID uuid.UUID
	for i := 0; i < n; i++ {
		id := uuid.New()
		intentIDs[i] = id.String()
		idempotencyKeys[i] = fmt.Sprintf("queryplans-intent-idem-%06d", i)
		lastID = id
	}

	if _, err := ex.Exec(ctx, `
		INSERT INTO intent_instance (
			tenant_id, intent_id, definition_ref, definition_version,
			request_digest, idempotency_key,
			request_state, execution_state, business_state, consistency_state, obligation_state,
			created_at, last_transition_at)
		SELECT $1, i.id::uuid, 'queryplans-seed-definition', 1,
			$2, i.idem,
			'DRAFT', 'NOT_PLANNED', 'NOT_STARTED', 'NOT_APPLICABLE', 'NOT_APPLICABLE',
			now(), now()
		FROM unnest($3::text[], $4::text[]) AS i(id, idem)`,
		tenant, hex64("queryplans-intent-request"), intentIDs, idempotencyKeys); err != nil {
		return uuid.Nil, fmt.Errorf("queryplans: seed intent_instance: %w", err)
	}

	if _, err := ex.Exec(ctx, `
		INSERT INTO intent_instance_context (
			tenant_id, intent_id, intent_family, execution_mode,
			feature_ref, feature_coverage_digest,
			origin_trust, origin_kind,
			correlation_id, trace_id,
			initiator_kind, initiator_principal_id, identity_assurance_ref,
			purpose, classification, retention_class,
			control_digest, risk_context_digest)
		SELECT $1, i.id::uuid, 'CHANGE_REQUEST', 'EXECUTE',
			'queryplans-seed-feature', $2,
			'UNVERIFIED', 'SYSTEM_EVENT',
			i.id, i.id,
			'SYSTEM_EVENT', 'queryplans-seed-principal', 'queryplans-seed-assurance',
			'queryplans-seed-purpose', 'INTERNAL', 'STANDARD',
			$3, $4
		FROM unnest($5::text[]) AS i(id)`,
		tenant, hex64("queryplans-feature-coverage"), hex64("queryplans-control"), hex64("queryplans-risk"),
		intentIDs); err != nil {
		return uuid.Nil, fmt.Errorf("queryplans: seed intent_instance_context: %w", err)
	}
	if _, err := ex.Exec(ctx, "ANALYZE intent_instance_context"); err != nil {
		return uuid.Nil, fmt.Errorf("queryplans: analyze intent_instance_context: %w", err)
	}
	return lastID, nil
}

func intentcontrolContextLoadEntry() Entry {
	return Entry{
		Name:                    "INTENTCONTROL_CONTEXT_LOAD",
		Owner:                   "internal/data/intentcontrol.ContextStore.Load (internal/data/intentcontrol/intent.go)",
		Table:                   "intent_instance_context",
		RowThreshold:            DefaultRowThreshold,
		ExpectedIndexSubstrings: []string{"intent_instance_context_pkey"},
		Prepare: func(ctx context.Context, ex dbport.Conn, tenant uuid.UUID, n int) (func(context.Context, dbport.Conn) error, error) {
			var targetIntent uuid.UUID
			seed := func(ctx context.Context, ex dbport.Conn, t uuid.UUID, rows int) error {
				intentID, err := seedIntentContexts(ctx, ex, t, rows)
				if err != nil {
					return err
				}
				if t == tenant {
					targetIntent = intentID
				}
				return nil
			}
			if err := seedWithDecoys(ctx, ex, tenant, n, seed); err != nil {
				return nil, err
			}
			return func(ctx context.Context, q dbport.Conn) error {
				_, err := (intentcontrol.ContextStore{}).Load(ctx, q, tenant, targetIntent)
				return err
			}, nil
		},
	}
}
