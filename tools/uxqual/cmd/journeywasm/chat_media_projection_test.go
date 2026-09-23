package main

import (
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
)

func TestChatMediaAttachmentsCarriesAuthoritativeRenditionMetadata(t *testing.T) {
	attachments := chatMediaAttachments(&chatv1.Post{References: []*chatv1.Reference{{
		Kind: chatv1.ReferenceKind_REFERENCE_KIND_MEDIA, Id: "artifact", Display: "camera.jpg",
		ContentType: "image/jpeg", ByteSize: 8192, Width: 4032, Height: 3024,
	}}})
	if len(attachments) != 1 {
		t.Fatalf("projected %d attachments, want 1", len(attachments))
	}
	got := attachments[0]
	if got.ID != "artifact" || got.Name != "camera.jpg" || got.ContentType != "image/jpeg" || got.Bytes != 8192 || got.Width != 4032 || got.Height != 3024 {
		t.Fatalf("attachment metadata = %+v", got)
	}
}
