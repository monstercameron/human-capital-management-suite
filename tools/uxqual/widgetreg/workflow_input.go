package widgetreg

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

// WorkflowInputWidget is the governed renderer registration for one workflow
// page input kind. The renderer is represented by a semantic role and input
// contract; it never carries HTML or executable code.
type WorkflowInputWidget struct {
	Kind              pagedef.WorkflowWidgetKind
	Role              string
	InputType         string
	SupportsTypeAhead bool
	ReferenceKind     string
	RequiresCurrency  bool
	RequiresPayBasis  bool
	EffectiveDating   bool
}

// WorkflowInputRegistry owns its definitions as a value. There is no package
// mutable registry, so tests and compositions cannot leak state into one
// another.
type WorkflowInputRegistry struct {
	widgets []WorkflowInputWidget
}

// NewWorkflowInputRegistry returns the closed input-widget vocabulary used by
// WFPAGE-013.
func NewWorkflowInputRegistry() WorkflowInputRegistry {
	return WorkflowInputRegistry{widgets: []WorkflowInputWidget{
		{Kind: pagedef.WorkflowWidgetText, Role: "textbox", InputType: "text"},
		{Kind: pagedef.WorkflowWidgetInteger, Role: "spinbutton", InputType: "number"},
		{Kind: pagedef.WorkflowWidgetDecimal, Role: "spinbutton", InputType: "number"},
		{Kind: pagedef.WorkflowWidgetCheckbox, Role: "checkbox", InputType: "checkbox"},
		{Kind: pagedef.WorkflowWidgetInstant, Role: "textbox", InputType: "datetime-local", EffectiveDating: true},
		{Kind: pagedef.WorkflowWidgetLocalDate, Role: "textbox", InputType: "date", EffectiveDating: true},
		{Kind: pagedef.WorkflowWidgetMoney, Role: "textbox", InputType: "text", RequiresCurrency: true, RequiresPayBasis: true},
		{Kind: pagedef.WorkflowWidgetChoice, Role: "combobox", InputType: "select"},
		{Kind: pagedef.WorkflowWidgetList, Role: "listbox", InputType: "list"},
		{Kind: pagedef.WorkflowWidgetPersonPicker, Role: "combobox", InputType: "text", SupportsTypeAhead: true, ReferenceKind: "person"},
		{Kind: pagedef.WorkflowWidgetPositionPicker, Role: "combobox", InputType: "text", SupportsTypeAhead: true, ReferenceKind: "position"},
		{Kind: pagedef.WorkflowWidgetOrganizationPicker, Role: "combobox", InputType: "text", SupportsTypeAhead: true, ReferenceKind: "organization_unit"},
		{Kind: pagedef.WorkflowWidgetCostCentrePicker, Role: "combobox", InputType: "text", SupportsTypeAhead: true, ReferenceKind: "cost_centre"},
	}}
}

// Lookup returns a defensive copy of the registration for kind.
func (r WorkflowInputRegistry) Lookup(kind pagedef.WorkflowWidgetKind) (WorkflowInputWidget, bool) {
	for _, widget := range r.widgets {
		if widget.Kind == kind {
			return widget, true
		}
	}
	return WorkflowInputWidget{}, false
}

// InputRenderRequest is the authorized, viewer-specific state needed to
// render a field. Options have already been filtered by the owning authority.
type InputRenderRequest struct {
	Widget          pagedef.WorkflowPageWidget
	Locale          string
	Error           string
	DisabledReason  string
	Options         []string
	ViewerMaySearch bool
}

// InputRenderResult is a semantic render projection. The IDs are stable so a
// renderer can associate label, description and error without guessing.
type InputRenderResult struct {
	Kind             pagedef.WorkflowWidgetKind
	Role             string
	InputType        string
	Label            string
	Description      string
	Error            string
	LabelID          string
	DescriptionID    string
	ErrorID          string
	DisabledReason   string
	DisabledReasonID string
	Locale           string
	Direction        string
	TypeAhead        bool
	Options          []string
	Currency         string
	PayBasis         string
	EffectiveDating  string
}

// Render validates the page binding against this registry and returns a
// localized-direction semantic view. The search flag only enables a picker;
// it does not perform a data lookup or bypass authorization.
func (r WorkflowInputRegistry) Render(req InputRenderRequest) (InputRenderResult, error) {
	widget, ok := r.Lookup(req.Widget.Kind)
	if !ok {
		return InputRenderResult{}, fmt.Errorf("widgetreg: unsupported workflow input kind %q", req.Widget.Kind)
	}
	locale := strings.ToLower(strings.TrimSpace(req.Locale))
	if locale != "en-us" && locale != "de-de" && locale != "ar" {
		return InputRenderResult{}, fmt.Errorf("widgetreg: unsupported locale %q", req.Locale)
	}
	result := InputRenderResult{
		Kind: req.Widget.Kind, Role: widget.Role, InputType: widget.InputType,
		Label: req.Widget.Label, Description: req.Widget.Description, Error: req.Error,
		LabelID: req.Widget.ID + "-label", DescriptionID: req.Widget.ID + "-description",
		ErrorID: req.Widget.ID + "-error", DisabledReason: req.DisabledReason,
		DisabledReasonID: req.Widget.ID + "-disabled-reason", Locale: locale,
		Direction: map[bool]string{true: "rtl", false: "ltr"}[locale == "ar"],
		TypeAhead: widget.SupportsTypeAhead && req.ViewerMaySearch,
		Options:   append([]string(nil), req.Options...), Currency: req.Widget.Currency,
		PayBasis: req.Widget.PayBasis, EffectiveDating: req.Widget.EffectiveDating,
	}
	if widget.RequiresCurrency && result.Currency == "" {
		return InputRenderResult{}, fmt.Errorf("widgetreg: money input %q requires currency", req.Widget.Binding)
	}
	if widget.RequiresPayBasis && result.PayBasis == "" {
		return InputRenderResult{}, fmt.Errorf("widgetreg: money input %q requires pay basis", req.Widget.Binding)
	}
	if widget.EffectiveDating && result.EffectiveDating == "" {
		result.EffectiveDating = "earliest; no retroactive change without reason"
	}
	return result, nil
}
