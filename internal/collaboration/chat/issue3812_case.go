package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Issue 3812: a conversation bound to a governed case. A binding names an
// opaque case reference (the case lives outside Chat), the one person who owns
// it, and whether the conversation is restricted. While a binding is in force:
//
//   - nobody changes who is in the conversation except the case owner. Not the
//     conversation's own owner, not a manager, not an administrator, and nobody
//     joins by themselves. A member may still leave on their own, except the
//     case owner, who cannot be removed or leave while the case is bound (an
//     administrator moves the case to another owner first);
//   - when it is restricted, a message cannot be forwarded out of it, a link to
//     it cannot be made, and a message of it cannot be saved or turned into work
//     elsewhere.
//
// The rules are decided here, on the server, before anything is written. The
// binding itself is held and changed outside this package.

// CaseAction names what a binding refused.
type CaseAction string

const (
	CaseActionMembership CaseAction = "membership"
	CaseActionForward    CaseAction = "forward"
	CaseActionShare      CaseAction = "share"
	CaseActionSave       CaseAction = "save"
)

// The reason references a transport reports, one per kind of refusal, so the
// client can say which rule applied.
const (
	ReasonCaseMembership = "chat.case_membership"
	ReasonCaseForward    = "chat.case_forward"
	ReasonCaseShare      = "chat.case_share"
	ReasonCaseSave       = "chat.case_save"
)

// CaseRestrictionError is a refusal because the conversation is bound to a case.
// It is a kind of permission denial, so a caller that only knows about denials
// still refuses the write. It carries the action and nothing else: not the case
// reference, not the owner.
type CaseRestrictionError struct{ Action CaseAction }

func (e *CaseRestrictionError) Error() string {
	return fmt.Sprintf("%s: case-bound conversation does not allow %s", ErrPermissionDenied, e.Action)
}

// Is makes the refusal a permission denial.
func (e *CaseRestrictionError) Is(target error) bool { return target == ErrPermissionDenied }

// Reason is the reference a transport reports for the refusal.
func (e *CaseRestrictionError) Reason() string {
	switch e.Action {
	case CaseActionMembership:
		return ReasonCaseMembership
	case CaseActionForward:
		return ReasonCaseForward
	case CaseActionShare:
		return ReasonCaseShare
	default:
		return ReasonCaseSave
	}
}

// CaseReason returns the reason reference when err is a case refusal.
func CaseReason(err error) (string, bool) {
	var refusal *CaseRestrictionError
	if errors.As(err, &refusal) {
		return refusal.Reason(), true
	}
	return "", false
}

// CaseBinding is the binding in force for a conversation.
type CaseBinding struct {
	TenantID       string
	ConversationID string
	// CaseRef is the opaque reference of the case in the system that owns it.
	CaseRef string
	// OwnerID is the person who owns the case: the only one who changes who is
	// in the conversation.
	OwnerID    string
	Restricted bool
	BoundBy    string
	BoundAt    time.Time
}

// CaseBindingPort reads the binding in force for a conversation of the tenant.
// bound is false when there is none. An error means the answer is not known,
// and the service refuses on it.
type CaseBindingPort interface {
	ActiveCase(ctx context.Context, tenant, conversation string) (binding CaseBinding, bound bool, err error)
}

// SetCaseBindings installs the binding reader. With none, no conversation is
// bound, as before.
func (s *Service) SetCaseBindings(p CaseBindingPort) { s.caseBindings = p }

// activeCase reads the binding, failing closed.
func (s *Service) activeCase(ctx context.Context, tenant, conversation string) (CaseBinding, bool, error) {
	if s.caseBindings == nil {
		return CaseBinding{}, false, nil
	}
	b, bound, err := s.caseBindings.ActiveCase(ctx, tenant, conversation)
	if err != nil {
		return CaseBinding{}, false, ErrUnavailable
	}
	return b, bound, nil
}

// CaseRestriction answers whether the action is refused because the conversation
// is bound to a restricted case. It is for callers outside the service (for
// example turning a message into a task) that move content out of a
// conversation; they ask after they have decided the person may read it.
func (s *Service) CaseRestriction(ctx context.Context, tenant, conversation string, action CaseAction) error {
	b, bound, err := s.activeCase(ctx, tenant, conversation)
	if err != nil {
		return err
	}
	if bound && b.Restricted {
		return &CaseRestrictionError{Action: action}
	}
	return nil
}

// caseRoster reads who may change the roster of a conversation. bound is true
// when a case is bound to it; owner is true when the caller is the case owner,
// whose word then replaces the ordinary owner or manager rule. The refusal of
// everyone else is left to caseRosterRefusal, so it can be given after the
// caller has shown they may read the conversation at all: a person who cannot
// open it is not told that it is bound to a case.
func (s *Service) caseRoster(ctx context.Context, p Principal, c Conversation) (bound, owner bool, err error) {
	b, bound, err := s.activeCase(ctx, c.TenantID, c.ID)
	if err != nil || !bound {
		return false, false, err
	}
	owner = p.TenantID == c.TenantID && strings.TrimSpace(p.SubjectID) != "" && p.SubjectID == b.OwnerID
	return true, owner, nil
}

// caseRosterRefusal is the refusal of a roster change by anyone but the case owner.
func caseRosterRefusal(bound, owner bool) error {
	if bound && !owner {
		return &CaseRestrictionError{Action: CaseActionMembership}
	}
	return nil
}

// caseOwnerIs reports whether subject is the case owner of the bound conversation.
func (s *Service) caseOwnerIs(ctx context.Context, c Conversation, subject string) (bool, error) {
	b, bound, err := s.activeCase(ctx, c.TenantID, c.ID)
	if err != nil || !bound {
		return false, err
	}
	return b.OwnerID == subject, nil
}
