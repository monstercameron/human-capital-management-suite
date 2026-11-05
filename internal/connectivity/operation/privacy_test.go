package operation_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
	privacydispatch "github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/dispatch"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

const (
	rev016Destination = "payroll.example"
	rev016Purpose     = "PAYROLL_SYNC"
	rev016Principal   = "worker-1"
	rev016Region      = "us-east-1"
)

func rev016Snapshot(t *testing.T, kind privacydispatch.SnapshotKind, decision dlp.Decision) privacydispatch.PolicySnapshot {
	t.Helper()
	sealed, err := privacydispatch.SealSnapshot(privacydispatch.PolicySnapshot{
		Kind: kind, Version: "policy.v1", Processor: "processor.payroll",
		Destination: rev016Destination, Region: rev016Region, Classification: dlp.ClassInternal,
		Purpose: rev016Purpose, TransferAssessment: "transfer.v1",
		DLPDecision: decision,
	}, "sig-"+string(kind))
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func rev016Binding(t *testing.T, decision dlp.Decision) (privacydispatch.Binding, privacydispatch.SnapshotSet) {
	t.Helper()
	set := privacydispatch.SnapshotSet{
		Processor: rev016Snapshot(t, privacydispatch.SnapshotProcessor, decision),
		Transfer:  rev016Snapshot(t, privacydispatch.SnapshotTransfer, decision),
		DLP:       rev016Snapshot(t, privacydispatch.SnapshotDLP, decision),
	}
	binding, err := privacydispatch.Bind(set)
	if err != nil {
		t.Fatal(err)
	}
	return binding, set
}

func rev016Policy(t *testing.T, decision dlp.Decision) *dlp.Policy {
	t.Helper()
	egress, err := outbound.NewPolicy(outbound.Destination{
		Name: rev016Destination, TrustBundleRef: "bundle:payroll:v1",
		Purposes: []string{rev016Purpose}, DataClasses: []string{string(dlp.ClassInternal)},
	})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := dlp.NewPolicy(egress, dlp.Clearance{
		Destination: rev016Destination, DataClass: dlp.ClassInternal, Decision: decision,
	})
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func rev016Leased(t *testing.T, j *operation.MemoryJournal, id uuid.UUID, resource string) operation.Lease {
	t.Helper()
	planned(t, j, id, resource, 1, operation.OrderingStrict)
	queued(t, j, id)
	return leaseFor(t, j, id)
}

func rev016Request(lease operation.Lease, writer operation.Writer, binding privacydispatch.Binding, current privacydispatch.SnapshotSet) operation.PrivacyDispatchRequest {
	return operation.PrivacyDispatchRequest{
		Lease: lease, Writer: writer, Binding: binding, Current: current,
		Principal: rev016Principal, DataClasses: []dlp.DataClass{dlp.ClassInternal},
	}
}

func rev016JournalEvents(t *testing.T, j *operation.MemoryJournal, id uuid.UUID) []operation.JournalEvent {
	t.Helper()
	entries, err := j.Journal(context.Background(), "tenant-promotion", id)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func rev016FindEvent(entries []operation.JournalEvent, name string) (operation.JournalEvent, bool) {
	for _, entry := range entries {
		if entry.Event == name {
			return entry, true
		}
	}
	return operation.JournalEvent{}, false
}

// TestTodo_REV_016_01 is the REV-016-01 primary: the journal's governed
// dispatch path resolves the current signed DLP/transfer snapshots through
// governance/privacy/dispatch before the provider writer runs. An allowed
// binding dispatches exactly once with PRIVACY_BOUND evidence; a denied
// binding returns EGRESS_BLOCKED with durable evidence and zero provider
// calls, leaving the operation leased for a corrected retry.
func TestTodo_REV_016_01(t *testing.T) {
	ctx := context.Background()

	allowed := newJournal()
	allowedID := uuid.New()
	claim := rev016Leased(t, allowed, allowedID, "worker:w-1")
	binding, current := rev016Binding(t, dlp.Allow)
	writer := operation.NewPayrollSync()
	result, err := allowed.DispatchWithPrivacy(ctx, rev016Request(claim, writer, binding, current), rev016Policy(t, dlp.Allow))
	if err != nil {
		t.Fatalf("allowed privacy dispatch: %v", err)
	}
	if !result.ProviderCall || len(writer.Calls()) != 1 {
		t.Fatalf("allowed dispatch calls=%d providerCall=%v, want exactly one provider call", len(writer.Calls()), result.ProviderCall)
	}
	if got := string(writer.Calls()[0].Payload); !strings.Contains(got, `"worker":"w-1"`) {
		t.Fatalf("provider payload = %q, want the journaled mapped payload", got)
	}
	bound, ok := rev016FindEvent(rev016JournalEvents(t, allowed, allowedID), "PRIVACY_BOUND")
	if !ok || bound.RequestDigest != binding.OperationDigest {
		t.Fatalf("PRIVACY_BOUND evidence missing or unbound: %+v", bound)
	}
	if err := allowed.VerifyJournal(ctx); err != nil {
		t.Fatalf("VerifyJournal after allow: %v", err)
	}

	denied := newJournal()
	deniedID := uuid.New()
	deniedClaim := rev016Leased(t, denied, deniedID, "worker:w-1")
	plannedBinding, currentSet := rev016Binding(t, dlp.Allow)
	currentSet.DLP.Revoked = true
	currentSet.DLP.Digest = privacydispatch.DigestSnapshot(currentSet.DLP)
	deniedWriter := operation.NewPayrollSync()
	_, err = denied.DispatchWithPrivacy(ctx, rev016Request(deniedClaim, deniedWriter, plannedBinding, currentSet), rev016Policy(t, dlp.Allow))
	if !errors.Is(err, privacydispatch.ErrEGRESSBlocked) {
		t.Fatalf("revoked snapshot err = %v, want EGRESS_BLOCKED", err)
	}
	if len(deniedWriter.Calls()) != 0 {
		t.Fatalf("denied dispatch made %d provider calls, want zero", len(deniedWriter.Calls()))
	}
	blocked, ok := rev016FindEvent(rev016JournalEvents(t, denied, deniedID), "EGRESS_BLOCKED")
	if !ok || blocked.RequestDigest != plannedBinding.OperationDigest {
		t.Fatalf("EGRESS_BLOCKED evidence missing or unbound: %+v", blocked)
	}
	op, err := denied.Get(ctx, "tenant-promotion", deniedID)
	if err != nil {
		t.Fatal(err)
	}
	if op.State != operation.StateLeased || len(op.Attempts) != 0 {
		t.Fatalf("denied operation state=%s attempts=%d, want LEASED with no attempt recorded", op.State, len(op.Attempts))
	}
	if err := denied.VerifyJournal(ctx); err != nil {
		t.Fatalf("VerifyJournal after deny: %v", err)
	}
}

// TestTodo_REV_016_01_Golden pins the exact allow-decision and PRIVACY_BOUND
// evidence bytes for the canonical fixture.
func TestTodo_REV_016_01_Golden(t *testing.T) {
	ctx := context.Background()
	j := newJournal()
	id := uuid.New()
	claim := rev016Leased(t, j, id, "worker:w-1")
	binding, current := rev016Binding(t, dlp.Allow)
	decision := privacydispatch.Revalidate(binding, current)
	if _, err := j.DispatchWithPrivacy(ctx, rev016Request(claim, operation.NewPayrollSync(), binding, current), rev016Policy(t, dlp.Allow)); err != nil {
		t.Fatalf("allowed privacy dispatch: %v", err)
	}
	bound, ok := rev016FindEvent(rev016JournalEvents(t, j, id), "PRIVACY_BOUND")
	if !ok {
		t.Fatal("PRIVACY_BOUND evidence missing")
	}
	got := fmt.Sprintf("decision: %s\nevent: %s %s->%s digest=%s\n",
		decision.Explain(), bound.Event, bound.From, bound.To, bound.RequestDigest)
	want, err := os.ReadFile(filepath.Join("testdata", "rev01601.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch:\n got: %q\nwant: %q", got, string(want))
	}
}

// TestTodo_REV_016_01_Security proves every deny shape fails closed with
// zero provider calls and leaks no payload bytes through the error or the
// journal evidence.
func TestTodo_REV_016_01_Security(t *testing.T) {
	ctx := context.Background()
	payload := `{"worker":"w-1","amount":"100.00"}`
	tests := []struct {
		name      string
		mutate    func(*privacydispatch.Binding, *privacydispatch.SnapshotSet)
		policy    dlp.Decision
		principal string
	}{
		{"destination changed", func(_ *privacydispatch.Binding, s *privacydispatch.SnapshotSet) {
			s.DLP.Destination = "other.example"
			s.DLP.Digest = privacydispatch.DigestSnapshot(s.DLP)
		}, dlp.Allow, rev016Principal},
		{"processor revoked", func(_ *privacydispatch.Binding, s *privacydispatch.SnapshotSet) {
			s.Processor.Revoked = true
			s.Processor.Digest = privacydispatch.DigestSnapshot(s.Processor)
		}, dlp.Allow, rev016Principal},
		{"transfer region changed", func(_ *privacydispatch.Binding, s *privacydispatch.SnapshotSet) {
			s.Transfer.Region = "eu-west-1"
			s.Transfer.Digest = privacydispatch.DigestSnapshot(s.Transfer)
		}, dlp.Allow, rev016Principal},
		{"dlp refused", func(_ *privacydispatch.Binding, s *privacydispatch.SnapshotSet) {
			s.DLP.DLPDecision = dlp.Refuse
			s.DLP.Digest = privacydispatch.DigestSnapshot(s.DLP)
		}, dlp.Allow, rev016Principal},
		{"live evaluation redacts", func(_ *privacydispatch.Binding, _ *privacydispatch.SnapshotSet) {
		}, dlp.Redact, rev016Principal},
		{"binding tampered", func(b *privacydispatch.Binding, _ *privacydispatch.SnapshotSet) {
			b.Planned.DLP.Digest = "tampered"
		}, dlp.Allow, rev016Principal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			j := newJournal()
			id := uuid.New()
			claim := rev016Leased(t, j, id, "worker:w-1")
			binding, current := rev016Binding(t, dlp.Allow)
			tc.mutate(&binding, &current)
			writer := operation.NewPayrollSync()
			req := rev016Request(claim, writer, binding, current)
			req.Principal = tc.principal
			_, err := j.DispatchWithPrivacy(ctx, req, rev016Policy(t, tc.policy))
			if !errors.Is(err, privacydispatch.ErrEGRESSBlocked) {
				t.Fatalf("err = %v, want EGRESS_BLOCKED", err)
			}
			if len(writer.Calls()) != 0 {
				t.Fatalf("made %d provider calls, want zero", len(writer.Calls()))
			}
			if strings.Contains(err.Error(), payload) {
				t.Fatalf("error leaks payload bytes: %v", err)
			}
			for _, entry := range rev016JournalEvents(t, j, id) {
				if strings.Contains(entry.RequestDigest, payload) || strings.Contains(entry.CredentialLeaseID, payload) {
					t.Fatalf("journal leaks payload bytes: %+v", entry)
				}
			}
			if err := j.VerifyJournal(ctx); err != nil {
				t.Fatalf("VerifyJournal: %v", err)
			}
		})
	}
}

// TestTodo_REV_016_01_Admission proves the live-path admission half of the
// governed dispatch: AdmitPrivacyDispatch evaluates the same gate and
// records the same durable evidence as DispatchWithPrivacy but performs no
// provider call itself, so the worker role can admit before its credentialed
// send. Admission is retry-safe: a second admission after an allow records
// another PRIVACY_BOUND event and still succeeds.
func TestTodo_REV_016_01_Admission(t *testing.T) {
	ctx := context.Background()
	j := newJournal()
	id := uuid.New()
	claim := rev016Leased(t, j, id, "worker:w-1")
	binding, current := rev016Binding(t, dlp.Allow)
	grant, err := operation.NewStaticPrivacyAuthority(operation.PrivacyGrant{
		Binding: binding, Current: current, Policy: rev016Policy(t, dlp.Allow),
		Principal: rev016Principal, DataClasses: []dlp.DataClass{dlp.ClassInternal},
	})
	if err != nil {
		t.Fatalf("NewStaticPrivacyAuthority: %v", err)
	}
	resolved, err := grant.ResolvePrivacyGrant(ctx, operation.Operation{})
	if err != nil {
		t.Fatalf("ResolvePrivacyGrant: %v", err)
	}
	admitted, err := j.AdmitPrivacyDispatch(ctx, claim, resolved)
	if err != nil {
		t.Fatalf("admit allowed grant: %v", err)
	}
	if admitted.State != operation.StateLeased {
		t.Fatalf("admitted state = %s, want LEASED (admission never sends)", admitted.State)
	}
	if _, ok := rev016FindEvent(rev016JournalEvents(t, j, id), "PRIVACY_BOUND"); !ok {
		t.Fatal("PRIVACY_BOUND evidence missing after admission")
	}
	if _, err := j.AdmitPrivacyDispatch(ctx, claim, resolved); err != nil {
		t.Fatalf("second admission (retry) failed: %v", err)
	}

	deniedID := uuid.New()
	deniedClaim := rev016Leased(t, j, deniedID, "worker:w-2")
	planned, revoked := rev016Binding(t, dlp.Allow)
	revoked.DLP.Revoked = true
	revoked.DLP.Digest = privacydispatch.DigestSnapshot(revoked.DLP)
	deniedAuthority, err := operation.NewStaticPrivacyAuthority(operation.PrivacyGrant{
		Binding: planned, Current: revoked, Policy: rev016Policy(t, dlp.Allow),
		Principal: rev016Principal, DataClasses: []dlp.DataClass{dlp.ClassInternal},
	})
	if err != nil {
		t.Fatalf("NewStaticPrivacyAuthority (revoked): %v", err)
	}
	deniedGrant, err := deniedAuthority.ResolvePrivacyGrant(ctx, operation.Operation{})
	if err != nil {
		t.Fatalf("ResolvePrivacyGrant (revoked): %v", err)
	}
	if _, err := j.AdmitPrivacyDispatch(ctx, deniedClaim, deniedGrant); !errors.Is(err, privacydispatch.ErrEGRESSBlocked) {
		t.Fatalf("admit revoked grant err = %v, want EGRESS_BLOCKED", err)
	}
	if _, ok := rev016FindEvent(rev016JournalEvents(t, j, deniedID), "EGRESS_BLOCKED"); !ok {
		t.Fatal("EGRESS_BLOCKED evidence missing after denied admission")
	}
	if err := j.VerifyJournal(ctx); err != nil {
		t.Fatalf("VerifyJournal: %v", err)
	}
}

// TestTodo_REV_016_01_Authority proves the static privacy authority fails
// fast at construction when either snapshot set is malformed or the policy
// is missing, so a deployment can never wire a broken authority. A revoked
// current set stays constructible: revocation is runtime state the gate
// evaluates per dispatch, which is what the admission deny case above
// proves.
func TestTodo_REV_016_01_Authority(t *testing.T) {
	binding, current := rev016Binding(t, dlp.Allow)
	if _, err := operation.NewStaticPrivacyAuthority(operation.PrivacyGrant{
		Binding: binding, Current: current, Policy: rev016Policy(t, dlp.Allow),
		Principal: rev016Principal, DataClasses: []dlp.DataClass{dlp.ClassInternal},
	}); err != nil {
		t.Fatalf("matching authority rejected: %v", err)
	}
	drifted := current
	drifted.Transfer.Region = "eu-west-1"
	drifted.Transfer.Digest = privacydispatch.DigestSnapshot(drifted.Transfer)
	if _, err := operation.NewStaticPrivacyAuthority(operation.PrivacyGrant{
		Binding: binding, Current: drifted, Policy: rev016Policy(t, dlp.Allow),
		Principal: rev016Principal, DataClasses: []dlp.DataClass{dlp.ClassInternal},
	}); err == nil {
		t.Fatal("drifted authority constructed without error")
	}
	if _, err := operation.NewStaticPrivacyAuthority(operation.PrivacyGrant{
		Binding: binding, Current: current, Principal: rev016Principal,
		DataClasses: []dlp.DataClass{dlp.ClassInternal},
	}); !errors.Is(err, operation.ErrPrivacyGateRequired) {
		t.Fatalf("nil-policy authority err = %v, want ErrPrivacyGateRequired", err)
	}
}

// TestTodo_REV_016_01_Integration proves the governed path end to end with
// the real DLP policy, the real snapshot sealer and the real journal: allow
// reaches the provider exactly once, then a revoked snapshot on a second
// operation in the same journal is blocked without touching the provider.
func TestTodo_REV_016_01_Integration(t *testing.T) {
	ctx := context.Background()
	j := newJournal()

	firstID := uuid.New()
	firstClaim := rev016Leased(t, j, firstID, "worker:w-1")
	firstBinding, firstCurrent := rev016Binding(t, dlp.Allow)
	firstWriter := operation.NewPayrollSync()
	if _, err := j.DispatchWithPrivacy(ctx, rev016Request(firstClaim, firstWriter, firstBinding, firstCurrent), rev016Policy(t, dlp.Allow)); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}

	secondID := uuid.New()
	secondClaim := rev016Leased(t, j, secondID, "worker:w-2")
	secondBinding, secondCurrent := rev016Binding(t, dlp.Allow)
	secondCurrent.Transfer.Revoked = true
	secondCurrent.Transfer.Digest = privacydispatch.DigestSnapshot(secondCurrent.Transfer)
	secondWriter := operation.NewPayrollSync()
	if _, err := j.DispatchWithPrivacy(ctx, rev016Request(secondClaim, secondWriter, secondBinding, secondCurrent), rev016Policy(t, dlp.Allow)); !errors.Is(err, privacydispatch.ErrEGRESSBlocked) {
		t.Fatalf("second dispatch err = %v, want EGRESS_BLOCKED", err)
	}

	if len(firstWriter.Calls()) != 1 || len(secondWriter.Calls()) != 0 {
		t.Fatalf("provider calls = %d/%d, want 1/0", len(firstWriter.Calls()), len(secondWriter.Calls()))
	}
	first, err := j.Get(ctx, "tenant-promotion", firstID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := j.Get(ctx, "tenant-promotion", secondID)
	if err != nil {
		t.Fatal(err)
	}
	if first.State != operation.StateProviderAccepted || second.State != operation.StateLeased {
		t.Fatalf("states = %s/%s, want PROVIDER_ACCEPTED/LEASED", first.State, second.State)
	}
	if err := j.VerifyJournal(ctx); err != nil {
		t.Fatalf("VerifyJournal: %v", err)
	}
}
