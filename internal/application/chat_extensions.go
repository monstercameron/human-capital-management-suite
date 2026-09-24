package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatadmission"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatappstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ChatExtensions binds app and governance operations to current conversation
// access. The caller identity must come from authenticated transport context.
type ChatExtensions struct {
	Conversations chat.ConversationService
	Apps          *chatapps.Service
	// AppCommandAuthorization resolves the invoking person's current authority
	// for one manifest command. Installation scopes never supply this decision.
	AppCommandAuthorization ChatAppCommandAuthorization
	Records                 *chatrecords.Service
	Recipients              *chatrecipient.Service
	Grants                  *ChatCompanyGrants
	TodoStore               *chatstore.Store
	// Admission bounds the extension lanes. Integration event pulls are derived
	// work and are shed first; governance records take the read lane. A nil
	// runtime leaves these calls unmetered, which is the streaming-disabled
	// composition.
	Admission *ChatStreamRuntime
}

// ChatAppCommandAuthorization checks a command capability against the current
// authenticated principal and conversation policy. Implementations must use
// current server-side authority, not installation grants or request fields.
type ChatAppCommandAuthorization interface {
	AuthorizeChatAppCommand(context.Context, chat.Principal, string, string, string) error
}

func (s *ChatExtensions) lease(ctx context.Context, lane chatadmission.Lane, tenant, conversation string) (*chatadmission.Lease, error) {
	if s == nil || s.Admission == nil || tenant == "" || conversation == "" {
		return nil, nil
	}
	return s.Admission.acquire(ctx, tenant, conversation, lane)
}

func (s *ChatExtensions) Counts(ctx context.Context, p chat.Principal, host, conversation string) (chatrecipient.Counts, error) {
	if s == nil || s.Recipients == nil {
		return chatrecipient.Counts{}, chat.ErrUnavailable
	}
	return s.Recipients.Counts(ctx, p, host, conversation)
}
func (s *ChatExtensions) ThreadFollow(ctx context.Context, p chat.Principal, host, conversation, root string) (chatrecipient.Follow, error) {
	if s == nil || s.Recipients == nil {
		return chatrecipient.Follow{}, chat.ErrUnavailable
	}
	return s.Recipients.Follow(ctx, p, host, conversation, root)
}
func (s *ChatExtensions) PutThreadFollow(ctx context.Context, p chat.Principal, host, conversation string, f chatrecipient.Follow, expected uint64) (chatrecipient.Follow, error) {
	if s == nil || s.Recipients == nil {
		return chatrecipient.Follow{}, chat.ErrUnavailable
	}
	return s.Recipients.PutFollow(ctx, p, host, conversation, f, expected)
}
func (s *ChatExtensions) Sidebar(ctx context.Context, p chat.Principal) (chatrecipient.Sidebar, error) {
	if s == nil || s.Recipients == nil {
		return chatrecipient.Sidebar{}, chat.ErrUnavailable
	}
	return s.Recipients.Sidebar(ctx, p)
}
func (s *ChatExtensions) PutSidebar(ctx context.Context, p chat.Principal, x chatrecipient.Sidebar, expected uint64) (chatrecipient.Sidebar, error) {
	if s == nil || s.Recipients == nil {
		return chatrecipient.Sidebar{}, chat.ErrUnavailable
	}
	return s.Recipients.PutSidebar(ctx, p, x, expected)
}
func (s *ChatExtensions) QuietHours(ctx context.Context, p chat.Principal) (chatrecipient.QuietHours, error) {
	if s == nil || s.Recipients == nil {
		return chatrecipient.QuietHours{}, chat.ErrUnavailable
	}
	return s.Recipients.QuietHours(ctx, p)
}
func (s *ChatExtensions) PutQuietHours(ctx context.Context, p chat.Principal, x chatrecipient.QuietHours, expected uint64) (chatrecipient.QuietHours, error) {
	if s == nil || s.Recipients == nil {
		return chatrecipient.QuietHours{}, chat.ErrUnavailable
	}
	return s.Recipients.PutQuietHours(ctx, p, x, expected)
}

