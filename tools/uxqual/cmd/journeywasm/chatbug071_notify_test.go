package main

import (
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestTodo_CHATBUG_071 holds the notification sound to the stored mention: a
// member named by a PERSON_MENTION reference hears the message in a channel,
// also when the channel is set to mentions only and when the text spells their
// name in a way their own session does not; a reference for somebody else, for
// an agent, or for the same identifier in another tenant is not a mention of
// the viewer.
func TestTodo_CHATBUG_071(t *testing.T) {
	now := time.Date(2026, time.October, 2, 9, 0, 0, 0, time.UTC)
	model := chatui.Model{
		CurrentUser: "hc-050", CurrentUserName: "Loretta H.", CurrentTenantID: "tenant",
		Conversations: []chatui.Conversation{{ID: "room", Kind: chatui.PublicChannel}},
		Preferences:   chatui.Preferences{Notifications: map[string]chatui.NotificationMode{"room": chatui.NotifyMention}},
	}
	post := func(refs ...*chatv1.Reference) *chatv1.Post {
		return &chatv1.Post{Id: "p", AuthorId: "walt", Body: "hello @Loretta Haynes", Sequence: 1, CreatedAt: timestamppb.New(now), References: refs}
	}
	person := func(tenant, id string) *chatv1.Reference {
		return &chatv1.Reference{Kind: chatv1.ReferenceKind_REFERENCE_KIND_PERSON_MENTION, TenantId: tenant, Id: id, Display: "Loretta Haynes"}
	}
	for name, tc := range map[string]struct {
		post *chatv1.Post
		want bool
	}{
		"a reference for the viewer":              {post(person("tenant", "hc-050")), true},
		"a reference with no tenant":              {post(person("", "hc-050")), true},
		"no reference, the name spelled apart":    {post(), false},
		"a reference for someone else":            {post(person("tenant", "hc-051")), false},
		"the same identifier in another tenant":   {post(person("other", "hc-050")), false},
		"an agent reference with the viewer's id": {post(&chatv1.Reference{Kind: chatv1.ReferenceKind_REFERENCE_KIND_AGENT_MENTION, TenantId: "tenant", Id: "hc-050"}), false},
	} {
		if got := shouldPlayChatSound(model, "room", tc.post, now, false, false); got != tc.want {
			t.Errorf("%s: sound = %v, want %v", name, got, tc.want)
		}
	}
	if chatPostReferencesViewer(nil, model) || chatPostReferencesViewer(post(person("tenant", "hc-050")), chatui.Model{}) {
		t.Error("a missing post or a viewer with no identity was taken as mentioned")
	}
}
