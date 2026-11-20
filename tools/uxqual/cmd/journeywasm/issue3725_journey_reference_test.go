package main

import (
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// "Send to chat" on a journey attaches the typed reference its address names.
func TestIssue3725_ShareReferenceIsBuiltFromTheJourneyAddress(t *testing.T) {
	cases := []struct {
		href, tenant string
		id           string
		ok           bool
	}{
		{"/workspace/app/journeys?journey=01a0-abc", "t1", "01a0-abc", true},
		{"https://hcm.example/workspace/app/journeys?journey=j1&locale=de-DE", "t1", "j1", true},
		{"/workspace/app/journeys?journey=", "t1", "", false},
		{"/workspace/app/journeys?journey=../x", "t1", "", false},
		{"/workspace/app/journeys?journey=j1&journey=j2", "t1", "", false},
		{"/workspace/app/journeys?journey=j1", "", "", false},
		{"/workspace/app/project?journey=j1", "t1", "", false},
	}
	for _, tc := range cases {
		ref := journeyShareReference(tc.href, tc.tenant)
		if tc.ok != (ref != nil) {
			t.Fatalf("%q: ref = %v, want ok=%v", tc.href, ref, tc.ok)
		}
		if ref != nil && (ref.GetKind() != chatv1.ReferenceKind_REFERENCE_KIND_JOURNEY || ref.GetId() != tc.id || ref.GetTenantId() != tc.tenant) {
			t.Fatalf("%q: ref = %v", tc.href, ref)
		}
	}
}

// The message carries the typed journey reference to the renderer; a reference
// the server withheld (no id) is dropped.
func TestIssue3725_PostJourneyReferencesReachTheMessage(t *testing.T) {
	post := &chatv1.Post{References: []*chatv1.Reference{
		{Kind: chatv1.ReferenceKind_REFERENCE_KIND_JOURNEY, TenantId: "t1", Id: "j1", Display: "Promotion request"},
		{Kind: chatv1.ReferenceKind_REFERENCE_KIND_JOURNEY, Display: "Restricted workflow request"},
		{Kind: chatv1.ReferenceKind_REFERENCE_KIND_PROJECT_TASK, TenantId: "t1", Id: "p1/k1"},
	}}
	got := chatWorkPostReferences(post)
	if len(got) != 2 || got[0].Kind != chatui.ChatReferenceJourney || got[0].ID != "j1" || got[1].Kind != chatui.ChatReferenceProjectTask {
		t.Fatalf("work references = %#v", got)
	}
}

// A card is read for a typed journey reference even when the body's address is
// on another origin or has been edited away.
func TestIssue3725_JourneyPreviewIDsIncludeTypedReferences(t *testing.T) {
	model := chatui.Model{EmbedOrigin: "https://app.example", Messages: []chatui.Message{
		{ID: "m1", Body: "see https://old.example/workspace/app/journeys?journey=j1", WorkReferences: []chatui.ChatReference{{Kind: chatui.ChatReferenceJourney, ID: "j1"}}},
		{ID: "m2", Body: "edited", WorkReferences: []chatui.ChatReference{{Kind: chatui.ChatReferenceJourney, ID: "j2"}}},
		{ID: "m3", Body: "https://old.example/workspace/app/journeys?journey=j3"},
	}}
	got := chatJourneyPreviewIDs(model)
	want := map[string]bool{"j1": true, "j2": true}
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want j1 and j2 only (a foreign address with no typed reference is not read)", got)
	}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("ids = %v", got)
		}
	}
}
