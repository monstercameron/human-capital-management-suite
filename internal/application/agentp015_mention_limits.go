package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona/limits"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var errPersonaMentionLimits = errors.New("application: persona mention limits unavailable")

const (
	// personaMentionLimitPolicyVersion names the served ceilings. A reservation
	// is settled only under the policy version that admitted it.
	personaMentionLimitPolicyVersion = "agentp-015.served.v1"
	// personaMentionSpendEstimateMicros is what one mention reserves against
	// the agent's daily ceiling. The run's own cost ceiling (the budget ledger)
	// still bounds what the model call may spend; this boundary cannot see the
	// actual figure, so a mention that ran is counted at the estimate.
	personaMentionSpendEstimateMicros int64 = 50_000
	// personaMentionDailySpendMicros is the per-agent, per-tenant daily ceiling:
	// a thousand answered mentions a day at the estimate.
	personaMentionDailySpendMicros int64 = 50_000_000
	// personaMentionLimitCloseTimeout bounds closing a reservation after the
	// run returns, when the request's own deadline may already have passed.
	personaMentionLimitCloseTimeout = 5 * time.Second
)

// servedPersonaMentionLimitPolicy is the admission policy for mentions: thirty
// an hour and three at once for one person asking one agent, 120 an hour in
// one conversation, and a daily spend ceiling for one agent in one tenant.
// The starter templates declare the same per-person numbers.
func servedPersonaMentionLimitPolicy() limits.Policy {
	return limits.Policy{
		Version:                 personaMentionLimitPolicyVersion,
		InvokerPerPersona:       limits.RateLimit{Max: 30, Window: time.Hour},
		InvokerConcurrency:      3,
		Conversation:            limits.RateLimit{Max: 120, Window: time.Hour},
		PersonaDailySpendMicros: personaMentionDailySpendMicros,
	}
}

// PersonaMentionLimitStores returns one tenant's atomic limit store.
type PersonaMentionLimitStores interface {
	PersonaMentionLimitStore(ctx context.Context, tenant string) (limits.Store, error)
}

// AgentPersonaMentionLimitStores reads the durable limit buckets kept in the
// agent database beside the persona versions.
type AgentPersonaMentionLimitStores struct {
	Store *agentpersonastore.Store
}

func (s AgentPersonaMentionLimitStores) PersonaMentionLimitStore(ctx context.Context, tenant string) (limits.Store, error) {
	if s.Store == nil {
		return nil, errPersonaMentionLimits
	}
	scoped, err := s.Store.ForTenant(ctx, values.TenantId(tenant))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errPersonaMentionLimits, err)
	}
	return scoped, nil
}

// PersonaMentionLimits is the mention admission the served invocation path
// applies before a run starts.
type PersonaMentionLimits struct {
	Policy              limits.Policy
	SpendEstimateMicros int64
	Stores              PersonaMentionLimitStores
	Now                 func() time.Time
}

// newServedPersonaMentionLimits binds the served policy to the durable store.
func newServedPersonaMentionLimits(personas *agentpersonastore.Store, now func() time.Time) (*PersonaMentionLimits, error) {
	if personas == nil || now == nil {
		return nil, errPersonaMentionLimits
	}
	return &PersonaMentionLimits{
		Policy: servedPersonaMentionLimitPolicy(), SpendEstimateMicros: personaMentionSpendEstimateMicros,
		Stores: AgentPersonaMentionLimitStores{Store: personas}, Now: now,
	}, nil
}

// validate is run once when the invocation path is composed, so an incomplete
// policy stops the composition instead of refusing every mention later.
func (l *PersonaMentionLimits) validate() error {
	if l == nil || isNilPersonaOutputPort(l.Stores) || l.Now == nil || l.SpendEstimateMicros <= 0 || l.SpendEstimateMicros > l.Policy.PersonaDailySpendMicros {
		return errPersonaMentionLimits
	}
	// limits.New holds the rule for a complete policy.
	if _, err := limits.New(l.Policy, l.Now, limits.NewMemoryStore()); err != nil {
		return fmt.Errorf("%w: %v", errPersonaMentionLimits, err)
	}
	return nil
}

