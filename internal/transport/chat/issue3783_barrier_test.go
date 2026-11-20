package chat

import (
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// ISSUE-3783: an information barrier is a permission denial with its own reason
// so the add-people dialog can explain it, and the message names no group.
func TestIssue3783_BarrierRefusalCarriesItsOwnReason_Security(t *testing.T) {
	st := status.Convert(callErr(fmt.Errorf("add: %w", chatcore.ErrInformationBarrier)))
	if st.Code() != codes.PermissionDenied {
		t.Fatalf("code = %v, want PermissionDenied", st.Code())
	}
	var found bool
	for _, d := range st.Details() {
		if detail, ok := d.(*commonv1.ErrorDetail); ok && detail.GetReasonRef() == chatcore.ReasonInformationBarrier {
			found = true
		}
	}
	if !found {
		t.Fatalf("no detail with reason %q in %v", chatcore.ReasonInformationBarrier, st.Details())
	}
	if lower := strings.ToLower(st.Message()); strings.Contains(lower, "sales") || strings.Contains(lower, "group") {
		t.Fatalf("message must not name a side: %q", st.Message())
	}
	plain := status.Convert(callErr(chatcore.ErrPermissionDenied))
	for _, d := range plain.Details() {
		if detail, ok := d.(*commonv1.ErrorDetail); ok && detail.GetReasonRef() == chatcore.ReasonInformationBarrier {
			t.Fatalf("an ordinary denial must not claim to be a barrier")
		}
	}
}