func (s *ChatExtensions) recordContext(ctx context.Context, p chat.Principal, conversation string) (context.Context, error) {
	if s.Records == nil || s.Records.Repo == nil {
		return nil, chat.ErrUnavailable
	}
	c, err := s.conversation(ctx, p, conversation)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, recordAuthorizationKey{}, recordAuthorization{tenant: c.TenantID, actor: p.SubjectID, conversation: conversation, revision: c.Revision}), nil
}
func (s *ChatExtensions) recordApp(ctx context.Context, p chat.Principal, v chatapps.Installation, action string) error {
	_, err := s.Records.AppendRecord(ctx, p.SubjectID, chatrecords.Record{TenantID: v.Tenant, ConversationID: v.Conversation, RecordID: "app:" + v.ID, Kind: chatrecords.KindAppChange, SourceID: v.ID, Revision: v.Revision}, action, action)
	return err
}

func (s *ChatExtensions) ProposeGrant(ctx context.Context, host, consumer, conversation, classification, residency string, expiresAt time.Time) (string, error) {
	if s == nil || s.Grants == nil {
		return "", chat.ErrUnavailable
	}
	return s.Grants.Propose(ctx, host, consumer, conversation, classification, residency, expiresAt, time.Now().UTC())
}
func (s *ChatExtensions) AcceptGrant(ctx context.Context, host, consumer, conversation, id string) error {
	if s == nil || s.Grants == nil {
		return chat.ErrUnavailable
	}
	return s.Grants.Accept(ctx, host, consumer, conversation, id, time.Now().UTC())
}
func (s *ChatExtensions) RevokeGrant(ctx context.Context, host, consumer, conversation, id string) error {
	if s == nil || s.Grants == nil {
		return chat.ErrUnavailable
	}
	return s.Grants.Revoke(ctx, host, consumer, conversation, id, time.Now().UTC())
}
func (s *ChatExtensions) SetChannelPolicy(ctx context.Context, host, conversation string, roles, qualifications, principals, tenants []string, mode chatpolicy.RoleMode, classification, residency string, expected uint64) error {
	if s == nil || s.Grants == nil {
		return chat.ErrUnavailable
	}
	return s.Grants.SetChannelPolicy(ctx, host, conversation, roles, qualifications, principals, tenants, mode, classification, residency, expected, time.Now().UTC())
}

// ChatAppAuthority adapts current conversation policy to the app capability
// port. Composition must supply it when constructing chatapps.Service.
type ChatAppAuthority struct{ Conversations chat.ConversationService }

type recordAuthorizationKey struct{}
type recordAuthorization struct {
	tenant, actor, conversation string
	revision                    uint64
	manager                     bool
}

// ChatRecordAuthority accepts only the live conversation decision carried by
// this application service. Direct calls to the records package stay denied.
type ChatRecordAuthority struct{}

func (ChatRecordAuthority) Authorize(ctx context.Context, actor, tenant, action, _ string) (string, error) {
	v, ok := ctx.Value(recordAuthorizationKey{}).(recordAuthorization)
	if !ok || v.actor != actor || v.tenant != tenant {
		return "", chat.ErrPermissionDenied
	}
	if v.manager && strings.HasPrefix(action, "retention.") {
		return "chat:tenant-admin:" + tenant, nil
	}
	if v.revision == 0 {
		return "", chat.ErrPermissionDenied
	}
	if action != "moderation.report" && !strings.HasPrefix(action, "chat.") && !(v.manager && strings.HasPrefix(action, "moderation.")) {
		return "", chat.ErrPermissionDenied
	}
	return fmt.Sprintf("chat:%s:%d", v.conversation, v.revision), nil
}

