package application

import (
	"context"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestDocumentPreviewsApplication_Integration(t *testing.T) {
	svc := documentServiceFixture(t)
	const tenant, owner, reader = "tenant-previews-app", "owner-a", "reader-b"
	ctx := documentReaderContext(t, tenant, reader)
	svc.ownerNames = func(_ context.Context, _ string, ids []string) (map[string]string, error) {
		out := map[string]string{}
		for _, id := range ids {
			out[id] = "Owner " + id
		}
		return out, nil
	}
	svc.chat = &fakeDocumentChat{rooms: []chatcore.Conversation{{ID: "conv-1", TenantID: tenant, Kind: chatcore.PublicChannel, Name: "people-ops", MemberCount: 3}}}
	shared, _, err := svc.CreateDocument(context.Background(), tenant, owner, "Shared guide", "# Shared guide\n\nAsk in #people-ops.\n")
	if err != nil {
		t.Fatal(err)
	}
	private, _, err := svc.CreateDocument(context.Background(), tenant, owner, "Private plan", "# Private plan\n\nNot for you.\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ShareDocument(context.Background(), tenant, owner, shared, reader, "viewer"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetDocumentPreviews(ctx, tenant, reader, []string{shared, private})
	if err != nil || len(got) != 2 {
		t.Fatalf("previews = %+v, %v", got, err)
	}
	if !got[0].Readable || got[0].Title != "Shared guide" || got[0].OwnerName != "Owner "+owner || got[0].Snippet != "Ask in #people-ops." {
		t.Fatalf("readable preview = %+v", got[0])
	}
	if got[1].Readable || got[1].Title != "" || got[1].OwnerID != "" || got[1].OwnerName != "" || got[1].Snippet != "" {
		t.Fatalf("private preview leaked %+v", got[1])
	}
	summary, _, _, err := svc.GetDocument(ctx, tenant, reader, shared)
	if err != nil || len(summary.Chat.Channels) != 1 || summary.Chat.Channels[0].ConversationID != "conv-1" || summary.Chat.Channels[0].MemberCount != 3 {
		t.Fatalf("chat references = %+v, %v", summary.Chat, err)
	}
}
