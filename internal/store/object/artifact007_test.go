package object

import (
	"context"
	"testing"
	"time"
)

func artifactGrant() UploadGrant {
	return UploadGrant{
		TenantID: "tenant-a", Principal: "uploader-a", Purpose: "evidence-bundle",
		ExpiresAt: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
	}
}

func startEvidenceUpload(t *testing.T, manager *MultipartManager, principal, idempotency string) MultipartUpload {
	t.Helper()
	grant := artifactGrant()
	grant.Principal = principal
	upload, err := manager.Start(context.Background(), MultipartRequest{
		TenantID: "tenant-a", ExpectedParts: 2, MaxBytes: 1 << 20,
		Grant: grant, IdempotencyKey: idempotency,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return upload
}

func putEvidenceParts(t *testing.T, manager *MultipartManager, upload MultipartUpload, principal string) {
	t.Helper()
	grant := artifactGrant()
	grant.Principal = principal
	for number, content := range map[int][]byte{1: []byte("part-one-"), 2: []byte("part-two")} {
		if _, err := manager.PutPart(context.Background(), MultipartPartRequest{
			UploadID: upload.UploadID, TenantID: "tenant-a",
			Grant: grant, PartNumber: number, Content: content,
		}); err != nil {
			t.Fatalf("PutPart(%d): %v", number, err)
		}
	}
}

func TestConcurrentMultipartCompletionBindsOnlyAuthorizedVersionChecksumAndHold(t *testing.T) {
	m, ctxA, _, _ := sealedFixture(t)
	ctx := context.Background()
	manager := NewMultipartManager(nil)
	log := NewRevisionLog(mustSealedStore(t, m))

	// Completion binds the provider version plus the verified checksum
	// to an immutable revision.
	upload := startEvidenceUpload(t, manager, "uploader-a", "case-file-1")
	putEvidenceParts(t, manager, upload, "uploader-a")
	revision := mustBindCompletion(t, log, ctx, ctxA, manager, upload, "case-file-1", "application/pdf", "provider-vid-1", 0)
	if revision.Generation == 0 || revision.Checksum == "" || revision.ProviderVersionID != "provider-vid-1" {
		t.Fatalf("revision: %+v", revision)
	}
	if revision.Hold {
		t.Fatalf("fresh revision holds: %+v", revision)
	}
	// A racing second completion for the same key refuses: conditional
	// binding admits only the authorized generation.
	racing := startEvidenceUpload(t, manager, "uploader-a", "case-file-1-race")
	putEvidenceParts(t, manager, racing, "uploader-a")
	racingContent := mustCompleteParts(t, manager, racing, "uploader-a")
	if _, err := log.BindCompletion(ctx, ctxA, racingContent, "case-file-1", "application/pdf", "provider-vid-2", 0); err == nil {
		t.Fatal("racing completion overwrote by key")
	}
	// Legal hold attaches to the exact revision, never to a `latest` alias.
	held := mustApplyHold(t, log, revision.ObjectID, revision.Generation, true)
	if !held.Hold || held.Generation != revision.Generation {
		t.Fatalf("hold: %+v", held)
	}
	// A held revision never deletes, including by governance bypass.
	if err := log.DeleteRevision(ctx, revision.ObjectID, revision.Generation, false); err == nil {
		t.Fatal("held revision deleted")
	}
	if err := log.DeleteRevision(ctx, revision.ObjectID, revision.Generation, true); err == nil {
		t.Fatal("governance bypass deleted a held revision")
	}
}
