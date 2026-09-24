package replan

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/fielddiff"
)

func drift(input string) FieldDiff {
	return FieldDiff{Input: input, Outcome: fielddiff.Outcome{Relation: fielddiff.RelationExternalAhead, Reason: fielddiff.ReasonExternalNewer, Ordered: true}}
}

func match(input string) FieldDiff {
	return FieldDiff{Input: input, Outcome: fielddiff.Outcome{Relation: fielddiff.RelationMatch, Reason: fielddiff.ReasonValuesEqual}}
}

func TestTodo_REPLAN_003(t *testing.T) {
	declaration := Declaration{
		ProposalRevisionID: "proposal:1",
		Components: []Component{
			{ID: "leave-eligibility", Dependencies: []Dependency{{Input: "balance", Classification: ClassificationMaterial}}},
			{ID: "evidence-review", Dependencies: []Dependency{{Input: "evidence", Classification: ClassificationMaterial}}},
			{ID: "notice", Dependencies: []Dependency{{Input: "balance", Classification: ClassificationInformative}}},
		},
	}
	result, err := Compute(declaration, []FieldDiff{drift("balance"), match("evidence"), drift("unknown")})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Invalidated, []string{"leave-eligibility"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("invalidated=%v, want %v", got, want)
	}
	if len(result.UnboundDrift) != 1 || result.UnboundDrift[0] != "unknown" {
		t.Fatalf("unbound drift=%v", result.UnboundDrift)
	}
	if len(result.Explain()) != 1 || result.Explain()[0].DriftedInputs[0] != "balance" {
		t.Fatalf("explain=%v", result.Explain())
	}
	if result.Digest == "" {
		t.Fatal("missing stable digest")
	}
}

func TestTodo_REPLAN_003_Property(t *testing.T) {
	declaration := Declaration{Components: []Component{
		{ID: "a", Dependencies: []Dependency{{Input: "changed", Classification: ClassificationMaterial}}},
		{ID: "b", Dependencies: []Dependency{{Input: "steady", Classification: ClassificationMaterial}}},
	}}
	result, err := Compute(declaration, []FieldDiff{drift("changed")})
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range result.Findings {
		if finding.ComponentID == "b" && finding.Invalidated {
			t.Fatalf("component without drift was invalidated: %+v", finding)
		}
	}
	permuted := Declaration{Components: []Component{declaration.Components[1], declaration.Components[0]}}
	other, err := Compute(permuted, []FieldDiff{drift("changed")})
	if err != nil || result.Digest != other.Digest {
		t.Fatalf("declaration order changed digest: %q != %q (err=%v)", result.Digest, other.Digest, err)
	}
}

func TestTodo_REPLAN_003_Race(t *testing.T) {
	declaration := Declaration{Components: []Component{{ID: "component", Dependencies: []Dependency{{Input: "x", Classification: ClassificationMaterial}}}}}
	diffs := []FieldDiff{drift("x")}
	const workers, iterations = 8, 25
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < iterations; i++ {
				result, err := Compute(declaration, diffs)
				if err != nil {
					errs <- err
					return
				}
				if result.Digest == "" || len(result.Invalidated) != 1 || result.Invalidated[0] != "component" {
					errs <- fmt.Errorf("worker %d run %d result=%+v", worker, i, result)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestTodo_REPLAN_003_Mutation(t *testing.T) {
	declaration := Declaration{Components: []Component{{ID: "component", Dependencies: []Dependency{{Input: "x", Classification: ClassificationMaterial}}}}}
	if _, err := Compute(declaration, []FieldDiff{{Input: "x", Outcome: fielddiff.Outcome{}}}); !errors.Is(err, ErrInvalidDiff) {
		t.Fatalf("invalid outcome error=%v", err)
	}
	if _, err := Compute(Declaration{Components: []Component{{ID: "component", Dependencies: []Dependency{{Input: "x", Classification: Classification("future")}}}}}, nil); !errors.Is(err, ErrInvalidDeclaration) {
		t.Fatalf("unknown classification error=%v", err)
	}
}

func TestReplanRejectsDuplicateInputWithDifferentOutcome(t *testing.T) {
	declaration := Declaration{Components: []Component{{ID: "component", Dependencies: []Dependency{{Input: "x", Classification: ClassificationMaterial}}}}}
	first := drift("x")
	second := first
	second.Outcome.Relation = fielddiff.RelationCanonicalAhead
	second.Outcome.Reason = fielddiff.ReasonCanonicalNewer
	if _, err := Compute(declaration, []FieldDiff{first, second}); !errors.Is(err, ErrInvalidDiff) {
		t.Fatalf("duplicate diff error=%v", err)
	}
}
