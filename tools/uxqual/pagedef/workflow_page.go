package pagedef

// This file extends the product PageDefinition vocabulary with the bounded
// data contract used by workflow input pages. It deliberately contains no
// renderer, HTML, executable expression, arbitrary URL, or capability
// implementation. A page can only bind paths proved by a compiled workflow.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

const (
	WorkflowPageSchema        = "hcmnext.uxqual.pagedef.WorkflowPageDefinition"
	WorkflowPageSchemaVersion = 1
)

// WorkflowWidgetKind is the closed renderer vocabulary for workflow inputs.
type WorkflowWidgetKind string

const (
	WorkflowWidgetText               WorkflowWidgetKind = "text"
	WorkflowWidgetInteger            WorkflowWidgetKind = "integer"
	WorkflowWidgetDecimal            WorkflowWidgetKind = "decimal"
	WorkflowWidgetCheckbox           WorkflowWidgetKind = "checkbox"
	WorkflowWidgetInstant            WorkflowWidgetKind = "instant"
	WorkflowWidgetLocalDate          WorkflowWidgetKind = "local_date"
	WorkflowWidgetMoney              WorkflowWidgetKind = "money"
	WorkflowWidgetChoice             WorkflowWidgetKind = "choice"
	WorkflowWidgetList               WorkflowWidgetKind = "list"
	WorkflowWidgetPersonPicker       WorkflowWidgetKind = "person_picker"
	WorkflowWidgetPositionPicker     WorkflowWidgetKind = "position_picker"
	WorkflowWidgetOrganizationPicker WorkflowWidgetKind = "organization_unit_picker"
	WorkflowWidgetCostCentrePicker   WorkflowWidgetKind = "cost_centre_picker"
)

// RuleOperator is the closed, non-executable rule vocabulary. Rule
// evaluation belongs to WFPAGE-015; this contract only carries its data.
type RuleOperator string

const (
	RuleEquals    RuleOperator = "equals"
	RuleNotEquals RuleOperator = "not_equals"
	RuleExists    RuleOperator = "exists"
	RuleNotExists RuleOperator = "not_exists"
	RuleIn        RuleOperator = "in"
)

// WorkflowCondition is a bounded comparison between declared input paths and
// literal values. Path2 is useful for cross-field comparisons; Value is a
// canonical literal, never code.
type WorkflowCondition struct {
	Path  string       `json:"path"`
	Op    RuleOperator `json:"op"`
	Path2 string       `json:"path2,omitempty"`
	Value string       `json:"value,omitempty"`
}

// WorkflowPageSection is one semantic section in the workflow page layout.
type WorkflowPageSection struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Columns int    `json:"columns"`
	StepID  string `json:"step_id,omitempty"`
}

// WorkflowPageStep is a guided-page step. It is separate from a section so a
// section can remain visible while a step changes.
type WorkflowPageStep struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// WorkflowPageWidget binds exactly one closed widget kind to one workflow
// input path. Labels and descriptions are data, not markup.
type WorkflowPageWidget struct {
	ID              string             `json:"id"`
	SectionID       string             `json:"section_id"`
	Column          int                `json:"column"`
	StepID          string             `json:"step_id,omitempty"`
	Kind            WorkflowWidgetKind `json:"kind"`
	Binding         string             `json:"binding"`
	Type            string             `json:"type"`
	Label           string             `json:"label"`
	Description     string             `json:"description,omitempty"`
	Required        bool               `json:"required"`
	Reference       string             `json:"reference,omitempty"`
	Currency        string             `json:"currency,omitempty"`
	PayBasis        string             `json:"pay_basis,omitempty"`
	EffectiveDating string             `json:"effective_dating,omitempty"`
}

// WorkflowPageRule carries a declarative rule for a bound input.
type WorkflowPageRule struct {
	ID      string            `json:"id"`
	Target  string            `json:"target"`
	Message string            `json:"message"`
	When    WorkflowCondition `json:"when"`
}

// WorkflowPageContentBlock references localized governed copy. It has no
// body field, so a tenant cannot smuggle free HTML or executable content into
// a page definition.
type WorkflowPageContentBlock struct {
	ID        string `json:"id"`
	SectionID string `json:"section_id"`
	Kind      string `json:"kind"` // note, guide, checklist
	TextKey   string `json:"text_key"`
}

