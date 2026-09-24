package main

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func chatStreamAccessFailure(err error) bool {
	code := status.Code(err)
	return code == codes.Unauthenticated || code == codes.PermissionDenied
}

// chatStreamApplyAccessLoss removes the private projection for the room whose
// live watch was refused. Drafts are left in the snapshot for chatui's
// revocation effect to tombstone through the normal owner callback.
func chatStreamApplyAccessLoss(model *chatui.Model, conversationID, tenant, subject string, err error) bool {
	if model == nil || !chatStreamAccessFailure(err) || conversationID == "" || model.SelectedID != conversationID || model.CurrentTenantID != tenant || model.CurrentUser != subject {
		return false
	}
	model.State = chatui.StateError
	model.Error = chatLoadFailureMessage("this conversation", err)
	if status.Code(err) == codes.PermissionDenied {
		model.RevokedConversationID = conversationID
	} else {
		model.RevokedConversationID = ""
	}
	model.Messages = nil
	model.Members = nil
	model.ThreadParentID, model.ThreadParent = "", nil
	model.ThreadMessages = nil
	model.ThreadLoading = false
	model.ThreadHasOlder, model.ThreadHasNewer = false, false
	model.ShowThread, model.ShowPerson, model.ShowDetails = false, false, false
	model.PersonDetails = nil
	model.SearchChannels, model.SearchPeople, model.SearchMessages = nil, nil, nil
	model.DocPreviews, model.Embeds = nil, nil
	model.EditDrafts = nil
	return true
}

// chatEventStream is the receiving half of a WatchConversation stream. The
// generated client satisfies it; a test supplies its own.
type chatEventStream interface {
	Recv() (*chatv1.WatchConversationResponse, error)
}

// chatStreamHooks are what draining a stream does with what it receives. They
// are injected so the loop itself -- the part that decided to stop early and
// took the conversation offline with it -- is testable without a browser.
type chatStreamHooks struct {
	// Apply folds one event in. active is false when this subscription has
	// been superseded, which ends the drain without being a failure.
	Apply func(event *chatv1.ConversationEvent, resume string) (outcome chatEventOutcome, active bool)
	// Applied runs for each event that changed something.
	Applied func(event *chatv1.ConversationEvent)
}

// chatStreamEnd says why a drain stopped.
type chatStreamEnd int

const (
	// chatStreamEndTransport means Recv returned: the stream itself ended.
	chatStreamEndTransport chatStreamEnd = iota
	// chatStreamEndGap means an event was missed and the caller must catch up.
	chatStreamEndGap
	// chatStreamEndSuperseded means this subscription is no longer the live one.
	chatStreamEndSuperseded
)

func (e chatStreamEnd) String() string {
	switch e {
	case chatStreamEndGap:
		return "gap"
	case chatStreamEndSuperseded:
		return "superseded"
	}
	return "transport"
}

// drainChatStream receives until the stream ends, an event is missed, or this
// subscription is superseded.
//
// It does not stop for anything else. An event it has already applied, an event
// with no post, an event kind it does not render: all of those continue the
// loop. Returning on one of them is how a room with history went quiet a few
// milliseconds after every open while the server held the stream wide open.
func drainChatStream(ctx context.Context, stream chatEventStream, hooks chatStreamHooks) (delivered bool, reason chatStreamEnd, termination error) {
	for {
		message, err := stream.Recv()
		if err != nil {
			// Whether this end was normal is decided by chatStreamRollover,
			// not here.
			return delivered, chatStreamEndTransport, err
		}
		event := message.GetEvent()
		if event == nil {
			// A message carrying only a resume cursor is not an end.
			continue
		}
		outcome, active := hooks.Apply(event, message.GetResumeCursor())
		if !active {
			return delivered, chatStreamEndSuperseded, nil
		}
		switch outcome {
		case chatEventGap:
			return delivered, chatStreamEndGap, nil
		case chatEventDuplicate:
			continue
		}
		delivered = true
		if hooks.Applied != nil {
			hooks.Applied(event)
		}
		if ctx.Err() != nil {
			return delivered, chatStreamEndSuperseded, ctx.Err()
		}
	}
}
