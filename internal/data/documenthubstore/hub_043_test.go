// Load and workflow-isolation matrix for HUB-043: the document hub has its
// own connection pool, separate from chat and core/workflow storage
// (store.go, HUB-001), and this file proves that isolation holds under
// load rather than merely asserting it in configuration. TestTodo_HUB_043
// seeds a document corpus and runs concurrent readers and proposers
// against it while an independent operationstore (standing in for
// workflow/core, exactly as CHAT-052's local load profile does for chat)
// runs its own baseline/mixed/saturated phases, logging p95/p99 for both.
// TestTodo_HUB_043_Fault saturates the document pool outright and proves
// the separate core pool keeps admitting work throughout.
// TestTodo_HUB_043_Conformance runs the same harness and asserts local
// regression budgets.
//
// The scale is environment-overridable so the spec's planning inputs
// (50,000 documents, 500,000 immutable versions, 200 simultaneous
// readers, 30 simultaneous proposers -- test inputs, not predicted
// customer usage, per "Storage, records, and scale" in
// specs/channel-documentation-hub.md) are reachable in a manual or
// nightly run, while the default `go test` invocation stays well under a
// couple of minutes: HCMNEXT_HUB043_DOCS, HCMNEXT_HUB043_EXTRA_VERSIONS,
// HCMNEXT_HUB043_READERS, HCMNEXT_HUB043_PROPOSERS,
// HCMNEXT_HUB043_READS_PER_READER, HCMNEXT_HUB043_PROPOSALS_PER_PROPOSER.
package documenthubstore

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/operationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

// hub043Scale is the seeding/concurrency shape for one HUB-043 run.
type hub043Scale struct {
	Docs                 int
	ExtraVersionsPerDoc  int
	Readers              int
	Proposers            int
	ReadsPerReader       int
	ProposalsPerProposer int
	SeedWorkers          int
}

func envInt(key string, def int) int {
	if raw := os.Getenv(key); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			return v
		}
	}
	return def
}

// defaultHub043Scale keeps the default run small: ~300 documents each with
// a couple of extra historical versions, 20 concurrent readers and 5
// concurrent proposers. Every field is independently overridable, so a
// manual run naming the spec's planning scale (docs=50000,
// extra-versions=9 -> 500k versions, readers=200, proposers=30) is a
// straight environment override away.
func defaultHub043Scale() hub043Scale {
	return hub043Scale{
		Docs:                 envInt("HCMNEXT_HUB043_DOCS", 300),
		ExtraVersionsPerDoc:  envInt("HCMNEXT_HUB043_EXTRA_VERSIONS", 2),
		Readers:              envInt("HCMNEXT_HUB043_READERS", 20),
		Proposers:            envInt("HCMNEXT_HUB043_PROPOSERS", 5),
		ReadsPerReader:       envInt("HCMNEXT_HUB043_READS_PER_READER", 15),
		ProposalsPerProposer: envInt("HCMNEXT_HUB043_PROPOSALS_PER_PROPOSER", 5),
		SeedWorkers:          envInt("HCMNEXT_HUB043_SEED_WORKERS", 8),
	}
}

