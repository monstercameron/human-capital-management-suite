package chatmedia

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func r3Uploads(t *testing.T, linked func(context.Context, Reference) (bool, error)) (*Chatattach001Uploads, string) {
	t.Helper()
	root := t.TempDir()
	store, err := NewFilesystemStore(root)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(context.Context, AccessRequest) error { return nil }
	media := New(Config{Store: store, Scanner: Chatattach001Scanner{}, Authorize: authorize})
	return &Chatattach001Uploads{Media: media, Root: root, Authorize: authorize, Linked: linked}, root
}

// A half-written ledger file (a crash between create and the last byte) used to
// make every later upload and every media read fail for every tenant, because
// the ledger is read whole before each one.
func TestR3_CorruptLedgerRecordDoesNotBlockUploadsOrReads(t *testing.T) {
	ctx := context.Background()
	uploads, root := r3Uploads(t, nil)
	if err := os.WriteFile(filepath.Join(root, "torn.attachment"), []byte(`{"ArtifactID":"ab`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := uploads.Retain(ctx, "tenant"); err != nil {
		t.Fatalf("a media read is refused by a torn ledger record: %v", err)
	}
	r := UploadRequest{TenantID: "tenant", ConversationID: "room", PrincipalID: "alice", Filename: "note.txt", DeclaredType: "text/plain", Content: []byte("hello")}
	if _, err := uploads.Upload(ctx, r); err != nil {
		t.Fatalf("an upload is refused by a torn ledger record: %v", err)
	}
}

// Reads call Retain once per image on screen. With RetainEvery set the ledger is
// reconciled with message retention at most that often per tenant, instead of
// one database question per stored attachment on every read.
func TestR3_RetainIsThrottledPerTenantWhenConfigured(t *testing.T) {
	ctx := context.Background()
	calls := 0
	uploads, _ := r3Uploads(t, func(context.Context, Reference) (bool, error) { calls++; return true, nil })
	now := time.Now().UTC()
	uploads.Now = func() time.Time { return now }
	uploads.RetainEvery = time.Minute
	r := UploadRequest{TenantID: "tenant", ConversationID: "room", PrincipalID: "alice", Filename: "note.txt", DeclaredType: "text/plain", Content: []byte("hello")}
	if _, err := uploads.Upload(ctx, r); err != nil {
		t.Fatal(err)
	}
	base := calls
	for i := 0; i < 5; i++ {
		if err := uploads.Retain(ctx, "tenant"); err != nil {
			t.Fatal(err)
		}
	}
	if calls-base > 1 {
		t.Fatalf("five reads in one minute asked the database %d times", calls-base)
	}
	if err := uploads.Retain(ctx, "other"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	before := calls
	if err := uploads.Retain(ctx, "tenant"); err != nil {
		t.Fatal(err)
	}
	if calls == before {
		t.Fatal("a read after the interval did not reconcile the ledger")
	}
}
