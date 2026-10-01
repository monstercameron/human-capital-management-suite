package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workerlifecyclestore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workerlifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var ErrPersonaOnboardingFacts = errors.New("application: current onboarding owner facts unavailable")

type PersonaOnboardingSnapshotSource interface {
	Get(context.Context, values.EntityRef) (workerlifecyclestore.Snapshot, error)
}

type PersonaOnboardingChannelPolicySource interface {
	CapturePersonaChannelPolicy(context.Context, string, string, string) (chatstore.PersonaChannelPolicySnapshot, error)
}

// NativePersonaOnboardingPort reads durable owner inputs; it never accepts
// readiness facts, task completions or a channel binding from the model.
type NativePersonaOnboardingPort struct {
	source    PersonaOnboardingSnapshotSource
	channels  PersonaOnboardingChannelPolicySource
	now       func() time.Time
	authority PersonaOnboardingAuthority
}

var _ PersonaOnboardingPort = (*NativePersonaOnboardingPort)(nil)

func NewNativePersonaOnboardingPort(source PersonaOnboardingSnapshotSource, channels PersonaOnboardingChannelPolicySource, now func() time.Time, authority PersonaOnboardingAuthority) (*NativePersonaOnboardingPort, error) {
	if isNilPersonaOutputPort(source) || isNilPersonaOutputPort(channels) || now == nil || isNilPersonaOutputPort(authority) {
		return nil, ErrPersonaOnboardingFacts
	}
	return &NativePersonaOnboardingPort{source: source, channels: channels, now: now, authority: authority}, nil
}

func (p *NativePersonaOnboardingPort) current(ctx context.Context, worker values.EntityRef) (workerlifecyclestore.Snapshot, workerlifecycle.ReadinessResolution, error) {
	fail := func(err error) (workerlifecyclestore.Snapshot, workerlifecycle.ReadinessResolution, error) {
		return workerlifecyclestore.Snapshot{}, workerlifecycle.ReadinessResolution{}, errors.Join(ErrPersonaOnboardingFacts, err)
	}
	if p == nil || ctx == nil || isNilPersonaOutputPort(p.source) || isNilPersonaOutputPort(p.authority) || p.now == nil || worker.Kind != "worker" {
		return fail(nil)
	}
	if err := personaWorker(ctx, worker); err != nil {
		return fail(err)
	}
	principal, _ := trust.FromContext(ctx)
	if err := p.authority.AuthorizePersonaOnboarding(ctx, principal, worker); err != nil {
		return fail(err)
	}
	snapshot, err := p.source.Get(ctx, worker)
	if err != nil {
		return fail(err)
	}
	if snapshot.Request.Plan.Worker != worker || snapshot.Validate() != nil || strings.TrimSpace(snapshot.DisplayName) == "" {
		return fail(nil)
	}
	asOf, err := values.ParseLocalDate(p.now().UTC().Format("2006-01-02"))
	if err != nil {
		return fail(err)
	}
	snapshot.Request.AsOf = asOf
	// This persona serves the HR/manager audience. Requirement owners retain
	// protected evidence content; manager checklist projection carries status
	// and references with a redacted account, as the lifecycle domain specifies.
	protected := map[string]bool{}
	for _, requirement := range snapshot.Request.Requirements {
		protected[requirement.RequirementID] = requirement.Protected
	}
	snapshot.Request.Facts = slices.Clone(snapshot.Request.Facts)
	for i := range snapshot.Request.Facts {
		if protected[snapshot.Request.Facts[i].RequirementID] {
			snapshot.Request.Facts[i].Summary = "REDACTED"
		}
	}
	readiness, err := workerlifecycle.ResolveOnboardingReadiness(snapshot.Request)
	if err != nil {
		return fail(err)
	}
	return snapshot, readiness, nil
}

func (p *NativePersonaOnboardingPort) Checklist(ctx context.Context, worker values.EntityRef) (workerlifecycle.ReadinessResolution, error) {
	_, readiness, err := p.current(ctx, worker)
	return readiness, err
}

