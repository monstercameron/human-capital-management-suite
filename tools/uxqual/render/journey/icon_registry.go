package journey

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// IconID is a semantic icon name. Callers should request an ID rather than
// embedding a private SVG or choosing a glyph by appearance.
type IconID string

const (
	IconBrandMark     IconID = "brand-mark"
	IconBrandWordmark IconID = "brand-wordmark"
	IconCheck         IconID = "check"
	IconArrowRight    IconID = "arrow-right"
	IconArrowLeft     IconID = "arrow-left"
	IconInfo          IconID = "info"
	IconSuccess       IconID = "success"
	IconWarning       IconID = "warning"
	IconDanger        IconID = "danger"
	IconLedger        IconID = "ledger"
	IconEmpty         IconID = "empty"
	IconClock         IconID = "clock"
	IconPerson        IconID = "person"
	IconSpark         IconID = "spark"
)

// IconSpec describes the governed presentation contract for one semantic
// icon. All current journey glyphs are decorative because adjacent localized
// text carries their meaning; the fields make that decision explicit to
// renderers and tests.
type IconSpec struct {
	ID                IconID
	Size              string
	StrokeWidth       string
	Decorative        bool
	AccessibleName    string
	Substitutable     bool
	SubstitutionGroup string
}

func iconSpec(id IconID) (IconSpec, bool) {
	switch id {
	case IconBrandMark:
		return IconSpec{ID: id, Size: "28", StrokeWidth: "2.5", Decorative: true, Substitutable: true, SubstitutionGroup: "brand"}, true
	case IconBrandWordmark:
		return IconSpec{ID: id, Size: "28", StrokeWidth: "2.5", Decorative: true, Substitutable: true, SubstitutionGroup: "brand"}, true
	case IconCheck:
		return IconSpec{ID: id, Size: "14", StrokeWidth: "2", Decorative: true}, true
	case IconArrowRight, IconArrowLeft:
		return IconSpec{ID: id, Size: iconSize, StrokeWidth: "2", Decorative: true, Substitutable: true, SubstitutionGroup: "navigation"}, true
	case IconInfo, IconSuccess, IconWarning, IconDanger, IconLedger:
		return IconSpec{ID: id, Size: "18", StrokeWidth: "2", Decorative: true}, true
	case IconEmpty:
		return IconSpec{ID: id, Size: "32", StrokeWidth: "2", Decorative: true}, true
	case IconClock, IconPerson:
		return IconSpec{ID: id, Size: "14", StrokeWidth: "2", Decorative: true}, true
	case IconSpark:
		return IconSpec{ID: id, Size: "12", StrokeWidth: "2", Decorative: true}, true
	default:
		return IconSpec{}, false
	}
}

// IconRegistry returns a copy so a customer cannot mutate the process-wide
// safety contract or alter another tenant's rendering.
func IconRegistry() map[IconID]IconSpec {
	ids := []IconID{IconBrandMark, IconBrandWordmark, IconCheck, IconArrowRight, IconArrowLeft, IconInfo, IconSuccess, IconWarning, IconDanger, IconLedger, IconEmpty, IconClock, IconPerson, IconSpark}
	registry := make(map[IconID]IconSpec, len(ids))
	for _, id := range ids {
		if spec, ok := iconSpec(id); ok {
			registry[id] = spec
		}
	}
	return registry
}

// IconPack contains optional customer substitutions using only closed,
// built-in semantic IDs. It deliberately has no callback or raw SVG escape
// hatch: status and safety glyphs can never be replaced by customer input.
type IconPack map[IconID]IconID

// RenderIcon resolves a semantic ID to the governed built-in glyph. A pack
// may replace brand/navigation artwork, but cannot replace status/safety
// meaning. Unknown IDs deliberately fall back to the neutral info glyph.
func RenderIcon(id IconID, class string, pack IconPack) ui.Node {
	if spec, ok := iconSpec(id); ok && spec.Substitutable && pack != nil {
		if replacement, exists := pack[id]; exists {
			if replacementSpec, valid := iconSpec(replacement); valid && replacementSpec.Substitutable && replacementSpec.SubstitutionGroup == spec.SubstitutionGroup {
				return builtInIcon(replacement, class)
			}
		}
	}
	return builtInIcon(id, class)
}

func builtInIcon(id IconID, class string) ui.Node {
	switch id {
	case IconBrandMark:
		return brandMark(class)
	case IconBrandWordmark:
		return brandWordmark(class)
	case IconCheck:
		return iconCheck(class)
	case IconArrowRight:
		return iconArrowRight(class)
	case IconArrowLeft:
		return iconArrowLeft(class)
	case IconInfo:
		return iconInfo(class)
	case IconSuccess:
		return iconSuccess(class)
	case IconWarning:
		return iconWarning(class)
	case IconDanger:
		return iconDanger(class)
	case IconLedger:
		return iconLedger(class)
	case IconEmpty:
		return iconEmpty(class)
	case IconClock:
		return iconClock(class)
	case IconPerson:
		return iconPerson(class)
	case IconSpark:
		return iconSpark(class)
	default:
		return iconInfo(class)
	}
}

// brandWordmark is a second governed brand treatment. It is intentionally a
// distinct built-in drawing so an approved customer brand substitution
// changes the rendered artwork without accepting raw SVG input.
func brandWordmark(class string) ui.Node {
	return html.Svg(html.Props{
		Class:  class,
		Width:  "28",
		Height: "28",
		Raw: map[string]any{
			"viewBox":     "0 0 32 32",
			"fill":        "none",
			"aria-hidden": "true",
			"focusable":   "false",
		},
	},
		html.Rect(html.Props{Raw: map[string]any{
			"x": "1.25", "y": "1.25", "width": "29.5", "height": "29.5", "rx": "8.5",
			"stroke": "currentColor", "stroke-width": "2.5", "fill": "none",
		}}),
		html.Path(html.Props{Raw: map[string]any{
			"d":      "M7 10h18M7 16h12M7 22h18",
			"stroke": "currentColor", "stroke-width": "2.5", "stroke-linecap": "round",
		}}),
	)
}
