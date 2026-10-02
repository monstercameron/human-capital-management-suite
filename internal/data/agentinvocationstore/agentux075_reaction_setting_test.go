package agentinvocationstore

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The owner's choice about an agent's reactions is kept per persona and per
// tenant; with no choice stored the agent reacts (AGENTUX-075).
func TestTodo_AGENTUX_075_ReactionSetting_Integration(t *testing.T) {
	store, tenantA, tenantB, cleanup := agentUXR5SrvFeedbackStore(t)
	defer cleanup()
	ctx := context.Background()
	at := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)

	if on, err := store.PersonaReactionsEnabled(ctx, tenantA.String(), "persona-a"); err != nil || !on {
		t.Fatalf("an agent with no stored choice: enabled=%v err=%v, want it to react", on, err)
	}
	off, err := store.SetPersonaReactions(ctx, tenantA.String(), "persona-a", "owner", false, at)
	if err != nil || off.React || off.Revision != 1 || off.UpdatedBy != "owner" {
		t.Fatalf("turning reactions off: %+v %v", off, err)
	}
	if on, err := store.PersonaReactionsEnabled(ctx, tenantA.String(), "persona-a"); err != nil || on {
		t.Fatalf("after turning them off: enabled=%v err=%v", on, err)
	}
	// One agent's choice is not another's, and not another tenant's.
	if on, err := store.PersonaReactionsEnabled(ctx, tenantA.String(), "persona-b"); err != nil || !on {
		t.Fatalf("another agent was switched off: enabled=%v err=%v", on, err)
	}
	if on, err := store.PersonaReactionsEnabled(ctx, tenantB.String(), "persona-a"); err != nil || !on {
		t.Fatalf("another tenant's agent was switched off: enabled=%v err=%v", on, err)
	}
	on, err := store.SetPersonaReactions(ctx, tenantA.String(), "persona-a", "owner-2", true, at.Add(time.Minute))
	if err != nil || !on.React || on.Revision != 2 || on.UpdatedBy != "owner-2" {
		t.Fatalf("turning reactions back on: %+v %v", on, err)
	}
	for name, call := range map[string]func() error{
		"no persona": func() error {
			_, err := store.SetPersonaReactions(ctx, tenantA.String(), "", "owner", false, at)
			return err
		},
		"no actor": func() error {
			_, err := store.SetPersonaReactions(ctx, tenantA.String(), "persona-a", " ", false, at)
			return err
		},
		"no tenant": func() error {
			_, err := store.SetPersonaReactions(ctx, "", "persona-a", "owner", false, at)
			return err
		},
		"no time": func() error {
			_, err := store.SetPersonaReactions(ctx, tenantA.String(), "persona-a", "owner", false, time.Time{})
			return err
		},
		"read, blank": func() error { _, err := store.PersonaReactionsEnabled(ctx, tenantA.String(), " "); return err },
	} {
		if err := call(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
}
