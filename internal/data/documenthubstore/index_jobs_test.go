package documenthubstore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func jobStatus(t *testing.T, s *Store, tenant, versionID string) (string, int, string) {
	t.Helper()
	var status, lastError string
	var attempts int
	err := s.RunTenantTx(context.Background(), tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT status, attempts, last_error FROM document_index_job WHERE tenant_id=$1 AND version_id=$2`, tenant, versionID).Scan(&status, &attempts, &lastError)
	})
	if errors.Is(err, dbport.ErrNoRows) {
		return "", 0, ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return status, attempts, lastError
}

func claimAll(t *testing.T, s *Store, tenant, model, owner string) []IndexJob {
	t.Helper()
	jobs, err := s.ClaimIndexJobs(context.Background(), tenant, model, owner, 50, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return jobs
}

var noRetry = IndexRetry{MaxAttempts: 3, Backoff: func(int) time.Duration { return time.Hour }}

func TestDocumentIndexQueue_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner, reader = "tenant-queue", "owner-q", "reader-q"
	local := EmbeddingModel{ID: "queue-local", Version: "1"}

	// No model configured: writes enqueue nothing.
	quiet, quietV, err := s.CreatePersonalDocument(ctx, tenant, owner, "Before indexing", "# Before indexing\n\nText.\n")
	if err != nil {
		t.Fatal(err)
	}
	if st, _, _ := jobStatus(t, s, tenant, quietV.ID); st != "" {
		t.Fatalf("job without a model: %q", st)
	}
	s.SetIndexModel(local.ID)
	if s.IndexModel() != local.ID {
		t.Fatal("index model not kept")
	}

	// Enqueue is part of the write's transaction.
	payroll, payrollV, err := s.CreatePersonalDocument(ctx, tenant, owner, "Payroll close runbook", "# Payroll close runbook\n\n## Steps\n\nClose the payroll.\n")
	if err != nil {
		t.Fatal(err)
	}
	if st, _, _ := jobStatus(t, s, tenant, payrollV.ID); st != IndexQueued {
		t.Fatalf("create job = %q", st)
	}
	rollback := errors.New("roll back")
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if err := s.enqueueIndexTx(ctx, tx, tenant, quiet, quietV.ID); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if st, _, _ := jobStatus(t, s, tenant, quietV.ID); st != "" {
		t.Fatalf("rolled-back write left a job: %q", st)
	}
	if _, err := s.CreatePersonalDocumentVersion(ctx, tenant, payroll, owner, "docv-stale", "Payroll", "# Payroll\n"); err == nil {
		t.Fatal("stale save accepted")
	}
	stats, err := s.IndexQueueStats(ctx, tenant, local.ID)
	if err != nil || stats.Queued != 1 {
		t.Fatalf("failed save enqueued: %+v, %v", stats, err)
	}

	// Pending counts what the reader can see and is not indexed yet.
	if err := s.SharePersonalDocumentRole(ctx, tenant, payroll, owner, reader, RoleViewer); err != nil {
		t.Fatal(err)
	}
	if n, err := s.SemanticPending(ctx, tenant, reader, local.ID); err != nil || n != 1 {
		t.Fatalf("reader pending = %d, %v", n, err)
	}
	if n, err := s.SemanticPending(ctx, tenant, owner, local.ID); err != nil || n != 2 {
		t.Fatalf("owner pending = %d, %v", n, err)
	}

	// A superseded draft is skipped; the newer one is embedded.
	draft, draftV, err := s.CreatePersonalDocument(ctx, tenant, owner, "Handbook", "# Handbook\n\nFirst draft.\n")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := s.CreatePersonalDocumentVersion(ctx, tenant, draft, owner, draftV.ID, "Handbook", "# Handbook\n\nSecond draft about payroll.\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range claimAll(t, s, tenant, local.ID, "worker-a") {
		state, err := s.RunIndexJob(ctx, job, "worker-a", local, payrollEmbed, noRetry)
		if err != nil {
			t.Fatal(err)
		}
		want := IndexDone
		if job.VersionID == draftV.ID {
			want = IndexSkipped
		}
		if state != want {
			t.Fatalf("job %s = %s, want %s", job.VersionID, state, want)
		}
	}
	if st, _, reason := jobStatus(t, s, tenant, draftV.ID); st != IndexSkipped || reason == "" {
		t.Fatalf("superseded job = %s %q", st, reason)
	}
	if st, _, _ := jobStatus(t, s, tenant, newer.ID); st != IndexDone {
		t.Fatalf("newer draft = %s", st)
	}
	if n, err := s.SemanticPending(ctx, tenant, owner, local.ID); err != nil || n != 1 {
		t.Fatalf("owner pending after drain = %d, %v (the pre-model document has no job yet)", n, err)
	}
	if n, err := s.EnqueueMissingIndexJobs(ctx, tenant, local.ID); err != nil || n != 1 {
		t.Fatalf("backfill = %d, %v", n, err)
	}
	for _, job := range claimAll(t, s, tenant, local.ID, "worker-a") {
		if _, err := s.RunIndexJob(ctx, job, "worker-a", local, payrollEmbed, noRetry); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := s.SemanticPending(ctx, tenant, owner, local.ID); n != 0 {
		t.Fatalf("owner pending after backfill = %d", n)
	}
	if n, err := s.EnqueueMissingIndexJobs(ctx, tenant, local.ID); err != nil || n != 0 {
		t.Fatalf("second backfill = %d, %v", n, err)
	}

	// Revoking the reader removes the vectors from their search at once.
	vectorOpts := ListOptions{Query: "wages", Mode: SearchMeaning, QueryVector: []float32{1, 0}, VectorModel: local.ID}
	res, err := s.SearchPersonalDocuments(ctx, tenant, reader, vectorOpts)
	if err != nil || res.Total != 1 || res.Rows[0].ID != payroll {
		t.Fatalf("reader meaning before revoke = %+v, %v", res, err)
	}
	if err := s.RevokePersonAccess(ctx, tenant, owner, payroll, reader); err != nil {
		t.Fatal(err)
	}
	if res, err := s.SearchPersonalDocuments(ctx, tenant, reader, vectorOpts); err != nil || res.Total != 0 {
		t.Fatalf("revoked reader meaning = %+v, %v", res, err)
	}
}

func TestDocumentIndexQueueRetryLeaseAndEgress_Integration(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner, reader = "tenant-retry", "owner-r", "reader-r"
	local := EmbeddingModel{ID: "retry-local", Version: "1"}
	s.SetIndexModel(local.ID)
	_, v, err := s.CreatePersonalDocument(ctx, tenant, owner, "Flaky", "# Flaky\n\nText.\n")
	if err != nil {
		t.Fatal(err)
	}
	failing := func(context.Context, []string) ([][]float32, error) { return nil, errors.New("model offline") }
	retry := IndexRetry{MaxAttempts: 2, Backoff: func(attempts int) time.Duration { return time.Duration(attempts) * time.Hour }}
	jobs := claimAll(t, s, tenant, local.ID, "w1")
	if len(jobs) != 1 || jobs[0].Attempts != 1 {
		t.Fatalf("claim = %+v", jobs)
	}
	if state, err := s.RunIndexJob(ctx, jobs[0], "w1", local, failing, retry); err != nil || state != IndexQueued {
		t.Fatalf("first failure = %s, %v", state, err)
	}
	if again := claimAll(t, s, tenant, local.ID, "w1"); len(again) != 0 {
		t.Fatalf("claimed during backoff: %+v", again)
	}
	if st, attempts, reason := jobStatus(t, s, tenant, v.ID); st != IndexQueued || attempts != 1 || reason != "model offline" {
		t.Fatalf("after first failure = %s %d %q", st, attempts, reason)
	}
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE document_index_job SET next_attempt_at=now() WHERE tenant_id=$1`, tenant)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	jobs = claimAll(t, s, tenant, local.ID, "w1")
	if state, err := s.RunIndexJob(ctx, jobs[0], "w1", local, failing, retry); err != nil || state != IndexFailed {
		t.Fatalf("last failure = %s, %v", state, err)
	}
	if st, attempts, reason := jobStatus(t, s, tenant, v.ID); st != IndexFailed || attempts != 2 || reason != "model offline" {
		t.Fatalf("failed job = %s %d %q", st, attempts, reason)
	}
	if n, _ := s.SemanticPending(ctx, tenant, owner, local.ID); n != 0 {
		t.Fatalf("failed job still pending: %d", n)
	}

	// A lease that runs out is reclaimed; the first worker's result is lost.
	_, v2, err := s.CreatePersonalDocument(ctx, tenant, owner, "Leased", "# Leased\n\nText.\n")
	if err != nil {
		t.Fatal(err)
	}
	first := claimAll(t, s, tenant, local.ID, "crashed")
	if len(first) != 1 || first[0].VersionID != v2.ID {
		t.Fatalf("lease claim = %+v", first)
	}
	if held := claimAll(t, s, tenant, local.ID, "other"); len(held) != 0 {
		t.Fatalf("claimed a live lease: %+v", held)
	}
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE document_index_job SET lease_until=now()-interval '1 second' WHERE tenant_id=$1 AND version_id=$2`, tenant, v2.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	reclaimed := claimAll(t, s, tenant, local.ID, "other")
	if len(reclaimed) != 1 || reclaimed[0].Attempts != 2 {
		t.Fatalf("reclaim = %+v", reclaimed)
	}
	if state, err := s.RunIndexJob(ctx, first[0], "crashed", local, payrollEmbed, retry); err != nil || state != IndexLost {
		t.Fatalf("stale worker = %s, %v", state, err)
	}
	if state, err := s.RunIndexJob(ctx, reclaimed[0], "other", local, payrollEmbed, retry); err != nil || state != IndexDone {
		t.Fatalf("reclaiming worker = %s, %v", state, err)
	}

	// A remote model skips drafts; publishing requeues the skipped job.
	remote := EmbeddingModel{ID: "retry-remote", Version: "1", External: true}
	s.SetIndexModel(remote.ID)
	doc, v3, err := s.CreatePersonalDocument(ctx, tenant, owner, "Remote", "# Remote\n\nText.\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range claimAll(t, s, tenant, remote.ID, "w1") {
		if state, err := s.RunIndexJob(ctx, job, "w1", remote, payrollEmbed, retry); err != nil || state != IndexSkipped {
			t.Fatalf("remote draft = %s, %v", state, err)
		}
	}
	if err := s.SharePersonalDocumentRole(ctx, tenant, doc, owner, reader, RoleViewer); err != nil {
		t.Fatal(err)
	}
	if st, attempts, _ := jobStatus(t, s, tenant, v3.ID); st != IndexQueued || attempts != 0 {
		t.Fatalf("publish did not requeue: %s %d", st, attempts)
	}
	for _, job := range claimAll(t, s, tenant, remote.ID, "w1") {
		if state, err := s.RunIndexJob(ctx, job, "w1", remote, payrollEmbed, retry); err != nil || state != IndexDone {
			t.Fatalf("published remote = %s, %v", state, err)
		}
	}
	if _, err := s.RunIndexJob(ctx, IndexJob{ModelID: "other"}, "w1", remote, payrollEmbed, retry); !errors.Is(err, ErrEmbeddingModel) {
		t.Fatalf("model mismatch = %v", err)
	}
	if _, err := s.ClaimIndexJobs(ctx, tenant, remote.ID, "", 1, time.Minute); err == nil {
		t.Fatal("anonymous claim accepted")
	}
	if _, err := s.EnqueueMissingIndexJobs(ctx, tenant, ""); !errors.Is(err, ErrEmbeddingModel) {
		t.Fatalf("backfill without a model = %v", err)
	}
}

func TestDocumentIndexQueueConcurrentClaims_Race(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	const tenant, owner = "tenant-race", "owner-race"
	s.SetIndexModel("race-model")
	for i := 0; i < 24; i++ {
		if _, _, err := s.CreatePersonalDocument(ctx, tenant, owner, fmt.Sprintf("Doc %d", i), "# Doc\n\nText.\n"); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	claimedBy := map[int64]string{}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, worker := range []string{"w-a", "w-b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				jobs, err := s.ClaimIndexJobs(ctx, tenant, "race-model", worker, 3, time.Minute)
				if err != nil {
					errs <- err
					return
				}
				if len(jobs) == 0 {
					return
				}
				mu.Lock()
				for _, j := range jobs {
					if prev, dup := claimedBy[j.ID]; dup {
						mu.Unlock()
						errs <- fmt.Errorf("job %d claimed by %s and %s", j.ID, prev, worker)
						return
					}
					claimedBy[j.ID] = worker
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if len(claimedBy) != 24 {
		t.Fatalf("claimed %d of 24", len(claimedBy))
	}
}
