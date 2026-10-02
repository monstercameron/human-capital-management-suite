package agentsecurity

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func agent039RefusalCode(err error) RefusalCode {
	var refused *Refusal
	if errors.As(err, &refused) {
		return refused.Code
	}
	return ""
}

// TestTodo_AGENT_039_Conformance walks the contract of the two controls: an
// evaluation gates exactly the release it sealed, and a disable for any scope
// stops matching work, refuses new leases in that scope, leaves other work
// running and always reports the non-AI path as available.
func TestTodo_AGENT_039_Conformance(t *testing.T) {
	release := testRelease()
	release.AgentVersion, release.PersonaID, release.PersonaVersion, release.InstallationID = "agent-v3", "persona-7", "persona-v2", "install-a"

	t.Run("a failed fixture never publishes", func(t *testing.T) {
		for _, fail := range []func(*EvalFixture){
			func(f *EvalFixture) { f.SafetyPass = false },
			func(f *EvalFixture) { f.GroundingPass = false },
			func(f *EvalFixture) { f.ToolSelectionPass = false },
		} {
			fixtures := passingFixtures()
			fail(&fixtures[1])
			publisher := NewPublisher()
			run, err := NewEvaluator().Evaluate(release, fixtures)
			if err != nil || run.Passed {
				t.Fatalf("run=%+v err=%v", run, err)
			}
			if err := publisher.Publish(run); agent039RefusalCode(err) != RefusalCapability || publisher.Published(run.Digest) {
				t.Fatalf("failed evaluation publish error = %v", err)
			}
		}
		empty, err := NewEvaluator().Evaluate(release, nil)
		if err != nil || empty.Passed {
			t.Fatalf("an unevaluated release passed: %+v, %v", empty, err)
		}
	})

	t.Run("a changed release cannot reuse an earlier evaluation", func(t *testing.T) {
		changes := map[string]func(*Release){
			"provider model":  func(r *Release) { r.Model = "m2" },
			"model digest":    func(r *Release) { r.ModelDigest = "sha256:m2" },
			"tool":            func(r *Release) { r.Tool = "people.search" },
			"tool version":    func(r *Release) { r.ToolVersion++ },
			"prompt":          func(r *Release) { r.PromptHash = "sha256:p2" },
			"agent version":   func(r *Release) { r.AgentVersion = "agent-v4" },
			"persona version": func(r *Release) { r.PersonaVersion = "persona-v3" },
			"installation":    func(r *Release) { r.InstallationID = "install-b" },
		}
		for name, change := range changes {
			evaluator, publisher := NewEvaluator(), NewPublisher()
			sealed, err := evaluator.Evaluate(release, passingFixtures())
			if err != nil {
				t.Fatal(err)
			}
			swapped := sealed
			change(&swapped.Release)
			if err := publisher.Publish(swapped); agent039RefusalCode(err) != RefusalOutput || publisher.Published(sealed.Digest) {
				t.Fatalf("%s: the earlier seal published a changed release: %v", name, err)
			}
			fresh, err := evaluator.Evaluate(swapped.Release, passingFixtures())
			if err != nil || fresh.Digest == sealed.Digest {
				t.Fatalf("%s: a new evaluation did not produce its own seal: %+v, %v", name, fresh, err)
			}
			if err := publisher.Publish(fresh); err != nil {
				t.Fatalf("%s: the re-evaluated release was refused: %v", name, err)
			}
		}
	})

	t.Run("a disable fences its scope and nothing else", func(t *testing.T) {
		target := Lease{ID: "target", Agent: "assistant", AgentVersion: "v4", PersonaID: "coach", PersonaVersion: "v2", InstallationID: "install-a", Model: "m1", Tool: "people.lookup", Tenant: "tenant-a", WriteCapable: true}
		scopes := map[string]struct {
			scope   DisableScope
			outside func(*Lease)
		}{
			"agent version":   {DisableScope{Agent: "assistant", AgentVersion: "v4"}, func(l *Lease) { l.AgentVersion = "v5" }},
			"persona version": {DisableScope{PersonaID: "coach", PersonaVersion: "v2"}, func(l *Lease) { l.PersonaVersion = "v3" }},
			"installation":    {DisableScope{InstallationID: "install-a"}, func(l *Lease) { l.InstallationID = "install-b" }},
			"model":           {DisableScope{Model: "m1"}, func(l *Lease) { l.Model = "m2" }},
			"tool":            {DisableScope{Tool: "people.lookup"}, func(l *Lease) { l.Tool = "people.search" }},
			"tenant":          {DisableScope{Tenant: "tenant-a"}, func(l *Lease) { l.Tenant = "tenant-b" }},
		}
		for name, tc := range scopes {
			switchBoard := NewKillSwitch()
			outside := target
			outside.ID = "outside"
			tc.outside(&outside)
			for _, lease := range []Lease{target, outside} {
				if err := switchBoard.Grant(lease); err != nil {
					t.Fatal(err)
				}
			}
			revoked, fallback := switchBoard.Disable(tc.scope)
			if revoked != 1 || !fallback.NonAIAvailable {
				t.Fatalf("%s: revoked=%d fallback=%+v", name, revoked, fallback)
			}
			ran := false
			stopped, err := switchBoard.RunStep("target", func() error { ran = true; return nil })
			if agent039RefusalCode(err) != RefusalEffectClass || ran || !stopped.NonAIAvailable {
				t.Fatalf("%s: a fenced lease started a step: ran=%t fallback=%+v err=%v", name, ran, stopped, err)
			}
			if checked, err := switchBoard.Check("target"); agent039RefusalCode(err) != RefusalEffectClass || !checked.NonAIAvailable {
				t.Fatalf("%s: a fenced lease was admitted: %+v, %v", name, checked, err)
			}
			again := target
			again.ID = "target-again"
			if err := switchBoard.Grant(again); agent039RefusalCode(err) != RefusalEffectClass {
				t.Fatalf("%s: a new lease was granted in the disabled scope: %v", name, err)
			}
			if _, err := switchBoard.RunStep("outside", func() error { ran = true; return nil }); err != nil || !ran {
				t.Fatalf("%s: the disable crossed its scope: ran=%t err=%v", name, ran, err)
			}
		}
		// An empty scope selects nothing; the explicit all-AI scope selects everything.
		switchBoard := NewKillSwitch()
		if err := switchBoard.Grant(target); err != nil {
			t.Fatal(err)
		}
		if revoked, _ := switchBoard.Disable(DisableScope{}); revoked != 0 {
			t.Fatalf("an empty scope revoked %d leases", revoked)
		}
		if revoked, fallback := switchBoard.Disable(DisableScope{AllAI: true}); revoked != 1 || !fallback.NonAIAvailable {
			t.Fatalf("all-AI disable revoked=%d fallback=%+v", revoked, fallback)
		}
	})
}

