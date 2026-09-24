package documentembed

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

func indexStore(t *testing.T) *documenthubstore.Store {
	t.Helper()
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(documenthubstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", db.Schema)
	u.RawQuery = q.Encode()
	s, err := documenthubstore.New(context.Background(), documenthubstore.Config{DSN: u.String()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// countingEmbedder is a local model that records batch sizes.
type countingEmbedder struct {
	mu      sync.Mutex
	batches []int
}

func (e *countingEmbedder) Model() string { return "counting" }
func (e *countingEmbedder) Local() bool   { return true }
func (e *countingEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.batches = append(e.batches, len(texts))
	e.mu.Unlock()
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = []float32{float32(len(text)), 1}
		if strings.Contains(text, "payroll") {
			out[i] = []float32{1, 0}
		}
	}
	return out, nil
}

func seedDocs(t *testing.T, s *documenthubstore.Store, tenant, owner string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		body := fmt.Sprintf("# Doc %d\n\n## One\n\nText one.\n\n## Two\n\nText two.\n\n## Three\n\nMore payroll text.\n", i)
		if _, _, err := s.CreatePersonalDocument(context.Background(), tenant, owner, fmt.Sprintf("Doc %d", i), body); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIndexerWorkersDrainQueue_Integration(t *testing.T) {
	s := indexStore(t)
	emb := &countingEmbedder{}
	s.SetIndexModel(emb.Model())
	const tenant, owner = "tenant-indexer", "owner-i"
	seedDocs(t, s, tenant, owner, 20)
	ctx := context.Background()
	if n, err := s.SemanticPending(ctx, tenant, owner, emb.Model()); err != nil || n != 20 {
		t.Fatalf("pending before = %d, %v", n, err)
	}
	ix := NewIndexer(s, emb, IndexerConfig{Workers: 2, Batch: 3, Interval: 20 * time.Millisecond})
	runCtx, stop := context.WithCancel(ctx)
	ix.Start(runCtx)
	ix.Notify(tenant)
	ix.Notify("")
	var nilIndexer *Indexer
	nilIndexer.Notify(tenant)
	deadline := time.Now().Add(30 * time.Second)
	var last int
	for {
		n, err := s.SemanticPending(ctx, tenant, owner, emb.Model())
		if err != nil {
			t.Fatal(err)
		}
		if n > last && last != 0 {
			t.Fatalf("pending went up: %d -> %d", last, n)
		}
		last = n
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("queue did not drain: %d pending", n)
		}
		time.Sleep(20 * time.Millisecond)
	}
	stop()
	ix.Wait()
	stats, err := s.IndexQueueStats(ctx, tenant, emb.Model())
	if err != nil || stats.Done != 20 || stats.Queued+stats.Running+stats.Failed != 0 {
		t.Fatalf("stats = %+v, %v", stats, err)
	}
	emb.mu.Lock()
	defer emb.mu.Unlock()
	for _, b := range emb.batches {
		if b > 3 {
			t.Fatalf("batch of %d sections exceeds 3", b)
		}
	}
	res, err := s.SearchPersonalDocuments(ctx, tenant, owner, documenthubstore.ListOptions{Query: "wages", Mode: documenthubstore.SearchMeaning, QueryVector: []float32{1, 0}, VectorModel: emb.Model(), Limit: 50})
	if err != nil || res.Total != 20 {
		t.Fatalf("meaning after drain = %d, %v", res.Total, err)
	}
}

func TestIndexerDrainReportsProgress_Integration(t *testing.T) {
	s := indexStore(t)
	emb := &countingEmbedder{}
	const tenant, owner = "tenant-drain", "owner-d"
	seedDocs(t, s, tenant, owner, 9)
	if n, err := s.EnqueueMissingIndexJobs(context.Background(), tenant, emb.Model()); err != nil || n != 9 {
		t.Fatalf("backfill = %d, %v", n, err)
	}
	ix := NewIndexer(s, emb, IndexerConfig{ClaimSize: 2})
	var reports []DrainProgress
	final, err := ix.Drain(context.Background(), tenant, func(p DrainProgress) { reports = append(reports, p) })
	if err != nil || final.Done != 9 || final.Processed != 9 || final.Queued != 0 {
		t.Fatalf("drain = %+v, %v", final, err)
	}
	if len(reports) != 5 || reports[0].Done != 2 || reports[0].Queued != 7 {
		t.Fatalf("progress = %+v", reports)
	}
	again, err := ix.Drain(context.Background(), tenant, nil)
	if err != nil || again.Processed != 0 {
		t.Fatalf("second drain = %+v, %v", again, err)
	}
}

func TestIndexerConfigBackoffAndThrottle(t *testing.T) {
	cfg, err := IndexerConfigFromEnv(func(string) string { return "" })
	if err != nil || cfg != DefaultIndexerConfig() {
		t.Fatalf("defaults = %+v, %v", cfg, err)
	}
	env := map[string]string{EnvWorkers: "3", EnvBatch: "16", EnvInterval: "500ms", EnvMaxPerMinute: "120"}
	cfg, err = IndexerConfigFromEnv(func(k string) string { return env[k] })
	if err != nil || cfg.Workers != 3 || cfg.Batch != 16 || cfg.Interval != 500*time.Millisecond || cfg.MaxPerMinute != 120 {
		t.Fatalf("env config = %+v, %v", cfg, err)
	}
	for k, v := range map[string]string{EnvWorkers: "0", EnvBatch: "x", EnvInterval: "-1s", EnvMaxPerMinute: "-2"} {
		if _, err := IndexerConfigFromEnv(func(key string) string {
			if key == k {
				return v
			}
			return ""
		}); err == nil {
			t.Fatalf("%s=%s accepted", k, v)
		}
	}
	for attempts, want := range map[int]time.Duration{1: 5 * time.Second, 2: 10 * time.Second, 4: 40 * time.Second, 30: 10 * time.Minute} {
		for i := 0; i < 20; i++ {
			if got := Backoff(attempts); got < want*8/10 || got > want*12/10 {
				t.Fatalf("Backoff(%d) = %v, want about %v", attempts, got, want)
			}
		}
	}
	// 120 sections a minute is one every 500ms: the third batch of four
	// waits for the first two batches' allowance.
	clock := time.Unix(0, 0)
	var slept []time.Duration
	ix := NewIndexer(nil, &countingEmbedder{}, IndexerConfig{MaxPerMinute: 120, Batch: 4})
	ix.now = func() time.Time { return clock }
	ix.sleep = func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }
	if _, err := ix.embedBatched(context.Background(), make([]string, 10)); err != nil {
		t.Fatal(err)
	}
	if len(slept) != 2 || slept[0] != 2*time.Second || slept[1] != 4*time.Second {
		t.Fatalf("throttle waits = %v", slept)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepContext(ctx, time.Hour); err == nil {
		t.Fatal("canceled sleep returned nil")
	}
	if err := sleepContext(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if m := StoreModel(&countingEmbedder{}); m.ID != "counting" || m.External {
		t.Fatalf("store model = %+v", m)
	}
}
