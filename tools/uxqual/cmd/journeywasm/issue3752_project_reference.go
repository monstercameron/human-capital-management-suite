package main

import (
	"net/url"
	"strings"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// projectShareReference is the typed reference "Send to chat" attaches to the
// post it writes (issue 3752): the board or task the shared address names. The
// server authorizes the sender against it and keeps only identifiers, so the
// card, backlinks and renames follow the data instead of the text of the link.
// It is nil when the address does not name a project, in which case the post
// goes out as it always did.
func projectShareReference(href, tenant string) *chatv1.Reference {
	parsed, err := url.Parse(href)
	if err != nil || parsed.Path != "/workspace/app/project" || strings.TrimSpace(tenant) == "" {
		return nil
	}
	values := parsed.Query()
	projectID, taskID := values.Get("project"), values.Get("task")
	if chatui.ProjectTaskPreviewKey(projectID, taskID) == "" || projectID == "" {
		return nil
	}
	id := projectID
	if taskID != "" {
		id += "/" + taskID
	}
	return &chatv1.Reference{Kind: chatv1.ReferenceKind_REFERENCE_KIND_PROJECT_TASK, TenantId: tenant, Id: id}
}

// chatWorkPostReferences are the typed project references a post carries. A
// reference the server withheld from this reader has no id and is dropped: the
// renderer then has nothing to name.
func chatWorkPostReferences(post *chatv1.Post) []chatui.ChatReference {
	var out []chatui.ChatReference
	for _, ref := range post.GetReferences() {
		if ref.GetKind() == chatv1.ReferenceKind_REFERENCE_KIND_PROJECT_TASK && strings.TrimSpace(ref.GetId()) != "" {
			out = append(out, chatui.ChatReference{Kind: chatui.ChatReferenceProjectTask, TenantID: ref.GetTenantId(), ID: ref.GetId(), Display: ref.GetDisplay()})
		}
		// Issue 3725: the workflow request a post shares.
		if ref.GetKind() == chatv1.ReferenceKind_REFERENCE_KIND_JOURNEY && strings.TrimSpace(ref.GetId()) != "" {
			out = append(out, chatui.ChatReference{Kind: chatui.ChatReferenceJourney, TenantID: ref.GetTenantId(), ID: ref.GetId(), Display: ref.GetDisplay()})
		}
	}
	return out
}