// WorkflowPageLinkSlot is a governed document reference slot. Links are
// resolved by the documents authority; a page never stores a pasted URL.
type WorkflowPageLinkSlot struct {
	ID          string `json:"id"`
	SectionID   string `json:"section_id"`
	DocumentRef string `json:"document_ref"`
	Mode        string `json:"mode"` // latest or pinned
}

// WorkflowPageVisibility is a bounded visibility condition for a section,
// widget, or content block.
type WorkflowPageVisibility struct {
	Target string            `json:"target"`
	When   WorkflowCondition `json:"when"`
}

// WorkflowPageDefinition is the versioned, renderer-independent workflow
// input page contract. WorkflowVersion binds it to one compiled workflow;
// PageVersion identifies the immutable page revision for a run.
type WorkflowPageDefinition struct {
	Schema          string                     `json:"schema"`
	SchemaVersion   int                        `json:"schema_version"`
	WorkflowKey     string                     `json:"workflow_key"`
	WorkflowVersion uint32                     `json:"workflow_version"`
	PageVersion     uint32                     `json:"page_version"`
	PageID          string                     `json:"page_id"`
	Sections        []WorkflowPageSection      `json:"sections"`
	Steps           []WorkflowPageStep         `json:"steps,omitempty"`
	Widgets         []WorkflowPageWidget       `json:"widgets"`
	Rules           []WorkflowPageRule         `json:"rules,omitempty"`
	ContentBlocks   []WorkflowPageContentBlock `json:"content_blocks,omitempty"`
	LinkSlots       []WorkflowPageLinkSlot     `json:"link_slots,omitempty"`
	Visibility      []WorkflowPageVisibility   `json:"visibility,omitempty"`
	Accessibility   Accessibility              `json:"accessibility"`
	BrandTokens     []string                   `json:"brand_tokens"`
}

var workflowPageIdentifier = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)

func workflowPageKinds() []WorkflowWidgetKind {
	return []WorkflowWidgetKind{
		WorkflowWidgetText, WorkflowWidgetInteger, WorkflowWidgetDecimal,
		WorkflowWidgetCheckbox, WorkflowWidgetInstant, WorkflowWidgetLocalDate,
		WorkflowWidgetMoney, WorkflowWidgetChoice, WorkflowWidgetList,
		WorkflowWidgetPersonPicker, WorkflowWidgetPositionPicker,
		WorkflowWidgetOrganizationPicker, WorkflowWidgetCostCentrePicker,
	}
}

func workflowPageKindKnown(k WorkflowWidgetKind) bool {
	for _, known := range workflowPageKinds() {
		if k == known {
			return true
		}
	}
	return false
}

