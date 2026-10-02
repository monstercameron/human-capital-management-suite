package application

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

// AgentAnnouncementHubAuthority keeps source identity and read decisions in
// the Documents hub. A placement alone does not imply a membership grant:
// PlaceDocument currently deploys a version, while Authorize owns access.
type AgentAnnouncementHubAuthority struct {
	Store *documenthubstore.Store
	Now   func() time.Time
}

func (a AgentAnnouncementHubAuthority) OfficialConversationPlacement(ctx context.Context, tenant, conversation, document, version, digest, anchor string) (bool, error) {
	if a.Store == nil || ctx == nil {
		return false, ErrAgentAnnouncementUnavailable
	}
	id, err := a.versionID(ctx, tenant, document, version)
	if err != nil {
		return false, err
	}
	placement, err := a.Store.CurrentPlacement(ctx, tenant, document, "placement", conversation)
	if errors.Is(err, documenthubstore.ErrNoDeployment) {
		return false, nil
	}
	if err != nil || !placement.IsOfficialPlacement() || placement.VersionID != id {
		return false, err
	}
	// The hub grants reads to individual subjects. It has no placement
	// membership grant contract, so an official deployment by itself cannot
	// bypass the per-member access checks.
	return false, nil
}

func (a AgentAnnouncementHubAuthority) AuthorizeDocumentRead(ctx context.Context, tenant, document, version, digest, anchor, home, subject string) error {
	if a.Store == nil || ctx == nil || strings.TrimSpace(home) == "" || strings.TrimSpace(subject) == "" {
		return ErrAgentAnnouncementDenied
	}
	var accessErr error
	if tenant == home {
		accessErr = a.Store.Authorize(ctx, tenant, document, "person", subject, documenthubstore.ActionRead)
	} else {
		if a.Now == nil {
			return ErrAgentAnnouncementDenied
		}
		accessErr = a.Store.AuthorizeCrossCompanyRead(ctx, tenant, document, home, subject, documenthubstore.ActionRead, a.Now().UTC())
	}
	if accessErr != nil {
		return accessErr
	}
	id, err := a.versionID(ctx, tenant, document, version)
	if err != nil {
		return err
	}
	valid, err := a.checkVersion(ctx, tenant, document, id, digest, anchor)
	if err != nil {
		return err
	}
	if !valid {
		return ErrAgentAnnouncementDenied
	}
	return nil
}

func (a AgentAnnouncementHubAuthority) versionID(ctx context.Context, tenant, document, version string) (string, error) {
	var id string
	number, numeric := strconv.ParseUint(version, 10, 64)
	err := a.Store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if numeric == nil {
			return tx.QueryRow(ctx, `SELECT id FROM (SELECT id,row_number() OVER (ORDER BY created_at,id) AS n FROM document_version WHERE tenant_id=$1 AND document_id=$2) v WHERE n=$3`, tenant, document, number).Scan(&id)
		}
		return tx.QueryRow(ctx, `SELECT id FROM document_version WHERE tenant_id=$1 AND document_id=$2 AND id=$3`, tenant, document, version).Scan(&id)
	})
	return id, err
}

func (a AgentAnnouncementHubAuthority) checkVersion(ctx context.Context, tenant, document, version, digest, anchor string) (bool, error) {
	var content, status string
	var published bool
	err := a.Store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT v.normalized_markdown,v.status,EXISTS(SELECT 1 FROM document_deployment d WHERE d.tenant_id=v.tenant_id AND d.document_id=v.document_id AND d.version_id=v.id) FROM document_version v JOIN document d ON d.tenant_id=v.tenant_id AND d.id=v.document_id WHERE v.tenant_id=$1 AND v.document_id=$2 AND v.id=$3 AND d.lifecycle<>'DISPOSED'`, tenant, document, version).Scan(&content, &status, &published)
	})
	if err != nil {
		return false, err
	}
	if anchor != "" {
		content = agentDocumentSection(content, anchor)
		if content == "" {
			return false, ErrAgentAnnouncementDenied
		}
	}
	return published && status != "retired" && personaRunT0ToolOutputDigest([]byte(content)) == digest, nil
}

// WithDocumentReadFence holds hub policy and live-pointer rows through the
// public commit. Grant revocation bumps document.policy_epoch in its own
// transaction; placement withdrawal changes the locked pointer.
func (a AgentAnnouncementHubAuthority) WithDocumentReadFence(ctx context.Context, tenant string, ids []string, fn func() error) error {
	if a.Store == nil || ctx == nil || fn == nil {
		return ErrAgentAnnouncementUnavailable
	}
	ids = append([]string(nil), ids...)
	sort.Strings(ids)
	return a.Store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		for _, id := range ids {
			var epoch uint64
			if err := tx.QueryRow(ctx, `SELECT policy_epoch FROM document WHERE tenant_id=$1 AND id=$2 AND lifecycle<>'DISPOSED' FOR SHARE`, tenant, id).Scan(&epoch); err != nil {
				return err
			}
			for _, query := range []string{
				`SELECT version_id FROM document_active_pointer WHERE tenant_id=$1 AND document_id=$2 ORDER BY scope_kind,scope_id FOR SHARE`,
				`SELECT id FROM document_crosscompany_grant WHERE tenant_id=$1 AND document_id=$2 ORDER BY id FOR SHARE`,
			} {
				rows, err := tx.Query(ctx, query, tenant, id)
				if err != nil {
					return err
				}
				for rows.Next() {
					var key string
					if err := rows.Scan(&key); err != nil {
						rows.Close()
						return err
					}
				}
				err = rows.Err()
				rows.Close()
				if err != nil {
					return err
				}
			}
		}
		return fn()
	})
}

func announcementDocumentCitation(citation agentsecurity.Citation) (AgentAnnouncementResolvedDocument, error) {
	if !strings.HasPrefix(citation.SourceID, "document:") || !personaRequestDigest(citation.Digest) {
		return AgentAnnouncementResolvedDocument{}, ErrAgentAnnouncementDenied
	}
	parts := strings.Split(strings.TrimPrefix(citation.SourceID, "document:"), "/version:")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(parts[0]+parts[1], "/\\ \t\r\n") {
		return AgentAnnouncementResolvedDocument{}, ErrAgentAnnouncementDenied
	}
	anchor := ""
	if citation.Location != citation.SourceID {
		if !strings.HasPrefix(citation.Location, citation.SourceID+"/section:") {
			return AgentAnnouncementResolvedDocument{}, ErrAgentAnnouncementDenied
		}
		anchor = strings.TrimPrefix(citation.Location, citation.SourceID+"/section:")
		if anchor == "" || agentdocref.Validate([]agentdocref.Reference{{DocumentID: parts[0], VersionMode: agentdocref.ModeLatestPublished, SectionAnchor: anchor, Label: "Source"}}, 1) != nil {
			return AgentAnnouncementResolvedDocument{}, ErrAgentAnnouncementDenied
		}
	}
	return AgentAnnouncementResolvedDocument{DocumentID: parts[0], Version: parts[1], Digest: citation.Digest, SectionAnchor: anchor}, nil
}
