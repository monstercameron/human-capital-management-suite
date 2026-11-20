package main

import (
	"strings"
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
)

func issue3962Uploaded(t *testing.T, s *chatattach001State, name, contentType string) string {
	t.Helper()
	key, ok := s.add("room", name, contentType, "blob:x", 8)
	if !ok || s.complete("room", key, "artifact-"+key, contentType, 8, 200) != chatattach001Uploaded {
		t.Fatal("upload did not complete")
	}
	return key
}

// TestIssue3962_DescriptionIsSentWithTheAttachment proves what the author wrote
// in the composer goes out on the media reference, beside the file name.
func TestIssue3962_DescriptionIsSentWithTheAttachment(t *testing.T) {
	s := &chatattach001State{}
	key := issue3962Uploaded(t, s, "chart.png", "image/png")
	s.setDescription("room", key, "  Bar chart of hires by team  ")
	refs, _, ok := s.begin("room", "tenant", "", nil)
	if !ok || len(refs) != 1 {
		t.Fatalf("begin = %v %v", refs, ok)
	}
	if got := refs[0]; got.GetAltText() != "Bar chart of hires by team" || got.GetDecorative() || got.GetDisplay() != "chart.png" || got.GetKind() != chatv1.ReferenceKind_REFERENCE_KIND_MEDIA {
		t.Fatalf("reference = %+v", got)
	}
}

// TestIssue3962_DecorativeSendsNoDescriptionAndKeepsTheText proves the decorative
// choice replaces the description on the wire but not in the draft, so
// unchecking it brings the person's words back.
func TestIssue3962_DecorativeSendsNoDescriptionAndKeepsTheText(t *testing.T) {
	s := &chatattach001State{}
	key := issue3962Uploaded(t, s, "divider.png", "image/png")
	s.setDescription("room", key, "A line")
	s.setDecorative("room", key, true)
	if f := s.projection("room").Files[0]; f.AltText != "A line" || !f.Decorative {
		t.Fatalf("draft = %+v", f)
	}
	refs, _, ok := s.begin("room", "tenant", "", nil)
	if !ok || refs[0].GetAltText() != "" || !refs[0].GetDecorative() {
		t.Fatalf("reference = %+v", refs)
	}
	s.finish("room", false)
	s.setDecorative("room", key, false)
	refs, _, _ = s.begin("room", "tenant", "", nil)
	if refs[0].GetAltText() != "A line" || refs[0].GetDecorative() {
		t.Fatalf("unchecked reference = %+v", refs[0])
	}
}

// TestIssue3962_OnlyImagesCarryADescription proves a document never sends one,
// and a description past the limit is cut to it rather than refused at Send.
func TestIssue3962_OnlyImagesCarryADescription(t *testing.T) {
	s := &chatattach001State{}
	doc := issue3962Uploaded(t, s, "policy.pdf", "application/pdf")
	s.setDescription("room", doc, "should not travel")
	s.setDecorative("room", doc, true)
	img := issue3962Uploaded(t, s, "big.png", "image/png")
	s.setDescription("room", img, strings.Repeat("ä", 1500))
	refs, _, ok := s.begin("room", "tenant", "", nil)
	if !ok || len(refs) != 2 {
		t.Fatalf("begin = %v", ok)
	}
	if refs[0].GetAltText() != "" || refs[0].GetDecorative() {
		t.Fatalf("document carries a description: %+v", refs[0])
	}
	if n := len([]rune(refs[1].GetAltText())); n != 1000 {
		t.Fatalf("description = %d characters", n)
	}
}

// TestIssue3962_PostedReferenceBecomesTheAttachmentDescription proves the
// timeline reads the description back from the post.
func TestIssue3962_PostedReferenceBecomesTheAttachmentDescription(t *testing.T) {
	post := &chatv1.Post{References: []*chatv1.Reference{
		{Kind: chatv1.ReferenceKind_REFERENCE_KIND_MEDIA, Id: "a1", Display: "IMG_1.png", ContentType: "image/png", AltText: "A team photo"},
		{Kind: chatv1.ReferenceKind_REFERENCE_KIND_MEDIA, Id: "a2", Display: "line.png", ContentType: "image/png", Decorative: true},
	}}
	got := chatMediaAttachments(post)
	if len(got) != 2 || got[0].AltText != "A team photo" || got[0].Name != "IMG_1.png" || !got[1].Decorative {
		t.Fatalf("attachments = %+v", got)
	}
}