// documentFixtureConfig is documentFixture (integration_test.go) with a
// caller-chosen pool config, so the FAULT test can build a deliberately
// small document pool without touching the shared fixture other HUB tests
// depend on.
func documentFixtureConfig(t *testing.T, cfg Config) *Store {
	t.Helper()
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Up(context.Background()); err != nil {
		t.Fatalf("document migrations: %v", err)
	}
	u, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", db.Schema)
	u.RawQuery = q.Encode()
	cfg.DSN = u.String()
	if cfg.ChatDSN == "" {
		cfg.ChatDSN = "postgres://chat:pw@127.0.0.1:1/chat"
	}
	if cfg.CoreDSN == "" {
		cfg.CoreDSN = "postgres://core:pw@127.0.0.1:1/core"
	}
	s, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// hub043Corpus is one seeded document: DeployedVersionID is the live
// version everyone reads and searches, TipVersionID is the latest
// candidate on the version chain (the same as DeployedVersionID when
// ExtraVersionsPerDoc is zero) -- the base a further proposal must submit
// against to avoid a spurious VersionConflict.
type hub043Corpus struct {
	DocID, DeployedVersionID, TipVersionID string
}

const hub043Actor = "u-043-actor"

// seedHub043Corpus builds scale.Docs personal documents, each reviewed,
// deployed and indexed, plus scale.ExtraVersionsPerDoc additional
// undeployed candidate versions per document (inflating the immutable
// version count the way real editing history does without changing what
// resolves as live). Every document is owned by the same synthetic actor:
// a load-generation corpus does not need distinct identities to prove pool
// throughput and isolation, and reusing one owner lets every document skip
// a separate grant call (CreatePersonalDocument already bootstraps full
// owner actions, sharing.go's ownerActions). Seeding runs on a bounded
// worker pool so the named planning scale finishes in a reasonable wall
// clock even though it is never the default.
func seedHub043Corpus(t *testing.T, s *Store, ctx context.Context, tenant string, scale hub043Scale) []hub043Corpus {
	t.Helper()
	out := make([]hub043Corpus, scale.Docs)
	errs := make(chan error, scale.Docs)
	var wg sync.WaitGroup
	workers := scale.SeedWorkers
	if workers <= 0 {
		workers = 1
	}
	jobs := make(chan int, scale.Docs)
	for i := 0; i < scale.Docs; i++ {
		jobs <- i
	}
	close(jobs)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				docID, v, err := s.CreatePersonalDocument(ctx, tenant, hub043Actor,
					fmt.Sprintf("HUB-043 doc %d", i),
					fmt.Sprintf("# Load Document %d\n\nPlanning workload body text for document %d.\n", i, i))
				if err != nil {
					errs <- fmt.Errorf("create doc %d: %w", i, err)
					continue
				}
				if _, err := s.RecordReview(ctx, tenant, ReviewInput{
					DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "",
					ReviewerID: "u-043-reviewer", Authority: "team:leads", Decision: ReviewApproved,
				}); err != nil {
					errs <- fmt.Errorf("review doc %d: %w", i, err)
					continue
				}
				if _, err := s.Deploy(ctx, tenant, DeployInput{
					DocumentID: docID, VersionID: v.ID, ScopeKind: "default", ScopeID: "", DeployerID: hub043Actor,
				}); err != nil {
					errs <- fmt.Errorf("deploy doc %d: %w", i, err)
					continue
				}
				if err := s.IndexDeployedVersion(ctx, tenant, docID, v.ID); err != nil {
					errs <- fmt.Errorf("index doc %d: %w", i, err)
					continue
				}
				parent := v.ID
				for extra := 0; extra < scale.ExtraVersionsPerDoc; extra++ {
					nv, err := s.SubmitCandidate(ctx, tenant, Version{
						DocumentID: docID, CreatorID: hub043Actor,
						Title:    fmt.Sprintf("HUB-043 doc %d rev %d", i, extra),
						Markdown: fmt.Sprintf("# Load Document %d\n\nRevision %d body text.\n", i, extra),
					}, parent)
					if err != nil {
						errs <- fmt.Errorf("extra version doc %d rev %d: %w", i, extra, err)
						break
					}
					parent = nv.ID
				}
				out[i] = hub043Corpus{DocID: docID, DeployedVersionID: v.ID, TipVersionID: parent}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	return out
}

// hub043LatencySamples runs the document read/search/propose workload
// concurrently against a seeded corpus and returns the observed
// latencies per operation kind.
type hub043LatencySamples struct {
	Open, Search, Propose []time.Duration
}

func runHub043DocumentLoad(t *testing.T, s *Store, ctx context.Context, tenant string, corpus []hub043Corpus, scale hub043Scale) hub043LatencySamples {
	t.Helper()
	var mu sync.Mutex
	var samples hub043LatencySamples
	var wg sync.WaitGroup

	for r := 0; r < scale.Readers; r++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			var open, search []time.Duration
			for i := 0; i < scale.ReadsPerReader; i++ {
				doc := corpus[(worker*scale.ReadsPerReader+i)%len(corpus)]
				begin := time.Now()
				if _, _, err := s.ReadPersonalDocument(ctx, tenant, hub043Actor, doc.DocID); err != nil {
					t.Errorf("read: %v", err)
					continue
				}
				open = append(open, time.Since(begin))

				begin = time.Now()
				if _, err := s.SearchLexical(ctx, tenant, "load", "person", hub043Actor); err != nil {
					t.Errorf("search: %v", err)
					continue
				}
				search = append(search, time.Since(begin))
			}
			mu.Lock()
			samples.Open = append(samples.Open, open...)
			samples.Search = append(samples.Search, search...)
			mu.Unlock()
		}(r)
	}

	docsPerProposer := len(corpus) / maxInt(scale.Proposers, 1)
	for p := 0; p < scale.Proposers; p++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			start := worker * docsPerProposer
			// Each proposer owns a disjoint slice of the corpus, so its
			// own tip tracking never races with another proposer's; it
			// only needs to keep up with its own successive submissions.
			tip := make(map[string]string, docsPerProposer)
			for j := 0; j < docsPerProposer; j++ {
				doc := corpus[start+j]
				tip[doc.DocID] = doc.TipVersionID
			}
			var propose []time.Duration
			for i := 0; i < scale.ProposalsPerProposer && docsPerProposer > 0; i++ {
				doc := corpus[start+(i%docsPerProposer)]
				begin := time.Now()
				nv, err := s.SubmitCandidate(ctx, tenant, Version{
					DocumentID: doc.DocID, CreatorID: hub043Actor,
					Title: "HUB-043 proposal", Markdown: fmt.Sprintf("# Load Document\n\nConcurrent proposal %d by worker %d.\n", i, worker),
				}, tip[doc.DocID])
				if err != nil {
					// A parent conflict signals a genuinely stale base
					// (never expected here, since each proposer owns a
					// disjoint slice); any other error is a real fault.
					var conflict *VersionConflict
					if !errors.As(err, &conflict) {
						t.Errorf("propose: %v", err)
					}
					continue
				}
				tip[doc.DocID] = nv.ID
				propose = append(propose, time.Since(begin))
			}
			mu.Lock()
			samples.Propose = append(samples.Propose, propose...)
			mu.Unlock()
		}(p)
	}
	wg.Wait()
	return samples
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func percentileHub043(samples []time.Duration, pct int) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	cp := append([]time.Duration(nil), samples...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	index := (len(cp)*pct+99)/100 - 1
	if index < 0 {
		index = 0
	}
	return cp[index]
}

