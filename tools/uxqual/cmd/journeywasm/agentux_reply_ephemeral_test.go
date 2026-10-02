package main

import (
	"io"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type agentUXReplyStreamFixture struct {
	messages []*chatv1.WatchConversationResponse
}

func (s *agentUXReplyStreamFixture) Recv() (*chatv1.WatchConversationResponse, error) {
	if len(s.messages) == 0 {
		return nil, io.EOF
	}
	message := s.messages[0]
	s.messages = s.messages[1:]
	return message, nil
}

func TestTodo_AGENTUX_025_EphemeralStreamCarriesPrivateAnswerToTheModel(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	delivery := &chatv1.EphemeralDelivery{Id: "answer", ThreadId: "question", Body: "Private answer", OnlyVisibleToYou: true, CreatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Hour))}
	model := chatui.Model{}
	stream := &agentReplyEphemeralStream{next: &agentUXReplyStreamFixture{messages: []*chatv1.WatchConversationResponse{{EphemeralDelivery: delivery, ResumeCursor: "recipient-cursor"}}}, apply: func(got *chatv1.EphemeralDelivery, resume string) bool {
		if resume != "recipient-cursor" {
			t.Fatalf("resume cursor = %q", resume)
		}
		return applyAgentReplyEphemeral(&model, got, now)
	}}
	message, err := stream.Recv()
	if err != nil || message.GetEphemeralDelivery().GetId() != "answer" || !stream.delivered || len(model.EphemeralMessages) != 1 || model.EphemeralMessages[0].ThreadID != "question" {
		t.Fatalf("stream message=%+v model=%+v delivered=%v err=%v", message, model.EphemeralMessages, stream.delivered, err)
	}
	if !applyAgentReplyEphemeral(&model, delivery, now) || len(model.EphemeralMessages) != 1 {
		t.Fatalf("replayed private answer duplicated: %+v", model.EphemeralMessages)
	}
}

func TestTodo_AGENTUX_026_LoadAdoptionPreservesStreamedPrivateAnswer(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	delivery := &chatv1.EphemeralDelivery{Id: "answer", ThreadId: "question", Body: "Private answer", OnlyVisibleToYou: true, CreatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Hour))}
	state := &chatState{
		generation: 4,
		model:      chatui.Model{SelectedID: "room"},
		cursor:     chatCursor{ConversationID: "room", Seen: map[string]uint64{}},
	}
	if !state.applyEphemeralDelivery(4, "room", delivery, now, "recipient-cursor") {
		t.Fatal("recipient-only answer was not applied while the conversation load was in flight")
	}
	loaded := chatui.Model{SelectedID: "room", Messages: []chatui.Message{{ID: "question"}}}
	if !state.adoptLoadedChatProjection(4, loaded, chatCursor{ConversationID: "room", Seen: map[string]uint64{}}, false) {
		t.Fatal("current conversation load was rejected")
	}
	got := state.snapshot()
	if len(got.EphemeralMessages) != 1 || got.EphemeralMessages[0].ID != "answer" {
		t.Fatalf("load replaced the streamed private answer: %+v", got.EphemeralMessages)
	}
}

func TestTodo_AGENTUX_028_EphemeralProjectionRejectsPublicExpiredAndMalformedRows(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	base := &chatv1.EphemeralDelivery{Id: "answer", ThreadId: "question", Body: "Private answer", OnlyVisibleToYou: true, CreatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Hour))}
	for _, mutate := range []func(*chatv1.EphemeralDelivery){
		func(v *chatv1.EphemeralDelivery) { v.OnlyVisibleToYou = false },
		func(v *chatv1.EphemeralDelivery) { v.ExpiresAt = timestamppb.New(now) },
		func(v *chatv1.EphemeralDelivery) { v.ThreadId = "" },
		func(v *chatv1.EphemeralDelivery) { v.Body = "" },
	} {
		candidate := proto.Clone(base).(*chatv1.EphemeralDelivery)
		mutate(candidate)
		if _, ok := agentReplyEphemeralMessage(candidate, now); ok {
			t.Fatalf("invalid recipient-only envelope accepted: id=%q thread=%q private=%t", candidate.GetId(), candidate.GetThreadId(), candidate.GetOnlyVisibleToYou())
		}
	}
}

func TestTodo_AGENTUX_029_ElapsedStateSurvivesProjectionRefresh(t *testing.T) {
	previous := []chatui.PersonaThreadInvocation{{Projection: chatui.PersonaProgressProjection{InvocationID: "invocation", Progress: &chatui.PersonaProgressProps{ElapsedSeconds: 17, Visible: true}}}}
	next := []chatui.PersonaThreadInvocation{{Projection: chatui.PersonaProgressProjection{InvocationID: "invocation", Progress: &chatui.PersonaProgressProps{Visible: true}}}}
	next = preservePersonaElapsed(previous, next)
	model := &chatui.Model{PersonaInvocations: next}
	if next[0].Projection.Progress.ElapsedSeconds != 17 || !advancePersonaElapsed(model) || model.PersonaInvocations[0].Projection.Progress.ElapsedSeconds != 18 {
		t.Fatalf("elapsed projection was not retained: %+v", next)
	}
}

func TestTodo_AGENTUX_029_CompletedPrivateInvocationWaitsForItsEnvelope(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "alice"}
	private := personaChatInvocations([]personaChatInvocation{{
		InvocationID: "invocation", PostID: "question", ConversationID: "room", InvokerID: "alice", AgentName: "Policy Helper", Status: "COMPLETED",
		PrivateConversationID: "agent-dm", PrivatePostID: "answer",
	}}, cfg, "room")
	// CHATBUG-079: the row keeps its place for the envelope as a stored answer,
	// not as a run at work.
	if len(private) != 1 || private[0].Projection.Progress != nil || !private[0].Projection.AnswerStored || private[0].Projection.PrivateReplyHref == "" {
		t.Fatalf("completed private invocation lost continuity before its envelope: %+v", private)
	}
	public := personaChatInvocations([]personaChatInvocation{{
		InvocationID: "public", PostID: "question", ConversationID: "room", InvokerID: "alice", AgentName: "Policy Helper", Status: "COMPLETED",
	}}, cfg, "room")
	if len(public) != 1 || public[0].Projection.Progress != nil || public[0].Projection.Failure != nil || public[0].Projection.PrivateReplyHref != "" {
		t.Fatalf("completed public invocation rendered a duplicate row: %+v", public)
	}
}
