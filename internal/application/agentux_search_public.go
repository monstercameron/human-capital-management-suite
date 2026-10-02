package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

// WorkspaceSectionDocumentAuthority extends the shared hub read decision to
// the exact bounded section bytes returned by workspace meaning search. It
// does not add an audience rule: the existing floor still checks each member.
type WorkspaceSectionDocumentAuthority struct{ AgentAnnouncementHubAuthority }

func (a WorkspaceSectionDocumentAuthority) AuthorizeDocumentRead(ctx context.Context, tenant, document, version, digest, anchor, home, subject string) error {
	err := a.AgentAnnouncementHubAuthority.AuthorizeDocumentRead(ctx, tenant, document, version, digest, anchor, home, subject)
	if err == nil || home != tenant || a.Store == nil {
		return err
	}
	id, resolveErr := a.versionID(ctx, tenant, document, version)
	if resolveErr != nil {
		return resolveErr
	}
	v, readErr := a.Store.ReadVersion(ctx, tenant, document, id, "person", subject)
	if readErr != nil {
		return readErr
	}
	section := agentDocumentSection(v.Markdown, anchor)
	if anchor == "" && len(documenthubstore.SplitSections(v.Markdown)) == 0 {
		section = v.Markdown
	}
	if section == "" || len(section) <= 8000 || personaRunT0ToolOutputDigest([]byte(boundedWorkspaceSection(section, 8000))) != digest {
		return err
	}
	valid, checkErr := a.checkVersion(ctx, tenant, document, id, personaRunT0ToolOutputDigest([]byte(section)), anchor)
	if checkErr != nil {
		return checkErr
	}
	if !valid {
		return ErrAgentAnnouncementDenied
	}
	return nil
}
