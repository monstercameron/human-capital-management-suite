package main

import (
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// "Send to chat" attaches the typed reference its link points at.
func TestIssue3752_ShareReferenceIsBuiltFromTheTaskAddress(t *testing.T) {
	cases := []struct {
		href, tenant string
		id           string
		ok           bool
	}{
		{"/workspace/app/project?project=p1&task=k1", "t1", "p1/k1", true},
		{"/workspace/app/project?project=p1", "t1", "p1", true},
		{"/workspace/app/project?project=p1&task=k1&view=task", "t1", "p1/k1", true},
		{"/workspace/app/project?task=k1", "t1", "", false},
		{"/workspace/app/project?project=p1/../x", "t1", "", false},
		{"/workspace/app/project?project=p1", "", "", false},
		{"/elsewhere?project=p1", "t1", "", false},
	}
	for _, tc := range cases {
		ref := projectShareReference(tc.href, tc.tenant)
		if tc.ok != (ref != nil) {
			t.Fatalf("%q: ref = %v, want ok=%v", tc.href, ref, tc.ok)
		}
		if ref != nil && (ref.GetKind() != chatv1.ReferenceKind_REFERENCE_KIND_PROJECT_TASK || ref.GetId() != tc.id || ref.GetTenantId() != tc.tenant) {
			t.Fatalf("%q: ref = %v", tc.href, ref)
		}
	}
}

// The message carries the typed reference to the renderer; a reference the
// server withheld (no id) and every other kind are not project references.
func TestIssue3752_PostProjectReferencesReachTheMessage(t *testing.T) {
	post := &chatv1.Post{References: []*chatv1.Reference{
		{Kind: chatv1.ReferenceKind_REFERENCE_KIND_PROJECT_TASK, TenantId: "t1", Id: "p1/k1", Display: "Ship it"},
		{Kind: chatv1.ReferenceKind_REFERENCE_KIND_PROJECT_TASK, Display: "Restricted project item"},
		{Kind: chatv1.ReferenceKind_REFERENCE_KIND_PERSON_MENTION, TenantId: "t1", Id: "u1", Display: "Ana"},
	}}
	got := chatWorkPostReferences(post)
	if len(got) != 1 || got[0].Kind != chatui.ChatReferenceProjectTask || got[0].ID != "p1/k1" {
		t.Fatalf("work references = %#v", got)
	}
	if mentions := personaChatPostReferences(post); len(mentions) != 1 || mentions[0].Kind != "PERSON_MENTION" {
		t.Fatalf("mention references changed: %#v", mentions)
	}
}

// A card is read for a typed reference even when the body's address is on
// another origin or has been edited away.
func TestIssue3752_PreviewRefsIncludeTypedReferences(t *testing.T) {
	model := chatui.Model{EmbedOrigin: "https://app.example", Messages: []chatui.Message{
		{ID: "m1", Body: "see https://old.example/workspace/app/project?project=p1&task=k1", WorkReferences: []chatui.ChatReference{{Kind: chatui.ChatReferenceProjectTask, ID: "p1/k1"}}},
		{ID: "m2", Body: "edited", WorkReferences: []chatui.ChatReference{{Kind: chatui.ChatReferenceProjectTask, ID: "p2"}}},
		{ID: "m3", Body: "https://app.example/workspace/app/project?project="},
	}}
	refs := chatProjectTaskPreviewRefs(model)
	keys := map[string]bool{}
	for _, ref := range refs {
		keys[chatui.ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID)] = true
	}
	if len(refs) != 2 || !keys[chatui.ProjectTaskPreviewKey("p1", "k1")] || !keys[chatui.ProjectTaskPreviewKey("p2", "")] {
		t.Fatalf("refs = %#v", refs)
	}
}
