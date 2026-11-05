package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/authority"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

var rev059Now = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

// rev059Amendment binds one transferred write: worker.title via UPDATE for
// acme-corp under grant-p1b-1, effective across rev059Now.
func rev059Amendment(t *testing.T) authority.Amendment {
	t.Helper()
	bound, err := authority.Bind(authority.Amendment{
		AmendmentID: "amd-rev059-1",
		Tenant:      values.TenantId("acme-corp"),
		Topology:    authority.TopologyTransferredAuthority,
		Fields:      []string{"worker.title"},
		Operations:  []string{"UPDATE"},
		From:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Until:       time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		GrantRef:    "grant-p1b-1",
	})
	if err != nil {
		t.Fatalf("bind amendment: %v", err)
	}
	return bound
}

func rev059Write(field string, op intent.WriteOperation) intent.PlannedWrite {
	return intent.PlannedWrite{
		Subject:                 intent.SubjectReference{SubjectID: "worker-1"},
		FieldPath:               field,
		CurrentCanonicalText:    "Engineer",
		ProposedCanonicalText:   "Senior Engineer",
		SourceAuthorityDecision: "authority:transfer/amd-rev059-1",
		Operation:               op,
	}
}

func rev059Start(tenant values.TenantId, writes ...intent.PlannedWrite) runtime.StartRequest {
	return runtime.StartRequest{
		TenantID:       uuid.New(),
		CellID:         "cell-local",
		Proposal:       runtime.ProposalBinding{Revision: intent.ProposalRevision{ProposalRevisionID: "rev-1", IntentID: "intent-1", Revision: 1, Tenant: tenant, Writes: writes}},
		ExpectedTenant: tenant,
	}
}

// rev059RecordingExecutor is a hermetic ProposalExecutor: it records every
// call and answers with a canned result, so the tests prove admitted
// starts reach the driver exactly once and refused starts never do.
type rev059RecordingExecutor struct {
	mu           sync.Mutex
	executes     []runtime.StartRequest
	resumes      []ExecutionResumeRequest
	timerResumes []ExecutionTimerResumeRequest
	result       ExecutionResult
	err          error
}

func (f *rev059RecordingExecutor) Execute(_ context.Context, start runtime.StartRequest) (ExecutionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executes = append(f.executes, start)
	return f.result, f.err
}

func (f *rev059RecordingExecutor) Resume(_ context.Context, req ExecutionResumeRequest) (ExecutionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumes = append(f.resumes, req)
	return f.result, f.err
}

func (f *rev059RecordingExecutor) ResumeTimer(_ context.Context, req ExecutionTimerResumeRequest) (ExecutionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.timerResumes = append(f.timerResumes, req)
	return f.result, f.err
}

func (f *rev059RecordingExecutor) counts() (executes, resumes, timerResumes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.executes), len(f.resumes), len(f.timerResumes)
}

func rev059CannedResult() ExecutionResult {
	return ExecutionResult{Status: ExecutionResultComplete, InstanceID: "instance-1", InstanceVersion: 3, VisitedNodes: []string{"END"}}
}

// rev059Service composes a real IntentService the way newLifecycleHarness
// does, plus a recording executor and an optional bound amendment.
func rev059Service(t *testing.T, amendment *authority.Amendment, grant string) (*IntentService, *rev059RecordingExecutor) {
	t.Helper()
	reg := lifecycleHarnessRegistry(t)
	caps := capability.NewRegistry()
	sink := NewMemoryEvidenceSink()
	gateway := capability.NewGateway(caps, sink)
	digester, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatalf("NewDefaultDigester: %v", err)
	}
	recorder := &rev059RecordingExecutor{result: rev059CannedResult()}
	svc, err := NewIntentService(Options{
		Definitions:        reg,
		Capabilities:       caps,
		Gateway:            gateway,
		Store:              newMemLifecycleStore(),
		Inputs:             noLifecycleInputs{},
		Digester:           digester,
		Controls:           NewControls(reg, caps),
		Clock:              func() values.Instant { return values.NewInstant(rev059Now) },
		Evidence:           sink,
		ProposalExecutor:   recorder,
		ExecutionAmendment: amendment,
		ExecutionGrant:     grant,
	})
	if err != nil {
		t.Fatalf("NewIntentService: %v", err)
	}
	return svc, recorder
}

