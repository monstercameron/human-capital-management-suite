package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestTodo_CHATUX_022 is the service half of "Mark unread from here": a rewind
// reaches the store's rewind with the caller's own identity, an ordinary
// update never does, and a caller who cannot open the conversation is refused
// before anything is written.
func TestTodo_CHATUX_022(t *testing.T) {
	now := time.Unix(20, 0).UTC()
	member := Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Role: Member, JoinedAt: &now, Revision: 2}
	ctx := context.Background()

	f := &fakeStore{conversation: conversation(), membership: member}
	s := newTestService(f, func() time.Time { return now })
	forged := ReadState{TenantID: "t1", ConversationID: "c1", SubjectID: "someone-else", HomeTenantID: "elsewhere", LastReadSequence: 4}
	got, err := s.UpdateReadState(ctx, UpdateReadStateRequest{Principal: principal(), ReadState: forged, ExpectedRevision: 3, Rewind: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.rewound) != 1 || f.rewound[0].SubjectID != "u1" || f.rewound[0].HomeTenantID != "t1" || f.rewound[0].LastReadSequence != 4 {
		t.Fatalf("the store was asked to rewind %+v, want the caller's own position at 4", f.rewound)
	}
	if got.LastReadSequence != 4 || got.SubjectID != "u1" {
		t.Fatalf("rewind answered %+v", got)
	}

	if _, err = s.UpdateReadState(ctx, UpdateReadStateRequest{Principal: principal(), ReadState: forged, ExpectedRevision: 3}); err != nil {
		t.Fatal(err)
	}
	if len(f.rewound) != 1 {
		t.Fatalf("an update without Rewind rewound the position: %+v", f.rewound)
	}

	if _, err = s.UpdateReadState(ctx, UpdateReadStateRequest{Principal: principal(), ReadState: forged, Rewind: true}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("a rewind with no expected revision=%v, want ErrInvalidArgument", err)
	}

	denied := NewService(f, func() time.Time { return now })
	denied.SetAuthority(denyConversationAuthority{base: verifiedAuthority{store: f}, deny: "c1"})
	before := f.mutations
	if _, err = denied.UpdateReadState(ctx, UpdateReadStateRequest{Principal: principal(), ReadState: forged, ExpectedRevision: 3, Rewind: true}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("a rewind by someone who cannot open the conversation=%v, want ErrPermissionDenied", err)
	}
	if f.mutations != before {
		t.Fatal("a refused rewind reached the store")
	}
}
