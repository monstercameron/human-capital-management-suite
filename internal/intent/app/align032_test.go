package app

import (
	"errors"
	"sync"
	"testing"
)

func align032Request() CorrectionRequest {
	return CorrectionRequest{
		Tenant: align030Tenant, IntentID: "intent-1",
		ProposalRevisionID: "proposal-1", ProposalDigest: "sha256:proposal",
		Kind: CorrectionRepairPlan, Reason: "recalculate the payroll effect",
		RequestedBy: "principal-admin", RequestedAt: align030Now,
	}
}

// TestTodo_ALIGN_032 proves governed correction and repair routes: a
// correction against the current revision digest routes by kind, a
// superseded revision routes to rebase, and altered content is refused.
func TestTodo_ALIGN_032(t *testing.T) {
	routes := map[string]string{
		CorrectionCorrectField: RouteCorrect,
		CorrectionRepairPlan:   RouteRepair,
		CorrectionWithdraw:     RouteWithdraw,
	}
	for kind, route := range routes {
		req := align032Request()
		req.Kind = kind
		decision, err := RouteCorrection(req, align030Tenant, "proposal-1", "sha256:proposal", align030Now)
		if err != nil {
			t.Fatalf("RouteCorrection(%s): %v", kind, err)
		}
		if !decision.Allowed || decision.Route != route {
			t.Fatalf("RouteCorrection(%s) = %+v, want allowed onto %s", kind, decision, route)
		}
		if err := decision.VerifyDecision(); err != nil {
			t.Fatalf("VerifyDecision(%s): %v", kind, err)
		}
	}
	// A superseded revision is not applied: the only governed route is
	// rebase.
	stale := align032Request()
	decision, err := RouteCorrection(stale, align030Tenant, "proposal-2", "sha256:proposal-2", align030Now)
	if err != nil {
		t.Fatalf("RouteCorrection(superseded): %v", err)
	}
	if decision.Allowed || decision.Route != RouteRebase {
		t.Fatalf("RouteCorrection(superseded) = %+v, want denied onto rebase", decision)
	}
	// Content altered under a known revision is tampering, not a correction.
	tampered := align032Request()
	tampered.ProposalDigest = "sha256:forged"
	if _, err := RouteCorrection(tampered, align030Tenant, "proposal-1", "sha256:proposal", align030Now); !errors.Is(err, ErrCorrectionTampered) {
		t.Fatalf("RouteCorrection(tampered) = %v, want ErrCorrectionTampered", err)
	}
}

