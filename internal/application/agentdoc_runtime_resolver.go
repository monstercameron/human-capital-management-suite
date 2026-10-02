package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

var (
	errAgentDocumentResolution  = errors.New("application: agent document resolution unavailable")
	errAgentDocumentNotFound    = errors.New("application: agent document is not visible")
	errAgentDocumentUnpublished = errors.New("application: agent document version is not published")
)

type agentDocumentVersion struct {
	number  uint64
	title   string
	content string
}

// agentDocumentReadSource is the injectable seam used by the resolver tests.
// Production's implementation delegates content reads to ReadVersion, the
// documentation hub's existing invoker-authorized read path.
type agentDocumentReadSource interface {
	LatestPublished(context.Context, agentdocref.Invoker, string) (agentDocumentVersion, error)
	PinnedPublished(context.Context, agentdocref.Invoker, string, uint64) (agentDocumentVersion, error)
}

type agentDocumentResolver struct {
	source agentDocumentReadSource
}

// NewAgentDocumentResolver adapts the existing documentation-hub store to the
// authority-free runtime resolver contract. The store remains the sole owner
// of read authorization and content access.
func NewAgentDocumentResolver(store *documenthubstore.Store) (agentdocref.Resolver, error) {
	if store == nil {
		return nil, errAgentDocumentResolution
	}
	return &agentDocumentResolver{source: documentHubAgentDocumentReader{store: store}}, nil
}

func newAgentDocumentResolver(source agentDocumentReadSource) *agentDocumentResolver {
	return &agentDocumentResolver{source: source}
}

func (r *agentDocumentResolver) Resolve(ctx context.Context, invoker agentdocref.Invoker, refs []agentdocref.Reference) ([]agentdocref.ResolvedDocument, []agentdocref.Omission, error) {
	if r == nil || r.source == nil || ctx == nil || strings.TrimSpace(invoker.TenantID) == "" || strings.TrimSpace(invoker.SubjectID) == "" || invoker.TenantID != strings.TrimSpace(invoker.TenantID) || invoker.SubjectID != strings.TrimSpace(invoker.SubjectID) {
		return nil, nil, errAgentDocumentResolution
	}
	if err := agentdocref.Validate(refs, agentdocref.MaxPersonaReferences); err != nil {
		return nil, nil, fmt.Errorf("%w: invalid references: %v", errAgentDocumentResolution, err)
	}
	resolved := make([]agentdocref.ResolvedDocument, 0, len(refs))
	omitted := make([]agentdocref.Omission, 0)
	for _, ref := range refs {
		version, err := r.read(ctx, invoker, ref)
		if err != nil {
			switch {
			case errors.Is(err, errAgentDocumentNotFound):
				// Missing and unreadable are intentionally indistinguishable. An
				// empty label tells the notice renderer to use a generic count.
				omitted = append(omitted, agentdocref.Omission{Reason: agentdocref.NotFound})
				continue
			case errors.Is(err, errAgentDocumentUnpublished):
				omitted = append(omitted, agentdocref.Omission{Label: ref.Label, Reason: agentdocref.NotPublished})
				continue
			default:
				return nil, nil, fmt.Errorf("%w: %v", errAgentDocumentResolution, err)
			}
		}
		content := version.content
		if ref.SectionAnchor != "" {
			content = agentDocumentSection(content, ref.SectionAnchor)
			if content == "" {
				omitted = append(omitted, agentdocref.Omission{Label: ref.Label, Reason: agentdocref.NotFound})
				continue
			}
		}
		resolved = append(resolved, agentdocref.ResolvedDocument{Reference: ref, Version: version.number, Title: version.title, Content: content})
	}
	budgeted := agentdocref.ApplyBudget(resolved, agentdocref.MaxContentCharacters)
	for i := len(budgeted); i < len(resolved); i++ {
		omitted = append(omitted, agentdocref.Omission{Label: resolved[i].Reference.Label, Reason: agentdocref.OverBudget})
	}
	if len(budgeted) > 0 && budgeted[len(budgeted)-1].Truncated {
		omitted = append(omitted, agentdocref.Omission{Label: budgeted[len(budgeted)-1].Reference.Label, Reason: agentdocref.OverBudget})
	}
	return budgeted, omitted, nil
}

func (r *agentDocumentResolver) read(ctx context.Context, invoker agentdocref.Invoker, ref agentdocref.Reference) (agentDocumentVersion, error) {
	if ref.VersionMode == agentdocref.ModePinned {
		return r.source.PinnedPublished(ctx, invoker, ref.DocumentID, ref.PinnedVersion)
	}
	return r.source.LatestPublished(ctx, invoker, ref.DocumentID)
}

