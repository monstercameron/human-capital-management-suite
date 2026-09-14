package object

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/envelope"
)

func mustSealedStore(t *testing.T, manager *envelope.Manager) *SealedObjectStore {
	t.Helper()
	store, err := NewSealedObjectStore(manager)
	if err != nil {
		t.Fatalf("NewSealedObjectStore: %v", err)
	}
	return store
}

func mustCompleteParts(t *testing.T, manager *MultipartManager, upload MultipartUpload, principal string) []byte {
	t.Helper()
	grant := artifactGrant()
	grant.Principal = principal
	completed, content, err := manager.Complete(context.Background(), MultipartCompleteRequest{
		UploadID: upload.UploadID, TenantID: "tenant-a", Grant: grant,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	_ = completed
	return content
}

func mustBindCompletion(t *testing.T, log *RevisionLog, ctx context.Context, cctx custody.Context, manager *MultipartManager, upload MultipartUpload, objectID, media, providerVID string, expected uint64) ArtifactRevision {
	t.Helper()
	content := mustCompleteParts(t, manager, upload, "uploader-a")
	revision, err := log.BindCompletion(ctx, cctx, content, objectID, media, providerVID, expected)
	if err != nil {
		t.Fatalf("BindCompletion: %v", err)
	}
	return revision
}

func mustApplyHold(t *testing.T, log *RevisionLog, objectID string, generation uint64, hold bool) ArtifactRevision {
	t.Helper()
	revision, err := log.ApplyHold(objectID, generation, hold)
	if err != nil {
		t.Fatalf("ApplyHold: %v", err)
	}
	return revision
}

// TestTodo_ARTIFACT_007_Property: completion checksum and generation
// binding hold across media types and sizes.
func TestTodo_ARTIFACT_007_Property(t *testing.T) {
	m, ctxA, _, _ := sealedFixture(t)
	ctx := context.Background()
	log := NewRevisionLog(mustSealedStore(t, m))
	manager := NewMultipartManager(nil)
	for i, media := range []string{"application/pdf", "image/png", "text/csv"} {
		upload := startEvidenceUpload(t, manager, "uploader-a", fmt.Sprintf("prop-object-%d", i))
		putEvidenceParts(t, manager, upload, "uploader-a")
		content := mustCompleteParts(t, manager, upload, "uploader-a")
		revision, err := log.BindCompletion(ctx, ctxA, content, "prop-object", media, "provider-vid-p", uint64(i))
		if err != nil {
			t.Fatalf("bind %d: %v", i, err)
		}
		if revision.Checksum != MultipartChecksum(content) || revision.MediaType != media {
			t.Fatalf("revision %d: %+v", i, revision)
		}
		if err := log.VerifyRevision(ctx, ctxA, "prop-object", revision.Generation); err != nil {
			t.Fatalf("verify %d: %v", i, err)
		}
	}
}

// TestTodo_ARTIFACT_007_Race: concurrent completions bind exactly one
// revision per generation.
func TestTodo_ARTIFACT_007_Race(t *testing.T) {
	m, ctxA, _, _ := sealedFixture(t)
	ctx := context.Background()
	log := NewRevisionLog(mustSealedStore(t, m))
	manager := NewMultipartManager(nil)
	const racers = 8
	var wg sync.WaitGroup
	errs := make([]error, racers)
	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Distinct uploads race to bind the same key: exactly one
			// wins the conditional generation, the rest conflict.
			upload, err := manager.Start(context.Background(), MultipartRequest{
				TenantID: "tenant-a", ExpectedParts: 1, MaxBytes: 1 << 20,
				Grant: artifactGrant(), IdempotencyKey: fmt.Sprintf("race-key-%d", i),
			})
			if err != nil {
				errs[i] = err
				return
			}
			grant := artifactGrant()
			if _, err := manager.PutPart(context.Background(), MultipartPartRequest{
				UploadID: upload.UploadID, TenantID: "tenant-a",
				Grant: grant, PartNumber: 1, Content: []byte("race-bytes"),
			}); err != nil {
				errs[i] = err
				return
			}
			_, content, err := manager.Complete(context.Background(), MultipartCompleteRequest{
				UploadID: upload.UploadID, TenantID: "tenant-a", Grant: grant,
			})
			if err != nil {
				errs[i] = err
				return
			}
			_, errs[i] = log.BindCompletion(ctx, ctxA, content, "race-object", "application/pdf", "provider-vid-r", 0)
		}(i)
	}
	wg.Wait()
	bound := 0
	for i := range racers {
		if errs[i] == nil {
			bound++
		}
	}
	if bound != 1 {
		t.Fatalf("bound=%d, want exactly 1", bound)
	}
	if history := log.History("race-object"); len(history) != 1 {
		t.Fatalf("history: %+v", history)
	}
}