func TestTodo_ALIGN_032_Property(t *testing.T) {
	first, err := RouteCorrection(align032Request(), align030Tenant, "proposal-1", "sha256:proposal", align030Now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RouteCorrection(align032Request(), align030Tenant, "proposal-1", "sha256:proposal", align030Now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("routing is not deterministic: %s != %s", first.Digest, second.Digest)
	}
}

func TestTodo_ALIGN_032_Golden(t *testing.T) {
	decision, err := RouteCorrection(align032Request(), align030Tenant, "proposal-1", "sha256:proposal", align030Now)
	if err != nil {
		t.Fatalf("RouteCorrection: %v", err)
	}
	const wantDigest = "sha256:ba204c1cf9936d5148c19f903913d7b436efa8097a63d463409143cc29fe7431"
	if decision.Digest != wantDigest {
		t.Fatalf("correction digest=%q want=%q", decision.Digest, wantDigest)
	}
}

func TestTodo_ALIGN_032_Security(t *testing.T) {
	// Corrections never cross tenants.
	if _, err := RouteCorrection(align032Request(), "vendor", "proposal-1", "sha256:proposal", align030Now); !errors.Is(err, ErrCorrectionInvalid) {
		t.Fatalf("RouteCorrection(foreign tenant) = %v, want ErrCorrectionInvalid", err)
	}
	// Unknown kinds are not corrections.
	unknown := align032Request()
	unknown.Kind = "rewrite_history"
	if _, err := RouteCorrection(unknown, align030Tenant, "proposal-1", "sha256:proposal", align030Now); !errors.Is(err, ErrCorrectionInvalid) {
		t.Fatalf("RouteCorrection(unknown kind) = %v, want ErrCorrectionInvalid", err)
	}
	// A reason is required: unexplained corrections are not governed.
	silent := align032Request()
	silent.Reason = ""
	if _, err := RouteCorrection(silent, align030Tenant, "proposal-1", "sha256:proposal", align030Now); !errors.Is(err, ErrCorrectionInvalid) {
		t.Fatalf("RouteCorrection(no reason) = %v, want ErrCorrectionInvalid", err)
	}
}

func TestTodo_ALIGN_032_Integration(t *testing.T) {
	plan := align030Plan(t, "idem-1", "plan-1", align030DefaultHeads())
	binding, err := BindAcceptedAction(align030Action(), plan)
	if err != nil {
		t.Fatalf("BindAcceptedAction: %v", err)
	}
	// A repair targeting exactly the bound revision and digest routes onto
	// the repair route; the binding and the correction agree on what is
	// current.
	decision, err := RouteCorrection(align032Request(), align030Tenant, binding.ProposalRevisionID, binding.ProposalDigest, align030Now)
	if err != nil {
		t.Fatalf("RouteCorrection(bound): %v", err)
	}
	if !decision.Allowed || decision.Route != RouteRepair {
		t.Fatalf("RouteCorrection(bound) = %+v, want allowed onto repair", decision)
	}
	// A correction minted before a re-preparation no longer targets current
	// content and is refused as tampering.
	if _, err := RouteCorrection(align032Request(), align030Tenant, binding.ProposalRevisionID, "sha256:reprepared", align030Now); !errors.Is(err, ErrCorrectionTampered) {
		t.Fatalf("RouteCorrection(reprepared) = %v, want ErrCorrectionTampered", err)
	}
}

func TestTodo_ALIGN_032_Fault(t *testing.T) {
	// Sixteen concurrent routings of the same request agree exactly.
	const routers = 16
	var wg sync.WaitGroup
	digests := make([]string, routers)
	errs := make([]error, routers)
	for i := 0; i < routers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			decision, err := RouteCorrection(align032Request(), align030Tenant, "proposal-1", "sha256:proposal", align030Now)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = decision.Digest
		}(i)
	}
	wg.Wait()
	for i := 0; i < routers; i++ {
		if errs[i] != nil {
			t.Fatalf("concurrent route %d: %v", i, errs[i])
		}
		if digests[i] != digests[0] {
			t.Fatalf("concurrent route %d diverged: %s != %s", i, digests[i], digests[0])
		}
	}
	// An empty current revision and a zero decision instant are invalid
	// before any routing.
	if _, err := RouteCorrection(align032Request(), align030Tenant, "", "", align030Now); !errors.Is(err, ErrCorrectionInvalid) {
		t.Fatalf("RouteCorrection(empty current) = %v, want ErrCorrectionInvalid", err)
	}
}

func TestTodo_ALIGN_032_Conformance(t *testing.T) {
	if got := ExplainCorrection(); got == "" {
		t.Fatal("correction routes have no explanation")
	}
	// The rebase refusal is itself a verifiable governed decision, and it
	// digests differently from an allowed routing of the same request.
	stale := align032Request()
	rebase, err := RouteCorrection(stale, align030Tenant, "proposal-2", "sha256:proposal-2", align030Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := rebase.VerifyDecision(); err != nil {
		t.Fatalf("VerifyDecision(rebase): %v", err)
	}
	allowed, err := RouteCorrection(align032Request(), align030Tenant, "proposal-1", "sha256:proposal", align030Now)
	if err != nil {
		t.Fatal(err)
	}
	if rebase.Digest == allowed.Digest {
		t.Fatal("rebase and allowed decisions share a digest")
	}
}

func FuzzTodo_ALIGN_032_Fuzz(f *testing.F) {
	f.Add("repair_plan", "a reason")
	f.Fuzz(func(t *testing.T, kind, reason string) {
		req := align032Request()
		req.Kind = kind
		req.Reason = reason
		first, firstErr := RouteCorrection(req, align030Tenant, "proposal-1", "sha256:proposal", align030Now)
		second, secondErr := RouteCorrection(req, align030Tenant, "proposal-1", "sha256:proposal", align030Now)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("routing is not deterministic: %v vs %v", firstErr, secondErr)
		}
		if firstErr == nil && first.Digest != second.Digest {
			t.Fatalf("routing digest is not deterministic: %s != %s", first.Digest, second.Digest)
		}
	})
}
