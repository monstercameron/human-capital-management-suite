package memory

import (
	"context"
	"errors"
	"testing"
)

func TestTodo_AGENT_040_InventoryMetadata(t *testing.T) {
	f := newFixture(t)
	item := testItem("metadata", KindTrace)
	if err := f.manager.Put(context.Background(), testActor(), item); err != nil {
		t.Fatal(err)
	}
	f.disposition.decision = DispositionDecision{Resolved: true, Held: true, CanDelete: true, Reason: "hold"}
	metadata, err := f.manager.InventoryMetadata(context.Background(), testActor())
	if err != nil || len(metadata) != 1 || !metadata[0].Held || metadata[0].CanDelete || metadata[0].Pin.ID != item.SourceID || metadata[0].Pin.Purpose != item.Purpose {
		t.Fatalf("metadata: %+v %v", metadata, err)
	}
	f.disposition.decision = DispositionDecision{}
	if _, err = f.manager.InventoryMetadata(context.Background(), testActor()); !errors.Is(err, ErrDisposition) {
		t.Fatalf("unknown hold: %v", err)
	}
	if _, err = f.manager.InventoryMetadata(context.Background(), Actor{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("anonymous metadata: %v", err)
	}
	if SourceTarget("a", "bc") == SourceTarget("ab", "c") {
		t.Fatal("source owner key is ambiguous")
	}
}

func TestTodo_AGENT_040_CurrentSourceRevisionRemainsUsable(t *testing.T) {
	f := newFixture(t)
	old := testItem("old-copy", KindCache)
	if err := f.manager.Put(context.Background(), testActor(), old); err != nil {
		t.Fatal(err)
	}
	f.sources.decision.Version = "v2"
	f.sources.decision.Digest = "sha256:updated"
	if _, err := f.manager.Read(context.Background(), testActor(), "raw", old.ID); !errors.Is(err, ErrStale) {
		t.Fatalf("stale prior version: %v", err)
	}
	updated := testItem("new-copy", KindCache)
	updated.SourceVersion = f.sources.decision.Version
	updated.SourceDigest = f.sources.decision.Digest
	if err := f.manager.Put(context.Background(), testActor(), updated); err != nil {
		t.Fatalf("fresh owner-approved source was fenced by old copy cleanup: %v", err)
	}
	if _, err := f.manager.Read(context.Background(), testActor(), "search", updated.ID); err != nil {
		t.Fatalf("current derived search copy: %v", err)
	}
}

func TestTodo_AGENT_040_CurrentSourceClass(t *testing.T) {
	f := newFixture(t)
	item := testItem("source-class", KindToolResult)
	f.sources.decision.DataClass = item.DataClass
	if err := f.manager.Put(context.Background(), testActor(), item); err != nil {
		t.Fatal(err)
	}
	f.sources.decision.DataClass = "RESTRICTED"
	if _, err := f.manager.Read(context.Background(), testActor(), "raw", item.ID); !errors.Is(err, ErrStale) {
		t.Fatalf("changed source class: %v", err)
	}
}