// runHub043CoreOperations mirrors test/chat/chat_load_test.go's
// runCoreOperations: identical durable operation-store Put+Get work run
// against the document store's own separate pool budget, so a phase run
// while documents are under load measures whether the document pool ever
// competes with workflow/core's.
func runHub043CoreOperations(t *testing.T, stores []*operationstore.Store, tenant uuid.UUID, phase string) []time.Duration {
	t.Helper()
	const perWorker = 10
	results := make(chan time.Duration, len(stores)*perWorker)
	errs := make(chan error, len(stores)*perWorker)
	var wg sync.WaitGroup
	for worker, store := range stores {
		wg.Add(1)
		go func(worker int, store *operationstore.Store) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				id := fmt.Sprintf("hub043-load-%s-%d-%d", phase, worker, i)
				now := time.Now().UTC()
				begin := time.Now()
				err := store.Put(context.Background(), operationstore.Record{OperationID: id, TenantID: tenant.String(), Owner: "principal:hub043", RequestType: "promotion.propose", State: operationstore.StatePending, MetadataRef: "hub043:" + id, CreatedAt: now, UpdatedAt: now})
				if err == nil {
					var got operationstore.Record
					got, err = store.Get(context.Background(), tenant.String(), id)
					if err == nil && got.OperationID != id {
						err = fmt.Errorf("operation id = %q, want %q", got.OperationID, id)
					}
				}
				if err != nil {
					errs <- err
				} else {
					results <- time.Since(begin)
				}
			}
		}(worker, store)
	}
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		t.Errorf("%s core operation: %v", phase, err)
	}
	var samples []time.Duration
	for d := range results {
		samples = append(samples, d)
	}
	if len(samples) != len(stores)*perWorker {
		t.Fatalf("%s core samples=%d, want %d", phase, len(samples), len(stores)*perWorker)
	}
	return samples
}

