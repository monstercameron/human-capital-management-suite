package chatshared

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The shared refusal keeps the wire shape both chat transports pin: invalid
// argument under the trusted-context reason, with one violation naming the
// forged field and the boundary rule.
func TestTodo_CHAT_SharedServerDerivedRefusal(t *testing.T) {
	err := ServerDerived("principal", "the acting principal is derived server-side from the verified credential")
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v", status.Code(err))
	}
	owned, ok := envelope.As(err)
	if !ok {
		t.Fatalf("error is not owned: %v", err)
	}
	if owned.ReasonRef() != ReasonCallerSelectedAuthority {
		t.Fatalf("reason = %q", owned.ReasonRef())
	}
	violations := owned.Detail().GetFieldViolations()
	if len(violations) != 1 || violations[0].GetFieldPath() != "principal" || violations[0].GetRuleRef() != RuleTrustedRequestBoundary {
		t.Fatalf("violations = %+v", violations)
	}
}
