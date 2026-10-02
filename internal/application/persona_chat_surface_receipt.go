package application

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaReplyReceiptStore interface {
	RecordReplyReceipt(context.Context, agentinvocationstore.ReplyReceipt) error
	ListReplyReceipts(context.Context, string, string, string) ([]agentinvocationstore.ReplyReceipt, error)
}

func (s *PersonaChatSurface) visiblePostActors(ctx context.Context, p *trust.Principal, room chat.Conversation) ([]personachat.PostActor, error) {
	out := make([]personachat.PostActor, 0)
	if s.Receipts == nil {
		return out, nil
	}
	receipts, err := s.Receipts.ListReplyReceipts(ctx, room.TenantID, p.Subject(), room.ID)
	if err != nil {
		return nil, personachat.ErrUnavailable
	}
	if len(receipts) == 0 {
		return out, nil
	}
	principal := chat.Principal{TenantID: room.TenantID, SubjectID: p.Subject()}
	visible := make(map[string]chat.Post)
	page := chat.Page{PageSize: 200}
	seen := make(map[string]bool)
	for {
		posts, err := s.Chat.ListPosts(ctx, chat.ListPostsRequest{Principal: principal, TenantID: room.TenantID, ConversationID: room.ID, Page: page})
		if err != nil {
			return nil, surfaceChatError(err)
		}
		for _, post := range posts.Posts {
			if post.TenantID == room.TenantID && post.ConversationID == room.ID && !post.Deleted && post.Revision == 1 {
				visible[post.ID] = post
			}
		}
		if posts.NextCursor == "" {
			break
		}
		if seen[posts.NextCursor] {
			return nil, personachat.ErrUnavailable
		}
		seen[posts.NextCursor], page.Cursor = true, posts.NextCursor
	}
	ephemeralIDs, err := s.deliveredPrivateCards(ctx, p, room, receipts)
	if err != nil {
		return nil, err
	}
	for _, receipt := range receipts {
		if receipt.TenantID != room.TenantID {
			return nil, personachat.ErrUnavailable
		}
		ids := make([]string, 0, 3)
		if post, ok := visible[receipt.PublicPostID]; ok && receipt.ConversationID == room.ID && post.AuthorID == receipt.PersonaID && post.ParentID == receipt.ThreadID {
			ids = append(ids, post.ID)
		}
		if receipt.InvokerID == p.Subject() {
			if post, ok := visible[receipt.PrivatePostID]; ok && receipt.PrivateConversationID == room.ID {
				ids = append(ids, post.ID)
			}
			if _, delivered := ephemeralIDs[receipt.EphemeralPostID]; delivered && receipt.ConversationID == room.ID {
				ids = append(ids, receipt.EphemeralPostID)
			}
		}
		for _, id := range ids {
			out = append(out, personachat.PostActor{PostID: id, PersonaID: receipt.PersonaID, AgentID: receipt.AgentID, InvokerHandle: receipt.InvokerHandle, Display: receipt.Display, PersonaVersion: receipt.PersonaVersion})
		}
	}
	slices.SortFunc(out, func(a, b personachat.PostActor) int { return strings.Compare(a.PostID, b.PostID) })
	return out, nil
}

// deliveredPrivateCards reads the private cards the receipts of the caller's own
// runs in this room name, by card id. A card that has expired, or that was
// delivered to somebody else, is not in the result. The card reader checks the
// caller's membership itself and shows each source as this reader may see it.
func (s *PersonaChatSurface) deliveredPrivateCards(ctx context.Context, p *trust.Principal, room chat.Conversation, receipts []agentinvocationstore.ReplyReceipt) (map[string]chat.EphemeralPost, error) {
	delivered := make(map[string]chat.EphemeralPost)
	wanted := make(map[string]bool)
	for _, receipt := range receipts {
		if receipt.TenantID == room.TenantID && receipt.InvokerID == p.Subject() && receipt.ConversationID == room.ID && receipt.EphemeralPostID != "" {
			wanted[receipt.EphemeralPostID] = true
		}
	}
	ephemeral, ok := s.privateCards()
	if !ok {
		return delivered, nil
	}
	principal := chat.Principal{TenantID: room.TenantID, SubjectID: p.Subject()}
	var after uint64
	for len(wanted) > 0 {
		posts, next, err := ephemeral.ListEphemeralPosts(ctx, chat.ListEphemeralPostsRequest{Principal: principal, TenantID: room.TenantID, ConversationID: room.ID, AfterSequence: after, PageSize: 100})
		if err != nil {
			return nil, surfaceChatError(err)
		}
		for _, post := range posts {
			if wanted[post.ID] && post.TenantID == room.TenantID && post.ConversationID == room.ID && post.RecipientHomeTenantID == room.TenantID && post.RecipientSubjectID == p.Subject() {
				delivered[post.ID] = post
				delete(wanted, post.ID)
			}
		}
		if next < after || (next == after && len(posts) > 0) {
			return nil, personachat.ErrUnavailable
		}
		if next == after {
			break
		}
		after = next
	}
	return delivered, nil
}

type personaReplyActor struct{ AgentID, Display, InvokerHandle string }
type personaReplyActorSource interface {
	ResolvePersonaReplyActor(context.Context, agentsecurity.FinalOutputIdentity) (personaReplyActor, error)
}