// TestTodo_AGENT_039_Golden pins the bytes of the two evidence seals: the
// evaluation that gates a release and the incident recorded when a control is
// used. A change to what either seal binds changes these digests.
func TestTodo_AGENT_039_Golden(t *testing.T) {
	release := Release{Agent: "assistant", AgentBuild: "b7", AgentVersion: "agent-v3", PersonaID: "persona-7", PersonaVersion: "persona-v2", InstallationID: "install-a", Model: "m1", ModelDigest: "sha256:m", Tool: "people.lookup", ToolVersion: 3, Prompt: "triage", PromptHash: "sha256:p"}
	run, err := NewEvaluator().Evaluate(release, passingFixtures())
	if err != nil {
		t.Fatal(err)
	}
	const wantRun = "sha256:c227fcd5d779e2c832b56ead3d76b78d63be2659563c8c3e878464c13b6eac0d"
	if !run.Passed || run.Sequence != 1 || run.Digest != wantRun {
		t.Fatalf("evaluation seal = %s (passed=%t sequence=%d), want %s", run.Digest, run.Passed, run.Sequence, wantRun)
	}
	attempts := []string{"people.lookup/v3:refused", "kill-switch:install-a"}
	incident, err := RecordIncident(release, "sha256:in", "sha256:out", attempts)
	if err != nil {
		t.Fatal(err)
	}
	const wantIncident = "sha256:f63bab774ff834f97d04d9a0ae3aeafbe0d48844aa59a5a98c3792ba911a158e"
	if incident.IncidentDigest != wantIncident {
		t.Fatalf("incident seal = %s, want %s", incident.IncidentDigest, wantIncident)
	}
	// The seals bind the exact version: the same material for the next persona
	// version is different evidence.
	next := release
	next.PersonaVersion = "persona-v3"
	nextRun, err := NewEvaluator().Evaluate(next, passingFixtures())
	if err != nil {
		t.Fatal(err)
	}
	nextIncident, err := RecordIncident(next, "sha256:in", "sha256:out", attempts)
	if err != nil || nextRun.Digest == run.Digest || nextIncident.IncidentDigest == incident.IncidentDigest {
		t.Fatalf("a version change kept the same evidence: run=%s incident=%s err=%v", nextRun.Digest, nextIncident.IncidentDigest, err)
	}
}

