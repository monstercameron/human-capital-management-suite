package application

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

// ensureLocalAgentDemoWorkspaceReaders makes the documents the preparation
// places in the demo conversation readable to every member of the workspace,
// so Assistant's workspace search has documents to find. It adds nothing but
// ordinary read grants, through the hub's own sharing path with the document's
// owner as the actor, exactly as that person would share the document with a
// colleague: no grant is invented for a group, a folder or a placement, and a
// person with a standing deny still cannot read the document, which then does
// not count as workspace-wide. A member who already holds a read grant is left
// alone, so a rerun adds only newly joined members and returns zero.
//
// It returns how many documents became readable by the whole workspace in this
// run. The seeded corpus holds no document shared with everyone (its shares
// are per-person subsets and its folders are one person's own filing), so these
// are the documents that make workspace search demonstrable.
func ensureLocalAgentDemoWorkspaceReaders(ctx context.Context, documents *documenthubstore.Store, directory WorkspaceDocumentMembers, tenant, owner, scope string) (int, error) {
	if documents == nil || isNilPersonaOutputPort(directory) || tenant == "" || owner == "" || scope == "" {
		return 0, fmt.Errorf("local agent demo workspace readers: dependencies are unavailable")
	}
	people, err := directory.WorkspaceDocumentMembers(ctx, tenant)
	if err != nil {
		return 0, fmt.Errorf("read the workspace directory: %w", err)
	}
	became := 0
	for _, document := range []localAgentDemoDocument{
		{title: localAgentDemoPolicyTitle, probe: localAgentDemoPolicyProbe},
		{title: localAgentDemoHolidayTitle, probe: localAgentDemoHolidayProbe},
	} {
		hits, err := documents.SearchOfficialPlacementLexical(ctx, tenant, scope, document.probe, "person", owner)
		if err != nil {
			return became, fmt.Errorf("find %s: %w", document.title, err)
		}
		documentID := ""
		for _, hit := range hits {
			if hit.Title == document.title {
				documentID = hit.DocumentID
			}
		}
		if documentID == "" {
			continue
		}
		before, err := documents.WorkspaceDocumentReadable(ctx, tenant, documentID, people)
		if err != nil {
			return became, err
		}
		for _, member := range people {
			if documents.Authorize(ctx, tenant, documentID, "person", member, documenthubstore.ActionRead) == nil {
				continue
			}
			if _, err := documents.ShareDocument(ctx, tenant, documentID, owner, documenthubstore.GrantInput{SubjectKind: "person", SubjectID: member, Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow, Purpose: "workspace_wide_read"}); err != nil {
				return became, fmt.Errorf("share %s with %s: %w", document.title, member, err)
			}
		}
		after, err := documents.WorkspaceDocumentReadable(ctx, tenant, documentID, people)
		if err != nil {
			return became, err
		}
		if after && !before {
			became++
		}
	}
	return became, nil
}
