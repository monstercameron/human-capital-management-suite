package chatgate

import "context"

// Issue 3818: an applicant whose request waited for a person is told what the
// person decided. Automatic and rule decisions are answered on the form at
// once and raise nothing.

// DecisionNotice names a decided request. Reason is what the administrator
// wrote; only the applicant is told it.
type DecisionNotice struct {
	Scope        Scope
	SubmissionID string
	// Revision is the request's revision after the decision, so a request
	// answered again and decided again is a new notice.
	Revision uint64
	Person   string
	Admitted bool
	Reason   string
}

// DecisionNotifier tells the applicant. Delivery is idempotent by
// (scope, SubmissionID, Revision).
type DecisionNotifier interface {
	NotifyDecision(context.Context, DecisionNotice) error
}

// tellApplicant is called after a decision is committed. A notice that cannot
// be sent never undoes the decision; the applicant still sees it on the form.
func (s *Service) tellApplicant(ctx context.Context, scope Scope, subs ...Submission) {
	if s.Decisions == nil {
		return
	}
	for _, sub := range subs {
		_ = s.Decisions.NotifyDecision(ctx, DecisionNotice{Scope: scope, SubmissionID: sub.ID, Revision: sub.Revision, Person: sub.Person, Admitted: sub.Status == "admitted", Reason: sub.Reason})
	}
}