func TestTodo_AGENT_039(t *testing.T) {
	evaluator := NewEvaluator()
	publisher := NewPublisher()
	release := testRelease()
	release.AgentVersion = "agent-v3"
	release.PersonaID = "persona-7"
	release.PersonaVersion = "persona-v2"
	release.InstallationID = "install-a"
	run, err := evaluator.Evaluate(release, passingFixtures())
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(run); err != nil {
		t.Fatalf("publish evaluated release: %v", err)
	}
	changed := release
	changed.PersonaVersion = "persona-v3"
	changedRun, err := evaluator.Evaluate(changed, passingFixtures())
	if err != nil {
		t.Fatal(err)
	}
	if changedRun.Digest == run.Digest {
		t.Fatal("new persona version reused the previous evaluation seal")
	}
	if err := publisher.Publish(changedRun); err != nil {
		t.Fatalf("publish independently evaluated persona version: %v", err)
	}
}

func TestTodo_AGENT_039_Security(t *testing.T) {
	switchBoard := NewKillSwitch()
	leases := []Lease{
		{ID: "target", Agent: "assistant", AgentVersion: "v4", PersonaID: "coach", PersonaVersion: "v2", InstallationID: "install-a", Tenant: "tenant-a", WriteCapable: true},
		{ID: "sibling-install", Agent: "assistant", AgentVersion: "v4", PersonaID: "coach", PersonaVersion: "v2", InstallationID: "install-b", Tenant: "tenant-a", WriteCapable: true},
		{ID: "sibling-version", Agent: "assistant", AgentVersion: "v4", PersonaID: "coach", PersonaVersion: "v3", InstallationID: "install-a", Tenant: "tenant-a", WriteCapable: true},
		{ID: "sibling-persona", Agent: "assistant", AgentVersion: "v4", PersonaID: "analyst", PersonaVersion: "v2", InstallationID: "install-a", Tenant: "tenant-a", WriteCapable: true},
	}
	for _, lease := range leases {
		if err := switchBoard.Grant(lease); err != nil {
			t.Fatal(err)
		}
	}
	revoked, fallback := switchBoard.Disable(DisableScope{
		Agent: "assistant", AgentVersion: "v4", PersonaID: "coach",
		PersonaVersion: "v2", InstallationID: "install-a", Tenant: "tenant-a",
	})
	if revoked != 1 || !fallback.NonAIAvailable {
		t.Fatalf("revoked=%d fallback=%+v", revoked, fallback)
	}
	if _, err := switchBoard.Invoke("target"); err == nil {
		t.Fatal("write-capable lease invoked after its exact installation was disabled")
	}
	for _, id := range []string{"sibling-install", "sibling-version", "sibling-persona"} {
		if _, err := switchBoard.Invoke(id); err != nil {
			t.Errorf("disable crossed scope into %s: %v", id, err)
		}
	}
}

func TestTodo_AGENT_039_Race(t *testing.T) {
	switchBoard := NewKillSwitch()
	for i := 0; i < 64; i++ {
		if err := switchBoard.Grant(Lease{ID: fmt.Sprintf("write-%d", i), Agent: "assistant", AgentVersion: "v4", InstallationID: "install-a", WriteCapable: true}); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	var disabled atomic.Bool
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		revoked, _ := switchBoard.Disable(DisableScope{Agent: "assistant", AgentVersion: "v4", InstallationID: "install-a"})
		if revoked != 64 {
			t.Errorf("revoked=%d, want all 64 scoped leases", revoked)
		}
		disabled.Store(true)
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 64; i++ {
			_, err := switchBoard.Invoke(fmt.Sprintf("write-%d", i))
			if err == nil && disabled.Load() {
				t.Errorf("lease %d started after disable returned", i)
			}
		}
	}()
	close(start)
	wg.Wait()
	for i := 0; i < 64; i++ {
		if _, err := switchBoard.Invoke(fmt.Sprintf("write-%d", i)); err == nil {
			t.Errorf("lease %d remained usable after disable", i)
		}
	}
}
