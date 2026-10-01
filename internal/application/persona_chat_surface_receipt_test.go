package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaSurfaceReceiptFixture struct {
	receipts []agentinvocationstore.ReplyReceipt
	writes   int
	err      error
}

func (s *personaSurfaceReceiptFixture) RecordReplyReceipt(_ context.Context, receipt agentinvocationstore.ReplyReceipt) error {
	s.writes++
	s.receipts = append(s.receipts, receipt)
	return s.err
}
func (s *personaSurfaceReceiptFixture) ListReplyReceipts(context.Context, string, string, string) ([]agentinvocationstore.ReplyReceipt, error) {
	return s.receipts, s.err
}

type personaReceiptActorFixture struct {
	actor    personaReplyActor
	identity agentsecurity.FinalOutputIdentity
}

type personaReceiptPagedChat struct {
	*personaSurfaceChatFixture
	requests []chat.ListEphemeralPostsRequest
	pages    [][]chat.EphemeralPost
	next     []uint64
}

func (s *personaReceiptPagedChat) ListEphemeralPosts(_ context.Context, r chat.ListEphemeralPostsRequest) ([]chat.EphemeralPost, uint64, error) {
	s.requests = append(s.requests, r)
	index := len(s.requests) - 1
	if r.PageSize > 100 || index >= len(s.pages) {
		return nil, r.AfterSequence, chat.ErrInvalidArgument
	}
	return s.pages[index], s.next[index], nil
}

func TestTodo_AGENTP_011_PrivateActorReceiptPaginatesHiddenOffsets(t *testing.T) {
	for _, tc := range []struct {
		name    string
		second  uint64
		visible bool
	}{
		{name: "visible after hidden rows", second: 101, visible: true},
		{name: "backwards cursor", second: 99},
		{name: "expired result", second: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			surface, ctx, room, _, _ := personaSurfaceFixture(t)
			paged := &personaReceiptPagedChat{personaSurfaceChatFixture: room, pages: [][]chat.EphemeralPost{nil, nil}, next: []uint64{100, tc.second}}
			if tc.visible {
				paged.pages[1] = []chat.EphemeralPost{{ID: "actual-private-101", TenantID: "tenant-a", ConversationID: "channel-a", RecipientHomeTenantID: "tenant-a", RecipientSubjectID: "user-a", Sequence: 101}}
			}
			surface.Chat = paged
			surface.Receipts = &personaSurfaceReceiptFixture{receipts: []agentinvocationstore.ReplyReceipt{{TenantID: "tenant-a", ConversationID: "channel-a", InvokerID: "user-a", PersonaID: "persona.coach", AgentID: "agent:coach", Display: "People Coach", InvokerHandle: "user-a", EphemeralPostID: "actual-private-101"}}}
			principal, _ := trust.FromContext(ctx)
			actors, err := surface.visiblePostActors(ctx, principal, room.room)
			if len(paged.requests) != 2 || paged.requests[0].PageSize != 100 || paged.requests[0].AfterSequence != 0 || paged.requests[1].AfterSequence != 100 {
				t.Fatalf("invalid recipient pagination: %+v", paged.requests)
			}
			if tc.visible {
				if err != nil || len(actors) != 1 || actors[0].PostID != "actual-private-101" || actors[0].InvokerHandle != "user-a" {
					t.Fatalf("private actor after hidden offsets: %+v %v", actors, err)
				}
			} else if tc.second < 100 {
				if !errors.Is(err, personachat.ErrUnavailable) || len(actors) != 0 {
					t.Fatalf("backwards private cursor accepted: %+v %v", actors, err)
				}
			} else if err != nil || len(actors) != 0 {
				t.Fatalf("expired private post retained attribution: %+v %v", actors, err)
			}
		})
	}
}

func (s *personaReceiptActorFixture) ResolvePersonaReplyActor(_ context.Context, identity agentsecurity.FinalOutputIdentity) (personaReplyActor, error) {
	s.identity = identity
	return s.actor, nil
}

type personaReceiptDeliveryFixture struct {
	receipt PersonaReplyDeliveryReceipt
	calls   int
}

func (s *personaReceiptDeliveryFixture) Deliver(context.Context, PersonaReplyDeliveryRequest) (PersonaReplyDeliveryReceipt, error) {
	s.calls++
	return s.receipt, nil
}

