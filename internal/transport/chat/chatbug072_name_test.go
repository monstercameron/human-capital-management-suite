package chat

import (
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestTodo_CHATBUG_072_NameRuleIsReportedAgainstTheNameField: a refused channel
// name is an invalid argument whose detail names the field, so the create form
// can print the rule under the box instead of a generic failure.
func TestTodo_CHATBUG_072_NameRuleIsReportedAgainstTheNameField(t *testing.T) {
	st := status.Convert(callErr(fmt.Errorf("create: %w", chatcore.ErrChannelName)))
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("code = %v", st.Code())
	}
	var found bool
	for _, d := range st.Details() {
		detail, ok := d.(*commonv1.ErrorDetail)
		if !ok || detail.GetReasonRef() != chatcore.ReasonChannelName {
			continue
		}
		for _, v := range detail.GetFieldViolations() {
			found = found || (v.GetFieldPath() == "name" && v.GetRuleRef() == ruleChannelName)
		}
	}
	if !found {
		t.Fatalf("no name-field violation with reason %q in %v", chatcore.ReasonChannelName, st.Details())
	}
}

// A person's own timeline, read over the RPC, keeps the system lines that say
// who was added (CHATUX-021); everything else that reads the conversation does
// not ask for them.
func TestTodo_CHATUX_021_TimelineReadAsksForSystemLines(t *testing.T) {
	rec := &listRecorder{transportChatFake: &transportChatFake{}}
	s := &server{deps: Dependencies{Service: rec}}
	if _, err := s.ListPosts(admittedChatContext(t), &chatv1.ListPostsRequest{TenantId: "server", ConversationId: "c"}); err != nil {
		t.Fatal(err)
	}
	if !rec.posts.IncludeSystem {
		t.Fatalf("the timeline read did not ask for system lines: %+v", rec.posts)
	}
}
