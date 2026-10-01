package agentegress

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

var egressNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func egressEvaluator(t *testing.T) *Evaluator {
	t.Helper()
	trust, err := outbound.NewPolicy(
		outbound.Destination{Name: "connection.hr", TrustBundleRef: "bundle:connection:v1", Purposes: []string{"agent.lookup"}, DataClasses: []string{string(trustdlp.ClassPublic), string(trustdlp.ClassPII), string(trustdlp.ClassCompensation)}},
		outbound.Destination{Name: "model.eu", TrustBundleRef: "bundle:model:v1", Purposes: []string{"agent.lookup"}, DataClasses: []string{string(trustdlp.ClassPublic), string(trustdlp.ClassPII)}},
	)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := trustdlp.NewPolicy(trust,
		trustdlp.Clearance{Destination: "connection.hr", Classes: []trustdlp.DataClass{trustdlp.ClassPublic, trustdlp.ClassPII, trustdlp.ClassCompensation}, Decision: trustdlp.Allow},
		trustdlp.Clearance{Destination: "model.eu", Classes: []trustdlp.DataClass{trustdlp.ClassPublic, trustdlp.ClassPII}, Decision: trustdlp.Allow},
	)
	if err != nil {
		t.Fatal(err)
	}
	pii, err := trustdlp.NewDetector("ssn", trustdlp.ClassPII, trustdlp.SeverityHigh, `\b\d{3}-\d{2}-\d{4}\b`)
	if err != nil {
		t.Fatal(err)
	}
	inspector, err := trustdlp.NewInspector(pii)
	if err != nil {
		t.Fatal(err)
	}
	evaluator, err := NewEvaluator(policy, inspector, trustdlp.NewReceiptLog())
	if err != nil {
		t.Fatal(err)
	}
	return evaluator
}

func egressProfile(id string, kind TargetKind, classes ...trustdlp.DataClass) Profile {
	return Profile{ID: id, Kind: kind, AllowedRegions: []string{"eu-west"}, AllowedClasses: classes, Retention: RetentionPolicy{Mode: RetentionNone}}
}

func egressTask(classes ...trustdlp.DataClass) TaskPolicy {
	return TaskPolicy{AllowedRegions: []string{"eu-west"}, AllowedResultClasses: classes, MaxExternalRetention: 0, ResultRetention: 2 * time.Hour}
}

func egressField(name string, value any, class trustdlp.DataClass) Field {
	return Field{Name: name, Value: value, Class: class, Taint: []string{"USER_DATA"}, Provenance: []string{"skill:people.lookup"}}
}

func refusalCode(t *testing.T, err error) RefusalCode {
	t.Helper()
	var refusal *Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("error = %v, want *Refusal", err)
	}
	return refusal.Code
}

