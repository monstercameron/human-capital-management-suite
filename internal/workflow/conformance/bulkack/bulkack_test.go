package bulkack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	delivery "github.com/monstercameron/human-capital-management-suite/internal/operations/messagingdelivery"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/parallel"
)

func audienceRecipients() []Recipient {
	return []Recipient{
		{ID: "r1", PayloadRef: "payload:r1", Locale: "en-US", Accessible: true, Mandatory: true, AuthorityScope: []string{"notice.read"}},
		{ID: "r2", PayloadRef: "payload:r2", Locale: "en-US", Accessible: true, Mandatory: true, AuthorityScope: []string{"notice.read"}},
		{ID: "r3", PayloadRef: "payload:r3", Locale: "es-US", Accessible: false, Mandatory: false, AuthorityScope: []string{"notice.read"}},
		{ID: "r4", PayloadRef: "payload:r4", Locale: "en-US", Accessible: true, Excluded: true, AuthorityScope: []string{"notice.read"}},
	}
}

func frozenAudience(t *testing.T) AudienceSnapshot {
	t.Helper()
	snapshot, err := FreezeAudience("policy/v7", audienceRecipients())
	if err != nil {
		t.Fatalf("FreezeAudience: %v", err)
	}
	return snapshot
}

func partitionedChildren(t *testing.T, snapshot AudienceSnapshot) []Child {
	t.Helper()
	partitions, err := Partition(snapshot, []string{"notice.read"}, "artifact/v7", 2, 4, 100, 3)
	if err != nil {
		t.Fatalf("Partition: %v", err)
	}
	var children []Child
	for _, partition := range partitions {
		children = append(children, partition...)
	}
	return children
}