func TestTodo_AGENTP_011_ReplyActorReceiptRequiresGenuineSealedOutput(t *testing.T) {
	validator, admission, run, _, _ := personaRunOutputFixture(t)
	output, err := validator.ValidateAndPersistPersonaOutput(context.Background(), admission, run, agentmodel.ModelResult{Text: "I can help explain the policy.", Finish: agentmodel.FinishComplete})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "alice", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "credential"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	store := &personaSurfaceReceiptFixture{}
	delivery := &personaReceiptDeliveryFixture{receipt: PersonaReplyDeliveryReceipt{Public: true, PublicPostID: "actual-post"}}
	actor := &personaReceiptActorFixture{actor: personaReplyActor{AgentID: "agent:canonical", Display: "Policy Helper", InvokerHandle: "alice"}}
	recorder := &personaReplyReceiptRecorder{next: delivery, store: store, actors: actor}
	request := PersonaReplyDeliveryRequest{Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}, Output: output, IdempotencyKey: admission.ID}
	receipt, err := recorder.Deliver(ctx, request)
	if err != nil || receipt.PublicPostID != "actual-post" || store.writes != 1 || actor.identity.InvocationID != admission.Request.Source.Key || store.receipts[0].OutputDigest != output.Digest() || store.receipts[0].PublicPostID != "actual-post" || store.receipts[0].AgentID != "agent:canonical" {
		t.Fatalf("receipt=%+v %v store=%+v actor=%+v", receipt, err, store, actor)
	}
	request.Output = agentsecurity.FinalOutputPersistence{}
	if _, err := recorder.Deliver(ctx, request); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("forged output=%v", err)
	}
	if delivery.calls != 1 {
		t.Fatal("unsealed output reached delivery")
	}
	if _, err := newPersonaReplyReceiptRecorder(nil, nil, nil); !errors.Is(err, ErrPersonaReplyDeliveryUnavailable) {
		t.Fatalf("missing composition=%v", err)
	}
	if _, err := (personaReplyActorStoreSource{}).ResolvePersonaReplyActor(ctx, output.Identity()); !errors.Is(err, ErrPersonaReplyDeliveryUnavailable) {
		t.Fatalf("nil actor store=%v", err)
	}
}

func TestTodo_AGENTP_011_SurfaceActorPayloadUsesOnlyCurrentlyVisibleActualPosts(t *testing.T) {
	s, ctx, room, _, _ := personaSurfaceFixture(t)
	room.posts = append(room.posts, chat.Post{ID: "actual-reply", TenantID: "tenant-a", ConversationID: "channel-a", AuthorID: "persona.coach", ParentID: "post-a", Revision: 1}, chat.Post{ID: "forged-reply", TenantID: "tenant-a", ConversationID: "channel-a", AuthorID: "user-a", ParentID: "post-a", Revision: 1})
	s.Receipts = &personaSurfaceReceiptFixture{receipts: []agentinvocationstore.ReplyReceipt{{TenantID: "tenant-a", InvokerID: "user-a", ConversationID: "channel-a", ThreadID: "post-a", PersonaID: "persona.coach", AgentID: "agent:coach", Display: "People Coach", InvokerHandle: "user-a", PublicPostID: "actual-reply"}, {TenantID: "tenant-a", InvokerID: "user-a", ConversationID: "channel-a", ThreadID: "post-a", PersonaID: "persona.coach", AgentID: "agent:coach", PublicPostID: "forged-reply"}, {TenantID: "tenant-a", InvokerID: "other-user", ConversationID: "channel-a", ThreadID: "post-a", PersonaID: "secret-persona", AgentID: "secret-agent", EphemeralPostID: "private-hidden", PrivateConversationID: "private-dm", PrivatePostID: "private-hidden-copy"}}}
	directory, err := s.Directory(ctx, "channel-a")
	if err != nil || len(directory.PostActors) != 1 || directory.PostActors[0].PostID != "actual-reply" || directory.PostActors[0].AgentID != "agent:coach" {
		t.Fatalf("visible actors=%+v %v", directory.PostActors, err)
	}
	room.posts[1].Deleted = true
	directory, err = s.Directory(ctx, "channel-a")
	if err != nil || len(directory.PostActors) != 0 {
		t.Fatalf("deleted actor=%+v %v", directory.PostActors, err)
	}
	room.members = nil
	if _, err := s.Directory(ctx, "channel-a"); !errors.Is(err, personachat.ErrDenied) {
		t.Fatalf("revoked room actor access=%v", err)
	}
}
