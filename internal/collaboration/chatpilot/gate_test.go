package chatpilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type verifierFunc func(context.Context, Reference, time.Time) (Verified, error)

func (f verifierFunc) Verify(ctx context.Context, ref Reference, now time.Time) (Verified, error) {
	return f(ctx, ref, now)
}

func TestTodo_CHAT_053(t *testing.T) {
	// This is the repository's existing proposed pilot fixture. Its owner,
	// cohort, switch and budget strings are not signed human evidence.
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "chat", "testdata", "pilot_gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	var proposedFixture struct {
		PilotOwner string   `json:"pilot_owner"`
		Cohorts    []string `json:"cohorts"`
		Evidence   struct {
			SignedWorkflowBaseline bool `json:"signed_workflow_baseline"`
			ObservedEmployeePilot  bool `json:"observed_employee_pilot"`
			IncidentDrill          bool `json:"incident_drill"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal(data, &proposedFixture); err != nil {
		t.Fatal(err)
	}
	if proposedFixture.PilotOwner == "" || len(proposedFixture.Cohorts) == 0 || proposedFixture.Evidence.SignedWorkflowBaseline || proposedFixture.Evidence.ObservedEmployeePilot || proposedFixture.Evidence.IncidentDrill {
		t.Fatalf("legacy fixture changed unexpectedly: %+v", proposedFixture)
	}
	// Convert only actual immutable references. Descriptive strings and caller
	// booleans in this legacy fixture do not become references or verified facts.
	packet := Packet{}
	decision := Review(context.Background(), packet, verifierFunc(func(context.Context, Reference, time.Time) (Verified, error) {
		t.Fatal("unreferenced evidence must not be requested")
		return Verified{}, nil
	}), time.Now().UTC())
	if decision.Ready || len(decision.Reasons) == 0 {
		t.Fatalf("empty, unsigned packet decision = %+v; want blocked", decision)
	}
	for _, kind := range requiredKinds {
		if !hasReason(decision, "missing signed evidence: "+string(kind)) {
			t.Errorf("decision does not identify missing %s: %v", kind, decision.Reasons)
		}
	}
	if got := Review(context.Background(), Packet{}, nil, time.Now()); got.Ready || !hasReason(got, "trusted evidence verifier is unavailable") {
		t.Fatalf("missing trusted verifier decision = %+v", got)
	}
}

func TestTodo_CHAT_053_Conformance(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	packet, facts := completeFixture(now)
	decision := Review(context.Background(), packet, verifierFunc(func(_ context.Context, ref Reference, _ time.Time) (Verified, error) {
		fact, ok := facts[ref.Kind]
		if !ok {
			return Verified{}, ErrEvidenceUnavailable
		}
		return fact, nil
	}), now)
	if !decision.Ready || len(decision.Reasons) != 0 {
		t.Fatalf("complete verified pilot evidence decision = %+v", decision)
	}

	tests := []struct {
		name   string
		mutate func(*Packet, map[Kind]Verified)
		want   string
	}{
		{"digest tamper", func(p *Packet, _ map[Kind]Verified) { p.Evidence[0].SHA256 = "bad" }, "invalid immutable reference"},
		{"unsigned source refusal", func(_ *Packet, f map[Kind]Verified) { delete(f, ScopeDecision) }, "verification failed"},
		{"tenant mismatch", func(_ *Packet, f map[Kind]Verified) { e := f[Adoption]; e.TenantID = "other-tenant"; f[Adoption] = e }, "not bound to the signed pilot tenant"},
		{"empty employee denominator", func(_ *Packet, f map[Kind]Verified) { e := f[Adoption]; e.Denominator = 0; f[Adoption] = e }, "valid observed numerator and employee denominator"},
		{"adoption below signed threshold", func(_ *Packet, f map[Kind]Verified) {
			e := f[Adoption]
			e.Numerator = 1
			e.Denominator = 100
			f[Adoption] = e
		}, "below the signed scope decision threshold"},
		{"missing dashboard series", func(_ *Packet, f map[Kind]Verified) {
			e := f[SLODashboard]
			e.Metrics = e.Metrics[1:]
			f[SLODashboard] = e
		}, "lacks a passing sampled metric"},
		{"SLO breach", func(_ *Packet, f map[Kind]Verified) {
			e := f[SLODashboard]
			e.Metrics[0].Observed = e.Metrics[0].Limit + 1
			f[SLODashboard] = e
		}, "lacks a passing sampled metric"},
		{"rollback not observed", func(_ *Packet, f map[Kind]Verified) {
			e := f[RollbackDrill]
			e.RollbackObserved = false
			f[RollbackDrill] = e
		}, "no successful observed rollback"},
		{"duplicate kind", func(p *Packet, _ map[Kind]Verified) { p.Evidence = append(p.Evidence, p.Evidence[0]) }, "duplicate evidence kind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, f := completeFixture(now)
			tt.mutate(&p, f)
			decision := Review(context.Background(), p, verifierFunc(func(_ context.Context, ref Reference, _ time.Time) (Verified, error) {
				fact, ok := f[ref.Kind]
				if !ok {
					return Verified{}, errors.New("signature or artifact verification refused")
				}
				return fact, nil
			}), now)
			if decision.Ready || !hasReason(decision, tt.want) {
				t.Fatalf("decision = %+v, want blocked reason containing %q", decision, tt.want)
			}
		})
	}
}

func completeFixture(now time.Time) (Packet, map[Kind]Verified) {
	start, end := now.Add(-24*time.Hour), now.Add(-time.Hour)
	facts := make(map[Kind]Verified, len(requiredKinds))
	packet := Packet{Evidence: make([]Reference, 0, len(requiredKinds))}
	for _, kind := range requiredKinds {
		body := []byte("signed-test-artifact-" + string(kind))
		sum := sha256.Sum256(body)
		ref := Reference{Kind: kind, ID: "fixture:" + string(kind), SHA256: hex.EncodeToString(sum[:])}
		verified := Verified{
			Reference: ref, Signer: "test-authority", TenantID: "partner-tenant",
			WindowStart: start, WindowEnd: end, ObservedAt: end,
		}
		if kind == ScopeDecision {
			verified.Owner, verified.DisplacedWork, verified.MinimumRate = "test-human-owner", "test-displaced-scope", 0.25
			verified.Budgets = make(map[string]MetricBudget, len(requiredMetrics))
			for _, name := range requiredMetrics {
				verified.Budgets[name] = MetricBudget{Limit: 20, Unit: "test-units"}
			}
		}
		if kind == ServedUse {
			verified.Numerator, verified.Denominator = 8, 10
		}
		if kind == Adoption {
			verified.Numerator, verified.Denominator = 5, 10
		}
		if kind == SLODashboard {
			for _, name := range requiredMetrics {
				verified.Metrics = append(verified.Metrics, Metric{Name: name, Samples: 100, Observed: 10, Limit: 20, Unit: "test-units"})
			}
		}
		if kind == RollbackDrill {
			verified.RollbackObserved = true
		}
		packet.Evidence = append(packet.Evidence, ref)
		facts[kind] = verified
	}
	return packet, facts
}

func hasReason(decision Decision, fragment string) bool {
	for _, reason := range decision.Reasons {
		if strings.Contains(reason, fragment) {
			return true
		}
	}
	return false
}
