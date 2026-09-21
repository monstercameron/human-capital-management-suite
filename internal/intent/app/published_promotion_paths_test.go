package app

import (
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// proposedBaseWithin returns an exact proposed base for current that the
// option's published rule admits: the published minimum increase when the
// edge declares bounds, and ten percent for an unbounded upward edge.
func proposedBaseWithin(t *testing.T, current string, option workspace.PromotionPathOption) string {
	t.Helper()
	base, ok := new(big.Rat).SetString(current)
	if !ok {
		t.Fatalf("current base %q", current)
	}
	increase := big.NewRat(10, 100)
	if option.MinimumBaseIncrease != "" {
		if increase, ok = new(big.Rat).SetString(option.MinimumBaseIncrease); !ok {
			t.Fatalf("minimum increase %q", option.MinimumBaseIncrease)
		}
	}
	return new(big.Rat).Mul(base, new(big.Rat).Add(big.NewRat(1, 1), increase)).FloatString(2)
}

// TestPublishedPromotionPathsListedAreAcceptedByTheLadderGate is PROMOUX-015's
// unit-level drift test: every path workforceOptions publishes is accepted by
// validatePublishedPromotionPath for its own source profile, and the two read
// the same list, so neither can grow an edge the other lacks. The integration
// half, against ListWorkers and Propose on the composed server, is
// TestPromotionPublishedPathsAreAllProposable in internal/application.
func TestPublishedPromotionPathsListedAreAcceptedByTheLadderGate(t *testing.T) {
	options, err := workforceOptions()
	if err != nil {
		t.Fatalf("workforceOptions: %v", err)
	}
	fixturePaths, err := fixtures.PromotionPaths()
	if err != nil {
		t.Fatalf("fixtures.PromotionPaths: %v", err)
	}
	demoEdges := demoworkforce.PromotionPaths()
	if len(demoEdges) == 0 || len(fixturePaths) == 0 {
		t.Fatal("the fixture must publish both job-architecture and demo ladder edges, or this test proves nothing")
	}
	if len(options.PromotionPaths) != len(fixturePaths)+len(demoEdges) {
		t.Fatalf("published %d paths, want %d fixture + %d demo", len(options.PromotionPaths), len(fixturePaths), len(demoEdges))
	}
	baseline := journeyBaselineFacts{currentBase: "112000.00", currency: "USD"}
	for _, option := range options.PromotionPaths {
		// Every published edge states the range the gate will hold a
		// proposal to, so a form never has to say none is available.
		if option.MinimumBaseIncrease == "" || option.MaximumBaseIncrease == "" {
			t.Errorf("published path %s carries no base-increase bounds", option.PathRef)
		}
		current := journeyCurrent{jobCode: option.SourceJobCode, grade: option.SourceGrade}
		in := workspace.ProposalInput{
			TargetJobCode: option.TargetJobCode, TargetGrade: option.TargetGrade,
			ProposedBase: proposedBaseWithin(t, baseline.currentBase, option),
		}
		if err := validatePublishedPromotionPath(current, in, baseline); err != nil {
			t.Errorf("published path %s %s/%s -> %s/%s refused by the ladder gate: %v", option.PathRef,
				option.SourceJobCode, option.SourceGrade, option.TargetJobCode, option.TargetGrade, err)
		}
	}

	t.Run("an unpublished target is refused", func(t *testing.T) {
		edge := demoEdges[0]
		err := validatePublishedPromotionPath(journeyCurrent{jobCode: edge.SourceJobCode, grade: edge.SourceGrade},
			workspace.ProposalInput{TargetJobCode: "EXEC-CEO", TargetGrade: "E7", ProposedBase: "120000.00"}, baseline)
		if !errors.Is(err, workspace.ErrJourneyInput) || !strings.Contains(err.Error(), "not a published next step") {
			t.Fatalf("unpublished target = %v, want the published-next-step input refusal", err)
		}
	})
	t.Run("an upward demo edge refuses a lower base", func(t *testing.T) {
		edge := demoEdges[0]
		err := validatePublishedPromotionPath(journeyCurrent{jobCode: edge.SourceJobCode, grade: edge.SourceGrade},
			workspace.ProposalInput{TargetJobCode: edge.TargetJobCode, TargetGrade: edge.TargetGrade, ProposedBase: "100000.00"}, baseline)
		if !errors.Is(err, workspace.ErrJourneyInput) || !strings.Contains(err.Error(), "proposed_base") {
			t.Fatalf("lower base on a demo edge = %v, want a proposed_base input refusal", err)
		}
	})
	t.Run("a job-architecture edge still enforces its published bounds", func(t *testing.T) {
		scope := fixturePaths[0]
		err := validatePublishedPromotionPath(journeyCurrent{jobCode: scope.SourceJobCode, grade: scope.SourceGrade},
			workspace.ProposalInput{TargetJobCode: scope.TargetJobCode, TargetGrade: scope.TargetGrade, ProposedBase: "112000.00"}, baseline)
		if !errors.Is(err, workspace.ErrJourneyInput) || !strings.Contains(err.Error(), "between") {
			t.Fatalf("zero increase on a bounded edge = %v, want the bounds refusal", err)
		}
	})
	t.Run("a malformed proposed base is refused", func(t *testing.T) {
		edge := demoEdges[0]
		err := validatePublishedPromotionPath(journeyCurrent{jobCode: edge.SourceJobCode, grade: edge.SourceGrade},
			workspace.ProposalInput{TargetJobCode: edge.TargetJobCode, TargetGrade: edge.TargetGrade, ProposedBase: "a lot"}, baseline)
		if !errors.Is(err, workspace.ErrJourneyInput) {
			t.Fatalf("malformed base = %v, want an input refusal", err)
		}
	})
}
