package chatstore

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestTodo_CHAT_022_Integration_RecipientCursorIntegrity(t *testing.T) {
	s := adapterDB(t)
	ctx := context.Background()
	c := chat.Conversation{ID: "recipient-integrity", TenantID: "host", Kind: chat.PrivateChannel, OwnerID: "sam", Revision: 1}
	local := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: "host", SubjectID: "sam", Role: chat.Manager, HistoryVisibility: chat.FullHistory}
	foreign := local
	foreign.HomeTenantID = "foreign"
	foreign.Role = chat.Member
	if _, err := s.CreateConversation(ctx, c, []chat.Membership{local, foreign}, ""); err != nil {
		t.Fatal(err)
	}
	state := chat.ReadState{TenantID: c.TenantID, ConversationID: c.ID, HomeTenantID: "host", SubjectID: "sam"}
	if _, err := s.PutReadState(ctx, state, 1); err != nil {
		t.Fatalf("zero cursor: %v", err)
	}
	state.LastReadSequence = 1
	if _, err := s.PutReadState(ctx, state, 2); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("future cursor: %v", err)
	}
	state.LastReadSequence = math.MaxUint64
	if _, err := s.PutReadState(ctx, state, 2); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("overflow cursor: %v", err)
	}
	p, err := s.SendPost(ctx, chat.SendPostRequest{Principal: chat.Principal{TenantID: "host", SubjectID: "sam"}, TenantID: c.TenantID, ConversationID: c.ID, IdempotencyKey: "one"}, chat.Post{AuthorID: "sam", Body: "one"})
	if err != nil {
		t.Fatal(err)
	}
	state.LastReadSequence = p.Sequence
	got, err := s.PutReadState(ctx, state, 2)
	if err != nil || got.LastReadSequence != p.Sequence || got.Revision != 3 {
		t.Fatalf("advance: %+v %v", got, err)
	}
	if _, err := s.PutReadState(ctx, state, 2); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("stale CAS: %v", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.PutReadState(ctx, state, 3)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, chat.ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent CAS: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent CAS successes=%d conflicts=%d", successes, conflicts)
	}
	late := chat.Membership{ConversationID: c.ID, TenantID: c.TenantID, HomeTenantID: "later", SubjectID: "sam", Role: chat.Member, HistoryVisibility: chat.FromJoin}
	if _, err := s.PutMembership(ctx, chat.Principal{TenantID: "host", SubjectID: "sam"}, late); err != nil {
		t.Fatal(err)
	}
	lateState := chat.ReadState{TenantID: c.TenantID, ConversationID: c.ID, HomeTenantID: "later", SubjectID: "sam", LastReadSequence: p.Sequence}
	if _, err := s.PutReadState(ctx, lateState, 1); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("pre-join cursor: %v", err)
	}
	other, err := s.GetReadState(ctx, c.TenantID, c.ID, "foreign", "sam")
	if err != nil || other.LastReadSequence != 0 || other.Revision != 1 {
		t.Fatalf("foreign cursor: %+v %v", other, err)
	}
	if _, err := s.PutReadState(ctx, chat.ReadState{TenantID: c.TenantID, ConversationID: c.ID, HomeTenantID: "missing", SubjectID: "sam"}, 1); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("nonmember cursor: %v", err)
	}
	if _, err := s.GetPreferences(ctx, c.TenantID, c.ID, "missing", "sam"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("nonmember preference read: %v", err)
	}
	if _, err := s.RemoveMembership(ctx, chat.Principal{TenantID: "host", SubjectID: "sam"}, c.TenantID, c.ID, "foreign", "sam", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetReadState(ctx, c.TenantID, c.ID, "foreign", "sam"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("removed cursor read: %v", err)
	}
	if _, err := s.PutReadState(ctx, chat.ReadState{TenantID: c.TenantID, ConversationID: c.ID, HomeTenantID: "foreign", SubjectID: "sam"}, 1); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("removed cursor write: %v", err)
	}
	if _, err := s.GetPreferences(ctx, c.TenantID, c.ID, "foreign", "sam"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("removed preference read: %v", err)
	}
	if _, err := s.PutPreferences(ctx, chat.NotificationPreferences{TenantID: c.TenantID, ConversationID: c.ID, HomeTenantID: "foreign", SubjectID: "sam", Muted: true}, 1); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("removed preference write: %v", err)
	}
}