// RetentionPolicy and PutRetentionPolicy require current tenant administration
// facts. The transport principal must match the verified identity in context.
func (s *ChatExtensions) RetentionPolicy(ctx context.Context, p chat.Principal, kind string) (chatrecords.RetentionPolicy, bool, error) {
	ctx, err := s.retentionContext(ctx, p)
	if err != nil {
		return chatrecords.RetentionPolicy{}, false, err
	}
	return s.Records.GetRetentionPolicy(ctx, p.SubjectID, p.TenantID, kind)
}

func (s *ChatExtensions) PutRetentionPolicy(ctx context.Context, p chat.Principal, policy chatrecords.RetentionPolicy, expected uint64) (chatrecords.RetentionPolicy, error) {
	ctx, err := s.retentionContext(ctx, p)
	if err != nil {
		return chatrecords.RetentionPolicy{}, err
	}
	if policy.TenantID != p.TenantID {
		return chatrecords.RetentionPolicy{}, chat.ErrPermissionDenied
	}
	return s.Records.PutRetentionPolicy(ctx, p.SubjectID, policy, expected)
}

func (s *ChatExtensions) retentionContext(ctx context.Context, p chat.Principal) (context.Context, error) {
	if s == nil || s.Records == nil || s.Grants == nil || p.TenantID == "" || p.SubjectID == "" {
		return nil, chat.ErrUnavailable
	}
	actor, err := s.Grants.admin(ctx, p.TenantID, time.Now().UTC())
	if err != nil || actor != p.SubjectID {
		return nil, chat.ErrPermissionDenied
	}
	return context.WithValue(ctx, recordAuthorizationKey{}, recordAuthorization{tenant: p.TenantID, actor: actor, manager: true}), nil
}

func (a ChatAppAuthority) CanUseConversation(ctx context.Context, actor chatapps.Actor, id string) error {
	if a.Conversations == nil || actor.Tenant == "" || actor.Principal == "" || actor.Conversation != id {
		return chat.ErrPermissionDenied
	}
	_, err := a.Conversations.GetConversation(ctx, chat.GetConversationRequest{Principal: chat.Principal{TenantID: actor.Tenant, SubjectID: actor.Principal}, TenantID: actor.Tenant, ConversationID: id})
	return err
}

func (a ChatAppAuthority) CanManageApp(ctx context.Context, actor chatapps.Actor, _ string) error {
	if a.Conversations == nil || actor.Tenant == "" || actor.Principal == "" || actor.Conversation == "" {
		return chat.ErrPermissionDenied
	}
	return canManageChatConversation(ctx, a.Conversations, chat.Principal{TenantID: actor.Tenant, SubjectID: actor.Principal}, actor.Conversation)
}

// canManageChatConversation uses the current membership, so a revoked manager
// cannot retain an installation grant through a stale caller supplied role.
func canManageChatConversation(ctx context.Context, service chat.ConversationService, p chat.Principal, id string) error {
	c, err := service.GetConversation(ctx, chat.GetConversationRequest{Principal: p, TenantID: p.TenantID, ConversationID: id})
	if err != nil {
		return err
	}
	if c.OwnerID == p.SubjectID {
		return nil
	}
	cursor := ""
	for {
		page, err := service.ListMemberships(ctx, chat.ListMembershipsRequest{Principal: p, TenantID: p.TenantID, ConversationID: id, Page: chat.Page{Cursor: cursor, PageSize: 200}})
		if err != nil {
			return err
		}
		for _, membership := range page.Memberships {
			if membership.HomeTenantID == p.TenantID && membership.SubjectID == p.SubjectID && membership.Role == chat.Manager && membership.LeftAt == nil {
				return nil
			}
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			return chat.ErrPermissionDenied
		}
		cursor = page.NextCursor
	}
}

