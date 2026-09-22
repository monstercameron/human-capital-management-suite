package workflow_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simassign"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcomp"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// TestTodo_WF_REV_003 proves the PRIMARY contract: the proposal effect
// items, the compiled node's cancel class and EffectRecord.Reversible all
// derive from simassign's single reversibility declaration. For every
// declared promotion effect the test projects the declaration through the
// real proposal shape (intent.PlannedEffect plus the plan's outbox,
// compensation and observation bindings), derives the compiled node's
// cancellation semantics from the declared compensation ref, and asserts
// the cancellation verdict agrees with the proposal: REVERSIBLE reverts,
// COMPENSATABLE compensates.
func TestTodo_WF_REV_003(t *testing.T) {
	revs := simassign.PromotionReversals()
	if len(revs) != 5 {
		t.Fatalf("declared reversals = %d, want the 5 promotion effect kinds", len(revs))
	}
	// The compensation half of the simulation union declares no second
	// table: both of its kinds resolve through the same declaration.
	for _, kind := range simcomp.EffectKinds() {
		if _, ok := simassign.ReversalFor(kind); !ok {
			t.Fatalf("simcomp kind %q resolves to no reversal: a second declaration exists", kind)
		}
	}
	for _, rev := range revs {
		t.Run(string(rev.Kind), func(t *testing.T) {
			// CompensationStrategy rides beside CompensationRef the way the
			// shared effect constructors fill it: from the same declaration.
			proposed := simassign.ProposedEffect{
				EffectID: "effect:" + string(rev.Kind), Kind: rev.Kind,
				DestinationRef: "dest/" + string(rev.Kind),
				Reversibility:  rev.Reversibility, CompensationRef: rev.CompensationRef,
				CompensationStrategy: rev.CompensationStrategy,
				ObservationRef:       rev.ObservationRef,
			}
			planned := proposed.PlannedEffect()
			if planned.Reversibility != string(rev.Reversibility) ||
				planned.CompensationRef != rev.CompensationRef ||
				planned.ObservationRef != rev.ObservationRef {
				t.Fatalf("proposal item = %+v, want the declared reversal", planned)
			}
			outbox := proposed.OutboxEffect()
			if outbox.Reversibility != string(rev.Reversibility) {
				t.Fatalf("outbox effect reversibility = %q, want %q", outbox.Reversibility, rev.Reversibility)
			}
			binding := proposed.Compensation()
			if binding.Strategy != rev.CompensationStrategy {
				t.Fatalf("compensation strategy = %q, want %q", binding.Strategy, rev.CompensationStrategy)
			}
			observation := proposed.Observation(values.NewInstant(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)))
			if observation.ObservationRef != rev.ObservationRef {
				t.Fatalf("observation ref = %q, want %q", observation.ObservationRef, rev.ObservationRef)
			}

			node := workflow.CompiledNode{
				ID: "node:" + string(rev.Kind), EffectClass: capability.EffectInternalMutation,
				CompensationRef: &workflow.ResolvedReference{
					Kind: workflow.RefCompensation, ID: rev.CompensationRef, Version: "1",
				},
			}
			sem, err := workflow.CancellationSemanticsOf(node)
			if err != nil {
				t.Fatalf("CancellationSemanticsOf: %v", err)
			}
			if sem.Class != workflow.CancelCompensable || sem.Compensation != rev.CompensationRef+"@1" {
				t.Fatalf("node cancel class = %+v, want COMPENSABLE %s@1", sem, rev.CompensationRef)
			}
			rec, ok := sem.Effect("node:"+string(rev.Kind)+"#1", true)
			if !ok {
				t.Fatal("a settled compensable node produced no effect record")
			}
			if want := rev.Reversibility == simassign.Reversible; rec.Reversible != want {
				t.Fatalf("EffectRecord.Reversible = %t, want %t for %s", rec.Reversible, want, rev.Reversibility)
			}
			outcome, err := workflow.DecideCancellation(workflow.CancellationRequest{
				RunID: "run-wf-rev-003", Revision: "rev-1", Phase: "RUNNING@execute",
				Effects: []workflow.EffectRecord{rec},
			})
			if err != nil {
				t.Fatalf("DecideCancellation: %v", err)
			}
			var wantDisposition string
			if rev.Reversibility == simassign.Reversible {
				wantDisposition = "REVERTED"
			} else {
				wantDisposition = "COMPENSATE:" + rev.CompensationRef + "@1"
			}
			if len(outcome.Effects) != 1 || outcome.Effects[0].Disposition != wantDisposition {
				t.Fatalf("verdict = %+v, want disposition %q: proposal and cancellation disagree",
					outcome.Effects, wantDisposition)
			}
		})
	}
}

