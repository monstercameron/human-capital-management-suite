package chatreferences

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestTodo_CHAT_027_PropertyReferenceKindsRemainTyped(t *testing.T) {
	for _, kind := range []chat.ReferenceKind{chat.PersonMention, chat.AgentMention, chat.ConversationMention} {
		ref := chat.Reference{Kind: kind, TenantID: "tenant-a", ID: "id-1", ConversationID: "conversation-a"}
		if ref.Kind != kind || ref.TenantID == "" || ref.ID == "" {
			t.Fatalf("reference lost typed identity: %#v", ref)
		}
	}
}

func TestTodo_CHAT_029_SecurityLinkIsLocatorOnly(t *testing.T) {
	codec := chat.OpaqueLocator{}
	token, err := codec.Encode("tenant-a", "conversation-a", "post-a")
	if err != nil {
		t.Fatal(err)
	}
	if token == "tenant-a/conversation-a/post-a" {
		t.Fatal("locator unexpectedly exposes a URL-shaped grant")
	}
	if _, _, _, err := codec.Decode(token + "x"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("tampered locator error = %v, want not found", err)
	}
}

func TestTodo_CHAT_030_IntegrationLinkDecodeDoesNotAuthorize(t *testing.T) {
	codec := chat.OpaqueLocator{}
	token, err := codec.Encode("tenant-a", "conversation-a", "post-a")
	if err != nil {
		t.Fatal(err)
	}
	// Decoding supplies a location only. The application must still call
	// ResolveConversationLink with an authenticated principal before reading.
	tenant, conversation, post, err := codec.Decode(token)
	if err != nil || tenant != "tenant-a" || conversation != "conversation-a" || post != "post-a" {
		t.Fatalf("decoded locator = %q/%q/%q, err=%v", tenant, conversation, post, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ctx.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("context = %v, want canceled", err)
	}
}
