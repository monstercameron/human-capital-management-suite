package chatextensions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
)

type Service interface {
	Counts(context.Context, chat.Principal, string, string) (chatrecipient.Counts, error)
	ThreadFollow(context.Context, chat.Principal, string, string, string) (chatrecipient.Follow, error)
	PutThreadFollow(context.Context, chat.Principal, string, string, chatrecipient.Follow, uint64) (chatrecipient.Follow, error)
	Sidebar(context.Context, chat.Principal) (chatrecipient.Sidebar, error)
	PutSidebar(context.Context, chat.Principal, chatrecipient.Sidebar, uint64) (chatrecipient.Sidebar, error)
	QuietHours(context.Context, chat.Principal) (chatrecipient.QuietHours, error)
	PutQuietHours(context.Context, chat.Principal, chatrecipient.QuietHours, uint64) (chatrecipient.QuietHours, error)
	Install(context.Context, chat.Principal, string, chatapps.Manifest, []string) (chatapps.Installation, error)
	ListInstallations(context.Context, chat.Principal, string) ([]chatapps.Installation, error)
	ChangeStatus(context.Context, chat.Principal, string, string, chatapps.Status) (chatapps.Installation, error)
	Invoke(context.Context, chat.Principal, string, string, chatapps.Callback) (chatapps.CallbackResult, error)
	Agent(context.Context, chat.Principal, string, string) (chatapps.Agent, error)
	ProposeIntent(context.Context, chat.Principal, string, chatapps.Proposal) (chatapps.ProposalReceipt, error)
	Report(context.Context, chat.Principal, chatrecords.Report) error
	Moderate(context.Context, chat.Principal, string, string, string, string, string, string) error
	IssueEventCursor(context.Context, chat.Principal, string, string, int64) (string, error)
	PullEvents(context.Context, chat.Principal, string, string, int) ([]chatapps.Event, string, error)
	ProposeGrant(context.Context, string, string, string, string, string, time.Time) (string, error)
	AcceptGrant(context.Context, string, string, string, string) error
	RevokeGrant(context.Context, string, string, string, string) error
	SetChannelPolicy(context.Context, string, string, []string, []string, []string, []string, chatpolicy.RoleMode, string, string, uint64) error
}

// RetentionService is optional for compatibility with deployments that have
// not enabled chat records governance. The handler fails closed without it.
type RetentionService interface {
	RetentionPolicy(context.Context, chat.Principal, string) (chatrecords.RetentionPolicy, bool, error)
	PutRetentionPolicy(context.Context, chat.Principal, chatrecords.RetentionPolicy, uint64) (chatrecords.RetentionPolicy, error)
}

type ChannelTodoService interface {
	ChannelTodo(context.Context, chat.Principal, string, string) (chatstore.ChannelTodoList, error)
	MutateChannelTodo(context.Context, chat.Principal, string, string, uint64, chatstore.ChannelTodoMutation) (chatstore.ChannelTodoList, error)
}

type ChannelWidgetService interface {
	ChannelWidgets(context.Context, chat.Principal, string, string) (chatstore.ChannelWidgets, error)
	MutateChannelWidget(context.Context, chat.Principal, string, string, uint64, chatstore.ChannelWidgetMutation) (chatstore.ChannelWidgets, error)
}

type ChannelPollService interface {
	ChannelPoll(context.Context, chat.Principal, string, string) (chatstore.ChannelPoll, error)
	MutateChannelPoll(context.Context, chat.Principal, string, string, uint64, chatstore.ChannelPollMutation) (chatstore.ChannelPoll, error)
}

