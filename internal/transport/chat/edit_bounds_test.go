package chat

import (
	"strings"
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestTodo_CHAT_018_EditPostBodyBounds pins the published send-path bounds on
// the edit path. An edit is a second write of the same body, so it is refused
// exactly like a send past either bound; without this, the bounds on SendPost
// are advisory and any client can grow a post without limit by editing it.
func TestTodo_CHAT_018_EditPostBodyBounds(t *testing.T) {
	ctx := admittedChatContext(t)
	s := &server{deps: Dependencies{Service: transportChatFake{}}}
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{"at the character bound", strings.Repeat("a", MaxPostBodyRunes), false},
		{"one character over", strings.Repeat("a", MaxPostBodyRunes+1), true},
		{"over the byte bound", strings.Repeat("あ", 1400), true},
		{"multibyte inside both bounds", strings.Repeat("あ", 1000), false},
	} {
		_, err := s.EditPost(ctx, &chatv1.EditPostRequest{Post: &chatv1.Post{Id: "p", ConversationId: "c"}, Body: tc.body, ExpectedRevision: 1})
		refused := err != nil
		if refused != tc.want {
			t.Fatalf("%s: refused = %v (err=%v)", tc.name, refused, err)
		}
		if !refused {
			continue
		}
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("%s: code = %v", tc.name, status.Code(err))
		}
		if got := violations(t, err); len(got) != 1 || got[0] != "body:"+ruleBodyBound {
			t.Fatalf("%s: violations = %v", tc.name, got)
		}
	}
}
