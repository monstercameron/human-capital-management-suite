package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/resources"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// The 2026-10-01 review cell fixed the mention path by hand at twenty-three
// gates; most fixes earned a regression test in the AGENTUX_SPEED_R and
// AGENTUX_PATH_T series. The three below had none: a mention run that held no
// resource lease, a re-issued security lease compared by issue time, and a
// delivery checkpoint that rechecked the audience its own reply had changed.

// TestTodo_AGENTUX_025_MentionRunAcquiresResourceLease pins the G10 fix: a
// model call made by a mention run holds no worker lease in its context, so
// the served resources port acquires one from the request's identity on the
// interactive lane. A task step that already holds a lease keeps it, and a
// lease that cannot be acquired fails the call instead of running unleashed.
func TestTodo_AGENTUX_025_MentionRunAcquiresResourceLease(t *testing.T) {
	var mu sync.Mutex
	var observed []AgentResourceObservation
	runtime, err := NewAgentResourceRuntime(AgentResourceRuntimeConfig{
		Policy: agentResourceTestPolicy(),
		ResolveIdentity: func(context.Context, string, string) (AgentResourceIdentity, error) {
			t.Error("mention lease acquisition consulted durable identity resolution")
			return AgentResourceIdentity{}, errors.New("must not resolve")
		},
		ObserveAdmission: func(o AgentResourceObservation) {
			mu.Lock()
			defer mu.Unlock()
			observed = append(observed, o)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ports := personaRunModelResources{runtime: runtime}
	request := AgentModelResourceRequest{TenantID: "a", UserID: "user", TaskID: "task", ProviderID: "openai", ModelProfileID: "profile"}
	drain := func() []AgentResourceObservation {
		mu.Lock()
		defer mu.Unlock()
		got := observed
		observed = nil
		return got
	}

	release, err := ports.AcquireAgentModelResources(context.Background(), request)
	if err != nil || release == nil {
		t.Fatalf("mention run acquired no lease: release=%v err=%v", release != nil, err)
	}
	got := drain()
	if len(got) != 1 || got[0].Outcome != "ADMITTED" || got[0].ProviderID != "openai" ||
		got[0].Identity != (AgentResourceIdentity{TenantID: "a", UserID: "user", TaskID: "task", Lane: resources.LaneInteractive}) {
		t.Fatalf("mention acquisition observations=%+v", got)
	}
	release()
	release()

	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if release, err := ports.AcquireAgentModelResources(cancelled, request); err == nil || release != nil {
		t.Fatalf("cancelled acquisition ran unleashed: release=%v err=%v", release != nil, err)
	}

	parent, err := runtime.Acquire(context.Background(), AgentResourceIdentity{TenantID: "a", UserID: "user", TaskID: "task", Lane: resources.LaneInteractive}, "openai")
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Release()
	drain()
	if release, err := ports.AcquireAgentModelResources(parent.Context(context.Background()), request); err != nil || release == nil {
		t.Fatalf("task step with a lease was not admitted: release=%v err=%v", release != nil, err)
	} else {
		release()
	}

	if _, err := (personaRunModelResources{}).AcquireAgentModelResources(context.Background(), request); !errors.Is(err, ErrAgentResourceIdentity) {
		t.Fatalf("nil resources err=%v", err)
	}
	withoutProvider := request
	withoutProvider.ProviderID = ""
	if _, err := ports.AcquireAgentModelResources(context.Background(), withoutProvider); !errors.Is(err, ErrAgentResourceIdentity) {
		t.Fatalf("providerless request err=%v", err)
	}
}

// TestTodo_AGENTUX_025_ReissuedLeaseMatchesByBindings pins the G12 fix: a
// security lease re-issued for the same admission matches by its bindings,
// not by its issue time. Rotated bindings and an inverted validity window
// still refuse.
func TestTodo_AGENTUX_025_ReissuedLeaseMatchesByBindings(t *testing.T) {
	at := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	admission, run := personaRunSecurityEvidenceFixture()
	reissued := personaRunSecurityLeaseFixture(tenantID, at)
	reissued.IssuedAt = at
	repository := &personaRunSecurityLeaseRepositoryFake{lease: reissued}
	resolver, err := NewDatabasePersonaRunSecurityLeaseResolver(PersonaRunSecurityLeaseResolverConfig{
		Leases: repository, TenantUUID: func(values.TenantId) uuid.UUID { return tenantID }, Now: func() time.Time { return at },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := resolver.ResolvePersonaRunSecurityLease(context.Background(), admission, run); err != nil || got != agentsecurity.KillSwitchLeaseID("lease-a") {
		t.Fatalf("re-issued lease resolved=%q err=%v", got, err)
	}

	rotated := reissued
	rotated.InstallationID = "install-b"
	repository.lease = rotated
	if _, err := resolver.ResolvePersonaRunSecurityLease(context.Background(), admission, run); !errors.Is(err, errPersonaRunSecurityLeaseResolver) {
		t.Fatalf("rotated bindings err=%v", err)
	}

	inverted := reissued
	inverted.ExpiresAt = inverted.IssuedAt
	repository.lease = inverted
	if _, err := resolver.ResolvePersonaRunSecurityLease(context.Background(), admission, run); !errors.Is(err, errPersonaRunSecurityLeaseResolver) {
		t.Fatalf("inverted validity window err=%v", err)
	}
}

type agentux025AdmissionReaderFake struct {
	record agentrun.Record
	calls  int
}

func (f *agentux025AdmissionReaderFake) GetByID(_ context.Context, _ string) (agentrun.Record, error) {
	f.calls++
	return f.record, nil
}

type agentux025VerifyAuthorityFake struct {
	snapshot agentrun.AuthoritySnapshot
	calls    int
}

func (f *agentux025VerifyAuthorityFake) VerifyAdmission(_ context.Context, _ agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	f.calls++
	return f.snapshot, nil
}

// TestTodo_AGENTUX_025_RecheckSkipsDeliveredReply pins the G23 fix: the final
// checkpoint of a delivered reply does not re-verify the audience the reply
// itself changed, while the record binding still holds and an undelivered
// checkpoint still refuses on changed authority.
func TestTodo_AGENTUX_025_RecheckSkipsDeliveredReply(t *testing.T) {
	admission, _ := personaRunSecurityEvidenceFixture()
	afterReply := admission.Authority
	afterReply.Audience.Digest = "audience-after-reply"
	afterReply.Context.SnapshotID = "context-after-reply"
	reader := &agentux025AdmissionReaderFake{record: admission}
	authority := &agentux025VerifyAuthorityFake{snapshot: afterReply}
	rechecker, err := NewPersonaRunAdmissionRechecker("tenant-a", reader, authority)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := rechecker.Recheck(withPersonaRunReplyDelivered(ctx, admission.ID), "tenant-a", admission.ID); err != nil {
		t.Fatalf("delivered checkpoint rechecked the changed audience: %v", err)
	}
	if authority.calls != 0 {
		t.Fatalf("delivered checkpoint consulted current authority %d times", authority.calls)
	}
	if err := rechecker.Recheck(ctx, "tenant-a", admission.ID); err == nil || !strings.Contains(err.Error(), "authority changed since admission") {
		t.Fatalf("undelivered checkpoint err=%v", err)
	}
	if authority.calls != 1 {
		t.Fatalf("undelivered checkpoint consulted current authority %d times", authority.calls)
	}
	if err := rechecker.Recheck(withPersonaRunReplyDelivered(ctx, admission.ID), "tenant-a", "admission-foreign"); err == nil {
		t.Fatal("delivered checkpoint skipped the record binding")
	}
}
