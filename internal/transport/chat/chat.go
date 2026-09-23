// Package chat exposes the canonical conversation service over gRPC and its
// integration HTTP projection. It contains transport adaptation only; all
// authorization, idempotency, revision and persistence decisions stay in the
// collaboration service port.
package chat

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ProcedurePrefix is the canonical Connect/gRPC HTTP path prefix. The cell
// mounts this prefix after applying shared HTTP admission.
const ProcedurePrefix = "/hcmnext.chat.v1.ConversationService/"

// Send-path bounds. They are exported so a client can refuse an over-long
// draft before spending a round trip on it, and they are enforced here rather
// than only in the core so the limit holds on every composition, including a
// gRPC server assembled without the shared admission interceptor.
const (
	// MaxPostBodyBytes is the wire ceiling on one post body. It is the shared
	// transport string bound (internal/transport enforces the same 4096 bytes
	// on every string field of every request it validates), restated here so
	// chat has one published number instead of an implicit one.
	MaxPostBodyBytes = 4096
	// MaxPostBodyRunes is the character ceiling. It is lower than
	// [MaxPostBodyBytes] so the limit a person actually hits is a count of
	// characters rather than a count of bytes, which would make the same
	// visible message legal in English and illegal in Arabic or Japanese.
	MaxPostBodyRunes = 4000
	// MaxRequestBytes bounds a decoded Connect request body, matching the
	// canonical edge's own bound.
	MaxRequestBytes = 4 << 20
)

// Server-side page ceilings for the two RPCs whose wire contract carries no
// page size at all. Clamping here is not pagination: it bounds one response so
// a conversation with ten thousand pins cannot be turned into one reply, and
// it is the smallest change available until the core port grows a Page.
const (
	// MaxListPinsPageSize bounds one ListPins response.
	MaxListPinsPageSize = 200
	// MaxReferenceCandidates bounds one ResolveReferences response.
	MaxReferenceCandidates = 50
)

// Reason and rule identifiers for the request-shape refusals this transport
// owns. The trusted-field reason mirrors internal/transport's own so a caller
// that forges a principal gets the same answer whichever surface it forges it
// on.
const (
	reasonCallerSelectedAuthority = "trusted_context.caller_selected_authority"
	reasonInvalidArgument         = "chat.invalid_argument"
	ruleTrustedRequestBoundary    = "trusted_request_boundary.server_derived_field"
	ruleBodyBound                 = "chat.post_body_bound"
	ruleCursorConflict            = "chat.cursor_conflict"
	// reasonStreamSend names a watch that ended because the caller's own
	// connection could not be written to.
	reasonStreamSend = "chat.stream.send_failed"
	// reasonStreamFailure names a watch that ended on a condition no chat
	// sentinel covers. The cause stays in the unprojected diagnostic.
	reasonStreamFailure = "chat.stream.failure"
)

type Dependencies struct{ Service chatcore.ConversationService }
type server struct {
	chatv1.UnimplementedConversationServiceServer
	deps Dependencies
}

func Register(srv *grpc.Server, deps Dependencies) {
	chatv1.RegisterConversationServiceServer(srv, &server{deps: deps})
}

