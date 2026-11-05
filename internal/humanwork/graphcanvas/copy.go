package graphcanvas

// CanvasCopy is the localizable chrome copy: canvas, toolbar, minimap and
// node/port templates. Geometry and keyboard directions never localize: the
// canvas is a spatial surface, so arrows move visually under every locale.
type CanvasCopy struct {
	CanvasLabel   string
	MinimapLabel  string
	ZoomInLabel   string
	ZoomOutLabel  string
	ResetLabel    string
	NodeLabel     func(label string) string
	PortLabel     func(nodeLabel, portLabel string) string
	EmptyLabel    string
	SelectedLabel func(label string) string
}

// CopyFor resolves the copy table for a locale, falling back to English for
// anything untranslated. Locales are matched case-insensitively on the
// language subtag ("ar-EG" resolves "ar").
func CopyFor(locale string) CanvasCopy {
	en := CanvasCopy{
		CanvasLabel:  "Graph canvas",
		MinimapLabel: "Minimap",
		ZoomInLabel:  "Zoom in",
		ZoomOutLabel: "Zoom out",
		ResetLabel:   "Reset view",
		EmptyLabel:   "Empty graph",
		NodeLabel: func(label string) string {
			if label == "" {
				return "Node"
			}
			return "Node " + label
		},
		PortLabel: func(nodeLabel, portLabel string) string {
			if portLabel == "" {
				return "Connect " + nodeLabel
			}
			return "Connect " + nodeLabel + " " + portLabel
		},
		SelectedLabel: func(label string) string {
			if label == "" {
				return "Selected node"
			}
			return "Selected node " + label
		},
	}
	switch languageSubtag(locale) {
	case "ar":
		ar := en
		ar.CanvasLabel = "لوحة الرسم البياني"
		ar.MinimapLabel = "خريطة مصغرة"
		ar.ZoomInLabel = "تكبير"
		ar.ZoomOutLabel = "تصغير"
		ar.ResetLabel = "إعادة ضبط العرض"
		ar.EmptyLabel = "رسم بياني فارغ"
		ar.NodeLabel = func(label string) string {
			if label == "" {
				return "عقدة"
			}
			return "عقدة " + label
		}
		ar.PortLabel = func(nodeLabel, portLabel string) string {
			if portLabel == "" {
				return "ربط " + nodeLabel
			}
			return "ربط " + nodeLabel + " " + portLabel
		}
		ar.SelectedLabel = func(label string) string {
			if label == "" {
				return "عقدة محددة"
			}
			return "عقدة محددة " + label
		}
		return ar
	case "de":
		de := en
		de.CanvasLabel = "Graph-Arbeitsfläche"
		de.MinimapLabel = "Minikarte"
		de.ZoomInLabel = "Vergrößern"
		de.ZoomOutLabel = "Verkleinern"
		de.ResetLabel = "Ansicht zurücksetzen"
		de.EmptyLabel = "Leerer Graph"
		de.NodeLabel = func(label string) string {
			if label == "" {
				return "Knoten"
			}
			return "Knoten " + label
		}
		de.PortLabel = func(nodeLabel, portLabel string) string {
			if portLabel == "" {
				return nodeLabel + " verbinden"
			}
			return nodeLabel + " " + portLabel + " verbinden"
		}
		de.SelectedLabel = func(label string) string {
			if label == "" {
				return "Ausgewählter Knoten"
			}
			return "Ausgewählter Knoten " + label
		}
		return de
	default:
		return en
	}
}

// DirFor resolves the base direction for a locale: rtl for right-to-left
// languages, ltr otherwise. Only chrome text follows direction; canvas
// geometry is direction-agnostic.
func DirFor(locale string) string {
	switch languageSubtag(locale) {
	case "ar", "he", "fa", "ur":
		return "rtl"
	default:
		return "ltr"
	}
}

func languageSubtag(locale string) string {
	for i := 0; i < len(locale); i++ {
		c := locale[i]
		if c == '-' || c == '_' {
			return lowerASCII(locale[:i])
		}
	}
	return lowerASCII(locale)
}

func lowerASCII(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}
