package onboarding_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/onboarding"
)

type cutoverSigner struct{ fail bool }

func (cutoverSigner) Authority() string { return "authority.test/cutover/v1" }
func (s cutoverSigner) Sign(p []byte) (string, error) {
	if s.fail {
		return "", errors.New("hsm offline")
	}
	sum := sha256.Sum256(append([]byte("cutover-key:"), p...))
	return "sig:" + hex.EncodeToString(sum[:]), nil
}
func (s cutoverSigner) Verify(p []byte, sig string) error {
	want, _ := cutoverSigner{}.Sign(p)
	if want != sig {
		return errors.New("bad signature")
	}
	return nil
}

var cutoverAt = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

func readyCutover() onboarding.CutoverEvidence {
	return onboarding.CutoverEvidence{
		Tenant: "acme", Domain: "people.employment", SourceAuthority: "authority:legacy-hris", TargetAuthority: "authority:hcmnext",
		Freeze:               onboarding.FreezeRecord{Token: "freeze-1", SourceVersion: "src-v42", FrozenAt: cutoverAt.Add(-2 * time.Hour)},
		CurrentSourceVersion: "src-v42", TargetWriters: []string{"writer:cutover"}, CutoverWriter: "writer:cutover",
		PriorEpoch: 3, CurrentEpoch: 3,
		Delta:            onboarding.DeltaRecord{BaseVersion: "src-v40", ThroughVersion: "src-v42", CommitDigest: "sha256:commit", Complete: true},
		SimulationDigest: "sha256:sim", SimulationZeroEffect: true, ApprovedSimulationDigest: "sha256:sim",
		ApprovedBy: "principal:cio", RequestedBy: "principal:migration-lead",
		ReplicationLag: 3 * time.Second, MaxReplicationLag: time.Minute, DecidedAt: cutoverAt,
	}
}

// TestTodo_ONBOARD_005 proves a cutover with every gate satisfied signs the
// next authority epoch bound to the freeze, delta, simulation and approval,
// and that a source change, a stale epoch or a dual writer blocks it with a
// safe abort that leaves authority with the source.
func TestTodo_ONBOARD_005(t *testing.T) {
	decision, err := onboarding.DecideCutover(readyCutover(), cutoverSigner{})
	if err != nil {
		t.Fatal(err)
	}
	e := decision.Epoch
	if decision.Outcome != onboarding.CutoverEpochSigned || e == nil || e.Epoch != 4 || decision.Authority != "authority:hcmnext" ||
		e.FrozenSourceVersion != "src-v42" || e.DeltaCommitDigest != "sha256:commit" || e.SimulationDigest != "sha256:sim" {
		t.Fatalf("decision = %+v", decision)
	}
	if err := onboarding.VerifyEpoch(*e, cutoverSigner{}); err != nil {
		t.Fatalf("verify epoch: %v", err)
	}
	tampered := *e
	tampered.ToAuthority = "authority:attacker"
	if onboarding.VerifyEpoch(tampered, cutoverSigner{}) == nil {
		t.Fatal("a tampered epoch verified")
	}

	for name, tc := range map[string]struct {
		mutate func(*onboarding.CutoverEvidence)
		gate   string
	}{
		"source changed":        {func(e *onboarding.CutoverEvidence) { e.CurrentSourceVersion = "src-v43" }, onboarding.GateSourceStable},
		"source written":        {func(e *onboarding.CutoverEvidence) { e.SourceWritesSinceFreeze = 1 }, onboarding.GateSourceStable},
		"stale epoch":           {func(e *onboarding.CutoverEvidence) { e.CurrentEpoch = 4 }, onboarding.GateEpoch},
		"dual writer":           {func(e *onboarding.CutoverEvidence) { e.TargetWriters = append(e.TargetWriters, "writer:hr-portal") }, onboarding.GateDualWriter},
		"no freeze":             {func(e *onboarding.CutoverEvidence) { e.Freeze.Token = "" }, onboarding.GateFreeze},
		"partial delta":         {func(e *onboarding.CutoverEvidence) { e.Delta.Pending = 2; e.Delta.Complete = false }, onboarding.GateDelta},
		"delta short of freeze": {func(e *onboarding.CutoverEvidence) { e.Delta.ThroughVersion = "src-v41" }, onboarding.GateDelta},
		"validation":            {func(e *onboarding.CutoverEvidence) { e.ValidationErrors = 1 }, onboarding.GateValidation},
		"effectful simulation":  {func(e *onboarding.CutoverEvidence) { e.SimulationZeroEffect = false }, onboarding.GateSimulation},
		"other approval":        {func(e *onboarding.CutoverEvidence) { e.ApprovedSimulationDigest = "sha256:old" }, onboarding.GateApproval},
		"self approval":         {func(e *onboarding.CutoverEvidence) { e.ApprovedBy = "PRINCIPAL:MIGRATION-LEAD" }, onboarding.GateApproval},
		"lag":                   {func(e *onboarding.CutoverEvidence) { e.ReplicationLag = 2 * time.Minute }, onboarding.GateLag},
		"reconciliation":        {func(e *onboarding.CutoverEvidence) { e.ReconciliationMismatches = 3 }, onboarding.GateReconciliation},
	} {
		ev := readyCutover()
		tc.mutate(&ev)
		d, err := onboarding.DecideCutover(ev, cutoverSigner{})
		if err != nil || d.Outcome != onboarding.CutoverAborted || d.FailedGate != tc.gate || d.Epoch != nil || d.Authority != "authority:legacy-hris" {
			t.Errorf("%s: %+v, %v (want abort at %s)", name, d, err, tc.gate)
		}
	}
}