func NewHandler(deps Dependencies, opts ...connect.HandlerOption) http.Handler {
	s := &server{deps: deps}
	// A caller-supplied option still wins: the bound is prepended, not
	// appended, so a composition that needs a different ceiling can say so.
	opts = append([]connect.HandlerOption{connect.WithReadMaxBytes(MaxRequestBytes)}, opts...)
	mux := http.NewServeMux()
	// Connect is the HTTP projection of the same generated RPC methods. The
	// handlers call the gRPC implementation directly, preserving outcomes.
	registerUnary(mux, opts, chatv1.ConversationService_CreateConversation_FullMethodName, func(c context.Context, r *chatv1.CreateConversationRequest) (*chatv1.CreateConversationResponse, error) {
		return s.CreateConversation(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_ListConversations_FullMethodName, func(c context.Context, r *chatv1.ListConversationsRequest) (*chatv1.ListConversationsResponse, error) {
		return s.ListConversations(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_GetConversation_FullMethodName, func(c context.Context, r *chatv1.GetConversationRequest) (*chatv1.GetConversationResponse, error) {
		return s.GetConversation(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_UpdateConversation_FullMethodName, func(c context.Context, r *chatv1.UpdateConversationRequest) (*chatv1.UpdateConversationResponse, error) {
		return s.UpdateConversation(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_ListMemberships_FullMethodName, func(c context.Context, r *chatv1.ListMembershipsRequest) (*chatv1.ListMembershipsResponse, error) {
		return s.ListMemberships(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_AddMembership_FullMethodName, func(c context.Context, r *chatv1.AddMembershipRequest) (*chatv1.AddMembershipResponse, error) {
		return s.AddMembership(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_RemoveMembership_FullMethodName, func(c context.Context, r *chatv1.RemoveMembershipRequest) (*chatv1.RemoveMembershipResponse, error) {
		return s.RemoveMembership(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_SendPost_FullMethodName, func(c context.Context, r *chatv1.SendPostRequest) (*chatv1.SendPostResponse, error) {
		return s.SendPost(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_ListPosts_FullMethodName, func(c context.Context, r *chatv1.ListPostsRequest) (*chatv1.ListPostsResponse, error) {
		return s.ListPosts(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_EditPost_FullMethodName, func(c context.Context, r *chatv1.EditPostRequest) (*chatv1.EditPostResponse, error) {
		return s.EditPost(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_DeletePost_FullMethodName, func(c context.Context, r *chatv1.DeletePostRequest) (*chatv1.DeletePostResponse, error) {
		return s.DeletePost(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_Search_FullMethodName, func(c context.Context, r *chatv1.SearchRequest) (*chatv1.SearchResponse, error) {
		return s.Search(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_GetReadState_FullMethodName, func(c context.Context, r *chatv1.GetReadStateRequest) (*chatv1.GetReadStateResponse, error) {
		return s.GetReadState(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_UpdateReadState_FullMethodName, func(c context.Context, r *chatv1.UpdateReadStateRequest) (*chatv1.UpdateReadStateResponse, error) {
		return s.UpdateReadState(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_GetPreferences_FullMethodName, func(c context.Context, r *chatv1.GetPreferencesRequest) (*chatv1.GetPreferencesResponse, error) {
		return s.GetPreferences(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_UpdatePreferences_FullMethodName, func(c context.Context, r *chatv1.UpdatePreferencesRequest) (*chatv1.UpdatePreferencesResponse, error) {
		return s.UpdatePreferences(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_AddReaction_FullMethodName, func(c context.Context, r *chatv1.AddReactionRequest) (*chatv1.AddReactionResponse, error) {
		return s.AddReaction(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_RemoveReaction_FullMethodName, func(c context.Context, r *chatv1.RemoveReactionRequest) (*chatv1.RemoveReactionResponse, error) {
		return s.RemoveReaction(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_ListReactions_FullMethodName, func(c context.Context, r *chatv1.ListReactionsRequest) (*chatv1.ListReactionsResponse, error) {
		return s.ListReactions(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_PinPost_FullMethodName, func(c context.Context, r *chatv1.PinPostRequest) (*chatv1.PinPostResponse, error) {
		return s.PinPost(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_UnpinPost_FullMethodName, func(c context.Context, r *chatv1.UnpinPostRequest) (*chatv1.UnpinPostResponse, error) {
		return s.UnpinPost(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_ListPins_FullMethodName, func(c context.Context, r *chatv1.ListPinsRequest) (*chatv1.ListPinsResponse, error) {
		return s.ListPins(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_ResolveReferences_FullMethodName, func(c context.Context, r *chatv1.ResolveReferencesRequest) (*chatv1.ResolveReferencesResponse, error) {
		return s.ResolveReferences(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_CreateShareLink_FullMethodName, func(c context.Context, r *chatv1.CreateShareLinkRequest) (*chatv1.CreateShareLinkResponse, error) {
		return s.CreateShareLink(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_ResolveShareLink_FullMethodName, func(c context.Context, r *chatv1.ResolveShareLinkRequest) (*chatv1.ResolveShareLinkResponse, error) {
		return s.ResolveShareLink(c, r)
	})
	registerUnary(mux, opts, chatv1.ConversationService_ForwardPost_FullMethodName, func(c context.Context, r *chatv1.ForwardPostRequest) (*chatv1.ForwardPostResponse, error) {
		return s.ForwardPost(c, r)
	})
	mux.Handle(chatv1.ConversationService_WatchConversation_FullMethodName, connect.NewServerStreamHandlerSimple(chatv1.ConversationService_WatchConversation_FullMethodName, func(c context.Context, r *chatv1.WatchConversationRequest, stream *connect.ServerStream[chatv1.WatchConversationResponse]) error {
		return s.stream(c, r, stream.Send)
	}, opts...))
	return mux
}

func registerUnary[Req, Res any](mux *http.ServeMux, opts []connect.HandlerOption, proc string, fn func(context.Context, *Req) (*Res, error)) {
	mux.Handle(proc, connect.NewUnaryHandler(proc, func(c context.Context, r *connect.Request[Req]) (*connect.Response[Res], error) {
		v, err := fn(c, r.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(v), nil
	}, opts...))
}

func trusted(ctx context.Context) (*trust.Principal, error) {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil {
		return nil, envelope.New(envelope.CodeUnauthenticated, "chat.no_principal", "the request carries no authenticated principal")
	}
	return p, nil
}
func principal(ctx context.Context) (chatcore.Principal, error) {
	p, err := trusted(ctx)
	if err != nil {
		return chatcore.Principal{}, err
	}
	return chatcore.Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject(), Roles: p.Roles()}, nil
}

// callErr projects a core condition onto the owned error model.
//
// Two mappings matter more than the rest. An unclassified error is a fault in
// this process, so it becomes a non-retryable internal condition rather than
// UNAVAILABLE, which would have a client retry a programming error until it
// gave up. Cancellation and deadline expiry are transport facts and keep the
// spellings internal/transport already publishes for them, so the same client
// sees the same answer whichever service it was talking to.
func callErr(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := envelope.As(err); ok {
		return err
	}
	code, reason := envelope.CodeUnspecified, "chat.internal_error"
	switch {
	case errors.Is(err, chatcore.ErrInvalidArgument):
		code, reason = envelope.CodeInvalidArgument, reasonInvalidArgument
	case errors.Is(err, chatcore.ErrUnauthenticated):
		code, reason = envelope.CodeUnauthenticated, "chat.unauthenticated"
	case errors.Is(err, chatcore.ErrPermissionDenied):
		code, reason = envelope.CodePermissionDenied, "chat.permission_denied"
	case errors.Is(err, chatcore.ErrNotFound):
		code, reason = envelope.CodeNotFound, "chat.not_found"
	case errors.Is(err, chatcore.ErrAlreadyExists):
		code, reason = envelope.CodeAlreadyExists, "chat.already_exists"
	case errors.Is(err, chatcore.ErrConflict):
		code, reason = envelope.CodeAborted, "chat.conflict"
	case errors.Is(err, chatcore.ErrUnavailable):
		code, reason = envelope.CodeUnavailable, "chat.service.unavailable"
	case errors.Is(err, context.DeadlineExceeded):
		code, reason = envelope.CodeDeadlineExceeded, "transport.deadline_exceeded"
	case errors.Is(err, context.Canceled):
		code, reason = envelope.CodeUnavailable, "transport.request_canceled"
	}
	owned := envelope.New(code, reason, "the chat operation could not be completed")
	if code == envelope.CodeUnspecified {
		// The cause never reaches the caller, but it must not be thrown away
		// either: the diagnostic is what a log record carries.
		owned.WithDiagnostic(err)
	}
	return owned
}

// serverDerived refuses a caller-supplied value for a field the server owns.
// Refusing rather than overwriting is the point: a client that believes it is
// posting as somebody else has to be told it is wrong, not quietly corrected.
func serverDerived(field, what string) error {
	return envelope.New(envelope.CodeInvalidArgument, reasonCallerSelectedAuthority,
		"the request may not select trusted context").
		WithViolation(field, what, ruleTrustedRequestBoundary)
}

// prep resolves the authenticated principal and refuses a request that tried
// to name one itself.
func (s *server) prep(ctx context.Context, supplied *chatv1.Principal) (chatcore.Principal, error) {
	p, err := principal(ctx)
	if err != nil {
		return chatcore.Principal{}, err
	}
	if s.deps.Service == nil {
		return chatcore.Principal{}, envelope.New(envelope.CodeUnavailable, "chat.service.unavailable", "chat service is not configured")
	}
	if supplied != nil {
		return chatcore.Principal{}, serverDerived("principal",
			"the acting principal is derived server-side from the verified credential")
	}
	return p, nil
}
func page(cursor string, size uint32) chatcore.Page {
	return chatcore.Page{Cursor: cursor, PageSize: size}
}

// checkBody enforces the published send-path bounds. The core refuses a blank
// body; nothing below the transport refuses an enormous one.
func checkBody(body string) error {
	switch {
	case len(body) > MaxPostBodyBytes:
		return envelope.New(envelope.CodeInvalidArgument, reasonInvalidArgument,
			"the post body exceeds the accepted size").
			WithViolation("body", "the body must not exceed "+strconv.Itoa(MaxPostBodyBytes)+" bytes", ruleBodyBound)
	case utf8.RuneCountInString(body) > MaxPostBodyRunes:
		return envelope.New(envelope.CodeInvalidArgument, reasonInvalidArgument,
			"the post body exceeds the accepted length").
			WithViolation("body", "the body must not exceed "+strconv.Itoa(MaxPostBodyRunes)+" characters", ruleBodyBound)
	}
	return nil
}

func (s *server) CreateConversation(c context.Context, r *chatv1.CreateConversationRequest) (*chatv1.CreateConversationResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	members := make([]chatcore.MemberRef, 0, len(r.GetMembers()))
	for _, m := range r.GetMembers() {
		if m != nil {
			members = append(members, chatcore.MemberRef{TenantID: m.GetTenantId(), SubjectID: m.GetSubjectId()})
		}
	}
	v, e := s.deps.Service.CreateConversation(c, chatcore.CreateConversationRequest{Principal: p, TenantID: r.GetTenantId(), Kind: kind(r.GetKind()), Name: r.GetName(), OwnerID: r.GetOwnerId(), Members: members, IdempotencyKey: r.GetIdempotencyKey()})
	if e != nil {
		return nil, callErr(e)
	}
	return &chatv1.CreateConversationResponse{Conversation: conversation(v)}, nil
}
func (s *server) ListConversations(c context.Context, r *chatv1.ListConversationsRequest) (*chatv1.ListConversationsResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.ListConversations(c, chatcore.ListConversationsRequest{Principal: p, TenantID: r.GetTenantId(), Page: page(r.GetCursor(), r.GetPageSize()), IncludeDiscoverable: r.GetIncludeDiscoverable()})
	if e != nil {
		return nil, callErr(e)
	}
	o := &chatv1.ListConversationsResponse{NextCursor: v.NextCursor}
	for _, x := range v.Conversations {
		o.Conversations = append(o.Conversations, conversation(x))
	}
	return o, nil
}
func (s *server) GetConversation(c context.Context, r *chatv1.GetConversationRequest) (*chatv1.GetConversationResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.GetConversation(c, chatcore.GetConversationRequest{Principal: p, TenantID: r.GetTenantId(), ConversationID: r.GetConversationId()})
	if e != nil {
		return nil, callErr(e)
	}
	return &chatv1.GetConversationResponse{Conversation: conversation(v)}, nil
}
func (s *server) UpdateConversation(c context.Context, r *chatv1.UpdateConversationRequest) (*chatv1.UpdateConversationResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.UpdateConversation(c, chatcore.UpdateConversationRequest{Principal: p, Conversation: conversationIn(r.GetConversation()), ExpectedRevision: r.GetExpectedRevision()})
	if e != nil {
		return nil, callErr(e)
	}
	return &chatv1.UpdateConversationResponse{Conversation: conversation(v)}, nil
}
func (s *server) ListMemberships(c context.Context, r *chatv1.ListMembershipsRequest) (*chatv1.ListMembershipsResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.ListMemberships(c, chatcore.ListMembershipsRequest{Principal: p, TenantID: r.GetTenantId(), ConversationID: r.GetConversationId(), Page: page(r.GetCursor(), r.GetPageSize())})
	if e != nil {
		return nil, callErr(e)
	}
	o := &chatv1.ListMembershipsResponse{NextCursor: v.NextCursor}
	for _, x := range v.Memberships {
		o.Memberships = append(o.Memberships, membership(x))
	}
	return o, nil
}
func (s *server) AddMembership(c context.Context, r *chatv1.AddMembershipRequest) (*chatv1.AddMembershipResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.AddMembership(c, chatcore.AddMembershipRequest{Principal: p, Membership: membershipIn(r.GetMembership())})
	if e != nil {
		return nil, callErr(e)
	}
	return &chatv1.AddMembershipResponse{Membership: membership(v)}, nil
}
func (s *server) RemoveMembership(c context.Context, r *chatv1.RemoveMembershipRequest) (*chatv1.RemoveMembershipResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.RemoveMembership(c, chatcore.RemoveMembershipRequest{Principal: p, TenantID: r.GetTenantId(), HomeTenantID: r.GetHomeTenantId(), ConversationID: r.GetConversationId(), SubjectID: r.GetSubjectId(), ExpectedRevision: r.GetExpectedRevision()})
	if e != nil {
		return nil, callErr(e)
	}
	return &chatv1.RemoveMembershipResponse{Membership: membership(v)}, nil
}
func (s *server) SendPost(c context.Context, r *chatv1.SendPostRequest) (*chatv1.SendPostResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	if e := checkBody(r.GetBody()); e != nil {
		return nil, e
	}
	if r.GetSourceAttribution() != nil {
		return nil, serverDerived("source_attribution",
			"attribution is derived server-side from the source post; use ForwardPost")
	}
	base := chatcore.SendPostRequest{Principal: p, TenantID: r.GetTenantId(), ConversationID: r.GetConversationId(), Body: r.GetBody(), ParentID: r.GetParentId(), IdempotencyKey: r.GetIdempotencyKey(), References: referencesIn(r.GetReferences())}
	var v chatcore.Post
	if len(r.GetReferences()) > 0 {
		refSvc, ok := s.deps.Service.(chatcore.ReferenceService)
		if !ok {
			return nil, callErr(chatcore.ErrUnavailable)
		}
		v, e = refSvc.SendPostWithReferences(c, chatcore.SendPostWithReferencesRequest{SendPostRequest: base, References: base.References})
	} else {
		v, e = s.deps.Service.SendPost(c, base)
	}
	if e != nil {
		return nil, callErr(e)
	}
	return &chatv1.SendPostResponse{Post: post(v)}, nil
}
func (s *server) ListPosts(c context.Context, r *chatv1.ListPostsRequest) (*chatv1.ListPostsResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.ListPosts(c, chatcore.ListPostsRequest{Principal: p, TenantID: r.GetTenantId(), ConversationID: r.GetConversationId(), AfterSequence: r.GetAfterSequence(), Page: page(r.GetCursor(), r.GetPageSize()), Descending: r.GetDescending(), BeforeSequence: r.GetBeforeSequence()})
	if e != nil {
		return nil, callErr(e)
	}
	o := &chatv1.ListPostsResponse{NextCursor: v.NextCursor}
	for _, x := range v.Posts {
		o.Posts = append(o.Posts, post(x))
	}
	return o, nil
}
func (s *server) EditPost(c context.Context, r *chatv1.EditPostRequest) (*chatv1.EditPostResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	x := r.GetPost()
	v, e := s.deps.Service.EditPost(c, chatcore.EditPostRequest{Principal: p, PostID: x.GetId(), TenantID: x.GetTenantId(), ConversationID: x.GetConversationId(), Body: r.GetBody(), ExpectedRevision: r.GetExpectedRevision()})
	if e != nil {
		return nil, callErr(e)
	}
	return &chatv1.EditPostResponse{Post: post(v)}, nil
}
func (s *server) DeletePost(c context.Context, r *chatv1.DeletePostRequest) (*chatv1.DeletePostResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.DeletePost(c, chatcore.DeletePostRequest{Principal: p, TenantID: r.GetTenantId(), ConversationID: r.GetConversationId(), PostID: r.GetPostId(), ExpectedRevision: r.GetExpectedRevision()})
	if e != nil {
		return nil, callErr(e)
	}
	return &chatv1.DeletePostResponse{Post: post(v)}, nil
}
func (s *server) Search(c context.Context, r *chatv1.SearchRequest) (*chatv1.SearchResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.Search(c, chatcore.SearchRequest{Principal: p, TenantID: r.GetTenantId(), Query: r.GetQuery(), ConversationID: r.GetConversationId(), AuthorID: r.GetAuthorId(), ChannelCursor: r.GetChannelCursor(), Page: page(r.GetCursor(), r.GetPageSize())})
	if e != nil {
		return nil, callErr(e)
	}
	o := &chatv1.SearchResponse{NextCursor: v.NextCursor, ChannelNextCursor: v.ChannelNextCursor}
	for _, x := range v.Results {
		o.Results = append(o.Results, &chatv1.SearchResult{Post: post(x.Post), ConversationName: x.ConversationName})
	}
	for _, x := range v.Channels {
		o.Channels = append(o.Channels, &chatv1.ChannelSearchResult{ConversationId: x.ConversationID, Name: x.Name, Kind: string(x.Kind), Joined: x.Joined})
	}
	return o, nil
}
func (s *server) GetReadState(c context.Context, r *chatv1.GetReadStateRequest) (*chatv1.GetReadStateResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.GetReadState(c, chatcore.GetReadStateRequest{Principal: p, TenantID: r.GetTenantId(), ConversationID: r.GetConversationId()})
	if e != nil {
		return nil, callErr(e)
	}
	return &chatv1.GetReadStateResponse{State: readState(v)}, nil
}
func (s *server) UpdateReadState(c context.Context, r *chatv1.UpdateReadStateRequest) (*chatv1.UpdateReadStateResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.UpdateReadState(c, chatcore.UpdateReadStateRequest{Principal: p, ReadState: readStateIn(r.GetState()), ExpectedRevision: r.GetExpectedRevision()})
	if e != nil {
		return nil, callErr(e)
	}
	return &chatv1.UpdateReadStateResponse{State: readState(v)}, nil
}
func (s *server) GetPreferences(c context.Context, r *chatv1.GetPreferencesRequest) (*chatv1.GetPreferencesResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.GetPreferences(c, chatcore.GetPreferencesRequest{Principal: p, TenantID: r.GetTenantId(), ConversationID: r.GetConversationId()})
	if e != nil {
		return nil, callErr(e)
	}
	return &chatv1.GetPreferencesResponse{Preferences: prefs(v)}, nil
}
func (s *server) UpdatePreferences(c context.Context, r *chatv1.UpdatePreferencesRequest) (*chatv1.UpdatePreferencesResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.UpdatePreferences(c, chatcore.UpdatePreferencesRequest{Principal: p, Preferences: prefsIn(r.GetPreferences()), ExpectedRevision: r.GetExpectedRevision()})
	if e != nil {
		return nil, callErr(e)
	}
	return &chatv1.UpdatePreferencesResponse{Preferences: prefs(v)}, nil
}
func (s *server) AddReaction(c context.Context, r *chatv1.AddReactionRequest) (*chatv1.AddReactionResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.AddReaction(c, chatcore.AddReactionRequest{Principal: p, Reaction: reactionIn(r.GetReaction())})
	if e != nil {
		return nil, callErr(e)
	}
	return &chatv1.AddReactionResponse{Reaction: reaction(v)}, nil
}
func (s *server) RemoveReaction(c context.Context, r *chatv1.RemoveReactionRequest) (*chatv1.RemoveReactionResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	e = s.deps.Service.RemoveReaction(c, chatcore.RemoveReactionRequest{Principal: p, TenantID: r.GetTenantId(), ConversationID: r.GetConversationId(), PostID: r.GetPostId(), Emoji: r.GetEmoji()})
	return &chatv1.RemoveReactionResponse{}, callErr(e)
}
func (s *server) ListReactions(c context.Context, r *chatv1.ListReactionsRequest) (*chatv1.ListReactionsResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.ListReactions(c, chatcore.ListReactionsRequest{Principal: p, TenantID: r.GetTenantId(), ConversationID: r.GetConversationId(), PostID: r.GetPostId(), Page: chatcore.Page{PageSize: r.GetPageSize(), Cursor: r.GetCursor()}})
	if e != nil {
		return nil, callErr(e)
	}
	out := &chatv1.ListReactionsResponse{NextCursor: v.NextCursor}
	for _, x := range v.Reactions {
		out.Reactions = append(out.Reactions, reaction(x))
	}
	return out, nil
}
func (s *server) PinPost(c context.Context, r *chatv1.PinPostRequest) (*chatv1.PinPostResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.PinPost(c, chatcore.PinPostRequest{Principal: p, Pin: pinIn(r.GetPin())})
	if e != nil {
		return nil, callErr(e)
	}
	return &chatv1.PinPostResponse{Pin: pin(v)}, nil
}
func (s *server) UnpinPost(c context.Context, r *chatv1.UnpinPostRequest) (*chatv1.UnpinPostResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	e = s.deps.Service.UnpinPost(c, chatcore.UnpinPostRequest{Principal: p, TenantID: r.GetTenantId(), ConversationID: r.GetConversationId(), PostID: r.GetPostId(), ExpectedRevision: r.GetExpectedRevision()})
	return &chatv1.UnpinPostResponse{}, callErr(e)
}
func (s *server) ListPins(c context.Context, r *chatv1.ListPinsRequest) (*chatv1.ListPinsResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.ListPins(c, chatcore.ListPinsRequest{Principal: p, TenantID: r.GetTenantId(), ConversationID: r.GetConversationId()})
	if e != nil {
		return nil, callErr(e)
	}
	if len(v) > MaxListPinsPageSize {
		v = v[:MaxListPinsPageSize]
	}
	o := &chatv1.ListPinsResponse{}
	for _, x := range v {
		o.Pins = append(o.Pins, pin(x))
	}
	return o, nil
}
func (s *server) WatchConversation(r *chatv1.WatchConversationRequest, stream chatv1.ConversationService_WatchConversationServer) error {
	return s.stream(stream.Context(), r, stream.Send)
}

// watchMaxLifetime is the ceiling on one chat watch, matching
// internal/transport/journey. A client that wants to watch for longer opens
// another stream carrying the cursor it now holds; nothing in this process
// holds a stream open indefinitely. Reaching it is not an error.
const watchMaxLifetime = 15 * time.Minute

// stream drives one WatchConversation stream for both transports.
//
// Three things the two hand-written loops it replaces did not do. It selects
// on the caller's context, so a client that goes away stops the send loop
// instead of blocking on a channel nobody reads. It stops at
// [watchMaxLifetime]. And when a send fails it drains the producer's channel
// before returning, because the core closes that channel from a goroutine that
// would otherwise block forever on a send nobody will receive.
func (s *server) stream(c context.Context, r *chatv1.WatchConversationRequest, send func(*chatv1.WatchConversationResponse) error) error {
	return streamErr(s.run(c, r, send))
}

// streamErr owns every error that leaves a watch, on both transports.
//
// The gRPC streaming handler and the Connect one share [server.run], so this is
// the single place the two paths can be given the same answer. Without it a
// failed watch left here unowned and the edge coerced it into
// transport.unclassified_failure with no reason at all: the request log recorded
// one nameless UNAVAILABLE for a bad cursor, a revoked membership, a saturated
// lane and a client that hung up alike.
func streamErr(err error) error {
	if err == nil {
		return nil
	}
	owned := err
	if _, already := envelope.As(err); !already {
		owned = callErr(err)
	}
	if e, named := envelope.As(owned); named && e.Code() == envelope.CodeUnspecified {
		// callErr's fallback is CodeUnspecified, which a log record reads as
		// success. A stream that ended on an unrecognised condition did not
		// succeed, so it is named as a retryable stream failure and keeps its
		// diagnostic for the record's classification.
		return envelope.New(envelope.CodeUnavailable, reasonStreamFailure,
			"the watch stream ended on an unrecognised condition").WithDiagnostic(err)
	}
	return owned
}

// run drives one WatchConversation stream for both transports.
func (s *server) run(c context.Context, r *chatv1.WatchConversationRequest, send func(*chatv1.WatchConversationResponse) error) error {
	ch, fail, err := s.watch(c, r)
	if err != nil {
		return err
	}
	ceiling := time.NewTimer(watchMaxLifetime)
	defer ceiling.Stop()
	for {
		select {
		case <-c.Done():
			go drain(ch)
			return nil
		case <-ceiling.C:
			// The declared ceiling, reached. The feed was healthy the whole
			// time, so this is a completed stream and not a failed one.
			go drain(ch)
			return nil
		case e, ok := <-ch:
			if !ok {
				// The producer stopped. A closed channel alone does not say
				// whether the feed completed or failed, and reporting every
				// failure as a clean end of stream is what made a browser see an
				// EOF milliseconds after opening and retry until it gave up. The
				// terminal cause, when the owner reports one, is the answer.
				return terminalErr(fail)
			}
			if err := send(&chatv1.WatchConversationResponse{Event: event(e.Event), ResumeCursor: e.ResumeCursor}); err != nil {
				// The client went away mid-send. The producer still needs
				// somebody to read until it closes, and the failure is named
				// rather than handed back raw: a nameless UNAVAILABLE in the
				// request log cannot be told apart from a broken subscribe.
				go drain(ch)
				return envelope.New(envelope.CodeUnavailable, reasonStreamSend,
					"the watch stream could not be written to the caller").WithDiagnostic(err)
			}
		}
	}
}

// drain reads a watch channel to completion so its producer can finish.
func drain(ch <-chan chatcore.WatchEvent) {
	for range ch {
	}
}

// terminalErr reads the one terminal cause a reporting owner publishes when its
// stream stops. An owner that reports nothing, or a feed that simply ended,
// yields nil and the stream completes.
func terminalErr(fail <-chan error) error {
	if fail == nil {
		return nil
	}
	select {
	case err, ok := <-fail:
		if !ok || err == nil {
			return nil
		}
		return err
	default:
		return nil
	}
}

func (s *server) watch(c context.Context, r *chatv1.WatchConversationRequest) (<-chan chatcore.WatchEvent, <-chan error, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, nil, e
	}
	if verified, ok := trust.FromContext(c); ok && verified != nil && (verified.SubjectKind() == trust.SubjectKindAgent || verified.SubjectKind() == trust.SubjectKindIntegration) && r.GetAfterSequence() != 0 {
		return nil, nil, callErr(chatcore.ErrInvalidArgument)
	}
	after, resume, e := watchStart(r.GetAfterSequence(), r.GetResumeCursor())
	if e != nil {
		return nil, nil, e
	}
	req := chatcore.WatchConversationRequest{Principal: p, TenantID: r.GetTenantId(), ConversationID: r.GetConversationId(), AfterSequence: after, ResumeCursor: resume}
	// A failed subscribe used to leave here unowned, so the edge coerced it into
	// transport.unclassified_failure and the log record said nothing about which
	// condition it was. It is classified like every other chat outcome instead,
	// and an unrecognised cause keeps its diagnostic.
	if reporting, ok := s.deps.Service.(chatcore.ErrorReportingWatcher); ok {
		ch, fail, watchErr := reporting.WatchConversationWithErrors(c, req)
		if watchErr != nil {
			return nil, nil, callErr(watchErr)
		}
		return ch, fail, nil
	}
	ch, e := s.deps.Service.WatchConversation(c, req)
	if e != nil {
		return nil, nil, callErr(e)
	}
	return ch, nil, nil
}

// watchStart separates the request's two ways of naming a starting position.
//
// after_sequence is the plain spelling and resume_cursor the opaque one. They
// used to be collapsed here, with after_sequence rendered as a decimal string
// and handed over as if it were a resume cursor; the stream then failed to
// verify it as a signed token and every such subscribe died as an internal
// failure. They now travel as the two distinct fields they are. Supplying both
// is refused rather than resolved by precedence, because a client that
// disagrees with itself about where it left off should be told so.
func watchStart(after uint64, resume string) (uint64, string, error) {
	if after != 0 && resume != "" {
		return 0, "", envelope.New(envelope.CodeInvalidArgument, reasonInvalidArgument,
			"the request names two starting positions").
			WithViolation("after_sequence", "supply either after_sequence or resume_cursor, not both", ruleCursorConflict)
	}
	return after, resume, nil
}

func (s *server) ResolveReferences(c context.Context, r *chatv1.ResolveReferencesRequest) (*chatv1.ResolveReferencesResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	refSvc, ok := s.deps.Service.(chatcore.ReferenceService)
	if !ok {
		return nil, callErr(chatcore.ErrUnavailable)
	}
	v, e := refSvc.SuggestReferences(c, chatcore.SuggestReferencesRequest{Principal: p, TenantID: r.GetTenantId(), ConversationID: r.GetConversationId(), Kind: referenceKind(r.GetKind()), Query: r.GetQuery()})
	if e != nil {
		return nil, callErr(e)
	}
	if len(v) > MaxReferenceCandidates {
		v = v[:MaxReferenceCandidates]
	}
	o := &chatv1.ResolveReferencesResponse{}
	for _, x := range v {
		o.Candidates = append(o.Candidates, &chatv1.ReferenceCandidate{Reference: reference(x.Reference), Eligible: x.Eligible})
	}
	return o, nil
}

func (s *server) CreateShareLink(c context.Context, r *chatv1.CreateShareLinkRequest) (*chatv1.CreateShareLinkResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	refSvc, ok := s.deps.Service.(chatcore.ReferenceService)
	if !ok {
		return nil, callErr(chatcore.ErrUnavailable)
	}
	v, e := refSvc.CreateShareLink(c, p, r.GetTenantId(), r.GetConversationId(), r.GetPostId())
	if e != nil {
		return nil, callErr(e)
	}
	return &chatv1.CreateShareLinkResponse{Link: link(v)}, nil
}

func (s *server) ResolveShareLink(c context.Context, r *chatv1.ResolveShareLinkRequest) (*chatv1.ResolveShareLinkResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	refSvc, ok := s.deps.Service.(chatcore.ReferenceService)
	if !ok {
		return nil, callErr(chatcore.ErrUnavailable)
	}
	v, postValue, e := refSvc.ResolveShareLink(c, p, r.GetToken())
	if e != nil {
		return nil, callErr(e)
	}
	o := &chatv1.ResolveShareLinkResponse{Conversation: conversation(v)}
	if postValue != nil {
		o.Post = post(*postValue)
	}
	return o, nil
}

func (s *server) ForwardPost(c context.Context, r *chatv1.ForwardPostRequest) (*chatv1.ForwardPostResponse, error) {
	p, e := s.prep(c, r.GetPrincipal())
	if e != nil {
		return nil, e
	}
	refSvc, ok := s.deps.Service.(chatcore.ReferenceService)
	if !ok {
		return nil, callErr(chatcore.ErrUnavailable)
	}
	if r.GetSourceAttribution() != nil {
		return nil, serverDerived("source_attribution",
			"attribution is derived server-side from the source post")
	}
	v, e := refSvc.ForwardPost(c, chatcore.ForwardPostRequest{Principal: p, SourceTenantID: r.GetSourceTenantId(), SourceConversationID: r.GetSourceConversationId(), SourcePostID: r.GetSourcePostId(), DestinationTenantID: r.GetDestinationTenantId(), DestinationConversationID: r.GetDestinationConversationId(), IdempotencyKey: r.GetIdempotencyKey()})
	if e != nil {
		return nil, callErr(e)
	}
	return &chatv1.ForwardPostResponse{Post: post(v)}, nil
}

func kind(v chatv1.ConversationKind) chatcore.ConversationKind {
	switch v {
	case chatv1.ConversationKind_CONVERSATION_KIND_PUBLIC_CHANNEL:
		return chatcore.PublicChannel
	case chatv1.ConversationKind_CONVERSATION_KIND_PRIVATE_CHANNEL:
		return chatcore.PrivateChannel
	case chatv1.ConversationKind_CONVERSATION_KIND_DIRECT:
		return chatcore.Direct
	case chatv1.ConversationKind_CONVERSATION_KIND_GROUP:
		return chatcore.Group
	default:
		return ""
	}
}
func role(v chatv1.MembershipRole) chatcore.MembershipRole {
	switch v {
	case chatv1.MembershipRole_MEMBERSHIP_ROLE_MEMBER:
		return chatcore.Member
	case chatv1.MembershipRole_MEMBERSHIP_ROLE_MANAGER:
		return chatcore.Manager
	default:
		return ""
	}
}
func history(v chatv1.ReadHistoryFrom) chatcore.ReadHistoryFrom {
	switch v {
	case chatv1.ReadHistoryFrom_READ_HISTORY_FROM_NONE:
		return chatcore.NoHistory
	case chatv1.ReadHistoryFrom_READ_HISTORY_FROM_JOIN:
		return chatcore.FromJoin
	case chatv1.ReadHistoryFrom_READ_HISTORY_FROM_FULL:
		return chatcore.FullHistory
	default:
		return ""
	}
}

// kindOut, roleOut, historyOut and eventKindOut are the outbound halves of the
// enum projections.
//
// They are switches rather than a lookup in the generated <Enum>_value map
// keyed by a concatenated prefix, because that lookup answers zero - the
// UNSPECIFIED member - for every core value whose spelling the proto does not
// share, and zero is indistinguishable from "the field was not set". A switch
// that has to be extended when either side gains a member is the point.
func kindOut(v chatcore.ConversationKind) chatv1.ConversationKind {
	switch v {
	case chatcore.PublicChannel:
		return chatv1.ConversationKind_CONVERSATION_KIND_PUBLIC_CHANNEL
	case chatcore.PrivateChannel:
		return chatv1.ConversationKind_CONVERSATION_KIND_PRIVATE_CHANNEL
	case chatcore.Direct:
		return chatv1.ConversationKind_CONVERSATION_KIND_DIRECT
	case chatcore.Group:
		return chatv1.ConversationKind_CONVERSATION_KIND_GROUP
	default:
		return chatv1.ConversationKind_CONVERSATION_KIND_UNSPECIFIED
	}
}
func roleOut(v chatcore.MembershipRole) chatv1.MembershipRole {
	switch v {
	case chatcore.Member:
		return chatv1.MembershipRole_MEMBERSHIP_ROLE_MEMBER
	case chatcore.Manager:
		return chatv1.MembershipRole_MEMBERSHIP_ROLE_MANAGER
	default:
		return chatv1.MembershipRole_MEMBERSHIP_ROLE_UNSPECIFIED
	}
}
func historyOut(v chatcore.ReadHistoryFrom) chatv1.ReadHistoryFrom {
	switch v {
	case chatcore.NoHistory:
		return chatv1.ReadHistoryFrom_READ_HISTORY_FROM_NONE
	case chatcore.FromJoin:
		return chatv1.ReadHistoryFrom_READ_HISTORY_FROM_JOIN
	case chatcore.FullHistory:
		return chatv1.ReadHistoryFrom_READ_HISTORY_FROM_FULL
	default:
		return chatv1.ReadHistoryFrom_READ_HISTORY_FROM_UNSPECIFIED
	}
}
func eventKindOut(v chatcore.ConversationEventKind) chatv1.ConversationEventKind {
	switch v {
	case chatcore.PostCreated:
		return chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_CREATED
	case chatcore.PostEdited:
		return chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_EDITED
	case chatcore.PostDeleted:
		return chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_POST_DELETED
	case chatcore.MembershipChanged:
		return chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_MEMBERSHIP_CHANGED
	case chatcore.ConversationUpdated:
		return chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_CONVERSATION_UPDATED
	case chatcore.ReactionChanged:
		return chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_REACTION_CHANGED
	case chatcore.PinChanged:
		return chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_PIN_CHANGED
	default:
		return chatv1.ConversationEventKind_CONVERSATION_EVENT_KIND_UNSPECIFIED
	}
}
func referenceKindOut(v chatcore.ReferenceKind) chatv1.ReferenceKind {
	switch v {
	case chatcore.PersonMention:
		return chatv1.ReferenceKind_REFERENCE_KIND_PERSON_MENTION
	case chatcore.AgentMention:
		return chatv1.ReferenceKind_REFERENCE_KIND_AGENT_MENTION
	case chatcore.ConversationMention:
		return chatv1.ReferenceKind_REFERENCE_KIND_CONVERSATION_REFERENCE
	case chatcore.MediaAttachment:
		return chatv1.ReferenceKind_REFERENCE_KIND_MEDIA
	default:
		return chatv1.ReferenceKind_REFERENCE_KIND_UNSPECIFIED
	}
}
func conversation(v chatcore.Conversation) *chatv1.Conversation {
	return &chatv1.Conversation{Id: v.ID, TenantId: v.TenantID, Kind: kindOut(v.Kind), Name: v.Name, OwnerId: v.OwnerID, Revision: v.Revision, Archived: v.Archived, Joined: v.Joined, MemberCount: v.MemberCount, LastActivityAt: ts(v.LastActivityAt)}
}

// conversationIn deliberately drops member_count and last_activity_at. They are
// derived by the store from the membership and post tables; accepting them from
// a caller would let an update echo a count nobody counted.
func conversationIn(v *chatv1.Conversation) chatcore.Conversation {
	if v == nil {
		return chatcore.Conversation{}
	}
	return chatcore.Conversation{ID: v.GetId(), TenantID: v.GetTenantId(), Kind: kind(v.GetKind()), Name: v.GetName(), OwnerID: v.GetOwnerId(), Revision: v.GetRevision(), Archived: v.GetArchived(), Joined: v.GetJoined()}
}
func membership(v chatcore.Membership) *chatv1.Membership {
	return &chatv1.Membership{ConversationId: v.ConversationID, HomeTenantId: v.HomeTenantID, SubjectId: v.SubjectID, Role: roleOut(v.Role), JoinedAt: ts(v.JoinedAt), LeftAt: ts(v.LeftAt), HistoryVisibility: historyOut(v.HistoryVisibility), Revision: v.Revision}
}
func membershipIn(v *chatv1.Membership) chatcore.Membership {
	if v == nil {
		return chatcore.Membership{}
	}
	return chatcore.Membership{ConversationID: v.GetConversationId(), HomeTenantID: v.GetHomeTenantId(), TenantID: v.GetHomeTenantId(), SubjectID: v.GetSubjectId(), Role: role(v.GetRole()), JoinedAt: timeIn(v.GetJoinedAt()), LeftAt: timeIn(v.GetLeftAt()), HistoryVisibility: history(v.GetHistoryVisibility()), Revision: v.GetRevision()}
}
func post(v chatcore.Post) *chatv1.Post {
	o := &chatv1.Post{Id: v.ID, ConversationId: v.ConversationID, TenantId: v.TenantID, AuthorId: v.AuthorID, AuthorHomeTenantId: v.AuthorHomeTenantID, Body: v.Body, Sequence: v.Sequence, Revision: v.Revision, ParentId: v.ParentID, Deleted: v.Deleted, CreatedAt: tsp(v.CreatedAt)}
	for _, r := range v.References {
		o.References = append(o.References, reference(r))
	}
	o.SourceAttribution = sourceAttribution(v.SourceAttribution)
	return o
}
func reaction(v chatcore.Reaction) *chatv1.Reaction {
	return &chatv1.Reaction{ConversationId: v.ConversationID, PostId: v.PostID, TenantId: v.TenantID, HomeTenantId: v.HomeTenantID, SubjectId: v.SubjectID, Emoji: v.Emoji, CreatedAt: tsp(v.CreatedAt)}
}
func reactionIn(v *chatv1.Reaction) chatcore.Reaction {
	if v == nil {
		return chatcore.Reaction{}
	}
	return chatcore.Reaction{ConversationID: v.GetConversationId(), PostID: v.GetPostId(), TenantID: v.GetTenantId(), HomeTenantID: v.GetHomeTenantId(), SubjectID: v.GetSubjectId(), Emoji: v.GetEmoji(), CreatedAt: timeVal(v.GetCreatedAt())}
}
func pin(v chatcore.Pin) *chatv1.Pin {
	o := &chatv1.Pin{ConversationId: v.ConversationID, PostId: v.PostID, TenantId: v.TenantID, PinnedBy: v.PinnedBy, PinnedByHomeTenantId: v.PinnedByHomeTenantID, Revision: v.Revision, CreatedAt: tsp(v.CreatedAt)}
	if v.Post != nil {
		o.Post = post(*v.Post)
	}
	return o
}
func pinIn(v *chatv1.Pin) chatcore.Pin {
	if v == nil {
		return chatcore.Pin{}
	}
	return chatcore.Pin{ConversationID: v.GetConversationId(), PostID: v.GetPostId(), TenantID: v.GetTenantId(), PinnedBy: v.GetPinnedBy(), PinnedByHomeTenantID: v.GetPinnedByHomeTenantId(), Revision: v.GetRevision(), CreatedAt: timeVal(v.GetCreatedAt())}
}
func readState(v chatcore.ReadState) *chatv1.ReadState {
	return &chatv1.ReadState{ConversationId: v.ConversationID, TenantId: v.TenantID, SubjectId: v.SubjectID, HomeTenantId: v.HomeTenantID, LastReadSequence: v.LastReadSequence, Revision: v.Revision}
}
func readStateIn(v *chatv1.ReadState) chatcore.ReadState {
	if v == nil {
		return chatcore.ReadState{}
	}
	return chatcore.ReadState{ConversationID: v.GetConversationId(), TenantID: v.GetTenantId(), SubjectID: v.GetSubjectId(), HomeTenantID: v.GetHomeTenantId(), LastReadSequence: v.GetLastReadSequence(), Revision: v.GetRevision()}
}
func prefs(v chatcore.NotificationPreferences) *chatv1.NotificationPreferences {
	return &chatv1.NotificationPreferences{ConversationId: v.ConversationID, TenantId: v.TenantID, SubjectId: v.SubjectID, HomeTenantId: v.HomeTenantID, Muted: v.Muted, MentionsOnly: v.MentionsOnly, Revision: v.Revision}
}
func prefsIn(v *chatv1.NotificationPreferences) chatcore.NotificationPreferences {
	if v == nil {
		return chatcore.NotificationPreferences{}
	}
	return chatcore.NotificationPreferences{ConversationID: v.GetConversationId(), TenantID: v.GetTenantId(), SubjectID: v.GetSubjectId(), HomeTenantID: v.GetHomeTenantId(), Muted: v.GetMuted(), MentionsOnly: v.GetMentionsOnly(), Revision: v.GetRevision()}
}
func event(v chatcore.ConversationEvent) *chatv1.ConversationEvent {
	o := &chatv1.ConversationEvent{Kind: eventKindOut(v.Kind), Sequence: v.Sequence, Revision: v.Revision, Removed: v.Removed}
	if v.Post != nil {
		o.Post = post(*v.Post)
	}
	if v.Membership != nil {
		o.Membership = membership(*v.Membership)
	}
	if v.Conversation != nil {
		o.Conversation = conversation(*v.Conversation)
	}
	if v.Reaction != nil {
		o.Reaction = reaction(*v.Reaction)
	}
	if v.Pin != nil {
		o.Pin = pin(*v.Pin)
	}
	return o
}
func ts(v *time.Time) *timestamppb.Timestamp {
	if v == nil {
		return nil
	}
	return tsp(*v)
}
func tsp(v time.Time) *timestamppb.Timestamp {
	if v.IsZero() {
		return nil
	}
	return timestamppb.New(v)
}
func timeIn(v *timestamppb.Timestamp) *time.Time {
	if v == nil {
		return nil
	}
	x := v.AsTime()
	return &x
}
func timeVal(v *timestamppb.Timestamp) time.Time {
	if v == nil {
		return time.Time{}
	}
	return v.AsTime()
}

func referenceKind(v chatv1.ReferenceKind) chatcore.ReferenceKind {
	switch v {
	case chatv1.ReferenceKind_REFERENCE_KIND_PERSON_MENTION:
		return chatcore.PersonMention
	case chatv1.ReferenceKind_REFERENCE_KIND_AGENT_MENTION:
		return chatcore.AgentMention
	case chatv1.ReferenceKind_REFERENCE_KIND_CONVERSATION_REFERENCE:
		return chatcore.ConversationMention
	case chatv1.ReferenceKind_REFERENCE_KIND_MEDIA:
		return chatcore.MediaAttachment
	default:
		return ""
	}
}
func reference(v chatcore.Reference) *chatv1.Reference {
	return &chatv1.Reference{Kind: referenceKindOut(v.Kind), TenantId: v.TenantID, Id: v.ID, Display: v.Display, ConversationId: v.ConversationID, ContentType: v.ContentType, ByteSize: v.ByteSize, Width: v.Width, Height: v.Height}
}
func referenceIn(v *chatv1.Reference) chatcore.Reference {
	if v == nil {
		return chatcore.Reference{}
	}
	return chatcore.Reference{Kind: referenceKind(v.GetKind()), TenantID: v.GetTenantId(), ID: v.GetId(), Display: v.GetDisplay(), ConversationID: v.GetConversationId(), ContentType: v.GetContentType(), ByteSize: v.GetByteSize(), Width: v.GetWidth(), Height: v.GetHeight()}
}
func referencesIn(v []*chatv1.Reference) []chatcore.Reference {
	o := make([]chatcore.Reference, 0, len(v))
	for _, x := range v {
		if x != nil {
			o = append(o, referenceIn(x))
		}
	}
	return o
}
func sourceAttribution(v *chatcore.SourceAttribution) *chatv1.SourceAttribution {
	if v == nil {
		return nil
	}
	return &chatv1.SourceAttribution{TenantId: v.TenantID, ConversationId: v.ConversationID, PostId: v.PostID, PostRevision: v.PostRevision, OriginalAuthorId: v.OriginalAuthorID}
}
func sourceAttributionIn(v *chatv1.SourceAttribution) *chatcore.SourceAttribution {
	if v == nil {
		return nil
	}
	o := chatcore.SourceAttribution{TenantID: v.GetTenantId(), ConversationID: v.GetConversationId(), PostID: v.GetPostId(), PostRevision: v.GetPostRevision(), OriginalAuthorID: v.GetOriginalAuthorId()}
	return &o
}
func sourceAttributionValue(v *chatv1.SourceAttribution) chatcore.SourceAttribution {
	if o := sourceAttributionIn(v); o != nil {
		return *o
	}
	return chatcore.SourceAttribution{}
}
func link(v chatcore.ConversationLink) *chatv1.ConversationLink {
	return &chatv1.ConversationLink{Url: v.URL, TenantId: v.TenantID, ConversationId: v.ConversationID, PostId: v.PostID}
}