// TestTodo_AGENT2_021 proves one evaluator minimizes skill arguments, checks
// the target profile before dispatch, and applies a bounded ledger retention
// policy to an inbound typed result.
func TestTodo_AGENT2_021(t *testing.T) {
	evaluator := egressEvaluator(t)
	decision, err := evaluator.EvaluateOutbound(OutboundRequest{
		TaskID: "task-1", Tenant: "tenant-a", Principal: "user-1", Purpose: "agent.lookup",
		Profile: egressProfile("connection.hr", TargetConnection, trustdlp.ClassPublic, trustdlp.ClassPII), Region: "eu-west",
		DeclaredFields: []string{"name"}, Fields: []Field{
			egressField("name", "Ada", trustdlp.ClassPublic),
			egressField("salary", 125000, trustdlp.ClassCompensation),
		}, Task: egressTask(trustdlp.ClassPublic, trustdlp.ClassPII), Now: egressNow,
	})
	if err != nil {
		t.Fatalf("EvaluateOutbound: %v", err)
	}
	if !decision.Allowed || decision.Receipt.Decision != DecisionAllow {
		t.Fatalf("decision = %+v, want allowed", decision)
	}
	var payload map[string]any
	if err := json.Unmarshal(decision.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 1 || payload["name"] != "Ada" {
		t.Fatalf("minimized payload = %#v, want only name", payload)
	}
	if strings.Contains(string(decision.Payload), "salary") || strings.Contains(decision.Receipt.Canonical(), "salary") {
		t.Fatal("minimum-necessary payload or receipt contains an undeclared salary field")
	}

	result, err := evaluator.AcceptInbound(InboundRequest{
		TaskID: "task-1", Tenant: "tenant-a", Principal: "user-1", Purpose: "agent.lookup",
		Profile: egressProfile("connection.hr", TargetConnection, trustdlp.ClassPublic, trustdlp.ClassPII), Region: "eu-west",
		Result: agentsecurity.TypedResult{Schema: "people.v1", Value: map[string]any{"name": "Ada"}, Validated: true, Taint: []string{"USER_DATA"}, Provenance: []string{"connection.hr:v1"}},
		Fields: []Field{egressField("name", "Ada", trustdlp.ClassPublic)}, Task: egressTask(trustdlp.ClassPublic, trustdlp.ClassPII), Now: egressNow,
	})
	if err != nil {
		t.Fatalf("AcceptInbound: %v", err)
	}
	if result.ExpiresAt != egressNow.Add(2*time.Hour) || result.Schema != "people.v1" || len(result.Taint) != 1 {
		t.Fatalf("retained result = %+v, want typed bounded result", result)
	}
}

// TestTodo_AGENT2_021_Golden pins the audit-safe canonical receipt and proves
// field declaration order does not change its digest.
func TestTodo_AGENT2_021_Golden(t *testing.T) {
	makeDecision := func(t *testing.T, declared []string) OutboundDecision {
		t.Helper()
		return mustOutbound(t, egressEvaluator(t), OutboundRequest{
			TaskID: "task-golden", Tenant: "tenant-a", Principal: "user-1", Purpose: "agent.lookup",
			Profile: egressProfile("model.eu", TargetModel, trustdlp.ClassPublic), Region: "eu-west", DeclaredFields: declared,
			Fields: []Field{egressField("name", "Ada", trustdlp.ClassPublic)}, Task: egressTask(trustdlp.ClassPublic), Now: egressNow,
		})
	}
	first, second := makeDecision(t, []string{"name"}), makeDecision(t, []string{"name"})
	if first.Receipt.Digest != second.Receipt.Digest || first.Receipt.Canonical() != second.Receipt.Canonical() {
		t.Fatalf("receipt is not stable: %s vs %s", first.Receipt.Digest, second.Receipt.Digest)
	}
	if strings.Contains(first.Receipt.Canonical(), "Ada") || strings.Contains(first.Receipt.Canonical(), "125000") {
		t.Fatal("receipt canonical form contains raw payload material")
	}
}

// TestTodo_AGENT2_021_Security proves residency, class, DLP detection and
// missing agentsecurity taint cannot be bypassed before a provider call.
func TestTodo_AGENT2_021_Security(t *testing.T) {
	evaluator := egressEvaluator(t)
	base := OutboundRequest{TaskID: "task-sec", Tenant: "tenant-a", Principal: "user-1", Purpose: "agent.lookup", Profile: egressProfile("model.eu", TargetModel, trustdlp.ClassPublic), Region: "eu-west", DeclaredFields: []string{"salary"}, Fields: []Field{egressField("salary", 125000, trustdlp.ClassCompensation)}, Task: egressTask(trustdlp.ClassPublic), Now: egressNow}
	if _, err := evaluator.EvaluateOutbound(base); refusalCode(t, err) != RefusalClass {
		t.Fatalf("salary class error = %v, want %s", err, RefusalClass)
	}
	base.Region = "us-east"
	base.DeclaredFields = []string{"name"}
	base.Fields = []Field{egressField("name", "Ada", trustdlp.ClassPublic)}
	if _, err := evaluator.EvaluateOutbound(base); refusalCode(t, err) != RefusalRegion {
		t.Fatalf("region error = %v, want %s", err, RefusalRegion)
	}
	base.Region = "eu-west"
	base.DeclaredFields = []string{"name"}
	base.Fields[0].Taint = nil
	if _, err := evaluator.EvaluateOutbound(base); refusalCode(t, err) != RefusalClassification {
		t.Fatalf("taint error = %v, want %s", err, RefusalClassification)
	}
	base.Fields[0] = egressField("name", "ssn 123-45-6789", trustdlp.ClassPublic)
	if _, err := evaluator.EvaluateOutbound(base); refusalCode(t, err) != RefusalClass {
		t.Fatalf("detected class error = %v, want %s", err, RefusalClass)
	}
	if got := len(evaluator.Receipts()); got != 0 {
		t.Fatalf("refused pre-dispatch requests appended %d receipts", got)
	}
	resultRequest := InboundRequest{
		TaskID: "task-sec", Tenant: "tenant-a", Principal: "user-1", Purpose: "agent.lookup",
		Profile: egressProfile("model.eu", TargetModel, trustdlp.ClassPublic), Region: "eu-west",
		Result: agentsecurity.TypedResult{Schema: "people.v1", Value: map[string]any{"name": "Ada", "salary": 125000}, Validated: true, Taint: []string{"USER_DATA"}, Provenance: []string{"model.eu:v1"}},
		Fields: []Field{egressField("name", "Ada", trustdlp.ClassPublic)}, Task: egressTask(trustdlp.ClassPublic), Now: egressNow,
	}
	if _, err := evaluator.AcceptInbound(resultRequest); refusalCode(t, err) != RefusalClassification {
		t.Fatalf("unclassified inbound result error = %v, want %s", err, RefusalClassification)
	}
}

// TestTodo_AGENT2_021_Integration covers the shared model/connection path and
// expiry deletion from the bounded task-memory projection.
func TestTodo_AGENT2_021_Integration(t *testing.T) {
	evaluator := egressEvaluator(t)
	for _, kind := range []TargetKind{TargetConnection, TargetModel} {
		decision, err := evaluator.EvaluateOutbound(OutboundRequest{
			TaskID: "task-integration", Tenant: "tenant-a", Principal: "user-1", Purpose: "agent.lookup",
			Profile: egressProfile(map[TargetKind]string{TargetConnection: "connection.hr", TargetModel: "model.eu"}[kind], kind, trustdlp.ClassPublic), Region: "eu-west",
			DeclaredFields: []string{"name"}, Fields: []Field{egressField("name", "Ada", trustdlp.ClassPublic)}, Task: egressTask(trustdlp.ClassPublic), Now: egressNow,
		})
		if err != nil || !decision.Allowed || decision.Receipt.Kind != kind {
			t.Fatalf("%s decision = %+v, err=%v", kind, decision, err)
		}
	}
	result, err := evaluator.AcceptInbound(InboundRequest{
		TaskID: "task-integration", Tenant: "tenant-a", Principal: "user-1", Purpose: "agent.lookup", Profile: egressProfile("model.eu", TargetModel, trustdlp.ClassPublic), Region: "eu-west",
		Result: agentsecurity.TypedResult{Schema: "answer.v1", Value: map[string]any{"answer": "ok"}, Validated: true, Taint: []string{"DERIVED"}, Provenance: []string{"model.eu:v1"}},
		Fields: []Field{egressField("answer", "ok", trustdlp.ClassPublic)}, Task: egressTask(trustdlp.ClassPublic), Now: egressNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	ledger := NewResultLedger()
	if err := ledger.Put("task-integration", result); err != nil {
		t.Fatal(err)
	}
	result.Value.(map[string]any)["name"] = "Mallory"
	result.Fields[0].Name = "tampered"
	if _, ok := ledger.Get("task-integration", egressNow.Add(time.Hour)); !ok {
		t.Fatal("unexpired result was not available")
	}
	stored, ok := ledger.Get("task-integration", egressNow.Add(time.Hour))
	if !ok || stored.Value.(map[string]any)["answer"] != "ok" || stored.Fields[0].Name != "answer" {
		t.Fatalf("ledger returned caller-mutated material: %+v", stored)
	}
	stored.Value.(map[string]any)["answer"] = "tampered-again"
	storedAgain, ok := ledger.Get("task-integration", egressNow.Add(time.Hour))
	if !ok || storedAgain.Value.(map[string]any)["answer"] != "ok" {
		t.Fatalf("ledger exposed mutable retained material: %+v", storedAgain)
	}
	if _, ok := ledger.Get("task-integration", egressNow.Add(2*time.Hour)); ok {
		t.Fatal("expired result remained in task memory")
	}
	if err := evaluatorReceiptsVerify(evaluator); err != nil {
		t.Fatal(err)
	}
}

func mustOutbound(t *testing.T, evaluator *Evaluator, request OutboundRequest) OutboundDecision {
	t.Helper()
	decision, err := evaluator.EvaluateOutbound(request)
	if err != nil {
		t.Fatal(err)
	}
	return decision
}

func evaluatorReceiptsVerify(evaluator *Evaluator) error {
	if evaluator == nil || evaluator.receipts == nil {
		return errors.New("missing receipt log")
	}
	return evaluator.receipts.Verify()
}