// TestTodo_ONBOARD_005_Mutation proves the gate order is the declared order
// and each gate is load-bearing: violating every gate at once reports the
// first, and removing violations one at a time walks the gates in order until
// the epoch is signed.
func TestTodo_ONBOARD_005_Mutation(t *testing.T) {
	breaks := []func(*onboarding.CutoverEvidence){
		func(e *onboarding.CutoverEvidence) { e.Freeze.FrozenAt = time.Time{} },
		func(e *onboarding.CutoverEvidence) { e.CurrentSourceVersion = "src-v99" },
		func(e *onboarding.CutoverEvidence) { e.TargetWriters = []string{"writer:rogue"} },
		func(e *onboarding.CutoverEvidence) { e.CurrentEpoch = 9 },
		func(e *onboarding.CutoverEvidence) { e.Delta.Failed = 1 },
		func(e *onboarding.CutoverEvidence) { e.ValidationErrors = 2 },
		func(e *onboarding.CutoverEvidence) { e.SimulationDigest = "" },
		func(e *onboarding.CutoverEvidence) { e.ApprovedBy = "" },
		func(e *onboarding.CutoverEvidence) { e.MaxReplicationLag = 0 },
		func(e *onboarding.CutoverEvidence) { e.ReconciliationMismatches = 1 },
	}
	gates := onboarding.CutoverGates()
	if len(gates) != len(breaks) {
		t.Fatalf("%d gates but %d breaks", len(gates), len(breaks))
	}
	for fixed := 0; fixed <= len(breaks); fixed++ {
		ev := readyCutover()
		for i := fixed; i < len(breaks); i++ {
			breaks[i](&ev)
		}
		d, err := onboarding.DecideCutover(ev, cutoverSigner{})
		if err != nil {
			t.Fatal(err)
		}
		if fixed == len(breaks) {
			if d.Outcome != onboarding.CutoverEpochSigned {
				t.Fatalf("all gates fixed = %+v", d)
			}
			continue
		}
		if d.FailedGate != gates[fixed] {
			t.Fatalf("with gates 0..%d fixed, failed gate = %s, want %s", fixed-1, d.FailedGate, gates[fixed])
		}
	}
	if _, err := onboarding.DecideCutover(readyCutover(), cutoverSigner{fail: true}); !errors.Is(err, onboarding.ErrCutoverSigning) {
		t.Fatalf("signer failure = %v", err)
	}
	if _, err := onboarding.DecideCutover(readyCutover(), nil); !errors.Is(err, onboarding.ErrCutoverSigning) {
		t.Fatalf("nil signer = %v", err)
	}
	if onboarding.VerifyEpoch(onboarding.AuthorityEpoch{}, nil) == nil {
		t.Fatal("nil verifier accepted")
	}
	same := readyCutover()
	same.TargetAuthority = same.SourceAuthority
	if d, _ := onboarding.DecideCutover(same, cutoverSigner{}); d.FailedGate != onboarding.GateFreeze {
		t.Fatalf("self-transfer = %+v", d)
	}
	noWriter := readyCutover()
	noWriter.CutoverWriter, noWriter.TargetWriters = "", nil
	if d, _ := onboarding.DecideCutover(noWriter, cutoverSigner{}); d.FailedGate != onboarding.GateDualWriter || !strings.Contains(d.Reason, "writer") {
		t.Fatalf("unnamed cutover writer = %+v", d)
	}
	early := readyCutover()
	early.DecidedAt = early.Freeze.FrozenAt.Add(-time.Second)
	if d, _ := onboarding.DecideCutover(early, cutoverSigner{}); d.FailedGate != onboarding.GateFreeze {
		t.Fatalf("decision before freeze = %+v", d)
	}
}