// TestTodo_REV_059_01 is the REV-059-01 primary: the P1B command path's own
// executor seam admits an in-scope write to the driver exactly once and
// refuses an out-of-scope field or operation with the amendment's own
// error before the driver runs, so the write never lands.
func TestTodo_REV_059_01(t *testing.T) {
	amendment := rev059Amendment(t)
	tenant := values.TenantId("acme-corp")
	ctx := context.Background()

	t.Run("an in-scope write reaches the driver exactly once", func(t *testing.T) {
		inner := &rev059RecordingExecutor{result: rev059CannedResult()}
		gated := NewAmendmentGatedExecutor(inner, &amendment, "grant-p1b-1", func() time.Time { return rev059Now })
		start := rev059Start(tenant, rev059Write("worker.title", intent.WriteOperationUpdate))
		got, err := gated.Execute(ctx, start)
		if err != nil {
			t.Fatalf("admitted execute: %v", err)
		}
		if want := rev059CannedResult(); got.Status != want.Status || got.InstanceID != want.InstanceID || got.InstanceVersion != want.InstanceVersion {
			t.Fatalf("result = %+v, want the inner result passed through", got)
		}
		if executes, _, _ := inner.counts(); executes != 1 {
			t.Fatalf("driver received %d starts, want exactly 1", executes)
		}
		if inner.executes[0].Proposal.Revision.IntentID != "intent-1" {
			t.Fatalf("driver received a rewritten start: %+v", inner.executes[0])
		}
	})

	t.Run("an out-of-scope field is refused with the amendment's own error and never lands", func(t *testing.T) {
		inner := &rev059RecordingExecutor{result: rev059CannedResult()}
		gated := NewAmendmentGatedExecutor(inner, &amendment, "grant-p1b-1", func() time.Time { return rev059Now })
		_, err := gated.Execute(ctx, rev059Start(tenant, rev059Write("worker.ssn", intent.WriteOperationUpdate)))
		if !errors.Is(err, authority.ErrTopologyMismatch) {
			t.Fatalf("execute err = %v, want the amendment topology refusal", err)
		}
		if executes, _, _ := inner.counts(); executes != 0 {
			t.Fatalf("refused start reached the driver %d times, want zero", executes)
		}
	})

	t.Run("an out-of-scope operation is refused before the driver runs", func(t *testing.T) {
		inner := &rev059RecordingExecutor{result: rev059CannedResult()}
		gated := NewAmendmentGatedExecutor(inner, &amendment, "grant-p1b-1", func() time.Time { return rev059Now })
		_, err := gated.Execute(ctx, rev059Start(tenant, rev059Write("worker.title", intent.WriteOperationDelete)))
		if !errors.Is(err, authority.ErrTopologyMismatch) {
			t.Fatalf("execute err = %v, want the amendment topology refusal", err)
		}
		if executes, _, _ := inner.counts(); executes != 0 {
			t.Fatalf("refused start reached the driver %d times, want zero", executes)
		}
	})

	t.Run("one out-of-scope write among in-scope writes refuses the whole start", func(t *testing.T) {
		inner := &rev059RecordingExecutor{result: rev059CannedResult()}
		gated := NewAmendmentGatedExecutor(inner, &amendment, "grant-p1b-1", func() time.Time { return rev059Now })
		_, err := gated.Execute(ctx, rev059Start(tenant,
			rev059Write("worker.title", intent.WriteOperationUpdate),
			rev059Write("worker.ssn", intent.WriteOperationUpdate),
		))
		if !errors.Is(err, authority.ErrTopologyMismatch) {
			t.Fatalf("execute err = %v, want the amendment topology refusal", err)
		}
		if executes, _, _ := inner.counts(); executes != 0 {
			t.Fatalf("refused start reached the driver %d times, want zero", executes)
		}
	})

	t.Run("resumes pass the same gate as starts", func(t *testing.T) {
		inner := &rev059RecordingExecutor{result: rev059CannedResult()}
		gated := NewAmendmentGatedExecutor(inner, &amendment, "grant-p1b-1", func() time.Time { return rev059Now })
		_, err := gated.Resume(ctx, ExecutionResumeRequest{Start: rev059Start(tenant, rev059Write("worker.ssn", intent.WriteOperationUpdate))})
		if !errors.Is(err, authority.ErrTopologyMismatch) {
			t.Fatalf("resume err = %v, want the amendment topology refusal", err)
		}
		_, err = gated.ResumeTimer(ctx, ExecutionTimerResumeRequest{Start: rev059Start(tenant, rev059Write("worker.title", intent.WriteOperationDelete))})
		if !errors.Is(err, authority.ErrTopologyMismatch) {
			t.Fatalf("timer resume err = %v, want the amendment topology refusal", err)
		}
		if _, resumes, timerResumes := inner.counts(); resumes != 0 || timerResumes != 0 {
			t.Fatalf("refused resumes reached the driver (%d, %d), want zero", resumes, timerResumes)
		}
		if _, err := gated.Resume(ctx, ExecutionResumeRequest{Start: rev059Start(tenant, rev059Write("worker.title", intent.WriteOperationUpdate))}); err != nil {
			t.Fatalf("admitted resume: %v", err)
		}
		if _, resumes, _ := inner.counts(); resumes != 1 {
			t.Fatalf("driver received %d resumes, want exactly 1", resumes)
		}
	})

	t.Run("no bound amendment leaves the executor unwrapped", func(t *testing.T) {
		inner := &rev059RecordingExecutor{result: rev059CannedResult()}
		if got := NewAmendmentGatedExecutor(inner, nil, "", func() time.Time { return rev059Now }); got != inner {
			t.Fatalf("unamended constructor wrapped the executor: %T", got)
		}
		svc, _ := rev059Service(t, nil, "")
		if _, wrapped := svc.executor.(*AmendmentGatedExecutor); wrapped {
			t.Fatal("a service composed with no amendment wraps its executor")
		}
	})

	t.Run("a composed service gates its own executor seam", func(t *testing.T) {
		svc, recorder := rev059Service(t, &amendment, "grant-p1b-1")
		if _, wrapped := svc.executor.(*AmendmentGatedExecutor); !wrapped {
			t.Fatalf("executor = %T, want the amendment gate in the service path", svc.executor)
		}
		_, err := svc.executor.Execute(ctx, rev059Start(tenant, rev059Write("worker.ssn", intent.WriteOperationUpdate)))
		if !errors.Is(err, authority.ErrTopologyMismatch) {
			t.Fatalf("service execute err = %v, want the amendment topology refusal", err)
		}
		if executes, _, _ := recorder.counts(); executes != 0 {
			t.Fatalf("refused start reached the driver %d times, want zero", executes)
		}
		if _, err := svc.executor.Execute(ctx, rev059Start(tenant, rev059Write("worker.title", intent.WriteOperationUpdate))); err != nil {
			t.Fatalf("admitted service execute: %v", err)
		}
		if executes, _, _ := recorder.counts(); executes != 1 {
			t.Fatalf("driver received %d starts, want exactly 1", executes)
		}
	})

	t.Run("a nil executor stays nil through the constructor", func(t *testing.T) {
		if got := NewAmendmentGatedExecutor(nil, &amendment, "grant-p1b-1", func() time.Time { return rev059Now }); got != nil {
			t.Fatalf("constructor over a nil executor returned %T, want nil", got)
		}
	})
}

