package productui

import "fmt"

// ContentEdit is one governed content edit: the binding index and
// the replacement values. Both value fields replace wholesale —
// empty means empty, never "leave unchanged" — so callers carry
// forward what they keep.
type ContentEdit struct {
	BindingIndex int
	Value        string
	DisplayValue string
}

// ContentVerdict is the editor answer: compatible plus the edited
// composition — always freshly copied, never aliasing the input —
// and the stable reasons when not. A refused edit returns the
// input composition unchanged.
type ContentVerdict struct {
	Compatible  bool
	Reasons     []string
	Composition PageComposition
}

// EditBindingContent sets one content-tier widget binding's values.
// Rules, in order: the index must name a binding; the widget must
// be registered and content-tier (governed contracts and
// sandboxed embeds are not author-editable); neither value may
// carry raw markup; masked bindings still need their display
// form. The markup sniff is deliberately conservative tag-open
// grammar — `<` plus a letter, slash, bang, or query refuses,
// bare angle brackets pass — and anything deeper (entities,
// URLs, origins) stays with the content-safety validation step
// at publication (ValidateContentSafety, folded into
// ValidateComposition). Classification, ceiling, and version checks stay
// in their own validate steps.
func EditBindingContent(composition PageComposition, registry WidgetRegistry, edit ContentEdit) ContentVerdict {
	edited := PageComposition{
		Purpose:               composition.Purpose,
		Audience:              composition.Audience,
		Floorplan:             composition.Floorplan,
		FloorplanVersion:      composition.FloorplanVersion,
		ClassificationCeiling: composition.ClassificationCeiling,
		Primitives:            append([]string(nil), composition.Primitives...),
		Regions:               append([]string(nil), composition.Regions...),
		Widgets:               append([]WidgetBinding(nil), composition.Widgets...),
		Actions:               append([]ActionBinding(nil), composition.Actions...),
	}
	refuse := func(reason string) ContentVerdict {
		pristine := PageComposition{
			Purpose:               composition.Purpose,
			Audience:              composition.Audience,
			Floorplan:             composition.Floorplan,
			FloorplanVersion:      composition.FloorplanVersion,
			ClassificationCeiling: composition.ClassificationCeiling,
			Primitives:            append([]string(nil), composition.Primitives...),
			Regions:               append([]string(nil), composition.Regions...),
			Widgets:               append([]WidgetBinding(nil), composition.Widgets...),
			Actions:               append([]ActionBinding(nil), composition.Actions...),
		}
		return ContentVerdict{Compatible: false, Reasons: []string{reason}, Composition: pristine}
	}
	if edit.BindingIndex < 0 || edit.BindingIndex >= len(composition.Widgets) {
		return refuse(fmt.Sprintf("unknown binding index %d", edit.BindingIndex))
	}
	binding := composition.Widgets[edit.BindingIndex]
	definition, ok := widgetByType(registry, binding.WidgetType)
	if !ok {
		return refuse(fmt.Sprintf("unknown widget %q", binding.WidgetType))
	}
	if definition.Tier != WidgetTierContent {
		return refuse(fmt.Sprintf("widget %q is not content-editable", binding.WidgetType))
	}
	if containsRawMarkup(edit.Value) {
		return refuse("content value carries raw markup")
	}
	if containsRawMarkup(edit.DisplayValue) {
		return refuse("content display value carries raw markup")
	}
	if composition.Widgets[edit.BindingIndex].Masked && edit.DisplayValue == "" {
		return refuse("masked binding needs a display value")
	}
	edited.Widgets[edit.BindingIndex].Value = edit.Value
	edited.Widgets[edit.BindingIndex].DisplayValue = edit.DisplayValue
	return ContentVerdict{Compatible: true, Composition: edited}
}

// widgetByType resolves one registered widget definition.
func widgetByType(registry WidgetRegistry, id string) (WidgetDefinition, bool) {
	for _, widget := range registry.Widgets {
		if widget.ID == id {
			return widget, true
		}
	}
	return WidgetDefinition{}, false
}

// containsRawMarkup sniffs tag-open grammar: `<` followed by a
// letter, slash, bang, or query. Bare angle brackets pass —
// conservative toward refusal, never toward invention.
func containsRawMarkup(value string) bool {
	for i := 0; i+1 < len(value); i++ {
		if value[i] != '<' {
			continue
		}
		next := value[i+1]
		if next == '/' || next == '!' || next == '?' ||
			(next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') {
			return true
		}
	}
	return false
}
