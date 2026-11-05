package accommodation

import (
	"fmt"

	leave "github.com/monstercameron/human-capital-management-suite/internal/domains/leave"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Case is one interactive-process instance: the request, its dialogue,
// considered options, at most one decision, and its reviews. Access is the
// run access policy medical detail is held under: every projection decides
// through it before field policy ever sees the viewer.
type Case struct {
	ID       string
	Request  Request
	Dialogue Dialogue
	Options  []Option
	Decision *Decision
	Reviews  []Review
	Access   runtime.RunAccessPolicy
}

// OpenCase opens a case for a sealed request under a usable access policy.
// The case takes the request's identity: one request, one case.
func OpenCase(req Request, access runtime.RunAccessPolicy) (*Case, error) {
	if err := req.Verify(); err != nil {
		return nil, err
	}
	if req.Status != RequestOpen {
		return nil, fmt.Errorf("%w: request %q is %s", ErrInvalidRecord, req.ID, req.Status)
	}
	if err := access.Validate(); err != nil {
		return nil, fmt.Errorf("accommodation: case access: %w", err)
	}
	return &Case{ID: req.ID, Request: req, Access: access}, nil
}

// RequestFromFollowUp receives return-to-work's Accommodation follow-up as a
// new request. Only the accommodation kind is received; reassignment and
// restriction intents belong to their own domains.
func RequestFromFollowUp(fu leave.FollowUpIntent, workerID, caseID string, submittedAt int64, limitations []string) (Request, error) {
	if fu.Kind != FollowUpKindAccommodation {
		return Request{}, fmt.Errorf("%w: follow-up kind %q is not received here", ErrInvalidRecord, fu.Kind)
	}
	return NewRequest(caseID, workerID, submittedAt, limitations, "", "")
}

// RecordDialogue appends one dialogue entry. Dialogue runs until the case
// closes; entries must not predate the request. The entry is stamped with
// the case identity before sealing, so transcript entries cannot be
// transplanted across cases.
func (c *Case) RecordDialogue(e DialogueEntry) error {
	if c == nil {
		return fmt.Errorf("%w: nil case", ErrInvalidRecord)
	}
	if c.Request.Status == RequestClosed {
		return fmt.Errorf("%w: case %q is closed", ErrInvalidRecord, c.ID)
	}
	if e.At < c.Request.SubmittedAt {
		return fmt.Errorf("%w: dialogue cannot predate the request", ErrInvalidRecord)
	}
	e.CaseID = c.ID
	next, err := c.Dialogue.Append(e)
	if err != nil {
		return err
	}
	c.Dialogue = next
	return nil
}

// Verify recomputes every seal on the case: request, dialogue transcript,
// options, decision and reviews. Any post-seal rewrite fails, whatever
// record it touched.
func (c Case) Verify() error {
	if err := c.Request.Verify(); err != nil {
		return err
	}
	if err := c.Dialogue.Verify(); err != nil {
		return err
	}
	for i, o := range c.Options {
		if err := o.Verify(); err != nil {
			return fmt.Errorf("option %d: %w", i, err)
		}
	}
	if c.Decision != nil {
		if err := c.Decision.Verify(); err != nil {
			return err
		}
	}
	for i, r := range c.Reviews {
		if err := r.Verify(); err != nil {
			return fmt.Errorf("review %d: %w", i, err)
		}
	}
	return nil
}

// Clone deep-copies the case: slices are re-backed and the decision is
// re-pointed, so sharing a Case never shares mutable state.
func (c Case) Clone() Case {
	out := c
	out.Request.Limitations = append([]string(nil), c.Request.Limitations...)
	out.Dialogue = append(Dialogue(nil), c.Dialogue...)
	out.Options = append([]Option(nil), c.Options...)
	out.Reviews = append([]Review(nil), c.Reviews...)
	if c.Decision != nil {
		dec := *c.Decision
		dec.Granted = append([]string(nil), c.Decision.Granted...)
		out.Decision = &dec
	}
	return out
}

// ConsiderOption records one evaluated option. Options accumulate until the
// case closes.
func (c *Case) ConsiderOption(o Option) error {
	if c == nil {
		return fmt.Errorf("%w: nil case", ErrInvalidRecord)
	}
	if c.Request.Status == RequestClosed {
		return fmt.Errorf("%w: case %q is closed", ErrInvalidRecord, c.ID)
	}
	if err := o.Verify(); err != nil {
		return err
	}
	if o.CaseID != c.ID {
		return fmt.Errorf("%w: option belongs to case %q", ErrInvalidRecord, o.CaseID)
	}
	for _, prior := range c.Options {
		if prior.ID == o.ID {
			return fmt.Errorf("%w: option %q is already considered", ErrInvalidRecord, o.ID)
		}
	}
	c.Options = append(c.Options, o)
	return nil
}

// Decide records the single decision. A decision requires dialogue and at
// least one considered option: the process shows its work before it rules.
// Only one decision stands; later change arrives as a review outcome.
func (c *Case) Decide(d Decision) error {
	if c == nil {
		return fmt.Errorf("%w: nil case", ErrInvalidRecord)
	}
	if c.Request.Status == RequestClosed {
		return fmt.Errorf("%w: case %q is closed", ErrInvalidRecord, c.ID)
	}
	if c.Decision != nil {
		return fmt.Errorf("%w: case %q already decided", ErrInvalidRecord, c.ID)
	}
	if err := d.Verify(); err != nil {
		return err
	}
	if d.CaseID != c.ID {
		return fmt.Errorf("%w: decision belongs to case %q", ErrInvalidRecord, d.CaseID)
	}
	if len(c.Dialogue) == 0 {
		return fmt.Errorf("%w: a decision requires dialogue", ErrInvalidRecord)
	}
	if len(c.Options) == 0 {
		return fmt.Errorf("%w: a decision requires at least one considered option", ErrInvalidRecord)
	}
	if d.DecidedAt < c.Dialogue[len(c.Dialogue)-1].At {
		return fmt.Errorf("%w: a decision cannot predate the dialogue", ErrInvalidRecord)
	}
	c.Decision = &d
	c.Request.Status = RequestDecided
	c.Request.Digest = requestDigest(c.Request)
	return nil
}

// RecordReview schedules one periodic review. Reviews require a decision,
// run in due-tick order, and only one may be outstanding: the next review
// schedules once the current one completes.
func (c *Case) RecordReview(r Review) error {
	if c == nil {
		return fmt.Errorf("%w: nil case", ErrInvalidRecord)
	}
	if c.Request.Status == RequestClosed {
		return fmt.Errorf("%w: case %q is closed", ErrInvalidRecord, c.ID)
	}
	if c.Decision == nil {
		return fmt.Errorf("%w: reviews require a decision", ErrInvalidRecord)
	}
	if err := r.Verify(); err != nil {
		return err
	}
	if r.CaseID != c.ID {
		return fmt.Errorf("%w: review belongs to case %q", ErrInvalidRecord, r.CaseID)
	}
	if r.DueAt <= c.Decision.DecidedAt {
		return fmt.Errorf("%w: a review cannot precede the decision", ErrInvalidRecord)
	}
	for _, prior := range c.Reviews {
		if prior.ID == r.ID {
			return fmt.Errorf("%w: review %q is already scheduled", ErrInvalidRecord, r.ID)
		}
		if prior.CompletedAt == 0 {
			return fmt.Errorf("%w: review %q is still outstanding", ErrInvalidRecord, prior.ID)
		}
		if r.DueAt <= prior.DueAt {
			return fmt.Errorf("%w: reviews run in due-tick order", ErrInvalidRecord)
		}
	}
	c.Reviews = append(c.Reviews, r)
	return nil
}

// CompleteReview stores a completed review over its scheduled entry.
func (c *Case) CompleteReview(r Review) error {
	if c == nil {
		return fmt.Errorf("%w: nil case", ErrInvalidRecord)
	}
	if err := r.Verify(); err != nil {
		return err
	}
	if r.CaseID != c.ID {
		return fmt.Errorf("%w: review belongs to case %q", ErrInvalidRecord, r.CaseID)
	}
	if r.CompletedAt == 0 {
		return fmt.Errorf("%w: review %q is not complete", ErrInvalidRecord, r.ID)
	}
	for i, prior := range c.Reviews {
		if prior.ID != r.ID {
			continue
		}
		if prior.CompletedAt != 0 {
			return fmt.Errorf("%w: review %q is already recorded complete", ErrInvalidRecord, r.ID)
		}
		if r.DueAt != prior.DueAt {
			return fmt.Errorf("%w: completion cannot move the due tick", ErrInvalidRecord)
		}
		c.Reviews[i] = r
		return nil
	}
	return fmt.Errorf("%w: review %q was never scheduled", ErrInvalidRecord, r.ID)
}

// Close ends the case. Dialogue, options, decisions and reviews are refused
// after close; the sealed records remain readable under access policy.
func (c *Case) Close() error {
	if c == nil {
		return fmt.Errorf("%w: nil case", ErrInvalidRecord)
	}
	if c.Decision == nil {
		return fmt.Errorf("%w: an undecided case cannot close", ErrInvalidRecord)
	}
	c.Request.Status = RequestClosed
	c.Request.Digest = requestDigest(c.Request)
	return nil
}
