package application

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workerlifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_021_Onboarding_NativePersistedTaskStates(t *testing.T) {
	port, source, _, ctx, _ := nativeOnboardingFixture(t)
	worker := source.snapshot.Request.Plan.Worker
	states, err := port.TaskStates(ctx, worker)
	if err != nil || len(states) != 2 || states[0].State != workerlifecycle.StatusChildEmitted || states[0].Intent == nil || states[0].Intent.ID != source.snapshot.Tracker.Children[0].IntentID || states[0].Outcome != "" || states[0].ObservationRef != "" || states[1].State != workerlifecycle.StatusChildPending || states[1].Intent != nil {
		t.Fatalf("initial actual state: %+v %v", states, err)
	}
	raw, err := json.Marshal(states[1])
	if err != nil || strings.Contains(string(raw), "Intent") || strings.Contains(string(raw), "Outcome") || strings.Contains(string(raw), "ObservationRef") {
		t.Fatalf("pending fabricated fields: %s %v", raw, err)
	}
	observation := values.EntityRef{Tenant: worker.Tenant, Kind: "observation", Id: uuid.NewString()}
	source.snapshot.Tracker, err = workerlifecycle.ObserveChild(source.snapshot.Tracker, "identity", observation, workerlifecycle.ChildObserved)
	if err != nil {
		t.Fatal(err)
	}
	states, err = port.TaskStates(ctx, worker)
	if err != nil || len(states) != 2 || states[0].State != workerlifecycle.StatusChildObserved || states[0].Outcome != workerlifecycle.ChildObserved || states[0].ObservationRef != observation.String() || states[1].State != workerlifecycle.StatusChildPending {
		t.Fatalf("retained observed outcome: %+v %v", states, err)
	}
	if source.snapshot.Tracker.Children[1].IntentID != "" {
		t.Fatal("read emitted pending work")
	}
	// Return data cannot rewrite the retained source, and a failed owner
	// transition must not invent a missing observation reference.
	states[0].Intent.ID = "tampered"
	if source.snapshot.Tracker.Children[0].IntentID == "tampered" {
		t.Fatal("task projection aliased owner state")
	}
	failed, failedSource, _, failedCtx, _ := nativeOnboardingFixture(t)
	failedSource.snapshot.Tracker, err = workerlifecycle.ObserveChild(failedSource.snapshot.Tracker, "identity", observation, workerlifecycle.ChildFailed)
	if err != nil {
		t.Fatal(err)
	}
	states, err = failed.TaskStates(failedCtx, failedSource.snapshot.Request.Plan.Worker)
	if err != nil || states[0].State != workerlifecycle.StatusChildFailed || states[0].Outcome != workerlifecycle.ChildFailed || states[0].ObservationRef != "" {
		t.Fatalf("failed outcome: %+v %v", states, err)
	}
	raw, err = json.Marshal(states[0])
	if err != nil || strings.Contains(string(raw), "ObservationRef") {
		t.Fatalf("failed reference fabricated: %s %v", raw, err)
	}
	directory := failed.authority.(*CurrentPersonaOnboardingAuthority).directory.(*nativeOnboardingDirectory)
	calls := failedSource.calls
	directory.subjects = nil
	if _, err = failed.TaskStates(failedCtx, failedSource.snapshot.Request.Plan.Worker); !errors.Is(err, ErrPersonaOnboardingFacts) || failedSource.calls != calls {
		t.Fatalf("revoked task target: %v calls=%d", err, failedSource.calls)
	}
}

func TestTodo_AGENTP_021_Onboarding_NativeExplicitOnboardingPlacement(t *testing.T) {
	for _, entry := range []struct {
		name, kind, placement, class string
		allowed                      bool
	}{
		{"public_onboarding", "PUBLIC_CHANNEL", "ONBOARDING", "PUBLIC", true},
		{"generic_public", "PUBLIC_CHANNEL", "PUBLIC", "PUBLIC", false},
		{"public_wrong_channel_class", "PUBLIC_CHANNEL", "ONBOARDING", "PRIVATE", false},
		{"private_onboarding", "PRIVATE_CHANNEL", "ONBOARDING", "PRIVATE", true},
		{"private", "PRIVATE_CHANNEL", "PRIVATE", "PRIVATE", true},
		{"private_wrong_placement", "PRIVATE_CHANNEL", "PUBLIC", "PRIVATE", false},
	} {
		t.Run(entry.name, func(t *testing.T) {
			port, source, channels, ctx, principal := nativeOnboardingFixture(t)
			channels.room.Kind = entry.kind
			channels.room.Policy.PlacementClass = entry.placement
			channels.room.Policy.AllowedChannelClasses = []string{entry.class}
			worker := source.snapshot.Request.Plan.Worker
			draft, err := port.DraftWelcome(ctx, worker)
			if err != nil {
				t.Fatal(err)
			}
			intent, err := port.PrepareWelcomePost(ctx, principal, worker, draft, source.snapshot.ChannelID)
			if entry.allowed {
				if err != nil || intent.ChannelRevision != channels.room.Revision || intent.ChannelPolicyRevision != channels.room.PolicyRevision {
					t.Fatalf("onboarding placement denied: %+v %v", intent, err)
				}
			} else if !errors.Is(err, ErrPersonaOnboardingFacts) {
				t.Fatalf("unreviewed placement admitted: %+v %v", intent, err)
			}
		})
	}
}
