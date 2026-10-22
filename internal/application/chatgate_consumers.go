package application

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// CHATGATE-009, 010, 011 and 013 on the served product: the gate's labels and
// segments, the audience of addressed content, and the operating settings. The
// pages reach them through the gate request like the rest of the gate; Chat's
// message and card paths reach the segments through chatgateSegmentPort.

// ChatgateConsumers is what the gate pages ask of the consumers of answers.
// *chatgate.Service implements it.
type ChatgateConsumers interface {
	SetSegments(context.Context, chatgate.Command, []chatgate.SegmentRule, string) error
	RevertSegments(context.Context, chatgate.Command, int, string) error
	SetAudienceLevels(context.Context, chatgate.Command, bool, bool) error
	SegmentOverview(context.Context, chatgate.Actor, chatgate.Scope) (chatgate.SegmentOverview, error)
	MySegments(context.Context, chatgate.Actor, chatgate.Scope) ([]chatgate.Placement, error)
	Reach(context.Context, chatgate.Actor, chatgate.Scope, []string, bool) (chatgate.Reach, error)
	Metrics(context.Context, chatgate.Actor, chatgate.Scope) (chatgate.GateMetrics, error)
	SetQueueDeadlines(context.Context, chatgate.Command, time.Duration, time.Duration) error
	SetExistingMembers(context.Context, chatgate.Command, string, time.Time) error
	Unanswered(context.Context, chatgate.Actor, chatgate.Scope) ([]string, error)
	RemoveUnanswered(context.Context, chatgate.Command, []string) error
	Operating(context.Context, chatgate.Actor, chatgate.Scope) (chatgate.Ops, error)
	// Override adds a person without their answers: the administrator's own,
	// audited act, for someone invited into the channel before they answered.
	Override(context.Context, chatgate.Command, string, string) error
}

var _ ChatgateConsumers = (*chatgate.Service)(nil)

func (a *ChatgateApplication) consumerAction(ctx context.Context, c chatgate.Command, actor chatgate.Actor, scope chatgate.Scope, r ChatgateRequest) (any, error) {
	cs := a.Consumers
	if cs == nil {
		return nil, chatgate.ErrUnavailable
	}
	switch r.Action {
	case "segments-save":
		return nil, cs.SetSegments(ctx, c, r.Rules, r.Reason)
	case "segments-revert":
		return nil, cs.RevertSegments(ctx, c, r.RulesVersion, r.Reason)
	case "levels-save":
		return nil, cs.SetAudienceLevels(ctx, c, r.Addressing, r.Restricted)
	case "ops-save":
		return nil, cs.SetQueueDeadlines(ctx, c, time.Duration(r.ReviewAfterHours)*time.Hour, time.Duration(r.EscalateAfterHours)*time.Hour)
	case "existing-save":
		var by time.Time
		if r.AnswerBy != "" {
			day, err := time.Parse("2006-01-02", r.AnswerBy)
			if err != nil {
				return nil, chatgate.ErrInvalid
			}
			by = day.Add(23*time.Hour + 59*time.Minute)
		}
		return nil, cs.SetExistingMembers(ctx, c, r.ExistingMode, by)
	case "remove-unanswered":
		return nil, cs.RemoveUnanswered(ctx, c, r.People)
	case "reach":
		return cs.Reach(ctx, actor, scope, r.Segments, r.Restricted)
	case "override":
		return nil, cs.Override(ctx, c, r.Person, r.Reason)
	}
	return nil, chatgate.ErrInvalid
}

// consumerView adds what an administrator reads beside the builder: the rules,
// counts per label and segment, the metrics, who has not answered and how many
// restricted messages the channel holds. A read that fails leaves its block out
// and the rest of the page as it was.
func (a *ChatgateApplication) consumerView(ctx context.Context, actor chatgate.Actor, scope chatgate.Scope, v *chatui.GateView) {
	if a.Consumers == nil {
		return
	}
	if overview, err := a.Consumers.SegmentOverview(ctx, actor, scope); err == nil {
		v.Segments = &overview
	}
	if metrics, err := a.Consumers.Metrics(ctx, actor, scope); err == nil {
		v.Metrics = &metrics
	}
	if ops, err := a.Consumers.Operating(ctx, actor, scope); err == nil {
		v.Ops = &ops
	}
	if people, err := a.Consumers.Unanswered(ctx, actor, scope); err == nil {
		v.Unanswered = people
	}
	if a.Restricted != nil {
		if n, err := a.Restricted.RestrictedCount(ctx, scope.Tenant, scope.Conversation); err == nil {
			v.RestrictedMessages = n
		}
	}
}

// chatgateSegmentPort lets Chat's message and card paths ask the gate about
// segments: may this person address them, and which of them is this person in.
// It reads the gate's state without a lock and never an answer.
type chatgateSegmentPort struct {
	state interface {
		SegmentState(context.Context, chatgate.Scope) (chatgate.SegmentState, error)
	}
	authority chatgate.Authority
}

var _ chat.SegmentAudiencePort = chatgateSegmentPort{}

func (p chatgateSegmentPort) Resolve(ctx context.Context, principal chat.Principal, tenant, conversation string, segments []string, restricted bool) (chat.PostAudience, error) {
	if principal.TenantID != tenant || principal.SubjectID == "" || p.state == nil || p.authority == nil {
		return chat.PostAudience{}, chat.ErrPermissionDenied
	}
	scope := chatgate.Scope{Tenant: tenant, Conversation: conversation}
	actor := chatgate.Actor{Tenant: tenant, Person: principal.SubjectID}
	if err := p.authority.Check(ctx, actor, scope, chatgate.PermissionMember); err != nil {
		return chat.PostAudience{}, chat.ErrPermissionDenied
	}
	policy, err := p.authority.Policy(ctx, scope)
	if err != nil {
		return chat.PostAudience{}, chat.ErrUnavailable
	}
	st, err := p.state.SegmentState(ctx, scope)
	if err != nil {
		return chat.PostAudience{}, chat.ErrUnavailable
	}
	manager := p.authority.Check(ctx, actor, scope, "admin") == nil
	reach, members, err := chatgate.ResolveAudience(st, manager, policy.Private, actor.Person, segments, restricted)
	switch {
	case err == nil:
		return chat.PostAudience{Segments: reach.Segments, Restricted: reach.Restricted, Members: members}, nil
	case errors.Is(err, chatgate.ErrInvalid):
		return chat.PostAudience{}, chat.ErrInvalidArgument
	default:
		return chat.PostAudience{}, chat.ErrPermissionDenied
	}
}

func (p chatgateSegmentPort) Signature(ctx context.Context, tenant, conversation, person string, audience []string) ([]string, error) {
	if p.state == nil {
		return nil, chat.ErrUnavailable
	}
	st, err := p.state.SegmentState(ctx, chatgate.Scope{Tenant: tenant, Conversation: conversation})
	if err != nil {
		return nil, err
	}
	return st.Signature(person, audience), nil
}
