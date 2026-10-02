package application

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// agentux034TypicalMember is the person whose reading a placement row counts:
// a member of the room other than the viewing administrator, preferring one who
// is not an administrator. An administrator may open documents an ordinary
// member may not, so counting what the administrator sees would tell the
// administrator the agent can answer when it cannot for the people who ask it
// (AGENTUX-034). A room with nobody else in it falls back to the viewer.
func agentux034TypicalMember(target productui.PersonaAdminTarget, viewer string) string {
	other := ""
	for _, member := range target.Members {
		if member.ID == "" || member.ID == viewer {
			continue
		}
		if !roleaccess.IsAdministratorRole(member.Role) {
			return member.ID
		}
		if other == "" {
			other = member.ID
		}
	}
	if other != "" {
		return other
	}
	return viewer
}

// agentux034MemberDocuments reads the official documents of one conversation as
// the typical member sees them. The count is the member's; the titles listed
// are the ones the member reads that the viewing administrator may also open,
// so the list never names a document the viewer cannot see and never lists one
// the count leaves out.
func agentux034MemberDocuments(ctx context.Context, documents PersonaAdminPlacementDocumentReader, tenant, viewer string, target productui.PersonaAdminTarget, conversation string) (int, []string, error) {
	member := agentux034TypicalMember(target, viewer)
	memberRows, err := documents.ListPersonaAdminPlacementDocuments(ctx, tenant, member, conversation)
	if err != nil {
		return 0, nil, err
	}
	viewerRows := memberRows
	if member != viewer {
		if viewerRows, err = documents.ListPersonaAdminPlacementDocuments(ctx, tenant, viewer, conversation); err != nil {
			return 0, nil, err
		}
	}
	visible := make(map[string]bool, len(viewerRows))
	for _, row := range viewerRows {
		visible[row.DocumentID] = true
	}
	var titles []string
	for _, row := range memberRows {
		if strings.TrimSpace(row.Title) != "" && visible[row.DocumentID] {
			titles = append(titles, row.Title)
		}
	}
	return len(memberRows), titles, nil
}
