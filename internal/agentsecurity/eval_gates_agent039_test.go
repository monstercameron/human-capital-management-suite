package agentsecurity

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

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