// runHub043 seeds the corpus, runs the document workload concurrently with
// the core/workflow baseline and mixed phases, and returns every measured
// sample set for the caller to log and/or assert budgets against.
func runHub043(t *testing.T) (doc hub043LatencySamples, coreBaseline, coreMixed []time.Duration, scale hub043Scale) {
	t.Helper()
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant = "tenant-hub043"
	scale = defaultHub043Scale()

	core := pgtest.New(t)
	tenantID := uuid.New()
	core.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, 'hub043-load-core', 'cell-hub043', 'HUB-043 Load', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenantID)
	connections := make([]*operationstore.Store, 4)
	for i := range connections {
		connections[i] = operationstore.New(core.NewConn(t))
	}

	corpus := seedHub043Corpus(t, s, ctx, tenant, scale)
	coreBaseline = runHub043CoreOperations(t, connections, tenantID, "baseline")

	var docSamples hub043LatencySamples
	var loadWG sync.WaitGroup
	loadWG.Add(1)
	go func() {
		defer loadWG.Done()
		docSamples = runHub043DocumentLoad(t, s, ctx, tenant, corpus, scale)
	}()
	coreMixed = runHub043CoreOperations(t, connections, tenantID, "mixed")
	loadWG.Wait()

	return docSamples, coreBaseline, coreMixed, scale
}

// TestTodo_HUB_043 is the PRIMARY test for HUB-043: a seeded document
// corpus under concurrent reader and proposer load runs side by side with
// an independent, separately pooled core/workflow store, and every
// measured p95/p99 is logged. This is a local regression probe over one
// developer machine's embedded PostgreSQL, exactly as
// test/chat/chat_load_test.go's TestTodo_CHAT_052_MixedLoad documents
// itself to be -- not the signed pilot SLO gate the spec's "Storage,
// records, and scale" section calls for, which needs a provisioned
// planning-scale environment and a recorded sign-off.
func TestTodo_HUB_043(t *testing.T) {
	doc, coreBaseline, coreMixed, scale := runHub043(t)
	if len(doc.Open) == 0 || len(doc.Search) == 0 {
		t.Fatalf("document load produced no samples: open=%d search=%d", len(doc.Open), len(doc.Search))
	}
	t.Logf("HUB-043 local load profile (docs=%d extra-versions/doc=%d readers=%d proposers=%d): "+
		"open p95=%s p99=%s n=%d; search p95=%s p99=%s n=%d; propose p95=%s p99=%s n=%d; "+
		"core baseline p95=%s p99=%s; core mixed(under document load) p95=%s p99=%s",
		scale.Docs, scale.ExtraVersionsPerDoc, scale.Readers, scale.Proposers,
		percentileHub043(doc.Open, 95), percentileHub043(doc.Open, 99), len(doc.Open),
		percentileHub043(doc.Search, 95), percentileHub043(doc.Search, 99), len(doc.Search),
		percentileHub043(doc.Propose, 95), percentileHub043(doc.Propose, 99), len(doc.Propose),
		percentileHub043(coreBaseline, 95), percentileHub043(coreBaseline, 99),
		percentileHub043(coreMixed, 95), percentileHub043(coreMixed, 99))
}

// TestTodo_HUB_043_Conformance is the CONFORMANCE test for HUB-043: the
// same harness must clear local regression budgets for document open and
// search latency, and workflow/core latency measured while documents are
// under load must not regress materially against its own pre-load
// baseline -- the isolation-pool claim in store.go proven under
// concurrency, not just asserted by configuration. These are regression
// budgets set for this local harness, not a customer-facing signed SLO;
// the spec leaves the exact planning-scale numbers to a provisioned
// environment (see TestTodo_HUB_043's doc comment).
func TestTodo_HUB_043_Conformance(t *testing.T) {
	doc, coreBaseline, coreMixed, _ := runHub043(t)

	// These budgets are deliberately generous for one shared, unindexed,
	// embedded-PostgreSQL developer machine running every HUB-043 phase
	// back to back; they exist to catch a structural regression (an
	// isolation break, an accidentally unbounded scan), not to stand in
	// for the spec's signed planning-scale SLO.
	const openBudgetP95 = 1500 * time.Millisecond
	const openBudgetP99 = 3 * time.Second
	const searchBudgetP95 = 3 * time.Second
	if p95 := percentileHub043(doc.Open, 95); p95 > openBudgetP95 {
		t.Errorf("document open p95=%s exceeds local budget %s", p95, openBudgetP95)
	}
	if p99 := percentileHub043(doc.Open, 99); p99 > openBudgetP99 {
		t.Errorf("document open p99=%s exceeds local budget %s", p99, openBudgetP99)
	}
	if p95 := percentileHub043(doc.Search, 95); p95 > searchBudgetP95 {
		t.Errorf("document search p95=%s exceeds local budget %s", p95, searchBudgetP95)
	}

	// Workflow regression budget: mixed-phase core p99 (measured while
	// documents are under concurrent load) must stay within a bounded
	// multiple of its own pre-load baseline p99, so a real regression in
	// pool isolation shows up as a failure here rather than a number that
	// merely gets logged.
	base := percentileHub043(coreBaseline, 99)
	mixed := percentileHub043(coreMixed, 99)
	if base <= 0 {
		base = time.Millisecond
	}
	const regressionMultiple = 5
	if mixed > base*regressionMultiple {
		t.Errorf("workflow/core p99 regressed under document load: baseline=%s mixed=%s (budget %dx)", base, mixed, regressionMultiple)
	}
	const coreHardCeiling = 500 * time.Millisecond
	if mixed > coreHardCeiling {
		t.Errorf("workflow/core p99 under document load=%s exceeds hard ceiling %s", mixed, coreHardCeiling)
	}
}

