package documentembed

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	mrand "math/rand/v2"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

// Environment variables that tune the background indexer for the hardware
// it runs on.
const (
	EnvWorkers      = "HCMNEXT_EMBEDDING_WORKERS"
	EnvBatch        = "HCMNEXT_EMBEDDING_BATCH"
	EnvInterval     = "HCMNEXT_EMBEDDING_INTERVAL"
	EnvMaxPerMinute = "HCMNEXT_EMBEDDING_MAX_PER_MINUTE"
)

// IndexerConfig tunes the indexer. Batch is sections per embedding call;
// MaxPerMinute caps sections embedded per minute across all workers (zero
// is unlimited).
type IndexerConfig struct {
	Workers, Batch, MaxPerMinute int
	Interval, Lease              time.Duration
	MaxAttempts                  int
	// ClaimSize is how many jobs one worker leases per poll.
	ClaimSize int
}

// DefaultIndexerConfig suits a small single-board computer: one worker,
// eight sections per call, a two-second idle poll.
func DefaultIndexerConfig() IndexerConfig {
	return IndexerConfig{Workers: 1, Batch: 8, Interval: 2 * time.Second, Lease: 2 * time.Minute, MaxAttempts: 5, ClaimSize: 4}
}

// IndexerConfigFromEnv reads the tuning variables over the defaults.
func IndexerConfigFromEnv(getenv func(string) string) (IndexerConfig, error) {
	cfg := DefaultIndexerConfig()
	ints := []struct {
		name string
		into *int
		min  int
	}{{EnvWorkers, &cfg.Workers, 1}, {EnvBatch, &cfg.Batch, 1}, {EnvMaxPerMinute, &cfg.MaxPerMinute, 0}}
	for _, f := range ints {
		raw := strings.TrimSpace(getenv(f.name))
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < f.min {
			return IndexerConfig{}, fmt.Errorf("document embedding: %s must be an integer of at least %d", f.name, f.min)
		}
		*f.into = n
	}
	if raw := strings.TrimSpace(getenv(EnvInterval)); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return IndexerConfig{}, fmt.Errorf("document embedding: %s must be a positive duration such as 2s", EnvInterval)
		}
		cfg.Interval = d
	}
	return cfg, nil
}

// Indexer is the background worker pool that drains the document index
// queue: it leases due jobs per tenant, embeds their sections in batches
// with the configured model and records each outcome. Requests never
// embed; they enqueue in their own transaction and Notify the indexer.
type Indexer struct {
	store    *documenthubstore.Store
	embedder Embedder
	model    documenthubstore.EmbeddingModel
	cfg      IndexerConfig
	owner    string
	wake     chan struct{}
	now      func() time.Time
	sleep    func(context.Context, time.Duration) error

	mu      sync.Mutex
	tenants map[string]bool
	next    time.Time // throttle: when the next section may be embedded
	wg      sync.WaitGroup
}

// StoreModel describes an embedder to the store's egress policy: anything
// not in process or on loopback is external and never sees drafts.
func StoreModel(e Embedder) documenthubstore.EmbeddingModel {
	return documenthubstore.EmbeddingModel{ID: e.Model(), Version: "1", External: !IsLocal(e)}
}

// NewIndexer builds an indexer for store and embedder; tenants are polled
// from the start, and Notify adds more.
func NewIndexer(store *documenthubstore.Store, embedder Embedder, cfg IndexerConfig, tenants ...string) *Indexer {
	def := DefaultIndexerConfig()
	if cfg.Workers <= 0 {
		cfg.Workers = def.Workers
	}
	if cfg.Batch <= 0 {
		cfg.Batch = def.Batch
	}
	if cfg.Interval <= 0 {
		cfg.Interval = def.Interval
	}
	if cfg.Lease <= 0 {
		cfg.Lease = def.Lease
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = def.MaxAttempts
	}
	if cfg.ClaimSize <= 0 {
		cfg.ClaimSize = def.ClaimSize
	}
	var suffix [4]byte
	_, _ = rand.Read(suffix[:])
	host, _ := os.Hostname()
	ix := &Indexer{
		store: store, embedder: embedder, model: StoreModel(embedder), cfg: cfg,
		owner: fmt.Sprintf("%s-%d-%s", host, os.Getpid(), hex.EncodeToString(suffix[:])),
		wake:  make(chan struct{}, 1), tenants: map[string]bool{}, now: time.Now, sleep: sleepContext,
	}
	for _, t := range tenants {
		if strings.TrimSpace(t) != "" {
			ix.tenants[t] = true
		}
	}
	return ix
}

// Notify records that tenant has queued work and wakes an idle worker. It
// never blocks and is safe on a nil indexer.
func (ix *Indexer) Notify(tenant string) {
	if ix == nil || strings.TrimSpace(tenant) == "" {
		return
	}
	ix.mu.Lock()
	ix.tenants[tenant] = true
	ix.mu.Unlock()
	select {
	case ix.wake <- struct{}{}:
	default:
	}
}