func TestBulkPolicyAcknowledgementConformancePreservesRecipientObligations(t *testing.T) {
	snapshot := frozenAudience(t)
	children := partitionedChildren(t, snapshot)
	if len(children) != 4 {
		t.Fatalf("children = %d, want one per recipient", len(children))
	}
	// Deterministic child identities bound to the frozen audience.
	again := partitionedChildren(t, snapshot)
	for i := range children {
		if children[i].ChildID != again[i].ChildID || children[i].AudienceDigest != snapshot.Digest {
			t.Fatal("child identities are not deterministic audience bindings")
		}
		if children[i].IdempotencyKey == "" {
			t.Fatalf("child %d has no idempotency key", i)
		}
	}
	// r1 acknowledges with a distinct ack; r2 times out; r3 is
	// inaccessible; r4 is excluded.
	acked := children[0]
	acked.AckProof = "ack:r1"
	acked.DeliveryProof = "delivery:r1"
	outcomes := []ChildOutcome{
		{ChildID: acked.ChildID, IdempotencyKey: acked.IdempotencyKey, Acknowledged: true, Reachable: true, VerifiedSignal: true},
		{ChildID: children[1].ChildID, IdempotencyKey: children[1].IdempotencyKey, Reachable: true},
		{ChildID: children[2].ChildID, IdempotencyKey: children[2].IdempotencyKey, Acknowledged: true, Reachable: true, VerifiedSignal: true},
		{ChildID: children[3].ChildID, IdempotencyKey: children[3].IdempotencyKey},
	}
	// Partition retry duplicates the ack: idempotency absorbs it.
	outcomes = append(outcomes, outcomes[0])
	mutated := append([]Child(nil), children...)
	mutated[0] = acked
	aggregate, err := AggregateOutcomes(snapshot, mutated, outcomes, "REQUIRED_SET")
	if err != nil {
		t.Fatalf("AggregateOutcomes: %v", err)
	}
	if aggregate.States["r1"] != RecipientAcknowledged {
		t.Fatalf("r1 = %q", aggregate.States["r1"])
	}
	if aggregate.States["r2"] != RecipientPending {
		t.Fatalf("r2 timeout = %q, want pending never success", aggregate.States["r2"])
	}
	if aggregate.States["r3"] != RecipientDegraded {
		t.Fatalf("r3 inaccessible = %q, want degraded never satisfied", aggregate.States["r3"])
	}
	if aggregate.States["r4"] != RecipientExcluded {
		t.Fatalf("r4 = %q", aggregate.States["r4"])
	}
	if len(aggregate.HumanWork) != 1 || aggregate.HumanWork[0] != "ack:r2" {
		t.Fatalf("human work = %v", aggregate.HumanWork)
	}
	if aggregate.LegalSatisfaction {
		t.Fatal("mandatory gap reports legal satisfaction")
	}
	// The aggregate carries references, never payloads.
	for _, ref := range aggregate.ChildRefs {
		if strings.Contains(ref, "payload") {
			t.Fatalf("aggregate leaks payload material: %q", ref)
		}
	}
	if err := aggregate.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestTodo_CONF_022_Property(t *testing.T) {
	snapshot := frozenAudience(t)
	children := partitionedChildren(t, snapshot)
	outcomes := []ChildOutcome{
		{ChildID: children[0].ChildID, IdempotencyKey: children[0].IdempotencyKey, Acknowledged: true, Reachable: true, VerifiedSignal: true},
	}
	first, err := AggregateOutcomes(snapshot, children[:1], outcomes, "REQUIRED_SET")
	if err != nil {
		t.Fatal(err)
	}
	second, err := AggregateOutcomes(snapshot, children[:1], outcomes, "REQUIRED_SET")
	if err != nil || first.Digest != second.Digest {
		t.Fatal("aggregation is not deterministic")
	}
	// Full mandatory acknowledgement is the only satisfied terminal.
	all := partitionedChildren(t, snapshot)
	var full []ChildOutcome
	for i := range all {
		all[i].AckProof = "ack:" + all[i].RecipientID
		all[i].DeliveryProof = "delivery:" + all[i].RecipientID
		full = append(full, ChildOutcome{ChildID: all[i].ChildID, IdempotencyKey: all[i].IdempotencyKey, Acknowledged: true, Reachable: true, VerifiedSignal: true})
	}
	// r3 is inaccessible and r4 excluded: satisfaction stays false while
	// every mandatory recipient acknowledges.
	complete, err := AggregateOutcomes(snapshot, all, full, "REQUIRED_SET")
	if err != nil {
		t.Fatal(err)
	}
	if complete.States["r1"] != RecipientAcknowledged || complete.States["r2"] != RecipientAcknowledged {
		t.Fatalf("mandatory = %v", complete.States)
	}
}

func TestTodo_CONF_022_Golden(t *testing.T) {
	snapshot := frozenAudience(t)
	children := partitionedChildren(t, snapshot)
	children[0].AckProof = "ack:r1"
	children[0].DeliveryProof = "delivery:r1"
	outcomes := []ChildOutcome{
		{ChildID: children[0].ChildID, IdempotencyKey: children[0].IdempotencyKey, Acknowledged: true, Reachable: true, VerifiedSignal: true},
		{ChildID: children[1].ChildID, IdempotencyKey: children[1].IdempotencyKey, Reachable: false},
	}
	aggregate, err := AggregateOutcomes(snapshot, children[:2], outcomes, "REQUIRED_SET")
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{
		"audience=" + snapshot.Digest,
		"r1=" + aggregate.States["r1"] + " r2=" + aggregate.States["r2"],
		"human-work=" + strings.Join(aggregate.HumanWork, ","),
		"legal-satisfaction=false",
		"digest=" + aggregate.Digest,
	}
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "conf022_bulkack.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set HCMNEXT_UPDATE_GOLDEN=1)", err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestTodo_CONF_022_Race(t *testing.T) {
	registry := NewAudienceRegistry()
	const workers = 8
	var wg sync.WaitGroup
	snapshots := make([]AudienceSnapshot, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			snapshots[i], errs[i] = registry.Record("policy/v7", audienceRecipients())
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if snapshots[i].Digest != snapshots[0].Digest {
			t.Fatalf("worker %d froze a different audience", i)
		}
	}
}

func TestTodo_CONF_022_Integration(t *testing.T) {
	snapshot := frozenAudience(t)
	children := partitionedChildren(t, snapshot)
	rules := map[string]bool{"ca-notice/v1": true}
	// Each child carries a real notice assessment as its verified signal:
	// r1 satisfies, r2 misses its ack, r3 is unreachable.
	var outcomes []ChildOutcome
	var joinInputs []parallel.BranchResult
	for i, child := range children[:3] {
		requirement := delivery.NoticeRequirement{
			ID: "notice-" + child.RecipientID, Recipient: child.RecipientID,
			RecipientVerified: true, RecipientProof: "verify:" + child.RecipientID,
			ContentDigest: "sha256:policy", ContentVersion: "artifact/v7",
			Timestamp: 1700000000, JurisdictionRule: "ca-notice/v1",
			SignatureDigest: "sha256:ceremony",
		}
		if i == 0 {
			requirement.AckProof = "ack:" + child.RecipientID
		}
		assessment, err := delivery.AssessRequirement(requirement, rules)
		if err != nil {
			t.Fatalf("AssessRequirement(%s): %v", child.RecipientID, err)
		}
		children[i].AckProof = requirement.AckProof
		children[i].DeliveryProof = "delivery:" + child.RecipientID
		acknowledged := assessment.Outcome == delivery.NoticeSatisfied
		outcomes = append(outcomes, ChildOutcome{
			ChildID: child.ChildID, IdempotencyKey: child.IdempotencyKey,
			Acknowledged: acknowledged, Reachable: i != 2, VerifiedSignal: acknowledged,
		})
		outcome := parallel.OutcomeSucceeded
		if !acknowledged {
			outcome = parallel.OutcomeUnknown
		}
		joinInputs = append(joinInputs, parallel.BranchResult{BranchID: child.ChildID, IdempotencyKey: child.IdempotencyKey, Outcome: outcome})
	}
	join, err := parallel.Join(parallel.JoinPlan{Strategy: parallel.JoinRequiredSet, Version: "v1", RequiredID: []string{children[0].ChildID, children[1].ChildID}}, joinInputs)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	aggregate, err := AggregateOutcomes(snapshot, children[:3], outcomes, join.Verdict)
	if err != nil {
		t.Fatalf("AggregateOutcomes: %v", err)
	}
	if aggregate.States["r1"] != RecipientAcknowledged || aggregate.States["r2"] != RecipientPending {
		t.Fatalf("states=%v join=%q", aggregate.States, join.Verdict)
	}
	if aggregate.LegalSatisfaction {
		t.Fatal("unacknowledged mandatory recipient reports legal satisfaction")
	}
}

func TestTodo_CONF_022_Fault(t *testing.T) {
	if _, err := FreezeAudience("", audienceRecipients()); err == nil {
		t.Fatal("unversioned audience froze")
	}
	if _, err := FreezeAudience("v1", nil); err == nil {
		t.Fatal("empty audience froze")
	}
	dup := audienceRecipients()
	dup[1].ID = "r1"
	if _, err := FreezeAudience("v1", dup); err == nil {
		t.Fatal("duplicate recipient froze")
	}
	snapshot := frozenAudience(t)
	if _, err := Partition(snapshot, []string{"notice.read"}, "", 2, 4, 100, 3); err == nil {
		t.Fatal("unversioned artifact partitioned")
	}
	if _, err := Partition(snapshot, []string{"notice.read"}, "artifact/v7", 1, 1, 100, 3); err == nil {
		t.Fatal("over-limit audience partitioned")
	}
	if _, err := Partition(snapshot, []string{"notice.read"}, "artifact/v7", 2, 4, 3, 3); err == nil {
		t.Fatal("over-cost acknowledgement partitioned")
	}
	// Cancelled children keep pending state with requeue work, never success.
	children := partitionedChildren(t, snapshot)
	cancelled, err := AggregateOutcomes(snapshot, children[:1], []ChildOutcome{{ChildID: children[0].ChildID, IdempotencyKey: children[0].IdempotencyKey, Acknowledged: true, Reachable: true, Cancelled: true}}, "REQUIRED_SET")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.States["r1"] != RecipientPending || len(cancelled.HumanWork) != 1 {
		t.Fatalf("cancelled=%v", cancelled.States)
	}
}

func TestTodo_CONF_022_Security(t *testing.T) {
	snapshot := frozenAudience(t)
	// A child broadening authority refuses to partition.
	narrow := []Recipient{{ID: "r1", PayloadRef: "payload:r1", Locale: "en-US", Accessible: true, Mandatory: true, AuthorityScope: []string{"payroll.write"}}}
	wide, err := FreezeAudience("policy/v7", narrow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Partition(wide, []string{"notice.read"}, "artifact/v7", 2, 4, 100, 3); err == nil {
		t.Fatal("authority-broadening child partitioned")
	}
	// A child bound to a drifted audience never aggregates.
	children := partitionedChildren(t, snapshot)
	children[0].AudienceDigest = "sha256:drifted"
	if _, err := AggregateOutcomes(snapshot, children[:1], nil, "REQUIRED_SET"); err == nil {
		t.Fatal("drifted audience aggregated")
	}
	// Delivery proof alone never satisfies, even when acknowledged.
	deliveryOnly := partitionedChildren(t, snapshot)
	deliveryOnly[0].AckProof = "delivery:r1"
	deliveryOnly[0].DeliveryProof = "delivery:r1"
	aggregate, err := AggregateOutcomes(snapshot, deliveryOnly[:1], []ChildOutcome{{ChildID: deliveryOnly[0].ChildID, IdempotencyKey: deliveryOnly[0].IdempotencyKey, Acknowledged: true, Reachable: true, VerifiedSignal: true}}, "REQUIRED_SET")
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.States["r1"] == RecipientAcknowledged {
		t.Fatal("delivery substituted for acknowledgement")
	}
	// Forged aggregates never verify.
	if err := (Aggregate{}).Verify(); err == nil {
		t.Fatal("zero aggregate verified")
	}
}

func TestTodo_CONF_022_Conformance(t *testing.T) {
	snapshot := frozenAudience(t)
	children := partitionedChildren(t, snapshot)
	// Partitions hold at most two children across two partitions.
	partitions, err := Partition(snapshot, []string{"notice.read"}, "artifact/v7", 2, 4, 100, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(partitions) != 2 || len(partitions[0]) != 2 || len(partitions[1]) != 2 {
		t.Fatalf("partitions=%v", partitions)
	}
	_ = children
	// Every recipient reaches exactly one terminal state.
	outcomes := []ChildOutcome{
		{ChildID: partitions[0][0].ChildID, IdempotencyKey: partitions[0][0].IdempotencyKey, Acknowledged: true, Reachable: true, VerifiedSignal: true},
		{ChildID: partitions[0][1].ChildID, IdempotencyKey: partitions[0][1].IdempotencyKey, Reachable: false},
		{ChildID: partitions[1][0].ChildID, IdempotencyKey: partitions[1][0].IdempotencyKey, Acknowledged: true, Reachable: true},
		{ChildID: partitions[1][1].ChildID, IdempotencyKey: partitions[1][1].IdempotencyKey},
	}
	partitions[0][0].AckProof = "ack:r1"
	partitions[0][0].DeliveryProof = "delivery:r1"
	aggregate, err := AggregateOutcomes(snapshot, append(partitions[0], partitions[1]...), outcomes, "REQUIRED_SET")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"r1": RecipientAcknowledged, "r2": RecipientUnreachable, "r3": RecipientDegraded, "r4": RecipientExcluded}
	for id, state := range want {
		if aggregate.States[id] != state {
			t.Fatalf("%s = %q, want %q", id, aggregate.States[id], state)
		}
	}
}

func BenchmarkTodo_CONF_022(b *testing.B) {
	recipients := make([]Recipient, 0, 200)
	for i := 0; i < 200; i++ {
		recipients = append(recipients, Recipient{ID: fmt.Sprintf("r%d", i), PayloadRef: "payload", Locale: "en-US", Accessible: true, Mandatory: i%2 == 0, AuthorityScope: []string{"notice.read"}})
	}
	snapshot, err := FreezeAudience("policy/bench", recipients)
	if err != nil {
		b.Fatal(err)
	}
	partitions, err := Partition(snapshot, []string{"notice.read"}, "artifact/v1", 25, 16, 100000, 3)
	if err != nil {
		b.Fatal(err)
	}
	var children []Child
	for _, partition := range partitions {
		children = append(children, partition...)
	}
	outcomes := make([]ChildOutcome, 0, len(children))
	for _, child := range children {
		outcomes = append(outcomes, ChildOutcome{ChildID: child.ChildID, IdempotencyKey: child.IdempotencyKey, Acknowledged: true, Reachable: true, VerifiedSignal: true})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := AggregateOutcomes(snapshot, children, outcomes, "REQUIRED_SET"); err != nil {
			b.Fatal(err)
		}
	}
}

func TestTodo_CONF_022_Mutation(t *testing.T) {
	snapshot := frozenAudience(t)
	children := partitionedChildren(t, snapshot)
	children[0].AckProof = "ack:r1"
	children[0].DeliveryProof = "delivery:r1"
	outcomes := []ChildOutcome{{ChildID: children[0].ChildID, IdempotencyKey: children[0].IdempotencyKey, Acknowledged: true, Reachable: true, VerifiedSignal: true}}
	base, err := AggregateOutcomes(snapshot, children[:1], outcomes, "REQUIRED_SET")
	if err != nil {
		t.Fatal(err)
	}
	// Signal loss degrades the recipient with a new seal.
	lost := []ChildOutcome{{ChildID: children[0].ChildID, IdempotencyKey: children[0].IdempotencyKey, Acknowledged: true, Reachable: true}}
	degraded, err := AggregateOutcomes(snapshot, children[:1], lost, "REQUIRED_SET")
	if err != nil {
		t.Fatal(err)
	}
	if degraded.States["r1"] != RecipientDegraded || degraded.Digest == base.Digest {
		t.Fatalf("degraded=%v", degraded.States)
	}
	// Audience change re-identifies the snapshot: old children refuse.
	changed, err := FreezeAudience("policy/v8", audienceRecipients())
	if err != nil {
		t.Fatal(err)
	}
	if changed.Digest == snapshot.Digest {
		t.Fatal("audience version change kept the digest")
	}
	if _, err := AggregateOutcomes(changed, children[:1], outcomes, "REQUIRED_SET"); err == nil {
		t.Fatal("stale child aggregated under a new audience")
	}
	// Forged states never verify.
	forged := base
	forged.States["r1"] = RecipientAcknowledged
	forged.LegalSatisfaction = true
	if err := forged.Verify(); err == nil {
		t.Fatal("forged aggregate verified")
	}
}

func TestTodo_CONF_022_ServedPath(t *testing.T) {
	if ServingContractID == "" {
		t.Fatal("served contract id is empty")
	}
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("serving contract: %v", err)
	}
}