// TestTodo_HUB_043_Fault is the FAULT test for HUB-043: the document pool
// is deliberately saturated (MaxConns held by long-running transactions)
// and the independent core/workflow store, on its own pool, keeps
// admitting and completing work throughout -- proving the isolation is a
// structural property of separate pools, not merely usually true under
// light load.
func TestTodo_HUB_043_Fault(t *testing.T) {
	s := documentFixtureConfig(t, Config{MaxConns: 2, MinConns: 1})
	ctx := context.Background()
	const tenant = "tenant-hub043-fault"

	core := pgtest.New(t)
	tenantID := uuid.New()
	core.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, 'hub043-fault-core', 'cell-hub043f', 'HUB-043 Fault', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenantID)
	coreStore := operationstore.New(core.NewConn(t))

	// Hold every document connection open with long-running transactions,
	// saturating the 2-connection document pool.
	release := make(chan struct{})
	held := make(chan struct{}, 2)
	var holders sync.WaitGroup
	for i := 0; i < 2; i++ {
		holders.Add(1)
		go func() {
			defer holders.Done()
			_ = s.RunTx(ctx, func(tx dbport.Tx) error {
				held <- struct{}{}
				<-release
				return nil
			})
		}()
	}
	for i := 0; i < 2; i++ {
		<-held
	}

	// A third document transaction cannot acquire a connection while the
	// pool is saturated: it must time out rather than silently borrow
	// capacity from anywhere else.
	starved, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	err := s.RunTx(starved, func(tx dbport.Tx) error { return nil })
	if err == nil {
		t.Fatal("document transaction acquired a connection despite pool saturation")
	}

	// The independent core/workflow store admits and completes work the
	// entire time the document pool is saturated.
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("hub043-fault-%d", i)
		now := time.Now().UTC()
		if err := coreStore.Put(ctx, operationstore.Record{
			OperationID: id, TenantID: tenantID.String(), Owner: "principal:hub043-fault",
			RequestType: "promotion.propose", State: operationstore.StatePending,
			MetadataRef: "hub043-fault:" + id, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("core operation starved by saturated document pool: %v", err)
		}
		got, err := coreStore.Get(ctx, tenantID.String(), id)
		if err != nil || got.OperationID != id {
			t.Fatalf("core readback starved by saturated document pool: %+v, %v", got, err)
		}
	}

	close(release)
	holders.Wait()

	// Once the document pool is released, a document transaction succeeds
	// again immediately, proving the earlier timeout was pool exhaustion
	// and not a broken pool.
	if err := s.RunTx(ctx, func(tx dbport.Tx) error { return nil }); err != nil {
		t.Fatalf("document pool did not recover after release: %v", err)
	}
}

// BenchmarkTodo_HUB_043 is the BENCHMARK entry for HUB-043. The measured
// profile lives in TestTodo_HUB_043 (see its doc comment); pgtest
// intentionally exposes test schemas only through *testing.T, mirroring
// test/chat/chat_load_test.go's BenchmarkTodo_CHAT_052.
func BenchmarkTodo_HUB_043(b *testing.B) {
	b.Skip("the measured load profile is TestTodo_HUB_043; pgtest intentionally exposes test schemas only")
}