// PrincipalGrantService is the principal-carrying form of the four grant and
// policy methods on [Service].
//
// Those four are the only methods whose subject is a pair of companies rather
// than the caller, so they were the only ones the caller's identity never
// reached: the transport forwarded client-supplied host and consumer tenant ids
// and dropped the principal on the floor. The transport now binds the
// authenticated tenant to the side of the grant the RPC acts for before it
// calls anything, and prefers this port when the composed service implements
// it, so the service can record who acted rather than re-deriving it from the
// context.
//
// It is a separate optional port rather than a change to [Service] because the
// composed implementation lives in a package this change does not own; a
// service that implements only [Service] keeps working and loses nothing but
// the recorded actor.
type PrincipalGrantService interface {
	ProposeGrantAs(context.Context, chat.Principal, string, string, string, string, string, time.Time) (string, error)
	AcceptGrantAs(context.Context, chat.Principal, string, string, string, string) error
	RevokeGrantAs(context.Context, chat.Principal, string, string, string, string) error
	SetChannelPolicyAs(context.Context, chat.Principal, string, string, []string, []string, []string, []string, chatpolicy.RoleMode, string, string, uint64) error
}

type Dependencies struct{ Service Service }
type server struct {
	chatv1.UnimplementedChatExtensionsServiceServer
	deps Dependencies
}

func Register(g *grpc.Server, d Dependencies) {
	chatv1.RegisterChatExtensionsServiceServer(g, &server{deps: d})
}
func NewHandler(d Dependencies, opts ...connect.HandlerOption) http.Handler {
	s := &server{deps: d}
	mux := http.NewServeMux()
	register(mux, opts, chatv1.ChatExtensionsService_GetCounts_FullMethodName, s.GetCounts)
	register(mux, opts, chatv1.ChatExtensionsService_GetThreadFollow_FullMethodName, s.GetThreadFollow)
	register(mux, opts, chatv1.ChatExtensionsService_PutThreadFollow_FullMethodName, s.PutThreadFollow)
	register(mux, opts, chatv1.ChatExtensionsService_GetSidebar_FullMethodName, s.GetSidebar)
	register(mux, opts, chatv1.ChatExtensionsService_PutSidebar_FullMethodName, s.PutSidebar)
	register(mux, opts, chatv1.ChatExtensionsService_GetQuietHours_FullMethodName, s.GetQuietHours)
	register(mux, opts, chatv1.ChatExtensionsService_PutQuietHours_FullMethodName, s.PutQuietHours)
	register(mux, opts, chatv1.ChatExtensionsService_InstallApp_FullMethodName, s.InstallApp)
	register(mux, opts, chatv1.ChatExtensionsService_ListApps_FullMethodName, s.ListApps)
	register(mux, opts, chatv1.ChatExtensionsService_ChangeAppStatus_FullMethodName, s.ChangeAppStatus)
	register(mux, opts, chatv1.ChatExtensionsService_InvokeApp_FullMethodName, s.InvokeApp)
	register(mux, opts, chatv1.ChatExtensionsService_GetAgent_FullMethodName, s.GetAgent)
	register(mux, opts, chatv1.ChatExtensionsService_ProposeAgentIntent_FullMethodName, s.ProposeAgentIntent)
	register(mux, opts, chatv1.ChatExtensionsService_ReportAbuse_FullMethodName, s.ReportAbuse)
	register(mux, opts, chatv1.ChatExtensionsService_ModerateAbuse_FullMethodName, s.ModerateAbuse)
	register(mux, opts, chatv1.ChatExtensionsService_IssueEventCursor_FullMethodName, s.IssueEventCursor)
	register(mux, opts, chatv1.ChatExtensionsService_PullAppEvents_FullMethodName, s.PullAppEvents)
	register(mux, opts, chatv1.ChatExtensionsService_ProposeCompanyGrant_FullMethodName, s.ProposeCompanyGrant)
	register(mux, opts, chatv1.ChatExtensionsService_AcceptCompanyGrant_FullMethodName, s.AcceptCompanyGrant)
	register(mux, opts, chatv1.ChatExtensionsService_RevokeCompanyGrant_FullMethodName, s.RevokeCompanyGrant)
	register(mux, opts, chatv1.ChatExtensionsService_SetChannelPolicy_FullMethodName, s.SetChannelPolicy)
	register(mux, opts, chatv1.ChatExtensionsService_GetRetentionPolicy_FullMethodName, s.GetRetentionPolicy)
	register(mux, opts, chatv1.ChatExtensionsService_PutRetentionPolicy_FullMethodName, s.PutRetentionPolicy)
	register(mux, opts, chatv1.ChatExtensionsService_GetChannelTodoList_FullMethodName, s.GetChannelTodoList)
	register(mux, opts, chatv1.ChatExtensionsService_MutateChannelTodoList_FullMethodName, s.MutateChannelTodoList)
	register(mux, opts, chatv1.ChatExtensionsService_GetChannelWidgets_FullMethodName, s.GetChannelWidgets)
	register(mux, opts, chatv1.ChatExtensionsService_MutateChannelWidget_FullMethodName, s.MutateChannelWidget)
	register(mux, opts, chatv1.ChatExtensionsService_GetChannelPoll_FullMethodName, s.GetChannelPoll)
	register(mux, opts, chatv1.ChatExtensionsService_MutateChannelPoll_FullMethodName, s.MutateChannelPoll)
	return mux
}
func register[Req, Res any](m *http.ServeMux, opts []connect.HandlerOption, path string, fn func(context.Context, *Req) (*Res, error)) {
	m.Handle(path, connect.NewUnaryHandler(path, func(ctx context.Context, r *connect.Request[Req]) (*connect.Response[Res], error) {
		v, e := fn(ctx, r.Msg)
		if e != nil {
			return nil, e
		}
		return connect.NewResponse(v), nil
	}, opts...))
}
func (s *server) principal(ctx context.Context) (chat.Principal, error) {
	if s.deps.Service == nil {
		return chat.Principal{}, envelope.New(envelope.CodeUnavailable, "chat.extensions.unavailable", "chat extensions are unavailable")
	}
	if _, ok := transport.InvocationFromContext(ctx); !ok {
		return chat.Principal{}, envelope.New(envelope.CodeUnauthenticated, "chat.no_trusted_context", "authentication required")
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil {
		return chat.Principal{}, envelope.New(envelope.CodeUnauthenticated, "chat.no_principal", "authentication required")
	}
	return chat.Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject()}, nil
}
func jsonResult[T any](wrap func([]byte) *T, v any) (*T, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	return wrap(b), nil
}
func mapped(err error) error {
	if err == nil {
		return nil
	}
	code := envelope.CodeUnavailable
	switch {
	case errors.Is(err, chat.ErrPermissionDenied), errors.Is(err, chatapps.ErrDenied), errors.Is(err, chatapps.ErrSuspended), errors.Is(err, chatrecords.ErrUnauthorized):
		code = envelope.CodePermissionDenied
	case errors.Is(err, chat.ErrUnauthenticated):
		code = envelope.CodeUnauthenticated
	case errors.Is(err, chat.ErrNotFound), errors.Is(err, chatapps.ErrNotFound):
		code = envelope.CodeNotFound
	case errors.Is(err, chat.ErrInvalidArgument), errors.Is(err, chatapps.ErrInvalid), errors.Is(err, chatrecords.ErrInvalid):
		code = envelope.CodeInvalidArgument
	case errors.Is(err, chat.ErrConflict), errors.Is(err, chatrecords.ErrConflict), errors.Is(err, chatapps.ErrRevoked), errors.Is(err, chatapps.ErrReplay), errors.Is(err, chatapps.ErrBudget), errors.Is(err, chatapps.ErrLoop):
		code = envelope.CodeAborted
	}
	return envelope.New(code, "chat.extensions.error", "chat extension operation failed")
}