// Start runs the workers until ctx ends; Wait blocks until they stop.
func (ix *Indexer) Start(ctx context.Context) {
	for i := 0; i < ix.cfg.Workers; i++ {
		ix.wg.Add(1)
		go func() {
			defer ix.wg.Done()
			ix.loop(ctx)
		}()
	}
}

// Wait blocks until every worker started by Start has returned.
func (ix *Indexer) Wait() { ix.wg.Wait() }

func (ix *Indexer) loop(ctx context.Context) {
	for ctx.Err() == nil {
		worked, err := ix.pass(ctx)
		if err != nil && ctx.Err() == nil {
			worked = false
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ix.wake:
		case <-time.After(ix.cfg.Interval):
		}
	}
}

func (ix *Indexer) tenantList() []string {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	out := make([]string, 0, len(ix.tenants))
	for t := range ix.tenants {
		out = append(out, t)
	}
	return out
}

// pass claims and processes one lease of jobs per tenant and reports
// whether it found any work.
func (ix *Indexer) pass(ctx context.Context) (bool, error) {
	worked := false
	for _, tenant := range ix.tenantList() {
		n, err := ix.drainOnce(ctx, tenant)
		if err != nil {
			return worked, err
		}
		worked = worked || n > 0
	}
	return worked, nil
}

// drainOnce leases up to ClaimSize due jobs for one tenant and runs them.
func (ix *Indexer) drainOnce(ctx context.Context, tenant string) (int, error) {
	jobs, err := ix.store.ClaimIndexJobs(ctx, tenant, ix.model.ID, ix.owner, ix.cfg.ClaimSize, ix.cfg.Lease)
	if err != nil {
		return 0, err
	}
	retry := documenthubstore.IndexRetry{MaxAttempts: ix.cfg.MaxAttempts, Backoff: Backoff}
	for _, job := range jobs {
		if _, err := ix.store.RunIndexJob(ctx, job, ix.owner, ix.model, ix.embedBatched, retry); err != nil {
			return len(jobs), err
		}
	}
	return len(jobs), nil
}

// Backoff waits 5s, 10s, 20s ... up to ten minutes, with 20% jitter either
// way so failed jobs do not retry in lockstep.
func Backoff(attempts int) time.Duration {
	base := 5 * time.Second * time.Duration(math.Pow(2, float64(min(max(0, attempts-1), 10))))
	base = min(base, 10*time.Minute)
	jitter := 0.8 + 0.4*mrand.Float64()
	return time.Duration(float64(base) * jitter)
}

// embedBatched embeds texts Batch sections at a time, pausing between
// batches to honor MaxPerMinute.
func (ix *Indexer) embedBatched(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += ix.cfg.Batch {
		batch := texts[start:min(len(texts), start+ix.cfg.Batch)]
		if err := ix.throttle(ctx, len(batch)); err != nil {
			return nil, err
		}
		vecs, err := ix.embedder.Embed(ctx, batch)
		if err != nil {
			return nil, err
		}
		if len(vecs) != len(batch) {
			return nil, errors.New("document embedding: the model returned the wrong number of vectors")
		}
		out = append(out, vecs...)
	}
	return out, nil
}

func (ix *Indexer) throttle(ctx context.Context, sections int) error {
	if ix.cfg.MaxPerMinute <= 0 {
		return nil
	}
	per := time.Minute / time.Duration(ix.cfg.MaxPerMinute)
	ix.mu.Lock()
	now := ix.now()
	start := now
	if ix.next.After(now) {
		start = ix.next
	}
	ix.next = start.Add(per * time.Duration(sections))
	ix.mu.Unlock()
	if wait := start.Sub(now); wait > 0 {
		return ix.sleep(ctx, wait)
	}
	return nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// DrainProgress is one progress report from Drain.
type DrainProgress struct {
	documenthubstore.IndexQueueStats
	Processed int
	Elapsed   time.Duration
}

// Drain runs the worker loop in process for one tenant until nothing is
// queued or running, reporting progress after every lease. Jobs waiting
// out a backoff are waited for, so Drain ends only when every job is done,
// skipped or failed.
func (ix *Indexer) Drain(ctx context.Context, tenant string, progress func(DrainProgress)) (DrainProgress, error) {
	started := ix.now()
	var last DrainProgress
	for {
		n, err := ix.drainOnce(ctx, tenant)
		if err != nil {
			return last, err
		}
		stats, err := ix.store.IndexQueueStats(ctx, tenant, ix.model.ID)
		if err != nil {
			return last, err
		}
		last = DrainProgress{IndexQueueStats: stats, Processed: last.Processed + n, Elapsed: ix.now().Sub(started)}
		if progress != nil && n > 0 {
			progress(last)
		}
		if stats.Queued+stats.Running == 0 {
			return last, nil
		}
		if n == 0 {
			if err := ix.sleep(ctx, min(ix.cfg.Interval, time.Second)); err != nil {
				return last, err
			}
		}
	}
}
