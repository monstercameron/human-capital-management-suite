package chatfilter

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestTodo_CHATMOD_003_EnableLimits: a switch that would leave a channel with
// a set of rules the compiler refuses is refused when it is made. It used to be
// stored, and then every message in the channel (or the workspace) failed.
func TestTodo_CHATMOD_003_EnableLimits(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	admin := Actor{Tenant: "tenant", Subject: "admin"}
	fresh := func() (*Service, *fixtureStore) {
		store := &fixtureStore{}
		return &Service{Store: store, Registry: NewRegistry(), Authority: fixtureAuthority{workspace: true, readable: true}, Delivery: &fixtureDelivery{}, Now: func() time.Time { return now }}, store
	}
	sends := func(t *testing.T, s *Service, channel string) {
		t.Helper()
		if _, err := s.Evaluate(ctx, Input{Tenant: "tenant", Channel: channel, Body: "an ordinary message"}, false); err != nil {
			t.Fatalf("a message in %q is refused by the filters themselves: %v", channel, err)
		}
	}
	builtin := Builtins()[0].ID

	t.Run("a built-in list cannot be switched to an action that needs a target", func(t *testing.T) {
		s, store := fresh()
		for _, channel := range []string{"", "general"} {
			if err := s.Enable(ctx, admin, Enablement{RuleID: builtin, Channel: channel, Enabled: true, Action: "notify"}, false); !errors.Is(err, ErrInvalid) {
				t.Fatalf("channel %q: notify with nowhere to notify was accepted: %v", channel, err)
			}
		}
		if len(store.enabled) != 0 {
			t.Fatalf("a refused switch was stored: %+v", store.enabled)
		}
		sends(t, s, "general")
		// The actions a list can take are still accepted.
		for _, action := range []string{"", "block", "mask", "flag"} {
			if err := s.Enable(ctx, admin, Enablement{RuleID: builtin, Enabled: true, Action: action}, false); err != nil {
				t.Fatalf("action %q on a built-in list: %v", action, err)
			}
			sends(t, s, "general")
		}
	})

	t.Run("one rule more than a channel can hold is refused", func(t *testing.T) {
		s, store := fresh()
		add := func(i int, channels ...string) string {
			d := Definition{ID: fmt.Sprintf("rule-%03d", i), Name: "Restricted term", Version: "1.0.0", Kind: "words", Match: []string{fmt.Sprintf("quartz%03d", i)}, Action: "mask", Channels: channels}
			if err := s.CreateVersion(ctx, admin, d); err != nil {
				t.Fatal(err)
			}
			return d.ID
		}
		for i := 0; i < maxActiveRules; i++ {
			if err := s.Enable(ctx, admin, Enablement{RuleID: add(i), Enabled: true}, false); err != nil {
				t.Fatalf("rule %d of %d: %v", i+1, maxActiveRules, err)
			}
		}
		sends(t, s, "general")
		over := add(maxActiveRules)
		if err := s.Enable(ctx, admin, Enablement{RuleID: over, Enabled: true}, true); !errors.Is(err, ErrInvalid) {
			t.Fatalf("rule %d was switched on: %v", maxActiveRules+1, err)
		}
		if len(store.enabled) != maxActiveRules {
			t.Fatalf("%d switches stored, want %d", len(store.enabled), maxActiveRules)
		}
		sends(t, s, "general")
		// A channel's own rule is one more for that channel.
		own := add(maxActiveRules+1, "general")
		if err := s.Enable(ctx, admin, Enablement{RuleID: own, Channel: "general", Enabled: true}, false); !errors.Is(err, ErrInvalid) {
			t.Fatalf("a channel's own rule was added to a full set: %v", err)
		}
		// Switching one off makes room, and switching off is never refused.
		if err := s.Enable(ctx, admin, Enablement{RuleID: "rule-000", Enabled: false}, false); err != nil {
			t.Fatalf("switching a rule off: %v", err)
		}
		if err := s.Enable(ctx, admin, Enablement{RuleID: over, Enabled: true}, false); err != nil {
			t.Fatalf("a rule in the room that was made: %v", err)
		}
		sends(t, s, "general")
	})

	t.Run("a workspace switch counts the channels that have settings of their own", func(t *testing.T) {
		s, _ := fresh()
		// The channel has one built-in list on by its own setting.
		if err := s.Enable(ctx, admin, Enablement{RuleID: builtin, Channel: "general", Enabled: true}, false); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < maxActiveRules-1; i++ {
			d := Definition{ID: fmt.Sprintf("wide-%03d", i), Name: "Restricted term", Version: "1.0.0", Kind: "words", Match: []string{fmt.Sprintf("topaz%03d", i)}, Action: "flag"}
			if err := s.CreateVersion(ctx, admin, d); err != nil {
				t.Fatal(err)
			}
			if err := s.Enable(ctx, admin, Enablement{RuleID: d.ID, Enabled: true}, false); err != nil {
				t.Fatalf("workspace rule %d: %v", i+1, err)
			}
		}
		// The workspace holds 127 and has room for one more; "general" is full.
		last := Definition{ID: "wide-last", Name: "Restricted term", Version: "1.0.0", Kind: "words", Match: []string{"onyx"}, Action: "flag"}
		if err := s.CreateVersion(ctx, admin, last); err != nil {
			t.Fatal(err)
		}
		if err := s.Enable(ctx, admin, Enablement{RuleID: last.ID, Enabled: true}, false); !errors.Is(err, ErrInvalid) {
			t.Fatalf("a workspace rule overfilled a channel with a setting of its own: %v", err)
		}
		sends(t, s, "general")
		sends(t, s, "other")
	})
}