func retentionOut(p chatrecords.RetentionPolicy) *chatv1.ChatRetentionPolicy {
	out := &chatv1.ChatRetentionPolicy{ConversationKind: p.Kind, Mode: string(p.Mode), AgeDays: p.AgeDays, BudgetBytes: p.BudgetBytes, Revision: p.Revision, UpdatedBy: p.UpdatedBy}
	if !p.BeforeDate.IsZero() {
		out.BeforeDateUnix = p.BeforeDate.Unix()
	}
	if !p.UpdatedAt.IsZero() {
		out.UpdatedAtUnix = p.UpdatedAt.Unix()
	}
	return out
}

func (s *server) GetRetentionPolicy(ctx context.Context, r *chatv1.GetRetentionPolicyRequest) (*chatv1.GetRetentionPolicyResponse, error) {
	p, err := s.principal(ctx)
	if err != nil {
		return nil, err
	}
	svc, ok := s.deps.Service.(RetentionService)
	if !ok {
		return nil, mapped(chat.ErrUnavailable)
	}
	v, found, err := svc.RetentionPolicy(ctx, p, r.GetConversationKind())
	if err != nil {
		return nil, mapped(err)
	}
	if !found {
		return &chatv1.GetRetentionPolicyResponse{}, nil
	}
	return &chatv1.GetRetentionPolicyResponse{Configured: true, Policy: retentionOut(v)}, nil
}