// PersonaMentionLimitError is the refusal of a mention that would pass a
// ceiling. It carries only what the person who asked may be told: which
// ceiling, and how long until it can be tried again. No model call was made.
type PersonaMentionLimitError struct {
	Code       limits.DenialCode
	Scope      limits.Scope
	RetryAfter time.Duration
}

func (e *PersonaMentionLimitError) Error() string {
	if e == nil {
		return errPersonaMentionLimits.Error()
	}
	return fmt.Sprintf("application: persona mention refused: %s, retry after %s", e.Code, e.RetryAfter.Round(time.Second))
}

// Unwrap keeps errors.Is(err, limits.ErrDenied) true for callers that only
// need to know the mention was refused by a limit.
func (e *PersonaMentionLimitError) Unwrap() error { return limits.ErrDenied }

// personaMentionLimitedRunStarter reserves a mention against every ceiling
// before the run starts and closes the reservation when the run returns. A
// refused mention never reaches the worker, so it costs no model call.
type personaMentionLimitedRunStarter struct {
	next   agentinvoke.RunStarter
	limits *PersonaMentionLimits
}

func (s personaMentionLimitedRunStarter) Start(ctx context.Context, request agentinvoke.RunRequest) error {
	if s.next == nil || s.limits == nil || isNilPersonaOutputPort(s.limits.Stores) || s.limits.Now == nil || ctx == nil {
		return errPersonaMentionLimits
	}
	version, err := strconv.ParseUint(strings.TrimPrefix(request.PersonaVersion, "v"), 10, 64)
	if err != nil || version == 0 {
		return fmt.Errorf("%w: persona version", errPersonaMentionLimits)
	}
	store, err := s.limits.Stores.PersonaMentionLimitStore(ctx, request.TenantID)
	if err != nil {
		return err
	}
	service, err := limits.New(s.limits.Policy, s.limits.Now, store)
	if err != nil {
		return fmt.Errorf("%w: %v", errPersonaMentionLimits, err)
	}
	reservation, err := service.Reserve(ctx, limits.Request{
		TenantID: request.TenantID, InvokerID: request.InvokerID, ConversationID: request.ConversationID,
		PersonaID: request.PersonaID, PersonaVersion: version, PolicyVersion: s.limits.Policy.Version,
		EstimatedSpendMicros: s.limits.SpendEstimateMicros,
	})
	if err != nil {
		var denial *limits.Denial
		if errors.As(err, &denial) {
			return &PersonaMentionLimitError{Code: denial.Code, Scope: denial.Scope, RetryAfter: denial.RetryAfter}
		}
		// The ceilings could not be read: admitting the mention would be
		// admitting it unmetered.
		return fmt.Errorf("%w: %v", errPersonaMentionLimits, err)
	}
	runErr := s.next.Start(ctx, request)
	// The run has returned; its request may be cancelled or out of time, and
	// the reservation must still be closed so the person's concurrent count
	// goes back down.
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), personaMentionLimitCloseTimeout)
	defer cancel()
	var ran *PersonaRunFailure
	if runErr == nil || errors.As(runErr, &ran) {
		// The run reached the model (it finished, or failed while running).
		err = reservation.Settle(closeCtx, s.limits.SpendEstimateMicros)
	} else {
		// The run was refused or could not start: nothing was spent.
		err = reservation.Release(closeCtx)
	}
	if err != nil {
		// The hourly and daily buckets roll over on their own, so a
		// reservation left open delays this person for at most the window.
		slog.WarnContext(ctx, "hcmnext.persona_mention_limit_close_failed", "invocation_id", request.InvocationID, "error", err.Error())
	}
	return runErr
}

// ResolveTargetAgentID passes through to the worker, which names the exact
// agent identity before a grant is issued. Limits do not change identity.
func (s personaMentionLimitedRunStarter) ResolveTargetAgentID(ctx context.Context, request agentinvoke.RunRequest) (string, error) {
	resolver, ok := s.next.(agentinvoke.TargetAgentResolver)
	if !ok || isNilPersonaOutputPort(resolver) {
		return "", errPersonaChatInvocation
	}
	return resolver.ResolveTargetAgentID(ctx, request)
}
