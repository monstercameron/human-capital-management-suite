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

// ISSUE-3812: each refusal a case binding makes carries its own reason, as a
// permission denial, and names neither the case nor its owner.
func TestIssue3812_CaseRefusalsCarryTheirOwnReason_Security(t *testing.T) {
	for _, tc := range []struct {
		action chatcore.CaseAction
		reason string
	}{
		{chatcore.CaseActionMembership, "chat.case_membership"},
		{chatcore.CaseActionForward, "chat.case_forward"},
		{chatcore.CaseActionShare, "chat.case_share"},
		{chatcore.CaseActionSave, "chat.case_save"},
	} {
		st := status.Convert(callErr(fmt.Errorf("op: %w", &chatcore.CaseRestrictionError{Action: tc.action})))
		if st.Code() != codes.PermissionDenied {
			t.Fatalf("%s: code = %v, want PermissionDenied", tc.action, st.Code())
		}
		found := false
		for _, d := range st.Details() {
			if detail, ok := d.(*commonv1.ErrorDetail); ok && detail.GetReasonRef() == tc.reason {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: no detail with reason %q in %v", tc.action, tc.reason, st.Details())
		}
		if lower := strings.ToLower(st.Message()); strings.Contains(lower, "er-") || strings.Contains(lower, "owner") {
			t.Fatalf("%s: message names the case or its owner: %q", tc.action, st.Message())
		}
	}
	plain := status.Convert(callErr(chatcore.ErrPermissionDenied))
	for _, d := range plain.Details() {
		if detail, ok := d.(*commonv1.ErrorDetail); ok && strings.HasPrefix(detail.GetReasonRef(), "chat.case_") {
			t.Fatal("an ordinary denial must not claim to be a case refusal")
		}
	}
}