// Validate checks only the closed, renderer-independent contract.
func (d WorkflowPageDefinition) Validate() []Violation {
	var out []Violation
	add := func(path, reason string) { out = append(out, Violation{Path: path, Reason: reason}) }
	if d.Schema != WorkflowPageSchema {
		add("schema", "must be the workflow page schema")
	}
	if d.SchemaVersion != WorkflowPageSchemaVersion {
		add("schema_version", "unsupported workflow page schema version")
	}
	if !workflowPageIdentifier.MatchString(d.WorkflowKey) {
		add("workflow_key", "must be a non-empty semantic key")
	}
	if d.WorkflowVersion < 1 {
		add("workflow_version", "must be positive")
	}
	if d.PageVersion < 1 {
		add("page_version", "must be positive")
	}
	if !workflowPageIdentifier.MatchString(d.PageID) {
		add("page_id", "must be a non-empty semantic id")
	}
	if len(d.Sections) == 0 {
		add("sections", "at least one section is required")
	}

	sections := map[string]WorkflowPageSection{}
	for i, s := range d.Sections {
		p := fmt.Sprintf("sections[%d]", i)
		if !workflowPageIdentifier.MatchString(s.ID) {
			add(p+".id", "must be a semantic id")
		}
		if _, ok := sections[s.ID]; ok {
			add(p+".id", "duplicate section id")
		}
		sections[s.ID] = s
		if s.Columns < 1 || s.Columns > 12 {
			add(p+".columns", "must be between 1 and 12")
		}
		if s.Label == "" {
			add(p+".label", "required")
		}
		if strings.ContainsAny(s.Label, "<>") {
			add(p+".label", "must not contain markup")
		}
	}
	steps := map[string]bool{}
	for i, s := range d.Steps {
		p := fmt.Sprintf("steps[%d]", i)
		if !workflowPageIdentifier.MatchString(s.ID) {
			add(p+".id", "must be a semantic id")
		}
		if steps[s.ID] {
			add(p+".id", "duplicate step id")
		}
		steps[s.ID] = true
		if s.Label == "" {
			add(p+".label", "required")
		}
	}
	bindings := map[string]bool{}
	widgets := map[string]WorkflowPageWidget{}
	for i, w := range d.Widgets {
		p := fmt.Sprintf("widgets[%d]", i)
		if !workflowPageIdentifier.MatchString(w.ID) {
			add(p+".id", "must be a semantic id")
		}
		if widgets[w.ID].ID != "" {
			add(p+".id", "duplicate widget id")
		}
		widgets[w.ID] = w
		if _, ok := sections[w.SectionID]; !ok {
			add(p+".section_id", "unknown section")
		}
		if w.Column < 1 || (sections[w.SectionID].Columns > 0 && w.Column > sections[w.SectionID].Columns) {
			add(p+".column", "outside the section columns")
		}
		if !workflowPageKindKnown(w.Kind) {
			add(p+".kind", "widget kind is not in the closed vocabulary")
		}
		if w.Binding == "" {
			add(p+".binding", "required")
		}
		if bindings[w.Binding] {
			add(p+".binding", "each input path may have exactly one control")
		}
		bindings[w.Binding] = true
		if w.Label == "" {
			add(p+".label", "required")
		}
		if strings.ContainsAny(w.Label+"\x00"+w.Description, "<>") {
			add(p+".label", "label and description must not contain markup")
		}
		if w.StepID != "" && !steps[w.StepID] {
			add(p+".step_id", "unknown step")
		}
	}
	for i, r := range d.Rules {
		p := fmt.Sprintf("rules[%d]", i)
		if !workflowPageIdentifier.MatchString(r.ID) {
			add(p+".id", "must be a semantic id")
		}
		if !bindings[r.Target] {
			add(p+".target", "must name a widget binding")
		}
		if r.Message == "" || strings.ContainsAny(r.Message, "<>") {
			add(p+".message", "must be a localized message key without markup")
		}
		validateCondition(add, p+".when", r.When)
	}
	for i, v := range d.Visibility {
		p := fmt.Sprintf("visibility[%d]", i)
		if v.Target == "" {
			add(p+".target", "required")
		}
		validateCondition(add, p+".when", v.When)
	}
	for i, c := range d.ContentBlocks {
		p := fmt.Sprintf("content_blocks[%d]", i)
		if _, ok := sections[c.SectionID]; !ok {
			add(p+".section_id", "unknown section")
		}
		if c.Kind != "note" && c.Kind != "guide" && c.Kind != "checklist" {
			add(p+".kind", "content kind is closed")
		}
		if c.TextKey == "" || strings.ContainsAny(c.TextKey, "<>") {
			add(p+".text_key", "must be a localized text key without markup")
		}
	}
	for i, l := range d.LinkSlots {
		p := fmt.Sprintf("link_slots[%d]", i)
		if _, ok := sections[l.SectionID]; !ok {
			add(p+".section_id", "unknown section")
		}
		if !strings.HasPrefix(l.DocumentRef, "document:") || strings.ContainsAny(l.DocumentRef, "<>\r\n") {
			add(p+".document_ref", "must be a governed document reference")
		}
		if l.Mode != "latest" && l.Mode != "pinned" {
			add(p+".mode", "must be latest or pinned")
		}
	}
	if len(d.Accessibility.Landmarks) == 0 {
		add("accessibility.landmarks", "at least one landmark is required")
	}
	if d.Accessibility.LiveRegion != LiveRegionOff && d.Accessibility.LiveRegion != LiveRegionPolite && d.Accessibility.LiveRegion != LiveRegionAssertive {
		add("accessibility.live_region", "invalid live-region politeness")
	}
	for i, t := range d.BrandTokens {
		if !brandTokenPattern.MatchString(t) {
			add(fmt.Sprintf("brand_tokens[%d]", i), "must be a semantic brand-token reference")
		}
	}
	return out
}