// TestTodo_ARTIFACT_007_Integration: abort then completion refuses; the
// tombstoned upload never binds unknown bytes.
func TestTodo_ARTIFACT_007_Integration(t *testing.T) {
	m, ctxA, _, _ := sealedFixture(t)
	ctx := context.Background()
	log := NewRevisionLog(mustSealedStore(t, m))
	manager := NewMultipartManager(nil)
	upload := startEvidenceUpload(t, manager, "uploader-a", "abort-object")
	grant := artifactGrant()
	if err := manager.Abort(ctx, MultipartAbortRequest{UploadID: upload.UploadID, TenantID: "tenant-a", Grant: grant}); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if _, _, err := manager.Complete(ctx, MultipartCompleteRequest{UploadID: upload.UploadID, TenantID: "tenant-a", Grant: grant}); err == nil {
		t.Fatal("completion after abort admitted")
	}
	if _, err := log.BindCompletion(ctx, ctxA, []byte("unknown-bytes"), "abort-object", "application/pdf", "provider-vid-x", 0); err != nil {
		t.Fatalf("fresh bind: %v", err)
	}
	// The aborted upload's bytes are gone: rebinding the same key moves
	// generation conditionally, never resurrects.
	second := startEvidenceUpload(t, manager, "uploader-a", "abort-object-2")
	putEvidenceParts(t, manager, second, "uploader-a")
	content := mustCompleteParts(t, manager, second, "uploader-a")
	revision, err := log.BindCompletion(ctx, ctxA, content, "abort-object", "application/pdf", "provider-vid-y", 1)
	if err != nil {
		t.Fatalf("second bind: %v", err)
	}
	if revision.Generation != 2 {
		t.Fatalf("generation=%d", revision.Generation)
	}
}

// TestTodo_ARTIFACT_007_Fault: empty content, unknown revisions and
// wrong generations fail closed.
func TestTodo_ARTIFACT_007_Fault(t *testing.T) {
	m, ctxA, _, _ := sealedFixture(t)
	ctx := context.Background()
	log := NewRevisionLog(mustSealedStore(t, m))
	if _, err := log.BindCompletion(ctx, ctxA, nil, "empty-object", "application/pdf", "vid", 0); err == nil {
		t.Fatal("empty completion bound")
	}
	if _, err := log.BindCompletion(ctx, ctxA, []byte("x"), "", "application/pdf", "vid", 0); err == nil {
		t.Fatal("nameless completion bound")
	}
	if _, err := log.ApplyHold("ghost", 1, true); err == nil {
		t.Fatal("hold on unknown revision admitted")
	}
	if err := log.DeleteRevision(ctx, "ghost", 1, false); err == nil {
		t.Fatal("delete of unknown revision admitted")
	}
	if err := log.VerifyRevision(ctx, ctxA, "ghost", 1); err == nil {
		t.Fatal("verify of unknown revision admitted")
	}
	if _, err := CollectOrphans(nil, time.Now()); err == nil {
		t.Fatal("nil-manager collection admitted")
	}
}

