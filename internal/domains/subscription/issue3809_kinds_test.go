package subscription

import "testing"

func TestIssue3809_ChatKindsAreDeclared(t *testing.T) {
	for _, kind := range []EventKind{EventChatMessagePosted, EventChatApprovalDecided} {
		if !kind.Valid() {
			t.Fatalf("declared chat event rejected: %q", kind)
		}
		if _, err := NewEventSchema(kind, 1, []SchemaField{{Name: "conversation_id", Type: TypeRef, Classification: ClassificationConfidential}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []EventKind{"chat.*", "chat.message", "chat.message.deleted", "CHAT.MESSAGE.POSTED", "chat.dm.posted"} {
		if kind.Valid() {
			t.Fatalf("undeclared chat event accepted: %q", kind)
		}
	}
}