func validateCondition(add func(string, string), path string, c WorkflowCondition) {
	if c.Path == "" {
		add(path+".path", "required")
	}
	switch c.Op {
	case RuleEquals, RuleNotEquals, RuleIn:
		if c.Value == "" && c.Path2 == "" {
			add(path+".value", "literal value or second path is required")
		}
	case RuleExists, RuleNotExists:
		if c.Value != "" || c.Path2 != "" {
			add(path+".value", "existence conditions cannot carry a value")
		}
	default:
		add(path+".op", "operator is not in the closed vocabulary")
	}
}

// ValidateAgainst proves that every page binding and condition refers to a
// workflow input. It also rejects a control whose kind cannot represent the
// compiled input type.
func (d WorkflowPageDefinition) ValidateAgainst(plan *workflow.CompiledWorkflow) []Violation {
	out := d.Validate()
	if plan == nil {
		return append(out, Violation{Path: "workflow", Reason: "compiled workflow is required"})
	}
	if d.WorkflowKey != plan.WorkflowID {
		out = append(out, Violation{Path: "workflow_key", Reason: "does not match compiled workflow"})
	}
	if d.WorkflowVersion != plan.Version {
		out = append(out, Violation{Path: "workflow_version", Reason: "does not match compiled workflow"})
	}
	inputs := make(map[string]workflow.Field, len(plan.Inputs))
	for _, f := range plan.Inputs {
		inputs[f.Path] = f
	}
	for i, w := range d.Widgets {
		f, ok := inputs[w.Binding]
		if !ok {
			out = append(out, Violation{Path: fmt.Sprintf("widgets[%d].binding", i), Reason: "path is not a compiled workflow input"})
			continue
		}
		if !workflowWidgetCompatible(w.Kind, f.Type.Kind, f.Type.Brand) {
			out = append(out, Violation{Path: fmt.Sprintf("widgets[%d].kind", i), Reason: fmt.Sprintf("%q cannot represent input type %s", w.Kind, f.Type.String())})
		}
	}
	paths := func(c WorkflowCondition, path string) {
		if _, ok := inputs[c.Path]; !ok {
			out = append(out, Violation{Path: path + ".path", Reason: "path is not a compiled workflow input"})
		}
		if c.Path2 != "" {
			if _, ok := inputs[c.Path2]; !ok {
				out = append(out, Violation{Path: path + ".path2", Reason: "path is not a compiled workflow input"})
			}
		}
	}
	for i, r := range d.Rules {
		paths(r.When, fmt.Sprintf("rules[%d].when", i))
	}
	for i, v := range d.Visibility {
		paths(v.When, fmt.Sprintf("visibility[%d].when", i))
	}
	return out
}

func workflowWidgetCompatible(kind WorkflowWidgetKind, valueKind workflow.Kind, brand string) bool {
	switch kind {
	case WorkflowWidgetText:
		return valueKind == workflow.KindString
	case WorkflowWidgetInteger:
		return valueKind == workflow.KindInteger
	case WorkflowWidgetDecimal:
		return valueKind == workflow.KindDecimal
	case WorkflowWidgetCheckbox:
		return valueKind == workflow.KindBool
	case WorkflowWidgetInstant:
		return valueKind == workflow.KindInstant
	case WorkflowWidgetLocalDate:
		return valueKind == workflow.KindLocalDate
	case WorkflowWidgetMoney:
		return valueKind == workflow.KindMoney
	case WorkflowWidgetChoice:
		return valueKind == workflow.KindEnum
	case WorkflowWidgetList:
		return valueKind == workflow.KindList
	case WorkflowWidgetPersonPicker:
		return valueKind == workflow.KindString && brand == "PersonID"
	case WorkflowWidgetPositionPicker:
		return valueKind == workflow.KindString && brand == "PositionID"
	case WorkflowWidgetOrganizationPicker:
		return valueKind == workflow.KindString && brand == "OrganizationUnitID"
	case WorkflowWidgetCostCentrePicker:
		return valueKind == workflow.KindString && brand == "CostCentreID"
	default:
		return false
	}
}

