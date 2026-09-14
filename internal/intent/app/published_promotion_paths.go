package app

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PROMOUX-015: one source of published promotion paths.
//
// ListWorkers published the fixed corpus's job-architecture paths plus the
// demo company's ladder (PROMOUX-001), but Propose's ladder gate read only
// fixtures.PromotionPaths, so every one of the 29 demo ladder edges a client
// was offered was refused on submit as "not a published next step". Both now
// read [publishedPromotionPaths], so an option cannot be listed without being
// accepted, or accepted without being listed.

// publishedPromotionPath is one published edge: the option clients are shown
// and the base-increase rule the ladder gate enforces for it.
type publishedPromotionPath struct {
	Option workspace.PromotionPathOption
	// bounds is the job-architecture edge whose published minimum and maximum
	// base increase apply. It is nil for a demo ladder edge, which publishes
	// no bounds: its only published rule is its UPWARD kind, and the gate
	// enforces exactly that ([allowsBaseIncrease]).
	bounds *fixtures.PromotionPathScope
}

// publishedPromotionPaths returns the fixed corpus's validated
// job-architecture paths followed by the demo company's ladder, in that
// order, which is the order ListWorkers has always published them in.
func publishedPromotionPaths() ([]publishedPromotionPath, error) {
	paths, err := fixtures.PromotionPaths()
	if err != nil {
		return nil, fmt.Errorf("app: journey: read the published promotion paths: %w", err)
	}
	edges := demoworkforce.PromotionPaths()
	out := make([]publishedPromotionPath, 0, len(paths)+len(edges))
	for i := range paths {
		scope := paths[i]
		path := scope.Path
		option := workspace.PromotionPathOption{
			PathRef: path.PathIDOrID(), Revision: path.Revision,
			SourceProfileRef: path.From.ProfileID,
			SourceJobCode:    scope.SourceJobCode, SourceGrade: scope.SourceGrade,
			TargetProfileRef: path.To.ProfileID,
			TargetJobCode:    scope.TargetJobCode, TargetGrade: scope.TargetGrade,
			TargetTitle: scope.TargetTitle, Kind: string(path.Kind),
			MinimumBaseIncrease:   path.MinimumBaseIncrease.String(),
			MaximumBaseIncrease:   path.MaximumBaseIncrease.String(),
			CompensationPolicyRef: path.CompensationPolicyRef.Ref + "@" + path.CompensationPolicyRef.Revision,
		}
		for _, rule := range path.BenefitEligibilityRuleRefs {
			option.BenefitRuleRefs = append(option.BenefitRuleRefs, rule.Ref+"@"+rule.Revision)
		}
		out = append(out, publishedPromotionPath{Option: option, bounds: &scope})
	}
	for _, edge := range edges {
		out = append(out, publishedPromotionPath{Option: workspace.PromotionPathOption{
			PathRef:       "demoworkforce:" + edge.OrgUnit + ":" + edge.SourceJobCode + "->" + edge.TargetJobCode,
			Revision:      "1",
			SourceJobCode: edge.SourceJobCode, SourceGrade: edge.SourceGrade,
			TargetJobCode: edge.TargetJobCode, TargetGrade: edge.TargetGrade,
			TargetTitle: edge.TargetTitle, Kind: "UPWARD",
		}})
	}
	return out, nil
}

// matches reports whether the edge leads from current's job and grade to the
// requested target.
func (p publishedPromotionPath) matches(sourceJobCode, sourceGrade, targetJobCode, targetGrade string) bool {
	return p.Option.SourceJobCode == sourceJobCode && p.Option.SourceGrade == sourceGrade &&
		p.Option.TargetJobCode == targetJobCode && p.Option.TargetGrade == targetGrade
}

// allowsBaseIncrease applies the edge's published rule to an exact increase
// fraction: the published minimum and maximum for a job-architecture edge,
// and for a demo ladder edge, which publishes no bounds, the only rule its
// UPWARD kind states -- the proposed base may not be lower than the current
// one. It returns the input error the proposal form reports.
func (p publishedPromotionPath) allowsBaseIncrease(increase values.Percentage) error {
	if p.bounds != nil {
		if err := p.bounds.Path.AllowsBaseIncrease(increase); err != nil {
			return journeyInputError("proposed_base", "the published ladder edge requires a base increase between "+
				p.bounds.Path.MinimumBaseIncrease.String()+" and "+p.bounds.Path.MaximumBaseIncrease.String()+" (decimal fractions)")
		}
		return nil
	}
	if increase.Fraction().Sign() < 0 {
		return journeyInputError("proposed_base", "an upward ladder edge does not accept a base below the current base")
	}
	return nil
}
