package schedule_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
)

func agentRunFixture() (schedule.TriggerDefinition, []schedule.AuthorizedTarget) {
	def, intents := fixture()
	target := &schedule.AgentRunTarget{
		Agent:     schedule.AgentVersionRef{ID: "agent:payroll-digest", Version: "7", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		SponsorID: "service:payroll-ops",
		Purpose:   def.Purpose,
		Budget:    schedule.AgentRunBudget{MaxCostMicros: 250000, MaxInputTokens: 12000, MaxOutputTokens: 2000},
		Destination: schedule.AgentRunDestination{
			AudienceID: "audience:payroll-leads", AudienceSnapshotID: "membership:42",
			AudienceDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
	}
	def.Target = schedule.IntentRef{}
	def.TargetKind = schedule.TargetAgentRun
	def.AgentRun = target
	return def, []schedule.AuthorizedTarget{{AgentRun: target, PublisherRef: intents[0].PublisherRef}}
}

func TestTodo_AGENT_029(t *testing.T) {
	if schedule.Version() != 2 {
		t.Fatalf("schedule schema version = %d, want 2 for the AGENT_RUN target contract", schedule.Version())
	}
	def, targets := agentRunFixture()
	reg := schedule.NewRegistry()
	published, err := schedule.Publish(reg, def, targets)
	if err != nil {
		t.Fatalf("Publish AGENT_RUN: %v", err)
	}
	if err := published.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if published.Definition.TargetKind != schedule.TargetAgentRun || published.Definition.AgentRun.Agent.Version != "7" {
		t.Fatalf("published target = %+v, want exact AGENT_RUN version 7", published.Definition)
	}
	if got := published.Definition.Explain(); got == "" || got == def.Target.String() {
		t.Fatalf("Explain did not identify the agent target: %q", got)
	}
	def.AgentRun.Agent.Version = "latest"
	if err := published.Verify(); err != nil {
		t.Fatalf("caller mutation changed immutable publication: %v", err)
	}
	stored, ok, err := reg.Get(published.Ref())
	if err != nil || !ok || stored.Definition.AgentRun.Agent.Version != "7" {
		t.Fatalf("stored target = %+v, found=%v err=%v; want pinned version 7", stored.Definition.AgentRun, ok, err)
	}
}

func TestTodo_AGENT_029_Golden(t *testing.T) {
	def, targets := agentRunFixture()
	published, err := schedule.Publish(schedule.NewRegistry(), def, targets)
	if err != nil {
		t.Fatal(err)
	}
	const wantDigest = "sha256:4d634d728adbfce3181b8bce3572c40c4ceabfec84f02e32ed6cae9de682c532"
	if published.Digest != wantDigest {
		t.Fatalf("canonical digest = %q, want %q", published.Digest, wantDigest)
	}
}

func TestTodo_AGENT_029_Conformance(t *testing.T) {
	legacy, intentTargets := fixture()
	if _, err := schedule.Publish(schedule.NewRegistry(), legacy, intentTargets); err != nil {
		t.Fatalf("legacy intent target changed: %v", err)
	}

	valid, targets := agentRunFixture()
	tests := []struct {
		name   string
		change func(*schedule.TriggerDefinition)
	}{
		{name: "unknown target kind", change: func(d *schedule.TriggerDefinition) { d.TargetKind = "AGENT_OR_SOMETHING" }},
		{name: "agent target missing", change: func(d *schedule.TriggerDefinition) { d.AgentRun = nil }},
		{name: "mixed target forms", change: func(d *schedule.TriggerDefinition) { d.Target = legacy.Target }},
		{name: "empty version", change: func(d *schedule.TriggerDefinition) { d.AgentRun.Agent.Version = "" }},
		{name: "purpose mismatch", change: func(d *schedule.TriggerDefinition) { d.AgentRun.Purpose = "different-purpose" }},
		{name: "unbounded budget", change: func(d *schedule.TriggerDefinition) { d.AgentRun.Budget.MaxOutputTokens = 0 }},
		{name: "destination not pinned", change: func(d *schedule.TriggerDefinition) { d.AgentRun.Destination.AudienceSnapshotID = "" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			def := valid
			copy := *valid.AgentRun
			def.AgentRun = &copy
			tc.change(&def)
			if _, err := schedule.Publish(schedule.NewRegistry(), def, targets); err == nil {
				t.Fatal("Publish accepted invalid target")
			}
		})
	}
}

func TestTodo_AGENT_029_Security(t *testing.T) {
	def, targets := agentRunFixture()
	if _, err := schedule.Publish(schedule.NewRegistry(), def, targets); err != nil {
		t.Fatalf("valid exact allowlist: %v", err)
	}
	intentOnly := fixtureTargetsForAgent(t)
	if _, err := schedule.Publish(schedule.NewRegistry(), def, intentOnly); !errors.Is(err, schedule.ErrUnauthorizedTarget) {
		t.Fatalf("intent-only allowlist error = %v, want unauthorized target", err)
	}

	mutations := []struct {
		name   string
		change func(*schedule.AgentRunTarget)
	}{
		{name: "agent version", change: func(v *schedule.AgentRunTarget) { v.Agent.Version = "8" }},
		{name: "manifest digest", change: func(v *schedule.AgentRunTarget) {
			v.Agent.Digest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		}},
		{name: "sponsor", change: func(v *schedule.AgentRunTarget) { v.SponsorID = "service:other" }},
		{name: "purpose", change: func(v *schedule.AgentRunTarget) { v.Purpose = "other-purpose" }},
		{name: "budget", change: func(v *schedule.AgentRunTarget) { v.Budget.MaxCostMicros++ }},
		{name: "destination", change: func(v *schedule.AgentRunTarget) { v.Destination.AudienceID = "audience:other" }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			candidate := def
			copy := *def.AgentRun
			tc.change(&copy)
			candidate.AgentRun = &copy
			if tc.name == "purpose" {
				candidate.Purpose = copy.Purpose
			}
			if _, err := schedule.Publish(schedule.NewRegistry(), candidate, targets); !errors.Is(err, schedule.ErrUnauthorizedTarget) {
				t.Fatalf("Publish error = %v, want unauthorized target", err)
			}
		})
	}

	def.Source = schedule.TriggerSource{Kind: schedule.SourceEvent, Event: schedule.EventFilter{EventType: schedule.EventTriggerFired, SchemaVersion: "1"}}
	if _, err := schedule.Publish(schedule.NewRegistry(), def, targets); !errors.Is(err, schedule.ErrRecursion) {
		t.Fatalf("AGENT_RUN self-firing event error = %v, want recursion refusal", err)
	}
}

func fixtureTargetsForAgent(t *testing.T) []schedule.AuthorizedTarget {
	t.Helper()
	_, targets := fixture()
	return targets
}
