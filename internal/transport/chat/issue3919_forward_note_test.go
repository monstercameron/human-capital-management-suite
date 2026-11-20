package chat

import (
	"context"
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type issue3919Forwarder struct {
	transportChatFake
	got chatcore.ForwardPostRequest
}

func (f *issue3919Forwarder) ForwardPost(_ context.Context, r chatcore.ForwardPostRequest) (chatcore.Post, error) {
	f.got = r
	return chatcore.Post{ID: "forwarded", TenantID: "server", ConversationID: "dm", SourceAttribution: &chatcore.SourceAttribution{PostID: "p", OriginalAuthorID: "ana", Note: r.Note}}, nil
}

// The note travels from the request to the service and back out on the new
// post's attribution, so the destination can show it beside the message.
func TestIssue3919_ForwardCarriesTheNoteBothWays(t *testing.T) {
	ctx := admittedChatContext(t)
	fake := &issue3919Forwarder{}
	s := &server{deps: Dependencies{Service: fake}}
	got, err := s.ForwardPost(ctx, &chatv1.ForwardPostRequest{SourceTenantId: "server", SourceConversationId: "c", SourcePostId: "p", DestinationTenantId: "server", DestinationConversationId: "dm", IdempotencyKey: "k", Note: "Can we talk?"})
	if err != nil {
		t.Fatal(err)
	}
	if fake.got.Note != "Can we talk?" {
		t.Fatalf("service saw note %q", fake.got.Note)
	}
	if note := got.GetPost().GetSourceAttribution().GetNote(); note != "Can we talk?" {
		t.Fatalf("response attribution note = %q", note)
	}
}