// TestTodo_ARTIFACT_007_Security: unauthorized principals bind nothing;
// holds survive re-verification.
func TestTodo_ARTIFACT_007_Security(t *testing.T) {
	m, ctxA, _, _ := sealedFixture(t)
	ctx := context.Background()
	log := NewRevisionLog(mustSealedStore(t, m))
	manager := NewMultipartManager(nil)
	upload := startEvidenceUpload(t, manager, "uploader-a", "sec-object")
	rogue := artifactGrant()
	rogue.Principal = "rogue"
	if _, _, err := manager.Complete(ctx, MultipartCompleteRequest{UploadID: upload.UploadID, TenantID: "tenant-a", Grant: rogue}); err == nil {
		t.Fatal("rogue completion admitted")
	}
	putEvidenceParts(t, manager, upload, "uploader-a")
	content := mustCompleteParts(t, manager, upload, "uploader-a")
	revision, err := log.BindCompletion(ctx, ctxA, content, "sec-object", "application/pdf", "vid-s", 0)
	if err != nil {
		t.Fatalf("BindCompletion: %v", err)
	}
	mustApplyHold(t, log, "sec-object", revision.Generation, true)
	if err := log.VerifyRevision(ctx, ctxA, "sec-object", revision.Generation); err != nil {
		t.Fatalf("held revision unverifiable: %v", err)
	}
	held := log.History("sec-object")
	if len(held) != 1 || !held[0].Hold {
		t.Fatalf("history: %+v", held)
	}
}

// TestTodo_ARTIFACT_007_Recovery: abandoned uploads collect boundedly;
// completed uploads are never swept.
func TestTodo_ARTIFACT_007_Recovery(t *testing.T) {
	manager := NewMultipartManager(func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) })
	old := startEvidenceUpload(t, manager, "uploader-a", "old-object")
	done := startEvidenceUpload(t, manager, "uploader-a", "done-object")
	putEvidenceParts(t, manager, done, "uploader-a")
	if _, _, err := manager.Complete(context.Background(), MultipartCompleteRequest{
		UploadID: done.UploadID, TenantID: "tenant-a", Grant: artifactGrant(),
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	report, err := CollectOrphans(manager, time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CollectOrphans: %v", err)
	}
	if len(report.Aborted) != 1 || report.Aborted[0] != old.UploadID {
		t.Fatalf("report: %+v", report)
	}
	if report.Digest == "" {
		t.Fatal("orphan report without digest")
	}
	// The swept upload stays tombstoned: late parts refuse.
	grant := artifactGrant()
	if _, err := manager.PutPart(context.Background(), MultipartPartRequest{
		UploadID: old.UploadID, TenantID: "tenant-a",
		Grant: grant, PartNumber: 1, Content: []byte("late"),
	}); err == nil {
		t.Fatal("late part admitted to a swept upload")
	}
}

// TestTodo_ARTIFACT_007_Mutation: generation and alias edges resolve on
// the documented side.
func TestTodo_ARTIFACT_007_Mutation(t *testing.T) {
	m, ctxA, _, _ := sealedFixture(t)
	ctx := context.Background()
	log := NewRevisionLog(mustSealedStore(t, m))
	manager := NewMultipartManager(nil)
	upload := startEvidenceUpload(t, manager, "uploader-a", "edge-object")
	putEvidenceParts(t, manager, upload, "uploader-a")
	content := mustCompleteParts(t, manager, upload, "uploader-a")
	// Creating with a nonzero expectation on an absent object refuses:
	// create means generation zero.
	if _, err := log.BindCompletion(ctx, ctxA, content, "edge-object", "application/pdf", "vid", 7); err == nil {
		t.Fatal("nonzero create expectation admitted")
	}
	first, err := log.BindCompletion(ctx, ctxA, content, "edge-object", "application/pdf", "vid", 0)
	if err != nil {
		t.Fatalf("BindCompletion: %v", err)
	}
	if first.Generation == 0 {
		t.Fatal("bound generation is zero")
	}
	// Holds address exact generations, never aliases: generation zero
	// was never bound, so it refuses even though the object exists.
	if _, err := log.ApplyHold("edge-object", 0, true); err == nil {
		t.Fatal("hold on unbound generation admitted")
	}
}
