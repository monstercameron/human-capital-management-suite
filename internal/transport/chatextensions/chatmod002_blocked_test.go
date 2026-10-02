package chatextensions

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// TestTodo_CHATMOD_002_ExtensionBlocked: a to-do, poll or widget text the
// filters refuse reaches the browser as an invalid argument naming the "text"
// field and the span, never as the generic "operation failed".
func TestTodo_CHATMOD_002_ExtensionBlocked(t *testing.T) {
	err := mapped(&chatfilter.BlockedError{Span: chatfilter.Span{Start: 8, End: 12}, Spans: []chatfilter.Span{{Start: 8, End: 12}}})
	st := status.Convert(err)
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("code %v", st.Code())
	}
	var detail *commonv1.ErrorDetail
	for _, d := range st.Details() {
		if ed, ok := d.(*commonv1.ErrorDetail); ok {
			detail = ed
		}
	}
	if detail == nil || detail.GetReasonRef() != "chat.content_blocked" || len(detail.GetFieldViolations()) != 1 || detail.GetFieldViolations()[0].GetFieldPath() != "text" || detail.GetFieldViolations()[0].GetDescription() != "8-12" {
		t.Fatalf("detail %+v", detail)
	}
}