// rev059RenderDenial renders one amendment refusal's owned projection as
// stable text: every field here must be deterministic across runs.
func rev059RenderDenial(err error) string {
	owned := executionError(err)
	return fmt.Sprintf("code: %s\nreason: %s\nmessage: %s\nretryable: %t\nhttp_status: %d\ngrpc_code: %s\nerror: %s\n",
		owned.Code(), owned.ReasonRef(), owned.Message(), owned.Retryable(),
		owned.HTTPStatus(), owned.GRPCStatus().Code(), owned.Error())
}

// TestTodo_REV_059_01_Golden pins the owned projection of an amendment
// denial: the same out-of-scope write must refuse with byte-identical
// text on every run and every transport.
func TestTodo_REV_059_01_Golden(t *testing.T) {
	amendment := rev059Amendment(t)
	inner := &rev059RecordingExecutor{result: rev059CannedResult()}
	gated := NewAmendmentGatedExecutor(inner, &amendment, "grant-p1b-1", func() time.Time { return rev059Now })
	_, err := gated.Execute(context.Background(), rev059Start(values.TenantId("acme-corp"), rev059Write("worker.ssn", intent.WriteOperationUpdate)))
	if !errors.Is(err, authority.ErrTopologyMismatch) {
		t.Fatalf("execute err = %v, want the amendment topology refusal", err)
	}
	got := rev059RenderDenial(err)
	want, readErr := os.ReadFile(filepath.Join("testdata", "rev05901_amendment_denied.golden"))
	if readErr != nil {
		t.Fatalf("read golden: %v\nactual:\n%s", readErr, got)
	}
	if got != string(want) {
		t.Fatalf("denial projection drifted.\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestTodo_REV_059_01_Security proves every amendment denial dimension
// refuses at the gate with the amendment's own typed error, and that no
// denial narrates the amendment's bound fields, operations or grant.
func TestTodo_REV_059_01_Security(t *testing.T) {
	amendment := rev059Amendment(t)
	tenant := values.TenantId("acme-corp")
	ctx := context.Background()
	secretFields := []string{"worker.title", "grant-p1b-1"}

	denied := func(t *testing.T, gated ProposalExecutor, start runtime.StartRequest, want error) error {
		t.Helper()
		_, err := gated.Execute(ctx, start)
		if !errors.Is(err, want) {
			t.Fatalf("execute err = %v, want %v", err, want)
		}
		for _, secret := range secretFields {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("denial enumerates bound material %q: %v", secret, err)
			}
		}
		owned := executionError(err)
		if owned.Code() != envelope.CodePermissionDenied || owned.ReasonRef() != "p1b.amendment_denied" {
			t.Fatalf("projection = (%s, %q), want (PERMISSION_DENIED, p1b.amendment_denied)", owned.Code(), owned.ReasonRef())
		}
		for _, secret := range secretFields {
			if strings.Contains(owned.Error(), secret) {
				t.Fatalf("projected denial enumerates bound material %q: %v", secret, owned.Error())
			}
		}
		return err
	}

	t.Run("another tenant's write is denied", func(t *testing.T) {
		inner := &rev059RecordingExecutor{result: rev059CannedResult()}
		gated := NewAmendmentGatedExecutor(inner, &amendment, "grant-p1b-1", func() time.Time { return rev059Now })
		denied(t, gated, rev059Start(values.TenantId("globex"), rev059Write("worker.title", intent.WriteOperationUpdate)), authority.ErrLocalWriteDenied)
		if executes, _, _ := inner.counts(); executes != 0 {
			t.Fatalf("refused start reached the driver %d times, want zero", executes)
		}
	})

	t.Run("a wrong grant is denied", func(t *testing.T) {
		inner := &rev059RecordingExecutor{result: rev059CannedResult()}
		gated := NewAmendmentGatedExecutor(inner, &amendment, "grant-forged", func() time.Time { return rev059Now })
		denied(t, gated, rev059Start(tenant, rev059Write("worker.title", intent.WriteOperationUpdate)), authority.ErrLocalWriteDenied)
		if executes, _, _ := inner.counts(); executes != 0 {
			t.Fatalf("refused start reached the driver %d times, want zero", executes)
		}
	})

	t.Run("a missing grant is denied", func(t *testing.T) {
		inner := &rev059RecordingExecutor{result: rev059CannedResult()}
		gated := NewAmendmentGatedExecutor(inner, &amendment, "", func() time.Time { return rev059Now })
		denied(t, gated, rev059Start(tenant, rev059Write("worker.title", intent.WriteOperationUpdate)), authority.ErrLocalWriteDenied)
		if executes, _, _ := inner.counts(); executes != 0 {
			t.Fatalf("refused start reached the driver %d times, want zero", executes)
		}
	})

	t.Run("an expired amendment is denied", func(t *testing.T) {
		inner := &rev059RecordingExecutor{result: rev059CannedResult()}
		after := amendment.Until.Add(time.Second)
		gated := NewAmendmentGatedExecutor(inner, &amendment, "grant-p1b-1", func() time.Time { return after })
		denied(t, gated, rev059Start(tenant, rev059Write("worker.title", intent.WriteOperationUpdate)), authority.ErrAmendmentInactive)
		if executes, _, _ := inner.counts(); executes != 0 {
			t.Fatalf("refused start reached the driver %d times, want zero", executes)
		}
	})

	t.Run("a revoked amendment is denied", func(t *testing.T) {
		revoked := amendment
		revoked.Revoked = true
		rebound, err := authority.Bind(revoked)
		if err != nil {
			t.Fatalf("rebind revoked: %v", err)
		}
		inner := &rev059RecordingExecutor{result: rev059CannedResult()}
		gated := NewAmendmentGatedExecutor(inner, &rebound, "grant-p1b-1", func() time.Time { return rev059Now })
		denied(t, gated, rev059Start(tenant, rev059Write("worker.title", intent.WriteOperationUpdate)), authority.ErrAmendmentInactive)
		if executes, _, _ := inner.counts(); executes != 0 {
			t.Fatalf("refused start reached the driver %d times, want zero", executes)
		}
	})

	t.Run("external authority denies every local write", func(t *testing.T) {
		external, err := authority.Bind(authority.Amendment{
			AmendmentID: "amd-rev059-external",
			Tenant:      tenant,
			Topology:    authority.TopologyExternalAuthority,
			Fields:      []string{"worker.title"},
			Operations:  []string{"UPDATE"},
			From:        amendment.From,
			Until:       amendment.Until,
		})
		if err != nil {
			t.Fatalf("bind external: %v", err)
		}
		inner := &rev059RecordingExecutor{result: rev059CannedResult()}
		gated := NewAmendmentGatedExecutor(inner, &external, "grant-p1b-1", func() time.Time { return rev059Now })
		denied(t, gated, rev059Start(tenant, rev059Write("worker.title", intent.WriteOperationUpdate)), authority.ErrLocalWriteDenied)
		if executes, _, _ := inner.counts(); executes != 0 {
			t.Fatalf("refused start reached the driver %d times, want zero", executes)
		}
	})

	t.Run("a legacy write with no operation semantic is denied, not admitted by omission", func(t *testing.T) {
		inner := &rev059RecordingExecutor{result: rev059CannedResult()}
		gated := NewAmendmentGatedExecutor(inner, &amendment, "grant-p1b-1", func() time.Time { return rev059Now })
		denied(t, gated, rev059Start(tenant, rev059Write("worker.title", intent.WriteOperationUnspecified)), authority.ErrTopologyMismatch)
		if executes, _, _ := inner.counts(); executes != 0 {
			t.Fatalf("refused start reached the driver %d times, want zero", executes)
		}
	})

	t.Run("a tampered amendment digest fails the cell, not the caller", func(t *testing.T) {
		tampered := amendment
		tampered.Fields = []string{"worker.title", "worker.ssn"}
		inner := &rev059RecordingExecutor{result: rev059CannedResult()}
		gated := NewAmendmentGatedExecutor(inner, &tampered, "grant-p1b-1", func() time.Time { return rev059Now })
		_, err := gated.Execute(ctx, rev059Start(tenant, rev059Write("worker.title", intent.WriteOperationUpdate)))
		if !errors.Is(err, authority.ErrDigestMismatch) {
			t.Fatalf("execute err = %v, want the digest refusal", err)
		}
		owned := executionError(err)
		if owned.Code() != envelope.CodeUnavailable || owned.ReasonRef() != "p1b.amendment_misconfigured" {
			t.Fatalf("projection = (%s, %q), want (UNAVAILABLE, p1b.amendment_misconfigured)", owned.Code(), owned.ReasonRef())
		}
		if executes, _, _ := inner.counts(); executes != 0 {
			t.Fatalf("refused start reached the driver %d times, want zero", executes)
		}
	})

	t.Run("a write-free revision passes the gate vacuously", func(t *testing.T) {
		inner := &rev059RecordingExecutor{result: rev059CannedResult()}
		gated := NewAmendmentGatedExecutor(inner, &amendment, "grant-p1b-1", func() time.Time { return rev059Now })
		if _, err := gated.Execute(ctx, rev059Start(tenant)); err != nil {
			t.Fatalf("write-free execute: %v", err)
		}
		if executes, _, _ := inner.counts(); executes != 1 {
			t.Fatalf("driver received %d starts, want exactly 1", executes)
		}
	})
}
