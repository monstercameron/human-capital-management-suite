package runstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

func TestTodo_AGENT_016(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 16, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	service, err := New(store, recheckerFunc(func(context.Context, string, string) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Start(ctx, acceptedAdmission(now))
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != run.AdmissionID || run.ID == "" || run.State != StateReady || len(run.Checkpoints) != 1 || run.Checkpoints[0].Phase != PhaseAdmission || run.Checkpoints[0].Digest != "sha256:"+run.RequestDigest || run.ContextDigest == "" {
		t.Fatalf("initial durable run = %+v", run)
	}
	claimed, err := service.Claim(ctx, run.ID, "worker-1", now, time.Minute)
	if err != nil || claimed.State != StateRunning || claimed.Lease == nil || claimed.Fence != 1 {
		t.Fatalf("Claim = %+v, %v", claimed, err)
	}
	checkpoint, err := service.Checkpoint(ctx, run.ID, "worker-1", claimed.Fence, claimed.Version, PhaseContext, 0, "ctx-1", testDigest("context"), now.Add(time.Second))
	if err != nil || len(checkpoint.Checkpoints) != 2 || checkpoint.Checkpoints[1].Phase != PhaseContext {
		t.Fatalf("context checkpoint = %+v, %v", checkpoint, err)
	}
	request, err := service.Checkpoint(ctx, run.ID, "worker-1", claimed.Fence, checkpoint.Version, PhaseModelCall, 1, "model-request-1", testDigest("request"), now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.Checkpoint(ctx, run.ID, "worker-1", claimed.Fence, request.Version, PhaseModelCall, 1, "model-response-1", testDigest("response"), now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	valid, err := service.Checkpoint(ctx, run.ID, "worker-1", claimed.Fence, response.Version, PhaseValidation, 1, "validated-output", testDigest("valid"), now.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	delivered, err := service.Checkpoint(ctx, run.ID, "worker-1", claimed.Fence, valid.Version, PhaseDelivery, 1, "delivery-receipt", testDigest("delivery"), now.Add(5*time.Second))
	if err != nil || delivered.State != StateCompleted || delivered.Lease != nil || delivered.TerminalCode != "DELIVERED" {
		t.Fatalf("delivery terminal = %+v, %v", delivered, err)
	}
	if _, err := service.Cancel(ctx, run.ID, delivered.Version, now.Add(6*time.Second)); !errors.Is(err, ErrTerminal) {
		t.Fatalf("cancel completed run = %v, want ErrTerminal", err)
	}
}

func TestTodo_AGENT_016_Fault(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 17, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	first, _ := New(store, recheckerFunc(func(context.Context, string, string) error { return nil }))
	run, err := first.Start(ctx, acceptedAdmission(now))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := first.Claim(ctx, run.ID, "worker-died", now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := first.BeginEffect(ctx, run.ID, "worker-died", "effect-1", "agent-run/effect-1", testDigest("args"), claimed.Fence, claimed.Version, now.Add(100*time.Millisecond))
	if err != nil || len(intent.Effects) != 1 || intent.Effects[0].Status != EffectUnknown {
		t.Fatalf("effect intent = %+v, %v", intent, err)
	}
	// A new process builds a new service over the same durable store.
	restarted, _ := New(store, recheckerFunc(func(context.Context, string, string) error { return nil }))
	recovered, err := restarted.Recover(ctx, run.ID, intent.Version, now.Add(2*time.Second))
	if err != nil || recovered.State != StateReconciling || recovered.Lease != nil || recovered.Effects[0].Status != EffectUnknown {
		t.Fatalf("restart recovery = %+v, %v", recovered, err)
	}
	if _, err := restarted.Claim(ctx, run.ID, "worker-2", now.Add(3*time.Second), time.Minute); !errors.Is(err, ErrInvalid) {
		t.Fatalf("claim before reconciling uncertain effect = %v", err)
	}
	reconciled, err := restarted.ReconcileEffect(ctx, run.ID, "effect-1", recovered.Version, EffectApplied, "owner-receipt", testDigest("effect-result"), now.Add(4*time.Second))
	if err != nil || reconciled.State != StateReady || reconciled.Effects[0].Status != EffectApplied {
		t.Fatalf("owner reconciliation = %+v, %v", reconciled, err)
	}
	claimedAgain, err := restarted.Claim(ctx, run.ID, "worker-2", now.Add(5*time.Second), time.Minute)
	if err != nil || claimedAgain.Fence <= claimed.Fence {
		t.Fatalf("fenced retry claim = %+v, %v", claimedAgain, err)
	}
}

func TestTodo_AGENT_016_Recovery(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 18, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	service, _ := New(store, recheckerFunc(func(context.Context, string, string) error { return nil }))
	run, err := service.Start(ctx, acceptedAdmission(now))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.Claim(ctx, run.ID, "worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := service.Cancel(ctx, run.ID, claimed.Version, now.Add(time.Second))
	if err != nil || cancelled.State != StateCancelled || cancelled.Lease != nil || cancelled.TerminalCode != "CANCELLED" {
		t.Fatalf("cancel running no-effect run = %+v, %v", cancelled, err)
	}
	if _, err := service.Claim(ctx, run.ID, "late-worker", now.Add(2*time.Second), time.Minute); !errors.Is(err, ErrInvalid) {
		t.Fatalf("claim cancelled run = %v", err)
	}
	expiring, err := service.Start(ctx, acceptedAdmissionWithKey(now, "expire"))
	if err != nil {
		t.Fatal(err)
	}
	expired, err := service.Expire(ctx, expiring.ID, expiring.Version, now.Add(3*time.Minute))
	if err != nil || expired.State != StateExpired || expired.TerminalCode != "EXPIRED" {
		t.Fatalf("expire run = %+v, %v", expired, err)
	}
}

func TestTodo_AGENT_016_Race(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 19, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	service, _ := New(store, recheckerFunc(func(context.Context, string, string) error { return nil }))
	run, err := service.Start(ctx, acceptedAdmission(now))
	if err != nil {
		t.Fatal(err)
	}
	const workers = 24
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	claimed := 0
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := service.Claim(ctx, run.ID, "worker-"+string(rune('a'+i)), now, time.Minute)
			if err == nil {
				mu.Lock()
				claimed++
				mu.Unlock()
			} else if !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrConflict) {
				t.Errorf("Claim error = %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if claimed != 1 {
		t.Fatalf("successful lease claims = %d, want one", claimed)
	}
}

func TestTodo_AGENT_016_Recheck(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 20, 0, 0, 0, time.UTC)
	denied := errors.New("grant revoked")
	allow := false
	store := NewMemoryStore()
	service, err := New(store, recheckerFunc(func(_ context.Context, _, admissionID string) error {
		if admissionID == "" {
			t.Fatal("recheck received an empty admission id")
		}
		if !allow {
			return denied
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Start(ctx, acceptedAdmission(now))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Claim(ctx, run.ID, "worker", now, time.Minute); !errors.Is(err, denied) {
		t.Fatalf("claim after grant revocation = %v", err)
	}
	stored, err := store.Get(ctx, run.ID)
	if err != nil || stored.State != StateReady || stored.Version != run.Version {
		t.Fatalf("denied claim mutated run: %+v, %v", stored, err)
	}
	allow = true
	claimed, err := service.Claim(ctx, run.ID, "worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	allow = false
	if _, err := service.Checkpoint(ctx, run.ID, "worker", claimed.Fence, claimed.Version, PhaseModelCall, 1, "request", testDigest("request"), now.Add(time.Second)); !errors.Is(err, denied) {
		t.Fatalf("model checkpoint after grant revocation = %v", err)
	}
	if _, err := service.BeginEffect(ctx, run.ID, "worker", "effect-1", "effect-key", testDigest("args"), claimed.Fence, claimed.Version, now.Add(time.Second)); !errors.Is(err, denied) {
		t.Fatalf("effect intent after grant revocation = %v", err)
	}
	stored, err = store.Get(ctx, run.ID)
	if err != nil || stored.Version != claimed.Version || len(stored.Effects) != 0 {
		t.Fatalf("denied effect mutated run: %+v, %v", stored, err)
	}
}

func acceptedAdmission(now time.Time) agentrun.Record { return acceptedAdmissionWithKey(now, "post-1") }

func acceptedAdmissionWithKey(now time.Time, key string) agentrun.Record {
	request := agentrun.Request{
		Source:      agentrun.SourceIdentity{TenantID: "tenant-a", Kind: agentrun.SourceChat, Key: key},
		LegalEntity: "entity-a", Agent: agentrun.VersionRef{AgentID: "agent-a", Version: "v1", Digest: testDigest("agent")},
		InstallationID: "install-a", Principal: agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: "agent-principal", InvokerID: "user-a", DelegatedCredentialRef: "credential-ref"},
		Purpose: "chat-assist", Audience: agentrun.AudienceScope{ID: "conversation-a", SnapshotID: "aud-1", Digest: testDigest("audience")},
		Context:  agentrun.ContextScope{ID: "thread-a", SnapshotID: "ctx-1", Digest: testDigest("context")},
		Deadline: now.Add(2 * time.Minute), Budget: agentrun.Budget{MaxCostMicros: 100, MaxInputTokens: 1000, MaxOutputTokens: 500}, CauseID: "cause-1",
	}
	snapshot := agentrun.AuthoritySnapshot{Agent: request.Agent, InstallationID: request.InstallationID, Principal: request.Principal,
		Audience: request.Audience, Context: request.Context, BudgetCeiling: request.Budget, GrantRef: "grant-1", PolicyDigest: testDigest("policy")}
	digest, _ := agentrun.AdmissionRequestDigest(request)
	id, _ := agentrun.AdmissionRequestID(request.Source)
	return agentrun.Record{ID: id, Request: request, RequestDigest: digest, Decision: agentrun.DecisionAccepted, Authority: snapshot, AdmittedAt: now}
}

func testDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

type recheckerFunc func(context.Context, string, string) error

func (f recheckerFunc) Recheck(ctx context.Context, tenantID, id string) error {
	return f(ctx, tenantID, id)
}

func TestTodo_AGENT_016_WaitKindValidation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 21, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	service, err := New(store, recheckerFunc(func(context.Context, string, string) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Start(ctx, acceptedAdmissionWithKey(now, "wait"))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.Claim(ctx, run.ID, "worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Park(ctx, run.ID, "worker", claimed.Fence, claimed.Version, WaitKind("UNKNOWN"), "signal-1", now.Add(time.Second)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Park with unknown wait kind = %v, want ErrInvalid", err)
	}
	waiting, err := service.Park(ctx, run.ID, "worker", claimed.Fence, claimed.Version, WaitSignal, "signal-1", now.Add(time.Second))
	if err != nil || waiting.State != StateWaiting || waiting.Lease != nil || waiting.WaitKind != WaitSignal {
		t.Fatalf("Park with valid wait kind = %+v, %v", waiting, err)
	}
	resumed, err := service.Resume(ctx, run.ID, waiting.Version, now.Add(2*time.Second))
	if err != nil || resumed.State != StateReady || resumed.WaitKind != "" || resumed.WaitRef != "" {
		t.Fatalf("Resume waiting run = %+v, %v", resumed, err)
	}
}

func TestTodo_AGENT_016_ResolveEffect(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status EffectStatus
		ref    string
		digest string
	}{
		{name: "applied", status: EffectApplied, ref: "owner-receipt", digest: testDigest("result")},
		{name: "not-applied", status: EffectNotApplied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
			store := NewMemoryStore()
			service, err := New(store, recheckerFunc(func(context.Context, string, string) error { return nil }))
			if err != nil {
				t.Fatal(err)
			}
			run, err := service.Start(ctx, acceptedAdmissionWithKey(now, tc.name))
			if err != nil {
				t.Fatal(err)
			}
			claimed, err := service.Claim(ctx, run.ID, "worker", now, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			intent, err := service.BeginEffect(ctx, run.ID, "worker", "effect-1", "key-1", testDigest("args"), claimed.Fence, claimed.Version, now.Add(time.Second))
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := service.ResolveEffect(ctx, run.ID, "worker", "effect-1", claimed.Fence, intent.Version, tc.status, tc.ref, tc.digest, now.Add(2*time.Second))
			if err != nil {
				t.Fatal(err)
			}
			if resolved.State != StateRunning || resolved.Lease == nil || resolved.Effects[0].Status != tc.status || resolved.Effects[0].ResultRef != tc.ref || resolved.Effects[0].ResultDigest != tc.digest || resolved.Effects[0].ResolvedAt.IsZero() {
				t.Fatalf("resolved effect state = %+v", resolved)
			}
			checkpointRef, checkpointDigest := tc.ref, tc.digest
			if tc.status == EffectNotApplied {
				checkpointRef, checkpointDigest = "effect-1", testDigest("args")
			}
			if got := resolved.Checkpoints[len(resolved.Checkpoints)-1]; got.Phase != PhaseToolCall || got.Attempt != 2 || got.Ref != checkpointRef || got.Digest != checkpointDigest {
				t.Fatalf("effect result checkpoint = %+v", got)
			}
		})
	}
}

func TestTodo_AGENT_016_Fail(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 11, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	service, err := New(store, recheckerFunc(func(context.Context, string, string) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Start(ctx, acceptedAdmissionWithKey(now, "failure"))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.Claim(ctx, run.ID, "worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Fail(ctx, run.ID, "worker", "provider unavailable", true, claimed.Fence, claimed.Version, now.Add(time.Second)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Fail with unsafe code = %v, want ErrInvalid", err)
	}
	unchanged, err := store.Get(ctx, run.ID)
	if err != nil || unchanged.Version != claimed.Version || unchanged.State != StateRunning {
		t.Fatalf("invalid failure mutated run: %+v, %v", unchanged, err)
	}
	failed, err := service.Fail(ctx, run.ID, "worker", "PROVIDER_UNAVAILABLE", true, claimed.Fence, claimed.Version, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != StateFailed || failed.Lease != nil || failed.TerminalCode != "PROVIDER_UNAVAILABLE" || !failed.FailureRequested || !failed.Retryable {
		t.Fatalf("typed failure = %+v", failed)
	}
}

func TestTodo_AGENT_016_RecoverAndExpire(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	service, err := New(store, recheckerFunc(func(context.Context, string, string) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Start(ctx, acceptedAdmissionWithKey(now, "recovered-no-effect"))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.Claim(ctx, run.ID, "worker", now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := service.Recover(ctx, run.ID, claimed.Version, now.Add(2*time.Second))
	if err != nil || recovered.State != StateReady || recovered.Lease != nil || recovered.Fence != claimed.Fence {
		t.Fatalf("expired lease recovery without effects = %+v, %v", recovered, err)
	}
	expiring, err := service.Start(ctx, acceptedAdmissionWithKey(now, "expire-running"))
	if err != nil {
		t.Fatal(err)
	}
	working, err := service.Claim(ctx, expiring.ID, "worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Expire(ctx, expiring.ID, working.Version, now.Add(time.Second)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("early expiry = %v, want ErrInvalid", err)
	}
	expired, err := service.Expire(ctx, expiring.ID, working.Version, expiring.Deadline)
	if err != nil || expired.State != StateExpired || expired.Lease != nil || expired.TerminalCode != "EXPIRED" || !expired.ExpireRequested {
		t.Fatalf("expiry with active lease = %+v, %v", expired, err)
	}
}

func TestTodo_AGENT_016_ResolveEffectRejectsUnsafeEvidence(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 13, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	service, err := New(store, recheckerFunc(func(context.Context, string, string) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Start(ctx, acceptedAdmissionWithKey(now, "resolve-validation"))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.Claim(ctx, run.ID, "worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := service.BeginEffect(ctx, run.ID, "worker", "effect-1", "key-1", testDigest("args"), claimed.Fence, claimed.Version, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		status EffectStatus
		ref    string
		digest string
	}{
		{name: "unknown status", status: EffectUnknown},
		{name: "applied without owner reference", status: EffectApplied, digest: testDigest("result")},
		{name: "applied without owner digest", status: EffectApplied, ref: "owner-receipt"},
		{name: "invalid optional reference", status: EffectNotApplied, ref: " owner-receipt"},
		{name: "invalid optional digest", status: EffectNotApplied, digest: " digest"},
		{name: "missing effect", status: EffectNotApplied, ref: "unused"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			effectID := "effect-1"
			if tc.name == "missing effect" {
				effectID = "missing"
			}
			_, err := service.ResolveEffect(ctx, run.ID, "worker", effectID, claimed.Fence, intent.Version, tc.status, tc.ref, tc.digest, now.Add(2*time.Second))
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("ResolveEffect error = %v, want ErrInvalid", err)
			}
			stored, err := store.Get(ctx, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Version != intent.Version || stored.Effects[0].Status != EffectUnknown || len(stored.Checkpoints) != len(intent.Checkpoints) {
				t.Fatalf("invalid observation mutated run: %+v", stored)
			}
		})
	}

	resolved, err := service.ResolveEffect(ctx, run.ID, "worker", "effect-1", claimed.Fence, intent.Version, EffectNotApplied, "", "", now.Add(2*time.Second))
	if err != nil || resolved.Effects[0].Status != EffectNotApplied {
		t.Fatalf("valid not-applied resolution = %+v, %v", resolved, err)
	}
	if _, err := service.ResolveEffect(ctx, run.ID, "worker", "effect-1", claimed.Fence, resolved.Version, EffectNotApplied, "", "", now.Add(3*time.Second)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("resolving an already resolved effect = %v, want ErrInvalid", err)
	}
}

func TestTodo_AGENT_016_FailWithUnknownEffectWaitsForRecovery(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	service, err := New(store, recheckerFunc(func(context.Context, string, string) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Start(ctx, acceptedAdmissionWithKey(now, "failure-recovery"))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.Claim(ctx, run.ID, "worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := service.BeginEffect(ctx, run.ID, "worker", "effect-1", "key-1", testDigest("args"), claimed.Fence, claimed.Version, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	failed, err := service.Fail(ctx, run.ID, "worker", "TOOL_FAILED", true, claimed.Fence, intent.Version, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != StateReconciling || failed.Lease != nil || !failed.FailureRequested || !failed.Retryable || failed.TerminalCode != "TOOL_FAILED" {
		t.Fatalf("ambiguous failure = %+v", failed)
	}
	reconciled, err := service.ReconcileEffect(ctx, run.ID, "effect-1", failed.Version, EffectNotApplied, "", "", now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.State != StateFailed || reconciled.TerminalCode != "TOOL_FAILED" || reconciled.Effects[0].Status != EffectNotApplied {
		t.Fatalf("reconciled failure = %+v", reconciled)
	}
}

func TestTodo_AGENT_016_RecoverExpiredLeaseTerminalizes(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	service, err := New(store, recheckerFunc(func(context.Context, string, string) error { return nil }))
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Start(ctx, acceptedAdmissionWithKey(now, "recover-expired"))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := service.Claim(ctx, run.ID, "worker", now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := service.Recover(ctx, run.ID, claimed.Version, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != StateExpired || recovered.TerminalCode != "EXPIRED" || !recovered.ExpireRequested || recovered.Lease != nil {
		t.Fatalf("expired lease recovery = %+v", recovered)
	}
}