// AuthorizeMedia is the live conversation check supplied to chatmedia.New.
// Transport must fill its request from the authenticated principal.
func (s *ChatExtensions) AuthorizeMedia(ctx context.Context, req chatmedia.AccessRequest) error {
	if s == nil || s.Conversations == nil {
		return chat.ErrUnavailable
	}
	if _, ok := transport.InvocationFromContext(ctx); !ok {
		return chat.ErrPermissionDenied
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.Subject() != req.PrincipalID {
		return chat.ErrPermissionDenied
	}
	_, err := s.Conversations.GetConversation(ctx, chat.GetConversationRequest{Principal: chat.Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject()}, TenantID: req.TenantID, ConversationID: req.ConversationID})
	return err
}

func (s *ChatExtensions) conversation(ctx context.Context, p chat.Principal, id string) (chat.Conversation, error) {
	if s == nil || s.Conversations == nil || p.TenantID == "" || p.SubjectID == "" || id == "" {
		return chat.Conversation{}, chat.ErrUnavailable
	}
	return s.Conversations.GetConversation(ctx, chat.GetConversationRequest{Principal: p, TenantID: p.TenantID, ConversationID: id})
}

func (s *ChatExtensions) actor(ctx context.Context, p chat.Principal, id string) (chatapps.Actor, error) {
	if _, err := s.conversation(ctx, p, id); err != nil {
		return chatapps.Actor{}, err
	}
	return chatapps.Actor{Tenant: p.TenantID, Principal: p.SubjectID, Conversation: id}, nil
}

func (s *ChatExtensions) manager(ctx context.Context, p chat.Principal, id string) (chatapps.Actor, error) {
	if s == nil || s.Conversations == nil || p.TenantID == "" || p.SubjectID == "" || id == "" {
		return chatapps.Actor{}, chat.ErrUnavailable
	}
	if err := canManageChatConversation(ctx, s.Conversations, p, id); err != nil {
		return chatapps.Actor{}, err
	}
	return chatapps.Actor{Tenant: p.TenantID, Principal: p.SubjectID, Conversation: id}, nil
}

func (s *ChatExtensions) Install(ctx context.Context, p chat.Principal, conversation string, m chatapps.Manifest, scopes []string) (chatapps.Installation, error) {
	if s == nil || s.Apps == nil {
		return chatapps.Installation{}, chat.ErrUnavailable
	}
	a, err := s.manager(ctx, p, conversation)
	if err != nil {
		return chatapps.Installation{}, err
	}
	ctx, err = s.recordContext(ctx, p, conversation)
	if err != nil {
		return chatapps.Installation{}, err
	}
	policy := ctx.Value(recordAuthorizationKey{}).(recordAuthorization)
	ctx = chatapps.WithAudit(ctx, a, p.TenantID, policy.revision)
	v, err := s.Apps.Install(chatappstore.WithTenant(ctx, p.TenantID), a, m, scopes, p.SubjectID)
	if err != nil {
		return v, err
	}
	if _, ok := s.Apps.Repo.(chatapps.AuditedRepository); ok {
		return v, nil
	}
	return v, s.recordApp(ctx, p, v, "chat.app.install")
}

func (s *ChatExtensions) ListInstallations(ctx context.Context, p chat.Principal, conversation string) ([]chatapps.Installation, error) {
	if s == nil || s.Apps == nil || s.Apps.Repo == nil {
		return nil, chat.ErrUnavailable
	}
	a, err := s.actor(ctx, p, conversation)
	if err != nil {
		return nil, err
	}
	return s.Apps.Repo.ByConversation(chatappstore.WithTenant(ctx, p.TenantID), a.Tenant, a.Conversation)
}

