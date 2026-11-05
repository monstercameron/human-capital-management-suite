package pagedef

import "github.com/monstercameron/human-capital-management-suite/internal/workflow"

// New hire reference identity. The key and workflow version track
// internal/workflow/hireexec; the journey test asserts they match so the
// shipped example cannot drift from the workflow it demonstrates.
const (
	NewHireWorkflowKey          = "hcmnext.workflows.new_hire"
	NewHireWorkflowVersion      = 2
	NewHireReferencePageID      = "hcmnext.workflows.new_hire.input"
	NewHireReferencePageVersion = 3
)

// NewHireReferencePage is the shipped New hire reference example: one
// details section with the seven governed journey controls (person,
// position, cost centre, salary, start date, contact, attachment), the
// review checklist, and the pinned hiring SOP slot. The byte identity is
// pinned by the WFPAGE-034 golden; change it only with a page version bump.
func NewHireReferencePage() WorkflowPageDefinition {
	return WorkflowPageDefinition{
		Schema: WorkflowPageSchema, SchemaVersion: WorkflowPageSchemaVersion,
		WorkflowKey: NewHireWorkflowKey, WorkflowVersion: NewHireWorkflowVersion, PageVersion: NewHireReferencePageVersion, PageID: NewHireReferencePageID,
		Sections: []WorkflowPageSection{{ID: "details", Label: "New hire details", Columns: 2}},
		Widgets: []WorkflowPageWidget{
			{ID: "field.person", SectionID: "details", Column: 1, Kind: WorkflowWidgetPersonPicker, Binding: "person", Type: "STRING#PersonID", Label: "Person", Required: true},
			{ID: "field.position", SectionID: "details", Column: 2, Kind: WorkflowWidgetPositionPicker, Binding: "position", Type: "STRING#PositionID", Label: "Position", Required: true},
			{ID: "field.cost-centre", SectionID: "details", Column: 1, Kind: WorkflowWidgetCostCentrePicker, Binding: "cost_centre", Type: "STRING#CostCentreID", Label: "Cost centre", Required: true},
			{ID: "field.salary", SectionID: "details", Column: 2, Kind: WorkflowWidgetMoney, Binding: "salary", Type: "MONEY", Label: "Salary", Required: true, Currency: "USD", PayBasis: "annual"},
			{ID: "field.start-date", SectionID: "details", Column: 1, Kind: WorkflowWidgetLocalDate, Binding: "start_date", Type: "LOCAL_DATE", Label: "Start date", Required: true, EffectiveDating: "earliest"},
			{ID: "field.contact", SectionID: "details", Column: 2, Kind: WorkflowWidgetText, Binding: "contact", Type: "STRING", Label: "Contact", Required: true},
			{ID: "field.attachment", SectionID: "details", Column: 1, Kind: WorkflowWidgetText, Binding: "attachment", Type: "STRING", Label: "Attachment"},
		},
		Rules: []WorkflowPageRule{
			{ID: "salary-band", Target: "salary", Message: "workflow.new_hire.salary_band", When: WorkflowCondition{Path: "salary", Op: RuleExists}},
			{ID: "start-date", Target: "start_date", Message: "workflow.new_hire.start_date", When: WorkflowCondition{Path: "start_date", Op: RuleExists}},
		},
		ContentBlocks: []WorkflowPageContentBlock{{ID: "guide", SectionID: "details", Kind: "checklist", TextKey: "workflow.new_hire.review"}},
		LinkSlots:     []WorkflowPageLinkSlot{{ID: "hiring-sop", SectionID: "details", DocumentRef: "document:hiring-sop@docv-7", Mode: "pinned"}},
		Accessibility: Accessibility{Landmarks: []string{"main", "form"}, LiveRegion: LiveRegionPolite},
		BrandTokens:   []string{"brand.color.primary", "brand.spacing.md", "brand.typography.body"},
	}
}

// NewHireCompanyRules are the shipped server-side company validations for
// the New hire reference page. The browser enforces the same rules for
// guidance; the server re-evaluates these compiled rules, so bypassing the
// client still yields SALARY_BAND and START_DATE findings.
func NewHireCompanyRules() []workflow.PageRule {
	return []workflow.PageRule{
		{
			Code: "SALARY_BAND", Message: "salary outside approved company band", Expression: `salary <= param("salary_band_max")`,
			Inputs: map[string]workflow.ValueType{"salary": {Kind: workflow.KindMoney}}, Parameters: map[string]workflow.ValueType{"salary_band_max": {Kind: workflow.KindMoney}},
		},
		{
			Code: "START_DATE", Message: "start date must be on or after the company policy date", Expression: `start_date >= date("2026-10-01")`,
			Inputs: map[string]workflow.ValueType{"start_date": {Kind: workflow.KindLocalDate}},
		},
	}
}
