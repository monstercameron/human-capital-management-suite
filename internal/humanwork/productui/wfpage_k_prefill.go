package productui

import "strings"

// PrefillSource identifies the server-owned record used to propose a page
// value. The source is metadata only; values are always read from the
// source snapshot supplied to ResolveWorkflowPagePrefill.
type PrefillSource string

const (
	PrefillPerson       PrefillSource = "person"
	PrefillPosition     PrefillSource = "position"
	PrefillOrganization PrefillSource = "organization_unit"
	PrefillPreviousRun  PrefillSource = "previous_run"
)

type WorkflowPagePrefillBinding struct {
	FieldID       string
	Source        PrefillSource
	SourceField   string
	SourceLabel   WorkflowPageText
	Authorized    AuthorizedField
	AllowOverride bool
}

// WorkflowPagePrefill is safe to hand to the browser. Value is populated
// only for an explicitly readable SHOW field; restricted raw values never
// cross this projection boundary.
type WorkflowPagePrefill struct {
	FieldID       string
	Value         string
	Source        PrefillSource
	SourceLabel   string
	AllowOverride bool
	Reason        string
}

// ResolveWorkflowPagePrefill applies field authorization before copying a
// source value. A mask/summary/derived disposition is suitable for display,
// but never for a form default: sending a stand-in or restricted raw value
// to a client would let the page pretend it had a value it may not submit.
func ResolveWorkflowPagePrefill(locale LocaleContext, bindings []WorkflowPagePrefillBinding, sources map[PrefillSource]map[string]string) []WorkflowPagePrefill {
	result := make([]WorkflowPagePrefill, 0, len(bindings))
	for _, binding := range bindings {
		if strings.TrimSpace(binding.FieldID) == "" || strings.TrimSpace(binding.SourceField) == "" {
			continue
		}
		projection := WorkflowPagePrefill{
			FieldID: binding.FieldID, Source: binding.Source, SourceLabel: binding.SourceLabel.Resolve(locale),
			AllowOverride: binding.AllowOverride,
		}
		value, present := sources[binding.Source][binding.SourceField]
		readable := binding.Authorized.Effect == PresentationAllow &&
			(binding.Authorized.Disposition == "" || binding.Authorized.Disposition == FieldShow)
		if !readable {
			projection.Reason = binding.Authorized.Reason
			if projection.Reason == "" {
				projection.Reason = locale.Text("provenance.value.unavailable")
			}
			result = append(result, projection)
			continue
		}
		if present {
			projection.Value = value
		}
		result = append(result, projection)
	}
	return result
}
