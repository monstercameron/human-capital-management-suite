package chat

import (
	"context"
	"errors"
	"testing"
)

// TestTodo_CHATUX_021_SystemLineIsNotSaved: the "added people" line is a record
// of the room, so Save for later refuses it and nothing is stored.
func TestTodo_CHATUX_021_SystemLineIsNotSaved(t *testing.T) {
	s, f, r := chatsaveService()
	f.post.Body = MembershipAddedBody("u2")
	if _, err := s.SaveForLater(context.Background(), r); !errors.Is(err, ErrInvalidArgument) || len(f.items) != 0 {
		t.Fatalf("the system line was saved: %v, %d items", err, len(f.items))
	}
	f.post.Body = "an ordinary message"
	if _, err := s.SaveForLater(context.Background(), r); err != nil || len(f.items) != 1 {
		t.Fatalf("an ordinary message was refused: %v", err)
	}
}
