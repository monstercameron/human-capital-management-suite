package productui

// Validation panel steps in authoring-flow order: the opening
// declaration, the governed floorplan choice, regions, widgets,
// content safety over content-tier values, actions, and the
// publication ceiling last.
const (
	ValidationStepPurpose       = "purpose"
	ValidationStepFloorplan     = "floorplan"
	ValidationStepRegions       = "regions"
	ValidationStepWidgets       = "widgets"
	ValidationStepContentSafety = "content-safety"
	ValidationStepActions       = "actions"
	ValidationStepCeiling       = "ceiling"
)

// ValidationFinding is one panel step: its name, whether the
// draft clears it, and the step validator's verbatim reasons.
type ValidationFinding struct {
	Step       string
	Compatible bool
	Reasons    []string
}

// ValidationReport is the findings panel's content model: the
// draft page, the overall publication gate (every step must
// clear), and every step finding in flow order. Rendering the
// panel stays with the studio surface; this report is what it
// renders.
type ValidationReport struct {
	Page       PageID
	Compatible bool
	Findings   []ValidationFinding
}

// ValidateComposition runs the whole draft composition chain and
// reports every step: purpose, floorplan compatibility, region
// composition, widget bindings, content safety, action bindings,
// and the classification ceiling. Every step always runs — a failure
// never hides the rest — so authors see everything at once. The
// panel gates publication, never replaces the step validators.
func ValidateComposition(draft PageDraft, catalog FloorplanCatalog, registry WidgetRegistry) ValidationReport {
	purpose := ValidatePagePurpose(draft.Composition)
	floorplan := ValidateFloorplanCompatibility(draft.Composition, catalog)
	regions := ValidateRegionComposition(draft.Composition)
	widgets := ValidateDraftWidgets(draft, registry)
	safety := ValidateContentSafety(draft, registry)
	actions := ValidateDraftActions(draft)
	ceiling := ValidateDraftCeiling(draft)
	findings := []ValidationFinding{
		{Step: ValidationStepPurpose, Compatible: purpose.Compatible, Reasons: purpose.Reasons},
		{Step: ValidationStepFloorplan, Compatible: floorplan.Compatible, Reasons: floorplan.Reasons},
		{Step: ValidationStepRegions, Compatible: regions.Compatible, Reasons: regions.Reasons},
		{Step: ValidationStepWidgets, Compatible: widgets.Compatible, Reasons: widgets.Reasons},
		{Step: ValidationStepContentSafety, Compatible: safety.Compatible, Reasons: safety.Reasons},
		{Step: ValidationStepActions, Compatible: actions.Compatible, Reasons: actions.Reasons},
		{Step: ValidationStepCeiling, Compatible: ceiling.Compatible, Reasons: ceiling.Reasons},
	}
	compatible := true
	for _, finding := range findings {
		compatible = compatible && finding.Compatible
	}
	return ValidationReport{Page: draft.Page, Compatible: compatible, Findings: findings}
}
