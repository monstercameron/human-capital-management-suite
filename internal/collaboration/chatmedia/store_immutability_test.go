package chatmedia

import (
	"context"
	"errors"
	"testing"
)

func TestTodo_CHAT_036_ArtifactIDsAreImmutable(t *testing.T) {
	ctx := context.Background()
	for name, store := range map[string]Store{
		"memory": NewMemoryStore(),
		"filesystem": func() Store {
			store, err := NewFilesystemStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			return store
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			original := Artifact{Reference: Reference{ArtifactID: "artifact-1", TenantID: "tenant-1", ConversationID: "conversation-1", MediaType: MediaMP3, Size: 4, State: StateQuarantined, AltText: "voice note"}, Content: []byte("ID3x")}
			if err := store.Quarantine(ctx, original); err != nil {
				t.Fatal(err)
			}
			if err := store.Quarantine(ctx, original); err != nil {
				t.Fatalf("identical retry = %v", err)
			}

			changedBytes := original
			changedBytes.Content = []byte("ID3y")
			if err := store.Quarantine(ctx, changedBytes); !errors.Is(err, ErrInvalid) {
				t.Fatalf("same ID with changed bytes = %v, want ErrInvalid", err)
			}
			changedAltText := original
			changedAltText.AltText = "different voice note"
			if err := store.Quarantine(ctx, changedAltText); !errors.Is(err, ErrInvalid) {
				t.Fatalf("same ID with changed description = %v, want ErrInvalid", err)
			}
			changedScope := original
			changedScope.ConversationID = "conversation-2"
			if err := store.Quarantine(ctx, changedScope); !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("same ID with foreign scope = %v, want ErrUnauthorized", err)
			}
			if err := store.SetVerdict(ctx, original.ArtifactID, original.TenantID, StateAdmitted, "", "scanner/test"); err != nil {
				t.Fatal(err)
			}

			got, err := store.Get(ctx, original.TenantID, original.ArtifactID)
			if err != nil || string(got.Content) != string(original.Content) {
				t.Fatalf("stored artifact = %q, %v; want original bytes", got.Content, err)
			}
		})
	}
}
