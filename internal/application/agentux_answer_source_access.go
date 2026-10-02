package application

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

// AgentUXAnswerSourceAccess composes Chat's reader projection with the hub's
// live access checks. It is installed on the chat service at assembly time.
type AgentUXAnswerSourceAccess struct{ Documents *documenthubstore.Store }

func (a AgentUXAnswerSourceAccess) ResolveAgentDocumentSource(ctx context.Context, reader chat.Principal, tenant, conversation string, source chat.AgentDocumentSource) (chat.AgentDocumentSource, error) {
	if a.Documents == nil || reader.TenantID != tenant || reader.SubjectID == "" {
		return source, chat.ErrPermissionDenied
	}
	baseTitle := strings.TrimSpace(strings.SplitN(source.Title, " · ", 2)[0])
	legacy := regexp.MustCompile(` \(version ([0-9]+)(?:,.*)?\)$`)
	legacyParts := legacy.FindStringSubmatch(baseTitle)
	baseTitle = legacy.ReplaceAllString(baseTitle, "")
	if source.DocumentID == "" {
		placements, err := a.Documents.ListReadableOfficialPlacementDocuments(ctx, tenant, conversation, "person", reader.SubjectID)
		if err != nil {
			return source, err
		}
		matches := 0
		for _, placement := range placements {
			if placement.Title == baseTitle {
				matches++
				source.DocumentID, source.VersionID = placement.DocumentID, placement.VersionID
			}
		}
		if matches == 0 {
			// Placement in this conversation is one way to find a cited
			// document, not the only one: a document the reader may read through
			// the hub's own check and that carries this exact title, once in the
			// tenant, is the document the answer named.
			found, err := a.Documents.ListReadableDeployedDocumentsByTitle(ctx, tenant, baseTitle, "person", reader.SubjectID)
			if err != nil {
				return source, err
			}
			if len(found) == 1 {
				matches = 1
				source.DocumentID, source.VersionID = found[0].DocumentID, found[0].VersionID
			}
		}
		if matches != 1 {
			return source, chat.ErrPermissionDenied
		}
		if len(legacyParts) > 1 {
			source.VersionID = ""
		}
	}
	// Resolve a legacy numeric pin before reading. Every subsequent read still
	// goes through ReadVersion as this reader, including an older version.
	if source.VersionID == "" && len(legacyParts) > 1 {
		sequence, _ := strconv.Atoi(legacyParts[1])
		if sequence < 1 {
			return source, chat.ErrPermissionDenied
		}
		err := a.Documents.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT id FROM document_version WHERE tenant_id=$1 AND document_id=$2 ORDER BY created_at,id OFFSET $3 LIMIT 1`, tenant, source.DocumentID, sequence-1).Scan(&source.VersionID)
		})
		if err != nil {
			return source, err
		}
	}
	if source.VersionID == "" {
		_, version, err := a.Documents.ReadPersonalDocument(ctx, tenant, reader.SubjectID, source.DocumentID)
		if err != nil {
			return source, err
		}
		source.VersionID = version.ID
	}
	version, err := a.Documents.ReadVersion(ctx, tenant, source.DocumentID, source.VersionID, "person", reader.SubjectID)
	if err != nil {
		return source, err
	}
	var sequence uint64
	err = a.Documents.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM document_version WHERE tenant_id=$1 AND document_id=$2 AND (created_at,id) <= ($3,$4)`, tenant, source.DocumentID, version.CreatedAt, version.ID).Scan(&sequence)
	})
	if err != nil || sequence == 0 {
		return source, chat.ErrPermissionDenied
	}
	source.Title = version.Title
	for _, block := range documenthubstore.DeriveBlocks(version.Markdown) {
		if source.SectionAnchor == "" && source.SectionTitle != "" && block.Heading == source.SectionTitle {
			source.SectionAnchor = block.ID
		}
		if block.ID == source.SectionAnchor || "sec-"+block.ID == source.SectionAnchor {
			source.Title += " · " + block.Heading
			break
		}
	}
	source.Title += " · " + displayVersion(sequence)
	query := url.Values{"document": {source.DocumentID}, "version": {source.VersionID}}
	target := url.URL{Path: "/workspace/app/docs", RawQuery: query.Encode(), Fragment: source.SectionAnchor}
	source.Href, source.Readable = target.String(), true
	return source, nil
}
