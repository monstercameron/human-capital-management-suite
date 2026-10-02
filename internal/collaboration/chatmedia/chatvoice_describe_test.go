package chatmedia

import (
	"context"
	"errors"
	"testing"
)

// Describe asks the store for (tenant, id). It used to pass them the other way
// round, so no admitted artifact was ever found by it.
func TestTodo_CHATVOICE_Describe_TenantFirst(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	media := New(Config{Store: store})
	a := Artifact{Reference: Reference{ArtifactID: "art-1", TenantID: "tenant", ConversationID: "room", MediaType: MediaType("audio/ogg"), Size: 4}, Content: []byte("data")}
	if err := store.Quarantine(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err := media.Describe(ctx, "tenant", "room", "art-1"); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("a quarantined artifact must not be described: %v", err)
	}
	if err := store.SetVerdict(ctx, "art-1", "tenant", StateAdmitted, "", ""); err != nil {
		t.Fatal(err)
	}
	ref, err := media.Describe(ctx, "tenant", "room", "art-1")
	if err != nil || ref.ArtifactID != "art-1" {
		t.Fatalf("admitted artifact: %+v %v", ref, err)
	}
	if _, err = media.Describe(ctx, "tenant", "other-room", "art-1"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("another conversation: %v", err)
	}
	if _, err = media.Describe(ctx, "other-tenant", "room", "art-1"); err == nil {
		t.Fatal("another tenant described the artifact")
	}
}
