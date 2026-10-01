package chat

import "strings"

// WorkflowPageReferenceKind is a reference-chip kind for a page draft or
// run. It carries selectors only; entered workflow values never enter chat.
const WorkflowPageReferenceKind ReferenceKind = "WORKFLOW_PAGE_REFERENCE"

// WorkflowPageReference is the server-owned selector behind a shared chip.
// Recipients resolve it with their own authority when they open the chip.
type WorkflowPageReference struct {
	TenantID    string
	WorkflowID  string
	PageID      string
	PageVersion int64
	DraftID     string
	RunID       string
}

func (reference WorkflowPageReference) valid() bool {
	return strings.TrimSpace(reference.TenantID) != "" &&
		strings.TrimSpace(reference.WorkflowID) != "" &&
		strings.TrimSpace(reference.PageID) != "" &&
		reference.PageVersion > 0 &&
		(reference.DraftID != "" || reference.RunID != "")
}

// NewWorkflowPageReference builds the durable chat Reference without a
// display snapshot. The page values are intentionally not accepted.
func NewWorkflowPageReference(reference WorkflowPageReference) (Reference, bool) {
	if !reference.valid() {
		return Reference{}, false
	}
	id := reference.WorkflowID + ":" + reference.PageID
	if reference.RunID != "" {
		id += ":run:" + reference.RunID
	} else {
		id += ":draft:" + reference.DraftID
	}
	return Reference{
		Kind: WorkflowPageReferenceKind, TenantID: reference.TenantID, ID: id,
		Display: "Workflow page", // neutral fallback until recipient resolution
	}, true
}

// WorkflowPageReferenceChip is the recipient-scoped rendering projection.
// An unauthorized recipient gets a neutral inert chip with no selector.
type WorkflowPageReferenceChip struct {
	Label       string
	WorkflowID  string
	PageID      string
	PageVersion int64
	Href        string
	Readable    bool
}

func ResolveWorkflowPageReferenceChip(reference WorkflowPageReference, readable bool, workflowTitle string, href string) WorkflowPageReferenceChip {
	if !readable || !reference.valid() {
		return WorkflowPageReferenceChip{Label: "Restricted workflow reference"}
	}
	return WorkflowPageReferenceChip{
		Label: workflowTitle, WorkflowID: reference.WorkflowID, PageID: reference.PageID,
		PageVersion: reference.PageVersion, Href: href, Readable: true,
	}
}
