package chatgate

import "context"

// Issue 3817: a request that waits for a person to decide it must reach the
// people who decide it. The service raises events for the audit trail; this is
// the one outward port that tells people.

const (
	// ReviewNoticeNew tells the channel's reviewers a request is waiting.
	ReviewNoticeNew = "new"
	// ReviewNoticeOverdue tells them it has waited past the first deadline.
	ReviewNoticeOverdue = "overdue"
	// ReviewNoticeEscalated tells the workspace administrators it has waited
	// past the second.
	ReviewNoticeEscalated = "escalated"
)

// ReviewNotice names a waiting request. It carries no answer text: the people
// told open the request to read it, under the same checks as everyone else.
type ReviewNotice struct {
	Scope        Scope
	SubmissionID string
	// Person is the applicant, so they are never told about their own request.
	Person string
	Level  string
}

// ReviewNotifier delivers a notice to the people who review the gate's channel
// (and, when escalated, the workspace administrators who may know the channel
// exists). Delivery must be idempotent by (scope, SubmissionID, Level): the
// sweep and a replayed request may both send the same notice.
type ReviewNotifier interface {
	NotifyReview(context.Context, ReviewNotice) error
}

// tellReviewers is called after a request is committed to the queue. A notice
// that cannot be sent never fails the person's request; the sweep raises the
// overdue notice later.
func (s *Service) tellReviewers(ctx context.Context, scope Scope, sub Submission) {
	if s.Notifier == nil || sub.Status != "review" {
		return
	}
	_ = s.Notifier.NotifyReview(ctx, ReviewNotice{Scope: scope, SubmissionID: sub.ID, Person: sub.Person, Level: ReviewNoticeNew})
}

// sweepNotice sends one deadline notice from inside the sweep's transaction,
// before the request is marked as told: a notice that fails leaves the request
// unmarked and fails the sweep, so the next sweep sends it again.
func (s *Service) sweepNotice(ctx context.Context, scope Scope, sub Submission, level string) error {
	if s.Notifier == nil {
		return nil
	}
	return s.Notifier.NotifyReview(ctx, ReviewNotice{Scope: scope, SubmissionID: sub.ID, Person: sub.Person, Level: level})
}