func (s *server) PutRetentionPolicy(ctx context.Context, r *chatv1.PutRetentionPolicyRequest) (*chatv1.PutRetentionPolicyResponse, error) {
	p, err := s.principal(ctx)
	if err != nil {
		return nil, err
	}
	if r.GetPolicy() == nil {
		return nil, mapped(chat.ErrInvalidArgument)
	}
	svc, ok := s.deps.Service.(RetentionService)
	if !ok {
		return nil, mapped(chat.ErrUnavailable)
	}
	x := r.GetPolicy()
	v := chatrecords.RetentionPolicy{TenantID: p.TenantID, Kind: x.GetConversationKind(), Mode: chatrecords.RetentionMode(x.GetMode()), AgeDays: x.GetAgeDays(), BudgetBytes: x.GetBudgetBytes()}
	if x.GetBeforeDateUnix() != 0 {
		v.BeforeDate = time.Unix(x.GetBeforeDateUnix(), 0).UTC()
	}
	v, err = svc.PutRetentionPolicy(ctx, p, v, r.GetExpectedRevision())
	if err != nil {
		return nil, mapped(err)
	}
	return &chatv1.PutRetentionPolicyResponse{Policy: retentionOut(v)}, nil
}
func (s *server) InstallApp(ctx context.Context, r *chatv1.InstallAppRequest) (*chatv1.InstallAppResponse, error) {
	p, e := s.principal(ctx)
	if e != nil {
		return nil, e
	}
	var m chatapps.Manifest
	if json.Unmarshal(r.GetManifestJson(), &m) != nil {
		return nil, mapped(chatapps.ErrInvalid)
	}
	v, e := s.deps.Service.Install(ctx, p, r.GetConversationId(), m, r.GetGrantedScopes())
	if e != nil {
		return nil, mapped(e)
	}
	return jsonResult(func(b []byte) *chatv1.InstallAppResponse { return &chatv1.InstallAppResponse{Json: b} }, v)
}
func (s *server) ListApps(ctx context.Context, r *chatv1.ListAppsRequest) (*chatv1.ListAppsResponse, error) {
	p, e := s.principal(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.ListInstallations(ctx, p, r.GetConversationId())
	if e != nil {
		return nil, mapped(e)
	}
	return jsonResult(func(b []byte) *chatv1.ListAppsResponse { return &chatv1.ListAppsResponse{Json: b} }, v)
}
func (s *server) ChangeAppStatus(ctx context.Context, r *chatv1.ChangeAppStatusRequest) (*chatv1.ChangeAppStatusResponse, error) {
	p, e := s.principal(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.ChangeStatus(ctx, p, r.GetConversationId(), r.GetInstallationId(), chatapps.Status(r.GetStatus()))
	if e != nil {
		return nil, mapped(e)
	}
	return jsonResult(func(b []byte) *chatv1.ChangeAppStatusResponse { return &chatv1.ChangeAppStatusResponse{Json: b} }, v)
}
func (s *server) InvokeApp(ctx context.Context, r *chatv1.InvokeAppRequest) (*chatv1.InvokeAppResponse, error) {
	p, e := s.principal(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.Invoke(ctx, p, r.GetConversationId(), r.GetInstallationId(), chatapps.Callback{Command: r.GetCommand(), IdempotencyKey: r.GetIdempotencyKey(), Args: r.GetArguments()})
	if e != nil {
		return nil, mapped(e)
	}
	return jsonResult(func(b []byte) *chatv1.InvokeAppResponse { return &chatv1.InvokeAppResponse{Json: b} }, v)
}
func (s *server) GetAgent(ctx context.Context, r *chatv1.GetAgentRequest) (*chatv1.GetAgentResponse, error) {
	p, e := s.principal(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.Agent(ctx, p, r.GetConversationId(), r.GetInstallationId())
	if e != nil {
		return nil, mapped(e)
	}
	return jsonResult(func(b []byte) *chatv1.GetAgentResponse { return &chatv1.GetAgentResponse{Json: b} }, v)
}
func (s *server) ProposeAgentIntent(ctx context.Context, r *chatv1.ProposeAgentIntentRequest) (*chatv1.ProposeAgentIntentResponse, error) {
	p, e := s.principal(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.ProposeIntent(ctx, p, r.GetConversationId(), chatapps.Proposal{AgentInstallation: r.GetInstallationId(), IntentType: r.GetIntentType(), IdempotencyKey: r.GetIdempotencyKey(), Arguments: r.GetArguments(), Evidence: r.GetEvidence()})
	if e != nil {
		return nil, mapped(e)
	}
	return jsonResult(func(b []byte) *chatv1.ProposeAgentIntentResponse { return &chatv1.ProposeAgentIntentResponse{Json: b} }, v)
}
func (s *server) ReportAbuse(ctx context.Context, r *chatv1.ReportAbuseRequest) (*chatv1.ReportAbuseResponse, error) {
	p, e := s.principal(ctx)
	if e != nil {
		return nil, e
	}
	e = s.deps.Service.Report(ctx, p, chatrecords.Report{ConversationID: r.GetConversationId(), ReportID: r.GetReportId(), TargetID: r.GetTargetId(), Reason: r.GetReason()})
	if e != nil {
		return nil, mapped(e)
	}
	return jsonResult(func(b []byte) *chatv1.ReportAbuseResponse { return &chatv1.ReportAbuseResponse{Json: b} }, map[string]bool{"reported": true})
}
func (s *server) ModerateAbuse(ctx context.Context, r *chatv1.ModerateAbuseRequest) (*chatv1.ModerateAbuseResponse, error) {
	p, e := s.principal(ctx)
	if e != nil {
		return nil, e
	}
	e = s.deps.Service.Moderate(ctx, p, r.GetConversationId(), r.GetCaseId(), r.GetAction(), r.GetTargetId(), r.GetReason(), r.GetEvidenceRef())
	if e != nil {
		return nil, mapped(e)
	}
	return jsonResult(func(b []byte) *chatv1.ModerateAbuseResponse { return &chatv1.ModerateAbuseResponse{Json: b} }, map[string]bool{"moderated": true})
}
func (s *server) IssueEventCursor(ctx context.Context, r *chatv1.IssueEventCursorRequest) (*chatv1.IssueEventCursorResponse, error) {
	p, e := s.principal(ctx)
	if e != nil {
		return nil, e
	}
	v, e := s.deps.Service.IssueEventCursor(ctx, p, r.GetConversationId(), r.GetInstallationId(), r.GetAfterSequence())
	if e != nil {
		return nil, mapped(e)
	}
	return jsonResult(func(b []byte) *chatv1.IssueEventCursorResponse { return &chatv1.IssueEventCursorResponse{Json: b} }, map[string]string{"cursor": v})
}
func (s *server) PullAppEvents(ctx context.Context, r *chatv1.PullAppEventsRequest) (*chatv1.PullAppEventsResponse, error) {
	p, e := s.principal(ctx)
	if e != nil {
		return nil, e
	}
	v, next, e := s.deps.Service.PullEvents(ctx, p, r.GetConversationId(), r.GetCursor(), int(r.GetLimit()))
	if e != nil {
		return nil, mapped(e)
	}
	return jsonResult(func(b []byte) *chatv1.PullAppEventsResponse { return &chatv1.PullAppEventsResponse{Json: b} }, struct {
		Events []chatapps.Event `json:"events"`
		Cursor string           `json:"cursor"`
	}{v, next})
}

// grantSide names which company of a bilateral grant an RPC acts for.
type grantSide uint8

const (
	// sideHost is the company that owns the conversation: it proposes the
	// grant and owns the channel policy.
	sideHost grantSide = iota
	// sideConsumer is the invited company: only it can consent.
	sideConsumer
	// sideEither is revocation, which either company may perform.
	sideEither
)

// grantPrincipal resolves the authenticated principal and refuses a request
// whose tenants do not include it on the side this RPC acts for.
//
// Without this check the four grant and policy RPCs were open proxies: any
// authenticated caller could name any two companies and either establish or
// tear down a sharing relationship between them. Membership of one side is not
// a nicety here, it is the whole authorization.
func (s *server) grantPrincipal(ctx context.Context, host, consumer string, side grantSide) (chat.Principal, error) {
	p, e := s.principal(ctx)
	if e != nil {
		return chat.Principal{}, e
	}
	if host == "" || (side != sideHost && consumer == "") {
		return chat.Principal{}, mapped(chat.ErrInvalidArgument)
	}
	ok := false
	switch side {
	case sideHost:
		ok = p.TenantID == host
	case sideConsumer:
		ok = p.TenantID == consumer
	case sideEither:
		ok = p.TenantID == host || p.TenantID == consumer
	}
	if !ok {
		return chat.Principal{}, mapped(chat.ErrPermissionDenied)
	}
	return p, nil
}

func (s *server) ProposeCompanyGrant(ctx context.Context, r *chatv1.ProposeCompanyGrantRequest) (*chatv1.ProposeCompanyGrantResponse, error) {
	p, e := s.grantPrincipal(ctx, r.GetHostTenantId(), r.GetConsumerTenantId(), sideHost)
	if e != nil {
		return nil, e
	}
	expires := time.Unix(r.GetExpiresAtUnix(), 0).UTC()
	var id string
	if svc, ok := s.deps.Service.(PrincipalGrantService); ok {
		id, e = svc.ProposeGrantAs(ctx, p, r.GetHostTenantId(), r.GetConsumerTenantId(), r.GetConversationId(), r.GetClassification(), r.GetResidency(), expires)
	} else {
		id, e = s.deps.Service.ProposeGrant(ctx, r.GetHostTenantId(), r.GetConsumerTenantId(), r.GetConversationId(), r.GetClassification(), r.GetResidency(), expires)
	}
	if e != nil {
		return nil, mapped(e)
	}
	return &chatv1.ProposeCompanyGrantResponse{GrantId: id}, nil
}
func (s *server) AcceptCompanyGrant(ctx context.Context, r *chatv1.AcceptCompanyGrantRequest) (*chatv1.AcceptCompanyGrantResponse, error) {
	p, e := s.grantPrincipal(ctx, r.GetHostTenantId(), r.GetConsumerTenantId(), sideConsumer)
	if e != nil {
		return nil, e
	}
	if svc, ok := s.deps.Service.(PrincipalGrantService); ok {
		e = svc.AcceptGrantAs(ctx, p, r.GetHostTenantId(), r.GetConsumerTenantId(), r.GetConversationId(), r.GetGrantId())
	} else {
		e = s.deps.Service.AcceptGrant(ctx, r.GetHostTenantId(), r.GetConsumerTenantId(), r.GetConversationId(), r.GetGrantId())
	}
	if e != nil {
		return nil, mapped(e)
	}
	return &chatv1.AcceptCompanyGrantResponse{Accepted: true}, nil
}
func (s *server) RevokeCompanyGrant(ctx context.Context, r *chatv1.RevokeCompanyGrantRequest) (*chatv1.RevokeCompanyGrantResponse, error) {
	p, e := s.grantPrincipal(ctx, r.GetHostTenantId(), r.GetConsumerTenantId(), sideEither)
	if e != nil {
		return nil, e
	}
	if svc, ok := s.deps.Service.(PrincipalGrantService); ok {
		e = svc.RevokeGrantAs(ctx, p, r.GetHostTenantId(), r.GetConsumerTenantId(), r.GetConversationId(), r.GetGrantId())
	} else {
		e = s.deps.Service.RevokeGrant(ctx, r.GetHostTenantId(), r.GetConsumerTenantId(), r.GetConversationId(), r.GetGrantId())
	}
	if e != nil {
		return nil, mapped(e)
	}
	return &chatv1.RevokeCompanyGrantResponse{Revoked: true}, nil
}
func (s *server) SetChannelPolicy(ctx context.Context, r *chatv1.SetChannelPolicyRequest) (*chatv1.SetChannelPolicyResponse, error) {
	p, e := s.grantPrincipal(ctx, r.GetHostTenantId(), "", sideHost)
	if e != nil {
		return nil, e
	}
	mode := chatpolicy.RolesAny
	if r.GetRequireAllRoles() {
		mode = chatpolicy.RolesAll
	}
	if svc, ok := s.deps.Service.(PrincipalGrantService); ok {
		e = svc.SetChannelPolicyAs(ctx, p, r.GetHostTenantId(), r.GetConversationId(), r.GetRequiredRoles(), r.GetQualifications(), r.GetAllowedPrincipals(), r.GetAllowedTenants(), mode, r.GetClassification(), r.GetResidency(), r.GetExpectedRevision())
	} else {
		e = s.deps.Service.SetChannelPolicy(ctx, r.GetHostTenantId(), r.GetConversationId(), r.GetRequiredRoles(), r.GetQualifications(), r.GetAllowedPrincipals(), r.GetAllowedTenants(), mode, r.GetClassification(), r.GetResidency(), r.GetExpectedRevision())
	}
	if e != nil {
		return nil, mapped(e)
	}
	return &chatv1.SetChannelPolicyResponse{Updated: true}, nil
}

// serverDerived refuses a caller-supplied value for a field the server owns.
// The reader of a recipient projection is always the authenticated principal,
// so a request that names a subject is refused rather than served somebody
// else's row or - as before - quietly served its own.
func serverDerived(field string) error {
	return envelope.New(envelope.CodeInvalidArgument, "trusted_context.caller_selected_authority",
		"the request may not select trusted context").
		WithViolation(field, "the field is derived server-side from the verified credential",
			"trusted_request_boundary.server_derived_field")
}

func (s *server) GetCounts(ctx context.Context, r *chatv1.GetCountsRequest) (*chatv1.GetCountsResponse, error) {
	p, e := s.principal(ctx)
	if e != nil {
		return nil, e
	}
	if r.GetSubjectId() != "" {
		return nil, serverDerived("subject_id")
	}
	v, e := s.deps.Service.Counts(ctx, p, r.GetTenantId(), r.GetConversationId())
	if e != nil {
		return nil, mapped(e)
	}
	return &chatv1.GetCountsResponse{Counts: &chatv1.ChatCounts{TenantId: r.GetTenantId(), ConversationId: r.GetConversationId(), SubjectId: p.SubjectID, HomeTenantId: p.TenantID, UnreadCount: v.Unread, MentionCount: v.Mentions}}, nil
}
func (s *server) GetThreadFollow(ctx context.Context, r *chatv1.GetThreadFollowRequest) (*chatv1.GetThreadFollowResponse, error) {
	p, e := s.principal(ctx)
	if e != nil {
		return nil, e
	}
	if r.GetSubjectId() != "" {
		return nil, serverDerived("subject_id")
	}
	v, e := s.deps.Service.ThreadFollow(ctx, p, r.GetTenantId(), r.GetConversationId(), r.GetRootPostId())
	if e != nil {
		return nil, mapped(e)
	}
	return &chatv1.GetThreadFollowResponse{Follow: followOut(p, r.GetTenantId(), r.GetConversationId(), v)}, nil
}
func (s *server) PutThreadFollow(ctx context.Context, r *chatv1.PutThreadFollowRequest) (*chatv1.PutThreadFollowResponse, error) {
	p, e := s.principal(ctx)
	if e != nil {
		return nil, e
	}
	x := r.GetFollow()
	if x == nil {
		return nil, mapped(chat.ErrInvalidArgument)
	}
	v, e := s.deps.Service.PutThreadFollow(ctx, p, x.GetTenantId(), x.GetConversationId(), chatrecipient.Follow{RootPostID: x.GetRootPostId(), Followed: x.GetFollowed()}, r.GetExpectedRevision())
	if e != nil {
		return nil, mapped(e)
	}
	return &chatv1.PutThreadFollowResponse{Follow: followOut(p, x.GetTenantId(), x.GetConversationId(), v)}, nil
}
func followOut(p chat.Principal, host, conversation string, v chatrecipient.Follow) *chatv1.ThreadFollow {
	return &chatv1.ThreadFollow{TenantId: host, ConversationId: conversation, SubjectId: p.SubjectID, HomeTenantId: p.TenantID, RootPostId: v.RootPostID, Followed: v.Followed, Revision: v.Revision}
}
func (s *server) GetSidebar(ctx context.Context, r *chatv1.GetSidebarRequest) (*chatv1.GetSidebarResponse, error) {
	p, e := s.principal(ctx)
	if e != nil {
		return nil, e
	}
	if r.GetTenantId() != "" {
		return nil, serverDerived("tenant_id")
	}
	if r.GetSubjectId() != "" {
		return nil, serverDerived("subject_id")
	}
	v, e := s.deps.Service.Sidebar(ctx, p)
	if e != nil {
		return nil, mapped(e)
	}
	return &chatv1.GetSidebarResponse{Sidebar: sidebarOut(p, v)}, nil
}
func (s *server) PutSidebar(ctx context.Context, r *chatv1.PutSidebarRequest) (*chatv1.PutSidebarResponse, error) {
	p, e := s.principal(ctx)
	if e != nil {
		return nil, e
	}
	x := r.GetSidebar()
	if x == nil {
		return nil, mapped(chat.ErrInvalidArgument)
	}
	v, e := s.deps.Service.PutSidebar(ctx, p, chatrecipient.Sidebar{Layout: []byte(x.GetLayoutJson())}, r.GetExpectedRevision())
	if e != nil {
		return nil, mapped(e)
	}
	return &chatv1.PutSidebarResponse{Sidebar: sidebarOut(p, v)}, nil
}
func sidebarOut(p chat.Principal, v chatrecipient.Sidebar) *chatv1.SidebarState {
	return &chatv1.SidebarState{TenantId: p.TenantID, SubjectId: p.SubjectID, HomeTenantId: p.TenantID, LayoutJson: string(v.Layout), Revision: v.Revision}
}
func (s *server) GetQuietHours(ctx context.Context, r *chatv1.GetQuietHoursRequest) (*chatv1.GetQuietHoursResponse, error) {
	p, e := s.principal(ctx)
	if e != nil {
		return nil, e
	}
	if r.GetTenantId() != "" {
		return nil, serverDerived("tenant_id")
	}
	if r.GetSubjectId() != "" {
		return nil, serverDerived("subject_id")
	}
	v, e := s.deps.Service.QuietHours(ctx, p)
	if e != nil {
		return nil, mapped(e)
	}
	return &chatv1.GetQuietHoursResponse{QuietHours: quietOut(p, v)}, nil
}
func (s *server) PutQuietHours(ctx context.Context, r *chatv1.PutQuietHoursRequest) (*chatv1.PutQuietHoursResponse, error) {
	p, e := s.principal(ctx)
	if e != nil {
		return nil, e
	}
	x := r.GetQuietHours()
	if x == nil {
		return nil, mapped(chat.ErrInvalidArgument)
	}
	v, e := s.deps.Service.PutQuietHours(ctx, p, chatrecipient.QuietHours{Timezone: x.GetTimezone(), StartMinute: int(x.GetStartMinute()), EndMinute: int(x.GetEndMinute()), Enabled: x.GetEnabled()}, r.GetExpectedRevision())
	if e != nil {
		return nil, mapped(e)
	}
	return &chatv1.PutQuietHoursResponse{QuietHours: quietOut(p, v)}, nil
}
func quietOut(p chat.Principal, v chatrecipient.QuietHours) *chatv1.QuietHours {
	return &chatv1.QuietHours{TenantId: p.TenantID, SubjectId: p.SubjectID, HomeTenantId: p.TenantID, Timezone: v.Timezone, StartMinute: uint32(v.StartMinute), EndMinute: uint32(v.EndMinute), Enabled: v.Enabled, Revision: v.Revision}
}