type personaReplyReceiptRecorder struct {
	next   PersonaRunReplyDeliverer
	store  personaReplyReceiptStore
	actors personaReplyActorSource
}

func newPersonaReplyReceiptRecorder(next PersonaRunReplyDeliverer, db *agentstore.Store, personas *agentpersonastore.Store) (PersonaRunReplyDeliverer, error) {
	if next == nil || db == nil || personas == nil {
		return nil, ErrPersonaReplyDeliveryUnavailable
	}
	store, err := agentinvocationstore.NewWithTenantUUID(db, pgstore.TenantID)
	if err != nil {
		return nil, err
	}
	return &personaReplyReceiptRecorder{next: next, store: store, actors: personaReplyActorStoreSource{store: personas}}, nil
}

// Deliver records only actual server delivery receipts and actor identity
// derived from the sealed output and exact published profile. Message body,
// author display and browser inputs cannot establish persona attribution.
func (r *personaReplyReceiptRecorder) Deliver(ctx context.Context, request PersonaReplyDeliveryRequest) (PersonaReplyDeliveryReceipt, error) {
	if r == nil || r.next == nil || r.store == nil || r.actors == nil {
		return PersonaReplyDeliveryReceipt{}, ErrPersonaReplyDeliveryUnavailable
	}
	identity := request.Output.Identity()
	p, ok := personaSurfacePrincipal(ctx)
	if !ok || p.Tenant().String() != identity.TenantID || p.Subject() != identity.InvokerID || request.Principal.TenantID != identity.TenantID || request.Principal.SubjectID != identity.InvokerID || request.Output.Digest() == "" {
		return PersonaReplyDeliveryReceipt{}, chat.ErrInvalidArgument
	}
	if _, _, err := request.Output.Payload(); err != nil {
		return PersonaReplyDeliveryReceipt{}, chat.ErrInvalidArgument
	}
	actor, err := r.actors.ResolvePersonaReplyActor(ctx, identity)
	if err != nil || actor.AgentID == "" || actor.Display == "" || actor.InvokerHandle == "" {
		return PersonaReplyDeliveryReceipt{}, ErrPersonaReplyDeliveryUnavailable
	}
	receipt, err := r.next.Deliver(ctx, request)
	if err != nil {
		return receipt, err
	}
	if !validPersonaRunReplyReceipt(receipt) {
		return PersonaReplyDeliveryReceipt{}, ErrPersonaReplyDeliveryUnavailable
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err = r.store.RecordReplyReceipt(writeCtx, agentinvocationstore.ReplyReceipt{TenantID: identity.TenantID, InvocationID: identity.InvocationID, RunID: identity.RunID, OutputID: identity.OutputID, OutputDigest: request.Output.Digest(), InvokerID: identity.InvokerID, ConversationID: identity.ConversationID, ThreadID: identity.ThreadID, InvokingPostID: identity.PostID, PersonaID: identity.PersonaID, PersonaVersion: identity.PersonaVersion, AgentID: actor.AgentID, Display: actor.Display, InvokerHandle: actor.InvokerHandle, PublicPostID: receipt.PublicPostID, EphemeralPostID: receipt.EphemeralPostID, PrivatePostID: receipt.DurableCopyPostID, PrivateConversationID: receipt.DurableCopyConversationID})
	if err != nil {
		return PersonaReplyDeliveryReceipt{}, ErrPersonaReplyDeliveryUnavailable
	}
	return receipt, nil
}

type personaReplyActorStoreSource struct{ store *agentpersonastore.Store }

func (s personaReplyActorStoreSource) ResolvePersonaReplyActor(ctx context.Context, identity agentsecurity.FinalOutputIdentity) (personaReplyActor, error) {
	if s.store == nil {
		return personaReplyActor{}, ErrPersonaReplyDeliveryUnavailable
	}
	version, err := strconv.ParseInt(identity.PersonaVersion, 10, 64)
	if err != nil || version <= 0 {
		return personaReplyActor{}, ErrPersonaReplyDeliveryUnavailable
	}
	scoped, err := s.store.Scoped(values.TenantId(identity.TenantID))
	if err != nil {
		return personaReplyActor{}, err
	}
	stored, err := scoped.GetVersion(ctx, identity.PersonaID, version)
	if err != nil {
		return personaReplyActor{}, err
	}
	profile, err := decodeAvailableProfile(stored)
	if err != nil {
		return personaReplyActor{}, err
	}
	identities, err := s.store.ListPersonaChatIdentities(ctx, identity.TenantID)
	if err != nil {
		return personaReplyActor{}, err
	}
	agentID := ""
	for _, candidate := range identities {
		if candidate.TenantID == values.TenantId(identity.TenantID) && candidate.PersonaID == identity.PersonaID && candidate.Active {
			if agentID != "" && agentID != candidate.AgentID {
				return personaReplyActor{}, ErrPersonaReplyDeliveryUnavailable
			}
			agentID = candidate.AgentID
		}
	}
	if agentID == "" {
		return personaReplyActor{}, ErrPersonaReplyDeliveryUnavailable
	}
	return personaReplyActor{AgentID: agentID, Display: profile.Profile.DisplayName, InvokerHandle: identity.InvokerID}, nil
}
