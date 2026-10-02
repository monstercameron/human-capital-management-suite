package chat

import (
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// TestTodo_CHATMOD_002_BlockedIsTheAuthorsToFix: a refused message used to fall
// through callErr to a non-retryable internal error ("the service did not
// answer"). It is now an invalid argument carrying the span, so the composer can
// tell the author which word.
func TestTodo_CHATMOD_002_BlockedIsTheAuthorsToFix(t *testing.T) {
	err := callErr(fmt.Errorf("send: %w", &chatfilter.BlockedError{RuleName: "Profanity", Span: chatfilter.Span{Start: 4, End: 8}}))
	st := status.Convert(err)
	if st.Code() != codes.InvalidArgument || len(st.Details()) == 0 {
		t.Fatalf("a blocked message is %v with %d details", st.Code(), len(st.Details()))
	}
	if st := status.Convert(callErr(chatfilter.ErrUnavailable)); st.Code() != codes.Unavailable {
		t.Fatalf("an unavailable filter is %v", st.Code())
	}
}
