package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// A run answers in the model's time plus three seconds: with a model whose two
// turns take a set time, everything else (claim, authority, tool, checkpoints,
// validation, delivery) adds no more than three seconds on the test database, and
// the run's own timing line says how the time was spent.
func TestTodo_AGENTUX_028_Integration(t *testing.T) {
	const turn = 400 * time.Millisecond
	timedCtx, timing, run, elapsed, err, model, outputs := agentUXSpeedRunWithModelDelay(t, turn)
	if err != nil || run.State != runstate.StateCompleted || model.calls != 2 || outputs != 1 {
		t.Fatalf("run = %s, %d model calls, %d outputs, %v", run.State, model.calls, outputs, err)
	}
	timing.mu.Lock()
	modelTime := timing.stages["model_execute"].Duration
	modelTurns := timing.stages["model_execute"].Count
	timing.mu.Unlock()
	if modelTurns != 2 || modelTime < 2*turn {
		t.Fatalf("the model's two turns were measured as %d turns, %s; each took %s", modelTurns, modelTime, turn)
	}
	if overhead := elapsed - modelTime; overhead > 3*time.Second {
		t.Fatalf("a run took %s with %s of it the model's: %s over the model's time, the limit is three seconds", elapsed, modelTime, overhead)
	}
	if elapsed >= 10*time.Second {
		t.Fatalf("the run took %s, the hard limit is ten seconds", elapsed)
	}
	agentUXSpeedEmit(timedCtx, false)
}

type agentUX028Policy struct{ allow bool }

func (p *agentUX028Policy) IsBoundT0Run(_ context.Context, request agentinvoke.RunRequest) (bool, error) {
	return p.allow && request.Grant.TargetAgentID == "agent-a" && len(request.Skills) == 1 && !request.Grant.ExpiresAt.IsZero(), nil
}

type agentUX028Persona struct{ state *agentinvoke.Admission }

func (p agentUX028Persona) Resolve(context.Context, agentinvoke.AdmissionRequest) (agentinvoke.Admission, error) {
	return *p.state, nil
}

type agentUX028Grants struct {
	grant foregroundPositiveStore
	epoch *uint64
}

func (g agentUX028Grants) ForTenant(context.Context, values.TenantId) (agentdelegation.GrantStore, error) {
	store := g.grant
	store.epoch = *g.epoch
	return store, nil
}

// Mutable authority (the installation, the revocation epoch, the stop controls,
// the person's membership and the audience) is read again at every boundary, so a
// change made mid-run stops the next step; and within one boundary the result is
// reused, so the run does not read it ten times (AGENTUX-028).
func TestTodo_AGENTUX_028_Security(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	request := foregroundAuthorityRequest(now)
	for _, change := range []struct {
		name  string
		apply func(admission *agentinvoke.Admission, epoch *uint64, policy *agentUX028Policy)
	}{
		{"installation suspended", func(a *agentinvoke.Admission, _ *uint64, _ *agentUX028Policy) { a.Installation.Suspended = true }},
		{"installation removed", func(a *agentinvoke.Admission, _ *uint64, _ *agentUX028Policy) { a.PersonaInstalled = false }},
		{"agent suspended", func(a *agentinvoke.Admission, _ *uint64, _ *agentUX028Policy) { a.Persona.Suspended = true }},
		{"person left the conversation", func(a *agentinvoke.Admission, _ *uint64, _ *agentUX028Policy) { a.HumanMember = false }},
		{"audience no longer includes the person", func(a *agentinvoke.Admission, _ *uint64, _ *agentUX028Policy) { a.AudienceMember = false }},
		{"revocation epoch moved", func(_ *agentinvoke.Admission, epoch *uint64, _ *agentUX028Policy) { *epoch = 2 }},
		{"stop control engaged", func(_ *agentinvoke.Admission, _ *uint64, policy *agentUX028Policy) { policy.allow = false }},
	} {
		t.Run(change.name, func(t *testing.T) {
			admission := agentinvoke.Admission{Persona: agentinvoke.Persona{ID: "persona-a", Version: "7", InstallationID: "install-a", Current: true}, Installation: agentinvoke.Installation{ID: "install-a", Current: true}, Discoverable: map[string][]string{"skill.read": {"scope:read"}}, HumanMember: true, AudienceMember: true, PersonaInstalled: true}
			epoch := uint64(1)
			policy := &agentUX028Policy{allow: true}
			authority := &PersonaForegroundRunAuthority{
				builder: &PersonaRunRequestBuilder{source: foregroundRequestSourceFunc(func(context.Context, agentinvoke.RunRequest) (PersonaRunRequestFacts, error) {
					return foregroundFactsFromRequest(request), nil
				})},
				persona: agentUX028Persona{state: &admission},
				grants:  agentUX028Grants{grant: foregroundPositiveStore{grant: foregroundPositiveGrant(now)}, epoch: &epoch},
				policy:  policy, now: func() time.Time { return now },
			}
			base := trust.WithPrincipal(context.Background(), foregroundPrincipal(t, now.Add(-time.Minute), now.Add(time.Hour), "persona-mention", "user-a", "tenant-a"))
			ctx, _ := withAgentUXRunTiming(base)
			if _, err := authority.VerifyAdmission(ctx, request); err != nil {
				t.Fatalf("before the change: %v", err)
			}
			change.apply(&admission, &epoch, policy)
			// Inside the boundary that already verified, the answer is reused.
			if _, err := authority.VerifyAdmission(ctx, request); err != nil {
				t.Fatalf("the boundary that verified was asked again: %v", err)
			}
			// The next boundary reads it again and refuses.
			agentUXSpeedInvalidateAuthority(ctx)
			if _, err := authority.VerifyAdmission(ctx, request); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
				t.Fatalf("after %s, the next boundary gave %v, want a refusal", change.name, err)
			}
		})
	}
}