func (s *ChatExtensions) ChangeStatus(ctx context.Context, p chat.Principal, conversation, id string, status chatapps.Status) (chatapps.Installation, error) {
	if s == nil || s.Apps == nil {
		return chatapps.Installation{}, chat.ErrUnavailable
	}
	a, err := s.manager(ctx, p, conversation)
	if err != nil {
		return chatapps.Installation{}, err
	}
	ctx, err = s.recordContext(ctx, p, conversation)
	if err != nil {
		return chatapps.Installation{}, err
	}
	policy := ctx.Value(recordAuthorizationKey{}).(recordAuthorization)
	ctx = chatapps.WithAudit(ctx, a, p.TenantID, policy.revision)
	v, err := s.Apps.ChangeStatus(chatappstore.WithTenant(ctx, p.TenantID), a, id, status)
	if err != nil {
		return v, err
	}
	if _, ok := s.Apps.Repo.(chatapps.AuditedRepository); ok {
		return v, nil
	}
	return v, s.recordApp(ctx, p, v, "chat.app.status")
}

func (s *ChatExtensions) Invoke(ctx context.Context, p chat.Principal, conversation, id string, cb chatapps.Callback) (chatapps.CallbackResult, error) {
	if s == nil || s.Apps == nil {
		return chatapps.CallbackResult{}, chat.ErrUnavailable
	}
	a, err := s.actor(ctx, p, conversation)
	if err != nil {
		return chatapps.CallbackResult{}, err
	}
	if s.Apps.Repo == nil {
		return chatapps.CallbackResult{}, chat.ErrUnavailable
	}
	ctx = chatappstore.WithTenant(ctx, p.TenantID)
	installation, err := s.Apps.Repo.Get(ctx, id)
	if err != nil {
		return chatapps.CallbackResult{}, err
	}
	if installation.Tenant != a.Tenant || installation.Conversation != a.Conversation {
		return chatapps.CallbackResult{}, chat.ErrPermissionDenied
	}
	if s.AppCommandAuthorization == nil {
		return chatapps.CallbackResult{}, chat.ErrPermissionDenied
	}
	var commandScope string
	for _, command := range installation.Manifest.Commands {
		if command.Name != cb.Command {
			continue
		}
		if commandScope != "" {
			return chatapps.CallbackResult{}, chat.ErrPermissionDenied
		}
		commandScope = command.Scope
	}
	if commandScope == "" || s.AppCommandAuthorization.AuthorizeChatAppCommand(ctx, p, conversation, installation.AppID, commandScope) != nil {
		return chatapps.CallbackResult{}, chat.ErrPermissionDenied
	}
	a.Scopes = []string{commandScope}
	cb.Actor = a
	cb.InstallationID = id
	return s.Apps.Invoke(ctx, a, id, cb)
}

func (s *ChatExtensions) Agent(ctx context.Context, p chat.Principal, conversation, id string) (chatapps.Agent, error) {
	if s == nil || s.Apps == nil {
		return chatapps.Agent{}, chat.ErrUnavailable
	}
	if _, err := s.actor(ctx, p, conversation); err != nil {
		return chatapps.Agent{}, err
	}
	a, err := s.Apps.Agent(chatappstore.WithTenant(ctx, p.TenantID), id)
	if err != nil {
		return chatapps.Agent{}, err
	}
	if a.InstallationID != p.TenantID+":"+conversation+":"+a.ID {
		return chatapps.Agent{}, chat.ErrPermissionDenied
	}
	return a, nil
}

func (s *ChatExtensions) ProposeIntent(ctx context.Context, p chat.Principal, conversation string, proposal chatapps.Proposal) (chatapps.ProposalReceipt, error) {
	if s == nil || s.Apps == nil {
		return chatapps.ProposalReceipt{}, chat.ErrUnavailable
	}
	a, err := s.actor(ctx, p, conversation)
	if err != nil {
		return chatapps.ProposalReceipt{}, err
	}
	proposal.Tenant = a.Tenant
	proposal.Principal = a.Principal
	proposal.Conversation = a.Conversation
	agent, err := s.Agent(ctx, p, conversation, proposal.AgentInstallation)
	if err != nil {
		return chatapps.ProposalReceipt{}, err
	}
	if agent.Status != chatapps.Active {
		return chatapps.ProposalReceipt{}, chatapps.ErrDenied
	}
	return s.Apps.ProposeIntent(chatappstore.WithTenant(ctx, p.TenantID), a, proposal)
}

