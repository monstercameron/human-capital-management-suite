package agentsecurity

import (
	"sync/atomic"
	"testing"
)

func TestTodo_AGENTP_016_Race(t *testing.T) {
	switchBoard := NewKillSwitch()
	if err := switchBoard.Grant(Lease{
		ID: "persona-run", Agent: "assistant", AgentVersion: "a2",
		PersonaID: "persona-7", PersonaVersion: "p3", InstallationID: "room-4",
		Tenant: "tenant-1",
	}); err != nil {
		t.Fatal(err)
	}

	stepEntered := make(chan struct{})
	finishStep := make(chan struct{})
	stepDone := make(chan error, 1)
	go func() {
		_, err := switchBoard.RunStep("persona-run", func() error {
			close(stepEntered)
			<-finishStep
			return nil
		})
		stepDone <- err
	}()
	<-stepEntered

	disableStarted := make(chan struct{})
	disableDone := make(chan int, 1)
	go func() {
		close(disableStarted)
		revoked, _ := switchBoard.Disable(DisableScope{PersonaID: "persona-7", InstallationID: "room-4", Tenant: "tenant-1"})
		disableDone <- revoked
	}()
	<-disableStarted
	select {
	case <-disableDone:
		t.Fatal("Disable committed while an admitted step was still running")
	default:
	}
	close(finishStep)
	if err := <-stepDone; err != nil {
		t.Fatalf("in-progress step: %v", err)
	}
	if revoked := <-disableDone; revoked != 1 {
		t.Fatalf("revoked=%d, want 1", revoked)
	}

	var starts atomic.Int32
	_, err := switchBoard.RunStep("persona-run", func() error {
		starts.Add(1)
		return nil
	})
	if err == nil {
		t.Fatal("step after committed suspend was admitted")
	}
	if got := starts.Load(); got != 0 {
		t.Fatalf("post-suspend step started %d times", got)
	}
}

func TestTodo_AGENTP_016_Security(t *testing.T) {
	switchBoard := NewKillSwitch()
	if err := switchBoard.Grant(Lease{ID: "scoped", PersonaID: "p1", InstallationID: "room-a", Tenant: "tenant-a"}); err != nil {
		t.Fatal(err)
	}
	if err := switchBoard.Grant(Lease{ID: "other-room", PersonaID: "p1", InstallationID: "room-b", Tenant: "tenant-a"}); err != nil {
		t.Fatal(err)
	}
	if err := switchBoard.Grant(Lease{ID: "other-tenant", PersonaID: "p1", InstallationID: "room-a", Tenant: "tenant-b"}); err != nil {
		t.Fatal(err)
	}
	if revoked, _ := switchBoard.Disable(DisableScope{PersonaID: "p1", InstallationID: "room-a", Tenant: "tenant-a"}); revoked != 1 {
		t.Fatalf("revoked=%d, want only the matching persona installation", revoked)
	}
	for _, id := range []string{"other-room", "other-tenant"} {
		if _, err := switchBoard.RunStep(id, func() error { return nil }); err != nil {
			t.Fatalf("scope disabled %q: %v", id, err)
		}
	}
}

func TestTodo_AGENTP_016_Security_DisabledScopesFenceNewLeases(t *testing.T) {
	for _, scope := range []DisableScope{
		{PersonaID: "p1", InstallationID: "room-a", Tenant: "tenant-a"},
		{AllAI: true},
	} {
		switchBoard := NewKillSwitch()
		if revoked, _ := switchBoard.Disable(scope); revoked != 0 {
			t.Fatalf("empty switch revoked %d leases", revoked)
		}
		if err := switchBoard.Grant(Lease{ID: "new", PersonaID: "p1", InstallationID: "room-a", Tenant: "tenant-a"}); err == nil {
			t.Fatal("new lease bypassed a committed disable")
		}
		if _, err := switchBoard.RunStep("new", func() error { t.Fatal("disabled scope began work"); return nil }); err == nil {
			t.Fatal("rejected new lease was installed")
		}
		if !scope.AllAI {
			if err := switchBoard.Grant(Lease{ID: "foreign", PersonaID: "p1", InstallationID: "room-a", Tenant: "tenant-b"}); err != nil {
				t.Fatalf("disable crossed tenants: %v", err)
			}
		}
	}
	// An empty selector has no authority to disable any future lease.
	switchBoard := NewKillSwitch()
	switchBoard.Disable(DisableScope{})
	if err := switchBoard.Grant(Lease{ID: "allowed"}); err != nil {
		t.Fatalf("empty scope disabled future work: %v", err)
	}
}

func TestTodo_AGENTP_016_Race_NewGrantsAfterDisable(t *testing.T) {
	switchBoard := NewKillSwitch()
	disabled := make(chan struct{})
	done := make(chan struct{})
	go func() {
		switchBoard.Disable(DisableScope{Tenant: "tenant-a"})
		close(disabled)
	}()
	go func() {
		defer close(done)
		<-disabled
		if err := switchBoard.Grant(Lease{ID: "late", Tenant: "tenant-a"}); err == nil {
			t.Error("grant after completed disable succeeded")
		}
	}()
	<-done
}
