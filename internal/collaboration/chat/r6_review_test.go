package chat

import (
	"strings"
	"testing"
)

// A share link and a reference preview show the post to whoever opens them, so
// they show what that reader may read: the mask applies there too (lane R6).
func TestR6_LinksShowTheMaskedBody(t *testing.T) {
	service, store, _ := maskFixture(t, "mask")
	store.posts = []Post{maskPost("p1", "writer", "hello quartz friends")}
	reader := Principal{TenantID: "t", SubjectID: "reader"}
	link, err := service.CreateShareLink(t.Context(), reader, "t", "c", "p1")
	if err != nil {
		t.Fatal(err)
	}
	_, post, err := service.ResolveShareLink(t.Context(), reader, link.URL[len("/chat/share/"):])
	if err != nil || post == nil || strings.Contains(post.Body, "quartz") {
		t.Fatalf("share link carried the masked word: %+v %v", post, err)
	}
	_, ref, err := service.ReadAuthorizedReference(t.Context(), reader, "t", "c", "p1")
	if err != nil || ref == nil || strings.Contains(ref.Body, "quartz") {
		t.Fatalf("reference preview carried the masked word: %+v %v", ref, err)
	}
}

// A mask rule hides a word from readers. Forwarding copied the stored body, so
// a reader who saw "[removed word]" could forward the message to a place with no
// such rule and read the word there (lane R6).
func TestR6_ForwardCopiesWhatTheForwarderSees(t *testing.T) {
	service, store, _ := maskFixture(t, "mask")
	store.posts = []Post{maskPost("p1", "writer", "hello quartz friends")}
	forwarder := Principal{TenantID: "t", SubjectID: "reader"}
	if _, err := service.ForwardPost(t.Context(), ForwardPostRequest{Principal: forwarder, SourceTenantID: "t", SourceConversationID: "c", SourcePostID: "p1", DestinationConversationID: "c", IdempotencyKey: "r6-forward"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(store.sent.Body, "quartz") || store.sent.Body != "hello [removed word] friends" {
		t.Fatalf("the forward carried the masked word: %q", store.sent.Body)
	}
}
