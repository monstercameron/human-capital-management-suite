package application

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/ownerops"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentPersonaRunTenantRunner interface {
	RunTenantTx(context.Context, uuid.UUID, func(dbport.Tx) error) error
}

type AgentPersonaRunSource struct {
	DB            agentPersonaRunTenantRunner
	TenantUUID    func(values.TenantId) uuid.UUID
	Directory     PersonaCatalogDirectory
	Conversations *ChatDirectoryPersonaCatalogTargets
	Personas      *agentpersonastore.Store
}

func (s AgentPersonaRunSource) DashboardProjection(ctx context.Context, principal *trust.Principal, audience ownerops.Audience) ([]AgentOwnerRunProjection, error) {
	if s.DB == nil || s.TenantUUID == nil || principal == nil || principal.Tenant().Validate() != nil {
		return nil, ownerops.ErrDenied
	}
	if audience == ownerops.AudienceOperator && !principalHasAdministratorRole(principal) {
		return nil, ownerops.ErrDenied
	}
	tenantID := s.TenantUUID(principal.Tenant())
	if tenantID == uuid.Nil {
		return nil, ownerops.ErrDenied
	}
	rows := []agentPersonaRunRow{}
	err := s.DB.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		// A run is linked to the invocation that caused it by the request's
		// cause id. The row names the agent people see (the persona and its
		// version), not the manifest the run executed under, so the owner's
		// list reads "Policy Helper · version 4" and the catalog can name it.
		queryRows, err := tx.Query(ctx, `SELECT re.run_id,re.revision,pi.persona_id,pi.persona_version,re.state,COALESCE(re.terminal_code,''),
			COALESCE(re.failure_gate,''),COALESCE(re.failure_owner,''),COALESCE(re.failure_location,''),
			re.created_at,re.updated_at,re.deadline,COALESCE(re.lease_until,'epoch'::timestamptz),pi.installation_id,pi.invoker_id,pi.conversation_id,pv.display_name,
			COALESCE((SELECT po.principal_id FROM persona_owners po
				WHERE po.tenant_id=pi.tenant_id AND po.persona_id=pi.persona_id AND po.owner_role='BUSINESS_OWNER'
				ORDER BY po.assigned_at DESC LIMIT 1),'')
			FROM agent_run_execution re
			JOIN agent_run_request ar ON ar.tenant_id=re.tenant_id AND ar.request_id=re.admission_id
			JOIN persona_invocations pi ON pi.tenant_id=ar.tenant_id AND pi.invocation_id=ar.cause_id
			JOIN persona_versions pv ON pv.tenant_id=pi.tenant_id AND pv.persona_id=pi.persona_id AND pv.version=pi.persona_version::bigint
			WHERE re.tenant_id=$1 AND ar.source_kind='PERSONA_MENTION'
			ORDER BY re.updated_at DESC, re.run_id COLLATE "C" DESC LIMIT 50`, tenantID)
		if err != nil {
			return err
		}
		defer queryRows.Close()
		for queryRows.Next() {
			var row agentPersonaRunRow
			if err := queryRows.Scan(&row.RunID, &row.Revision, &row.AgentID, &row.Version, &row.State, &row.FailureCode,
				&row.FailureGate, &row.FailureOwner, &row.FailureLocation, &row.StartedAt, &row.UpdatedAt,
				&row.Deadline, &row.LeaseUntil, &row.InstallationID, &row.InvokerID, &row.ConversationID, &row.AgentName, &row.BusinessOwner); err != nil {
				return err
			}
			rows = append(rows, row)
		}
		return queryRows.Err()
	})
	if err != nil {
		return nil, err
	}
	out := make([]AgentOwnerRunProjection, 0, len(rows))
	for _, row := range rows {
		if !agentPersonaRunAudienceAllows(row, principal, audience) {
			continue
		}
		projection := AgentOwnerRunProjection{
			View:    ownerops.TaskView{TaskID: row.RunID, AgentID: row.AgentID, Version: row.Version, InstallationID: row.InstallationID, State: row.State, FailureCode: row.FailureCode, Revision: uint64(row.Revision)},
			OwnerID: row.BusinessOwner, RequestedBy: s.personLabel(ctx, principal.Tenant(), row.InvokerID),
			Location:    s.conversationLabel(ctx, principal, row.ConversationID, row.AgentName),
			FailureGate: row.FailureGate, FailureOwner: row.FailureOwner, FailureLocation: row.FailureLocation,
			StartedAt: row.StartedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(), Deadline: row.Deadline.UTC(), LeaseUntil: row.LeaseUntil.UTC(),
		}
		if strings.HasPrefix(projection.Location, "#") || strings.HasPrefix(projection.Location, "Your conversation with ") {
			projection.ConversationID = row.ConversationID
		}
		projection.ViewerDirect = row.InvokerID == principal.Subject() && strings.HasPrefix(projection.Location, "Your conversation with ")
		out = append(out, projection)
	}
	if len(out) == 0 {
		return nil, ownerops.ErrDenied
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (s AgentPersonaRunSource) personLabel(ctx context.Context, tenant values.TenantId, subject string) string {
	if s.Directory == nil {
		return subject
	}
	target, err := s.Directory.ResolvePersonaCatalogTarget(ctx, tenant, subject)
	if err != nil || target.ID != subject || strings.TrimSpace(target.Label) == "" {
		return subject
	}
	if len(strings.Fields(target.Label)) < 2 {
		if full := agentControlSubjectLabel(subject); len(strings.Fields(full)) >= 2 {
			return full
		}
	}
	return target.Label
}

func (s AgentPersonaRunSource) conversationLabel(ctx context.Context, principal *trust.Principal, conversationID, agentName string) string {
	if s.Conversations == nil || s.Conversations.Chat == nil || principal == nil || conversationID == "" {
		return privateAgentConversationLabel(agentName)
	}
	chatPrincipal := chat.Principal{TenantID: principal.Tenant().String(), SubjectID: principal.Subject()}
	rooms, _, err := s.Conversations.listRooms(ctx, chatPrincipal, principal.Tenant())
	if err != nil {
		return ""
	}
	for _, room := range rooms {
		if room.ID != conversationID {
			continue
		}
		if room.Kind == chat.Direct {
			name := s.directConversationPeerLabel(ctx, chatPrincipal, principal.Tenant(), room.ID)
			if name != "" {
				return "Your conversation with " + name
			}
		}
		if strings.TrimSpace(room.Name) == "" {
			return ""
		}
		if room.Kind == chat.PublicChannel || room.Kind == chat.PrivateChannel {
			return "#" + strings.TrimPrefix(room.Name, "#")
		}
		return room.Name
	}
	return privateAgentConversationLabel(agentName)
}

func privateAgentConversationLabel(agentName string) string {
	if strings.TrimSpace(agentName) == "" {
		return ""
	}
	return "A person's private conversation with " + strings.TrimSpace(agentName)
}

func (s AgentPersonaRunSource) directConversationPeerLabel(ctx context.Context, principal chat.Principal, tenant values.TenantId, conversationID string) string {
	members, _, err := s.Conversations.listMembers(ctx, principal, tenant, conversationID)
	if err != nil {
		return ""
	}
	for _, member := range members {
		if member.SubjectID == principal.SubjectID {
			continue
		}
		if label := s.agentIdentityLabel(ctx, tenant, member); label != "" {
			return label
		}
		if s.Conversations.Directory == nil {
			return ""
		}
		target, err := s.Conversations.Directory.ResolvePersonaCatalogTarget(ctx, tenant, member.SubjectID)
		if err != nil || target.ID != member.SubjectID || strings.TrimSpace(target.Label) == "" {
			return ""
		}
		return target.Label
	}
	return ""
}

func (s AgentPersonaRunSource) agentIdentityLabel(ctx context.Context, tenant values.TenantId, member chat.Membership) string {
	if values.TenantId(member.HomeTenantID) != tenant {
		return ""
	}
	store, ok := s.personaTenant(ctx, tenant)
	if !ok {
		return ""
	}
	identity, err := store.LookupPersonaChatIdentity(ctx, member.SubjectID)
	if err != nil || !identity.Active {
		return ""
	}
	published, err := store.ListPublished(ctx)
	if err != nil {
		return ""
	}
	for _, version := range published {
		if version.PersonaID == identity.PersonaID && strings.TrimSpace(version.DisplayName) != "" {
			return strings.TrimSpace(version.DisplayName)
		}
	}
	return ""
}

func (s AgentPersonaRunSource) personaTenant(ctx context.Context, tenant values.TenantId) (interface {
	LookupPersonaChatIdentity(context.Context, string) (agentpersonastore.PersonaChatIdentity, error)
	ListPublished(context.Context) ([]agentpersonastore.PersonaVersion, error)
}, bool) {
	if s.Personas == nil {
		return nil, false
	}
	store, err := s.Personas.ForTenant(ctx, tenant)
	if err != nil || store == nil {
		return nil, false
	}
	return store, true
}

type agentPersonaRunRow struct {
	RunID, AgentID, Version, State, FailureCode string
	FailureGate, FailureOwner, FailureLocation  string
	InstallationID, InvokerID, ConversationID   string
	AgentName, BusinessOwner                    string
	Revision                                    int64
	StartedAt, UpdatedAt, Deadline, LeaseUntil  time.Time
}

func agentPersonaRunAudienceAllows(row agentPersonaRunRow, principal *trust.Principal, audience ownerops.Audience) bool {
	switch audience {
	case ownerops.AudienceMember:
		return row.InvokerID == principal.Subject()
	case ownerops.AudienceOwner:
		return row.BusinessOwner == principal.Subject()
	case ownerops.AudienceOperator:
		return principalHasAdministratorRole(principal)
	default:
		return false
	}
}

func principalHasAdministratorRole(principal *trust.Principal) bool {
	for _, role := range principal.Roles() {
		if roleaccess.IsAdministratorRole(role) {
			return true
		}
	}
	return false
}

type AgentCombinedRunSource struct {
	Sources []AgentOwnerRunSource
}

func (s AgentCombinedRunSource) DashboardProjection(ctx context.Context, principal *trust.Principal, audience ownerops.Audience) ([]AgentOwnerRunProjection, error) {
	out := []AgentOwnerRunProjection{}
	for _, source := range s.Sources {
		if source == nil {
			continue
		}
		rows, err := source.DashboardProjection(ctx, principal, audience)
		if errors.Is(err, ownerops.ErrDenied) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = mergeAgentOwnerRunProjections(out, rows)
	}
	if len(out) == 0 {
		return nil, ownerops.ErrDenied
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

var _ AgentOwnerRunSource = AgentCombinedRunSource{}
