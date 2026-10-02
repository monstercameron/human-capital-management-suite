package chatfilter

import (
	"context"
	"fmt"
	"time"
)

// CHATMOD-003, the clauses the first build left open.
//
// Whose filter it is. A filter for one channel can be written by a workspace
// administrator or by that channel's manager, and the stored definition did
// not say which. A manager could therefore save a weaker version of a filter
// an administrator had written for the channel, or switch it off. Each version
// now records the authority it was saved under, and a filter saved under the
// workspace's authority takes the workspace's authority to change or switch.
// Versions stored before this field existed carry no authority; they are
// treated as the workspace's, because nothing proves a manager wrote them and
// the failure in the other direction is the one the rule exists to prevent.
//
// Telling a channel. The "notify" action records a hit and the delivery port
// tells the channel the filter names. A filter is refused when it is saved if
// the delivery port cannot find that channel, so no filter says it tells
// somebody and then tells nobody.
//
// A deadline. Every pattern and list is bounded when it is saved, so judging a
// message is bounded too; the budget is the second fence, measured on a clock.
// A message that cannot be judged in time is not judged at all: the caller is
// told the filters are unavailable, exactly as when the store cannot be read,
// and nothing is sent unfiltered.

const (
	// AuthorityWorkspace and AuthorityChannel are the two values of
	// Definition.Authority.
	AuthorityWorkspace = "workspace"
	AuthorityChannel   = "channel"

	// DefaultEvaluationBudget is how long one message may take to judge. The
	// measured cost with every built-in list on is under two milliseconds, so
	// this is reached only by a fault, not by a slow day.
	DefaultEvaluationBudget = 250 * time.Millisecond
)

// ErrDeadline is ErrUnavailable with the reason: the rules that are on could
// not judge the message within the budget.
var ErrDeadline = fmt.Errorf("%w: evaluation deadline exceeded", ErrUnavailable)

// ErrUnknownTarget is ErrInvalid with the reason: a "notify" filter names a
// destination the delivery port cannot tell.
var ErrUnknownTarget = fmt.Errorf("%w: unknown notification target", ErrInvalid)

// TargetResolver is the part of a delivery port that knows whom a target
// names. A delivery port that implements it is asked when a "notify" filter is
// saved; one that does not is trusted to deliver whatever it is given.
type TargetResolver interface {
	ResolveFilterTarget(ctx context.Context, actor Actor, target string) error
}

func (e *Evaluator) now() time.Time {
	if e.clock != nil {
		return e.clock()
	}
	return time.Now()
}

// deadline is when this evaluation must stop. A negative budget switches the
// fence off (the pure evaluator in a property test has no clock to answer to).
func (e *Evaluator) deadline() time.Time {
	budget := e.budget
	if budget == 0 {
		budget = DefaultEvaluationBudget
	}
	if budget < 0 {
		return time.Time{}
	}
	return e.now().Add(budget)
}

func (e *Evaluator) expired(deadline time.Time) bool {
	return !deadline.IsZero() && e.now().After(deadline)
}

// workspaceOwned reports whether a stored filter takes the workspace's
// authority to change: it says so, or it predates the field.
func workspaceOwned(d Definition) bool { return d.Authority != AuthorityChannel }

// checkTarget refuses a "notify" filter whose destination the delivery port
// does not know.
func (s *Service) checkTarget(ctx context.Context, a Actor, d Definition) error {
	action, ok := s.Registry.actions[d.Action]
	if !ok || !action.RequiresTarget {
		return nil
	}
	resolver, ok := s.Delivery.(TargetResolver)
	if !ok {
		return nil
	}
	return resolver.ResolveFilterTarget(ctx, a, d.Target)
}