// Report takes the read lane: a governance record is user initiated, so it is
// bounded but not shed before ordinary reads.
func (s *ChatExtensions) Report(ctx context.Context, p chat.Principal, r chatrecords.Report) error {
	if s == nil || s.Records == nil {
		return chat.ErrUnavailable
	}
	lease, err := s.lease(ctx, chatadmission.LaneRead, p.TenantID, r.ConversationID)
	if err != nil {
		return err
	}
	defer releaseChatLease(lease)
	c, err := s.conversation(ctx, p, r.ConversationID)
	if err != nil {
		return err
	}
	r.TenantID = p.TenantID
	r.ReporterID = p.SubjectID
	ctx = context.WithValue(ctx, recordAuthorizationKey{}, recordAuthorization{tenant: p.TenantID, actor: p.SubjectID, conversation: r.ConversationID, revision: c.Revision})
	return s.Records.Report(ctx, p.SubjectID, r)
}

func (s *ChatExtensions) Moderate(ctx context.Context, p chat.Principal, conversation, caseID, action, target, reason, evidence string) error {
	if s == nil || s.Records == nil {
		return chat.ErrUnavailable
	}
	lease, err := s.lease(ctx, chatadmission.LaneRead, p.TenantID, conversation)
	if err != nil {
		return err
	}
	defer releaseChatLease(lease)
	if _, err := s.manager(ctx, p, conversation); err != nil {
		return err
	}
	c, err := s.conversation(ctx, p, conversation)
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, recordAuthorizationKey{}, recordAuthorization{tenant: p.TenantID, actor: p.SubjectID, conversation: conversation, revision: c.Revision, manager: true})
	_, err = s.Records.ModerateAudited(ctx, p.SubjectID, p.TenantID, conversation, caseID, action, target, reason, evidence, c.Revision)
	return err
}

func (s *ChatExtensions) IssueEventCursor(ctx context.Context, p chat.Principal, conversation, installation string, after int64) (string, error) {
	if s == nil || s.Apps == nil {
		return "", chat.ErrUnavailable
	}
	if _, err := s.actor(ctx, p, conversation); err != nil {
		return "", err
	}
	if installation != "" {
		if s.Apps.Repo == nil {
			return "", chat.ErrUnavailable
		}
		v, err := s.Apps.Repo.Get(chatappstore.WithTenant(ctx, p.TenantID), installation)
		if err != nil {
			return "", err
		}
		if v.Tenant != p.TenantID || v.Conversation != conversation || v.Status != chatapps.Active {
			return "", chat.ErrPermissionDenied
		}
	}
	return s.Apps.IssueCursor(chatapps.Cursor{Tenant: p.TenantID, Principal: p.SubjectID, Conversation: conversation, Installation: installation, After: after, Expires: time.Now().Add(5 * time.Minute).Unix()})
}

// PullEvents is the integration event lane: derived, ephemeral work that the
// spec sheds before anything a person is waiting on.
func (s *ChatExtensions) PullEvents(ctx context.Context, p chat.Principal, conversation, token string, limit int) ([]chatapps.Event, string, error) {
	if s == nil || s.Apps == nil {
		return nil, "", chat.ErrUnavailable
	}
	lease, err := s.lease(ctx, chatadmission.LaneDerived, p.TenantID, conversation)
	if err != nil {
		return nil, "", err
	}
	defer releaseChatLease(lease)
	a, err := s.actor(ctx, p, conversation)
	if err != nil {
		return nil, "", err
	}
	return s.Apps.Pull(chatappstore.WithTenant(ctx, p.TenantID), a, token, limit)
}
