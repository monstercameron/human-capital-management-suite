package chat

import (
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// ISSUE-3706: a public channel whose name is taken is an invalid argument whose
// detail names the name field with the taken-name reason, so the create dialog
// keeps itself open and says the name is taken instead of creating a duplicate.
func TestIssue3706_NameTakenIsReportedAgainstTheNameField(t *testing.T) {
	st := status.Convert(callErr(fmt.Errorf("create: %w", chatcore.ErrChannelNameTaken)))
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", st.Code())
	}
	var found bool
	for _, d := range st.Details() {
		detail, ok := d.(*commonv1.ErrorDetail)
		if !ok || detail.GetReasonRef() != chatcore.ReasonChannelNameTaken {
			continue
		}
		for _, v := range detail.GetFieldViolations() {
			found = found || (v.GetFieldPath() == "name" && v.GetRuleRef() == ruleChannelNameTaken)
		}
	}
	if !found {
		t.Fatalf("no name-field violation with reason %q in %v", chatcore.ReasonChannelNameTaken, st.Details())
	}
}