type documentHubAgentDocumentReader struct {
	store *documenthubstore.Store
}

func (r documentHubAgentDocumentReader) LatestPublished(ctx context.Context, invoker agentdocref.Invoker, documentID string) (agentDocumentVersion, error) {
	if err := r.authorize(ctx, invoker, documentID); err != nil {
		return agentDocumentVersion{}, err
	}
	deployment, err := r.store.ResolveDeployment(ctx, invoker.TenantID, documentID, "default", "")
	if errors.Is(err, documenthubstore.ErrNoDeployment) {
		return agentDocumentVersion{}, errAgentDocumentUnpublished
	}
	if err != nil {
		return agentDocumentVersion{}, err
	}
	number, err := r.versionNumber(ctx, invoker.TenantID, documentID, deployment.VersionID)
	if err != nil {
		return agentDocumentVersion{}, err
	}
	return r.readVersion(ctx, invoker, documentID, deployment.VersionID, number)
}

func (r documentHubAgentDocumentReader) PinnedPublished(ctx context.Context, invoker agentdocref.Invoker, documentID string, number uint64) (agentDocumentVersion, error) {
	if err := r.authorize(ctx, invoker, documentID); err != nil {
		return agentDocumentVersion{}, err
	}
	var versionID, status string
	var published bool
	err := r.store.RunTenantTx(ctx, invoker.TenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT ordered.id, ordered.status,
			EXISTS (SELECT 1 FROM document_deployment d WHERE d.tenant_id=$1 AND d.document_id=$2 AND d.version_id=ordered.id)
			FROM (SELECT id,status,row_number() OVER (ORDER BY created_at,id) AS number FROM document_version WHERE tenant_id=$1 AND document_id=$2) ordered
			WHERE ordered.number=$3`, invoker.TenantID, documentID, number).Scan(&versionID, &status, &published)
	})
	if errors.Is(err, dbport.ErrNoRows) {
		return agentDocumentVersion{}, errAgentDocumentNotFound
	}
	if err != nil {
		return agentDocumentVersion{}, err
	}
	if !published || strings.EqualFold(status, "retired") {
		return agentDocumentVersion{}, errAgentDocumentUnpublished
	}
	return r.readVersion(ctx, invoker, documentID, versionID, number)
}

func (r documentHubAgentDocumentReader) authorize(ctx context.Context, invoker agentdocref.Invoker, documentID string) error {
	if r.store == nil || ctx == nil {
		return errAgentDocumentResolution
	}
	if err := r.store.Authorize(ctx, invoker.TenantID, documentID, "person", invoker.SubjectID, documenthubstore.ActionRead); err != nil {
		if errors.Is(err, documenthubstore.ErrDenied) {
			return errAgentDocumentNotFound
		}
		return err
	}
	return nil
}

func (r documentHubAgentDocumentReader) versionNumber(ctx context.Context, tenantID, documentID, versionID string) (uint64, error) {
	var number uint64
	err := r.store.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT ordered.number FROM
			(SELECT id,row_number() OVER (ORDER BY created_at,id) AS number FROM document_version WHERE tenant_id=$1 AND document_id=$2) ordered
			WHERE ordered.id=$3`, tenantID, documentID, versionID).Scan(&number)
	})
	if errors.Is(err, dbport.ErrNoRows) {
		return 0, errAgentDocumentNotFound
	}
	return number, err
}

func (r documentHubAgentDocumentReader) readVersion(ctx context.Context, invoker agentdocref.Invoker, documentID, versionID string, number uint64) (agentDocumentVersion, error) {
	version, err := r.store.ReadVersion(ctx, invoker.TenantID, documentID, versionID, "person", invoker.SubjectID)
	if errors.Is(err, documenthubstore.ErrDenied) {
		return agentDocumentVersion{}, errAgentDocumentNotFound
	}
	if err != nil {
		return agentDocumentVersion{}, err
	}
	if version.DocumentID != documentID || version.ID != versionID || number == 0 || strings.TrimSpace(version.Markdown) == "" {
		return agentDocumentVersion{}, errAgentDocumentNotFound
	}
	return agentDocumentVersion{number: number, title: version.Title, content: version.Markdown}, nil
}

func agentDocumentSection(markdown, anchor string) string {
	for _, section := range documenthubstore.SplitSections(markdown) {
		if section.BlockID == anchor {
			return section.Text
		}
	}
	return ""
}

var _ agentdocref.Resolver = (*agentDocumentResolver)(nil)