// TestTodo_WF_REV_003_Property proves the agreement holds for every plan,
// not just the promotion's: for each declared reversal, in both settled and
// unsettled states, the proposal's reversibility class and the cancellation
// verdict can never disagree. Unsettled effects always repair; settled
// REVERSIBLE effects always revert; settled COMPENSATABLE effects always
// name their declared compensation. An undeclared kind and an
// uncompensatable write keep their existing fail-closed verdicts.
func TestTodo_WF_REV_003_Property(t *testing.T) {
	for _, rev := range simassign.PromotionReversals() {
		for _, settled := range []bool{true, false} {
			name := fmt.Sprintf("%s/settled=%t", rev.Kind, settled)
			t.Run(name, func(t *testing.T) {
				node := workflow.CompiledNode{
					ID: "n", EffectClass: capability.EffectInternalMutation,
					CompensationRef: &workflow.ResolvedReference{
						Kind: workflow.RefCompensation, ID: rev.CompensationRef, Version: "7",
					},
				}
				sem, err := workflow.CancellationSemanticsOf(node)
				if err != nil {
					t.Fatalf("CancellationSemanticsOf: %v", err)
				}
				rec, ok := sem.Effect("n#1", settled)
				if !ok {
					t.Fatal("a write node produced no effect record")
				}
				outcome, err := workflow.DecideCancellation(workflow.CancellationRequest{
					RunID: "r", Revision: "v", Phase: "p",
					Effects: []workflow.EffectRecord{rec},
				})
				if err != nil {
					t.Fatalf("DecideCancellation: %v", err)
				}
				got := outcome.Effects[0].Disposition
				switch {
				case !settled && (got != "AMBIGUOUS" || outcome.Decision != workflow.RepairRequired):
					t.Fatalf("unsettled %s = %q/%s, want AMBIGUOUS/REPAIR_REQUIRED", rev.Kind, got, outcome.Decision)
				case settled && rev.Reversibility == simassign.Reversible &&
					(got != "REVERTED" || outcome.Decision != workflow.Cancelled):
					t.Fatalf("settled %s = %q/%s, want REVERTED/CANCELLED", rev.Kind, got, outcome.Decision)
				case settled && rev.Reversibility == simassign.Compensatable &&
					(got != "COMPENSATE:"+rev.CompensationRef+"@7" || outcome.Decision != workflow.CompensationRequired):
					t.Fatalf("settled %s = %q/%s, want COMPENSATE/ COMPENSATION_REQUIRED", rev.Kind, got, outcome.Decision)
				}
			})
		}
	}

	t.Run("undeclared kind has no reversal", func(t *testing.T) {
		if _, ok := simassign.ReversalFor(simassign.EffectKind("promotion.nothing.revision")); ok {
			t.Fatal("an undeclared kind resolved to a reversal: effects can be simulated with no undo contract")
		}
	})

	t.Run("uncompensatable write stays irreversible", func(t *testing.T) {
		node := workflow.CompiledNode{ID: "pay", EffectClass: capability.EffectExternalMutation}
		sem, err := workflow.CancellationSemanticsOf(node)
		if err != nil || sem.Class != workflow.CancelIrreversible {
			t.Fatalf("semantics = %+v, %v", sem, err)
		}
		rec, ok := sem.Effect("pay#1", true)
		if !ok || rec.Reversible || rec.Compensation != "" {
			t.Fatalf("record = %+v, %v", rec, ok)
		}
		outcome, err := workflow.DecideCancellation(workflow.CancellationRequest{
			RunID: "r", Revision: "v", Phase: "p", Effects: []workflow.EffectRecord{rec},
		})
		if err != nil {
			t.Fatalf("DecideCancellation: %v", err)
		}
		if outcome.Effects[0].Disposition != "IRREVERSIBLE" || outcome.Decision != workflow.CannotCancel {
			t.Fatalf("outcome = %+v", outcome)
		}
	})

	t.Run("declaration covers both undo classes", func(t *testing.T) {
		seen := map[simassign.Reversibility]bool{}
		for _, rev := range simassign.PromotionReversals() {
			seen[rev.Reversibility] = true
		}
		if !seen[simassign.Reversible] || !seen[simassign.Compensatable] {
			t.Fatalf("classes = %v, want both REVERSIBLE and COMPENSATABLE declared", seen)
		}
	})
}

// TestTodo_WF_REV_003_Golden pins the promotion effect items: the exact
// undo contract every proposal, plan and cancellation derives from. Any
// change to a kind, class, compensation or observation fails here first,
// forcing the GOLDEN update to be reviewed rather than silent.
func TestTodo_WF_REV_003_Golden(t *testing.T) {
	var lines []string
	for _, rev := range simassign.PromotionReversals() {
		lines = append(lines, fmt.Sprintf("kind=%s|reversibility=%s|compensation=%s|strategy=%s|observation=%s",
			rev.Kind, rev.Reversibility, rev.CompensationRef, rev.CompensationStrategy, rev.ObservationRef))
	}
	sort.Strings(lines)
	rendered := strings.Join(lines, "\n") + "\n"
	sum := sha256.Sum256([]byte(rendered))
	if got := hex.EncodeToString(sum[:]); got != wfRev003GoldenDigest {
		t.Fatalf("reversibility declaration digest = %s, want %s\nrendered:\n%s", got, wfRev003GoldenDigest, rendered)
	}
}

const wfRev003GoldenDigest = "39a0e90d5efa4adb5010e7ec011cf0b3e90ddc1a0e4085810dad196656d81dd0"