// Canonical returns stable JSON with the schema identity forced to the
// current contract. JSON struct field order and every slice order are
// intentional semantic order; no map is serialized.
func (d WorkflowPageDefinition) Canonical() []byte {
	d.Schema = WorkflowPageSchema
	d.SchemaVersion = WorkflowPageSchemaVersion
	b, _ := json.Marshal(d)
	return b
}

// Digest returns the immutable page-definition identity.
func (d WorkflowPageDefinition) Digest() string {
	sum := sha256.Sum256(d.Canonical())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// DefaultWorkflowPageDefinition deterministically projects the compiled
// workflow's typed inputs into one usable page. The compiler's field metadata
// supplies labels, descriptions and groups; path-derived labels are the
// compatibility fallback for older definitions that predate metadata.
func DefaultWorkflowPageDefinition(plan *workflow.CompiledWorkflow) (WorkflowPageDefinition, error) {
	if plan == nil {
		return WorkflowPageDefinition{}, fmt.Errorf("workflow page: compiled workflow is required")
	}
	if err := plan.Verify(); err != nil {
		return WorkflowPageDefinition{}, fmt.Errorf("workflow page: compiled workflow is not verified: %w", err)
	}
	d := WorkflowPageDefinition{
		Schema: WorkflowPageSchema, SchemaVersion: WorkflowPageSchemaVersion,
		WorkflowKey: plan.WorkflowID, WorkflowVersion: plan.Version, PageVersion: 1,
		PageID: plan.WorkflowID + ".input", Accessibility: Accessibility{Landmarks: []string{"main", "form"}, LiveRegion: LiveRegionPolite},
		BrandTokens: []string{"brand.color.primary", "brand.spacing.md", "brand.typography.body"},
	}
	groups := map[string]string{}
	for _, f := range plan.Inputs {
		group := f.Group
		if group == "" {
			group = f.Path
			if dot := strings.IndexByte(group, '.'); dot > 0 {
				group = group[:dot]
			}
		}
		if group == "" {
			group = "general"
		}
		sectionID, exists := groups[group]
		if !exists {
			sectionID = "section." + group
			groups[group] = sectionID
			d.Sections = append(d.Sections, WorkflowPageSection{ID: sectionID, Label: titleFromPath(group), Columns: 1})
		}
		kind := defaultWidgetKind(f.Type.Kind)
		id := "field." + strings.NewReplacer(".", "__", "-", "_").Replace(f.Path)
		d.Widgets = append(d.Widgets, WorkflowPageWidget{ID: id, SectionID: sectionID, Column: 1, Kind: kind, Binding: f.Path, Type: f.Type.String(), Label: fieldLabel(f), Description: f.Description, Required: !f.Type.Nullable})
	}
	return d, nil
}

func defaultWidgetKind(k workflow.Kind) WorkflowWidgetKind {
	switch k {
	case workflow.KindInteger:
		return WorkflowWidgetInteger
	case workflow.KindDecimal:
		return WorkflowWidgetDecimal
	case workflow.KindBool:
		return WorkflowWidgetCheckbox
	case workflow.KindInstant:
		return WorkflowWidgetInstant
	case workflow.KindLocalDate:
		return WorkflowWidgetLocalDate
	case workflow.KindMoney:
		return WorkflowWidgetMoney
	case workflow.KindEnum:
		return WorkflowWidgetChoice
	case workflow.KindList:
		return WorkflowWidgetList
	default:
		return WorkflowWidgetText
	}
}

func fieldLabel(f workflow.Field) string {
	if f.Label != "" {
		return f.Label
	}
	return titleFromPath(f.Path)
}

func titleFromPath(path string) string {
	path = strings.ReplaceAll(strings.ReplaceAll(path, "_", " "), ".", " ")
	words := strings.Fields(path)
	for i, word := range words {
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

// SortedWidgetBindings is a small deterministic projection useful to
// renderers and tests. It does not alter the page's semantic order.
func (d WorkflowPageDefinition) SortedWidgetBindings() []WorkflowPageWidget {
	out := append([]WorkflowPageWidget(nil), d.Widgets...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Binding < out[j].Binding })
	return out
}
