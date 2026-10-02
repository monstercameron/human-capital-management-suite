package main

import (
	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatPostReferencesViewer reports whether a post carries a mention reference
// for the viewer (CHATBUG-071). A mention chosen from the list is stored as a
// reference; the sound used to look only for "@name" in the text, which misses
// a member whose name the sender's list spells differently from their own
// session. The reference's tenant is the mentioned person's home tenant, which
// this client takes to be the signed-in tenant, as it does for reactions.
func chatPostReferencesViewer(post *chatv1.Post, model chatui.Model) bool {
	if post == nil || model.CurrentUser == "" {
		return false
	}
	for _, reference := range post.GetReferences() {
		if reference.GetKind() != chatv1.ReferenceKind_REFERENCE_KIND_PERSON_MENTION || reference.GetId() != model.CurrentUser {
			continue
		}
		if reference.GetTenantId() == "" || model.CurrentTenantID == "" || reference.GetTenantId() == model.CurrentTenantID {
			return true
		}
	}
	return false
}