// Tasks projects already emitted durable children. Pending templates have no
// intent identity and are never emitted by a T0 read.
func (p *NativePersonaOnboardingPort) Tasks(ctx context.Context, worker values.EntityRef) ([]workerlifecycle.ChildIntent, error) {
	states, err := p.TaskStates(ctx, worker)
	if err != nil {
		return nil, err
	}
	tasks := make([]workerlifecycle.ChildIntent, 0, len(states))
	for _, state := range states {
		if state.Intent == nil {
			continue
		}
		tasks = append(tasks, *state.Intent)
	}
	return tasks, nil
}

func onboardingWelcomeDraft(snapshot workerlifecyclestore.Snapshot) string {
	return fmt.Sprintf("Welcome, %s! Your onboarding plan begins on %s. We look forward to helping you get started.", snapshot.DisplayName, snapshot.Request.Plan.EventDate)
}

// DraftWelcome produces a zero-effect proposal without copying protected
// requirement evidence or treating planned work as complete.
func (p *NativePersonaOnboardingPort) DraftWelcome(ctx context.Context, worker values.EntityRef) (string, error) {
	snapshot, _, err := p.current(ctx, worker)
	if err != nil {
		return "", err
	}
	return onboardingWelcomeDraft(snapshot), nil
}

// PrepareWelcomePost binds the proposal to the worker's owner-assigned channel
// and its current manager ceiling. It does not deliver or assert confirmation;
// the sealed persona output gateway still owns T2 admission and room fencing.
func (p *NativePersonaOnboardingPort) PrepareWelcomePost(ctx context.Context, principal *trust.Principal, worker values.EntityRef, draft, channel string) (PersonaWelcomePostIntent, error) {
	if p == nil || ctx == nil || principal == nil || isNilPersonaOutputPort(p.channels) {
		return PersonaWelcomePostIntent{}, ErrPersonaOnboardingFacts
	}
	trusted, ok := trust.FromContext(ctx)
	if !ok || trusted != principal {
		return PersonaWelcomePostIntent{}, ErrPersonaOnboardingFacts
	}
	snapshot, _, err := p.current(ctx, worker)
	if err != nil {
		return PersonaWelcomePostIntent{}, err
	}
	if channel != snapshot.ChannelID || draft != onboardingWelcomeDraft(snapshot) {
		return PersonaWelcomePostIntent{}, ErrPersonaOnboardingFacts
	}
	room, err := p.channels.CapturePersonaChannelPolicy(ctx, worker.Tenant.String(), channel, principal.Subject())
	channelClass := "PRIVATE"
	placements := []string{"PRIVATE", "ONBOARDING"}
	if room.Kind == "PUBLIC_CHANNEL" {
		channelClass = "PUBLIC"
		placements = []string{"ONBOARDING"}
	}
	if err != nil || room.TenantID != worker.Tenant.String() || room.ConversationID != channel || room.ManagerID != principal.Subject() || room.Revision == 0 || room.PolicyRevision <= 0 || room.ExternalMembers || room.GuestMembers || room.Policy.AllowExternalMembers || room.Policy.AllowCrossCompanyMembers || room.Policy.AlwaysPrivate || !slices.Contains([]string{"PUBLIC_CHANNEL", "PRIVATE_CHANNEL"}, room.Kind) || !slices.Contains(placements, room.Policy.PlacementClass) || !slices.Contains([]string{"T2", "T3"}, room.Policy.MaxTier) || !slices.Contains(room.Policy.AllowedDataClasses, "ONBOARDING") || !slices.Contains(room.Policy.AllowedChannelClasses, channelClass) {
		return PersonaWelcomePostIntent{}, errors.Join(ErrPersonaOnboardingFacts, err)
	}
	return PersonaWelcomePostIntent{Worker: worker, WorkerRevision: snapshot.WorkerRevision, SnapshotRevision: snapshot.Revision, PlanDigest: snapshot.Request.Plan.CanonicalDigest, Channel: channel, ChannelRevision: room.Revision, ChannelPolicyRevision: room.PolicyRevision, Draft: draft}, nil
}
