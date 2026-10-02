package chat

import (
	"context"
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// rewindRecorder keeps the read-state request the transport handed the service.
type rewindRecorder struct {
	*transportChatFake
	got chatcore.UpdateReadStateRequest
}

func (r *rewindRecorder) UpdateReadState(_ context.Context, request chatcore.UpdateReadStateRequest) (chatcore.ReadState, error) {
	r.got = request
	return request.ReadState, nil
}

// TestTodo_CHATUX_022 holds the wire to the service for "Mark unread from
// here": the rewind flag, the sequence and the expected revision arrive as
// sent, and a request without the flag is an ordinary advance.
func TestTodo_CHATUX_022(t *testing.T) {
	ctx := admittedChatContext(t)
	f := &rewindRecorder{transportChatFake: &transportChatFake{}}
	s := &server{deps: Dependencies{Service: f}}
	state := &chatv1.ReadState{ConversationId: "c", LastReadSequence: 7}

	got, err := s.UpdateReadState(ctx, &chatv1.UpdateReadStateRequest{State: state, ExpectedRevision: 3, Rewind: true})
	if err != nil {
		t.Fatal(err)
	}
	if !f.got.Rewind || f.got.ReadState.LastReadSequence != 7 || f.got.ExpectedRevision != 3 || f.got.Principal.SubjectID != "u" {
		t.Fatalf("the service was asked %+v, want a rewind to 7 at revision 3 by the admitted caller", f.got)
	}
	if got.GetState().GetLastReadSequence() != 7 {
		t.Fatalf("answer = %+v, want the rewound position", got.GetState())
	}

	if _, err = s.UpdateReadState(ctx, &chatv1.UpdateReadStateRequest{State: state, ExpectedRevision: 3}); err != nil {
		t.Fatal(err)
	}
	if f.got.Rewind {
		t.Fatal("a request without rewind reached the service as a rewind")
	}
}
