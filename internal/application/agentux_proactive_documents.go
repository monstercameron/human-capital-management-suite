package application

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type AgentAnnouncementServicePrincipalSource interface {
	CurrentAnnouncementServicePrincipal(context.Context, string, string) (AgentAnnouncementInstallationIdentity, error)
}

type AgentAnnouncementInstallationIdentity struct{ SubjectID, PrincipalID, ConversationID, PersonaID string }

type announcementPreviewKey struct{}

type AgentAnnouncementDocumentRefusal struct{ Unreadable int }

func (e AgentAnnouncementDocumentRefusal) Error() string {
	return "announcement sources cannot be read under current document authority"
}
func (e AgentAnnouncementDocumentRefusal) Is(target error) bool {
	return target == ErrAgentAnnouncementNotPublic || target == ErrAgentAnnouncementDenied
}

type AgentAnnouncementServicePrincipal struct {
	Agents     *agentstore.Store
	Personas   *agentpersonastore.Store
	Principals PersonaPrincipalAuthority
	TenantUUID func(values.TenantId) uuid.UUID
	Now        func() time.Time
}

func (s AgentAnnouncementServicePrincipal) CurrentAnnouncementServicePrincipal(ctx context.Context, tenant, installation string) (AgentAnnouncementInstallationIdentity, error) {
	if s.Agents == nil || s.Personas == nil || s.Principals == nil || s.TenantUUID == nil || s.Now == nil || ctx == nil {
		return AgentAnnouncementInstallationIdentity{}, ErrAgentAnnouncementUnavailable
	}
	key := s.TenantUUID(values.TenantId(tenant))
	if key == uuid.Nil {
		return AgentAnnouncementInstallationIdentity{}, ErrAgentAnnouncementDenied
	}
	var conversation, persona string
	var version int64
	err := s.Agents.RunTenantTx(ctx, key, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT conversation_id,persona_id,persona_version FROM persona_installations WHERE tenant_id=$1 AND installation_id=$2 AND state='ACTIVE'`, key, installation).Scan(&conversation, &persona, &version)
	})
	if err != nil {
		return AgentAnnouncementInstallationIdentity{}, ErrAgentAnnouncementDenied
	}
	scoped, err := s.Personas.Scoped(values.TenantId(tenant))
	if err != nil {
		return AgentAnnouncementInstallationIdentity{}, err
	}
	_, current, err := scoped.ReadCurrentPersonaAuthority(ctx, conversation, persona)
	if err != nil || current.InstallationID != installation || current.PersonaVersion != version {
		return AgentAnnouncementInstallationIdentity{}, ErrAgentAnnouncementDenied
	}
	binding, err := scoped.ResolvePersonaAgentPrincipal(ctx, persona, version)
	if err != nil {
		return AgentAnnouncementInstallationIdentity{}, err
	}
	principal, err := s.Principals.CurrentPrincipal(ctx, values.TenantId(tenant), binding.PrincipalID)
	if err != nil || principal.TenantID != key || principal.PrincipalID != binding.PrincipalID || principal.Kind != "SERVICE" || principal.Lifecycle != "ACTIVE" || principal.ExpiresAt != nil && !principal.ExpiresAt.After(s.Now()) || strings.TrimSpace(principal.Subject) == "" {
		return AgentAnnouncementInstallationIdentity{}, ErrAgentAnnouncementDenied
	}
	return AgentAnnouncementInstallationIdentity{SubjectID: principal.Subject, PrincipalID: principal.PrincipalID.String(), ConversationID: conversation, PersonaID: persona}, nil
}

// AgentAnnouncementReferenceResolver uses the shared reference value and
// section/budget rules, but resolves reads as a SERVICE. It never inserts a
// human into agentdocref.Invoker or borrows the announcement owner's grants.
type AgentAnnouncementReferenceResolver struct {
	Hub        *documenthubstore.Store
	Principals AgentAnnouncementServicePrincipalSource
}

func (r AgentAnnouncementReferenceResolver) ResolveAnnouncementDocuments(ctx context.Context, tenant, installation string, refs []agentdocref.Reference) ([]AgentAnnouncementResolvedDocument, error) {
	if r.Hub == nil || r.Principals == nil || ctx == nil || agentdocref.Validate(refs, 5) != nil || len(refs) == 0 {
		return nil, ErrAgentAnnouncementInvalid
	}
	identity, err := r.Principals.CurrentAnnouncementServicePrincipal(ctx, tenant, installation)
	if err != nil {
		return nil, err
	}
	var resolved []agentdocref.ResolvedDocument
	if preview, _ := ctx.Value(announcementPreviewKey{}).(bool); preview {
		if draft, ok := ctx.Value(announcementDraftKey{}).(agentstore.Announcement); ok && draft.TenantKey == tenant && draft.InstallationID == installation {
			identity.SubjectID = draft.OwnerID
		} else {
			return nil, ErrAgentAnnouncementDenied
		}
	}
	unreadable := 0
	for _, ref := range refs {
		document, err := r.resolveDocument(ctx, tenant, identity, ref)
		if err != nil {
			if errors.Is(err, ErrAgentAnnouncementDenied) || errors.Is(err, documenthubstore.ErrDenied) || errors.Is(err, dbport.ErrNoRows) {
				unreadable++
				continue
			}
			return nil, err
		}
		resolved = append(resolved, document)
	}
	if unreadable > 0 {
		return nil, AgentAnnouncementDocumentRefusal{Unreadable: unreadable}
	}
	budgeted := agentdocref.ApplyBudget(resolved, agentdocref.MaxContentCharacters)
	if len(budgeted) != len(refs) {
		return nil, ErrAgentAnnouncementDenied
	}
	out := make([]AgentAnnouncementResolvedDocument, 0, len(budgeted))
	for _, doc := range budgeted {
		if doc.Truncated {
			return nil, ErrAgentAnnouncementDenied
		}
		out = append(out, AgentAnnouncementResolvedDocument{DocumentID: doc.Reference.DocumentID, Version: strconv.FormatUint(doc.Version, 10), Title: doc.Title, Content: doc.Content, Digest: personaRunT0ToolOutputDigest([]byte(doc.Content)), SectionAnchor: doc.Reference.SectionAnchor})
	}
	return out, nil
}

func (r AgentAnnouncementReferenceResolver) resolveDocument(ctx context.Context, tenant string, identity AgentAnnouncementInstallationIdentity, ref agentdocref.Reference) (agentdocref.ResolvedDocument, error) {
	kind := "service"
	if preview, _ := ctx.Value(announcementPreviewKey{}).(bool); preview {
		kind = "person"
		if err := r.Hub.Authorize(ctx, tenant, ref.DocumentID, kind, identity.SubjectID, documenthubstore.ActionManage); err != nil {
			return agentdocref.ResolvedDocument{}, err
		}
	}
	if err := r.Hub.Authorize(ctx, tenant, ref.DocumentID, kind, identity.SubjectID, documenthubstore.ActionRead); err != nil {
		return agentdocref.ResolvedDocument{}, err
	}
	if kind == "service" {
		record, ok := ctx.Value(announcementDraftKey{}).(agentstore.Announcement)
		if !ok || record.ID == "" || record.TenantKey != tenant || record.InstallationID == "" {
			return agentdocref.ResolvedDocument{}, ErrAgentAnnouncementDenied
		}
		if err := r.Hub.AuthorizeAnnouncementDocument(ctx, tenant, ref.DocumentID, identity.SubjectID, record.ID); err != nil {
			return agentdocref.ResolvedDocument{}, err
		}
	}
	var versionID string
	var number uint64
	var err error
	if ref.VersionMode == agentdocref.ModeLatestPublished {
		deployment, err := r.Hub.CurrentPlacement(ctx, tenant, ref.DocumentID, "placement", identity.ConversationID)
		if err != nil || !deployment.IsOfficialPlacement() {
			deployment, err = r.Hub.ResolveDeployment(ctx, tenant, ref.DocumentID, "default", "")
		}
		if err != nil {
			return agentdocref.ResolvedDocument{}, err
		}
		versionID = deployment.VersionID
		number, err = (documentHubAgentDocumentReader{store: r.Hub}).versionNumber(ctx, tenant, ref.DocumentID, versionID)
		if err != nil {
			return agentdocref.ResolvedDocument{}, err
		}
	} else {
		number = ref.PinnedVersion
		versionID, err = (AgentAnnouncementHubAuthority{Store: r.Hub}).versionID(ctx, tenant, ref.DocumentID, strconv.FormatUint(number, 10))
		if err != nil {
			return agentdocref.ResolvedDocument{}, err
		}
	}
	version, err := r.Hub.ReadVersion(ctx, tenant, ref.DocumentID, versionID, kind, identity.SubjectID)
	if err != nil {
		return agentdocref.ResolvedDocument{}, err
	}
	content := version.Markdown
	if ref.SectionAnchor != "" {
		content = agentDocumentSection(content, ref.SectionAnchor)
	}
	if content == "" {
		return agentdocref.ResolvedDocument{}, ErrAgentAnnouncementDenied
	}
	valid, err := (AgentAnnouncementHubAuthority{Store: r.Hub}).checkVersion(ctx, tenant, ref.DocumentID, versionID, personaRunT0ToolOutputDigest([]byte(content)), ref.SectionAnchor)
	if err != nil || !valid {
		return agentdocref.ResolvedDocument{}, errors.Join(ErrAgentAnnouncementDenied, err)
	}
	return agentdocref.ResolvedDocument{Reference: ref, Version: number, Title: version.Title, Content: content}, nil
}
