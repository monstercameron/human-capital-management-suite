package journey

import (
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UIPOLISH_010(t *testing.T) {
	registry := IconRegistry()
	if len(registry) != 14 {
		t.Fatalf("registry contains %d icons, want 14", len(registry))
	}
	for id, spec := range registry {
		if spec.ID != id || spec.Size == "" || spec.StrokeWidth == "" {
			t.Errorf("%q has incomplete governed geometry: %+v", id, spec)
		}
		if !spec.Decorative || spec.AccessibleName != "" {
			t.Errorf("%q has inconsistent decorative accessibility policy: %+v", id, spec)
		}
	}
}

func TestTodo_UIPOLISH_010_Golden(t *testing.T) {
	ids := make([]string, 0, len(IconRegistry()))
	for id := range IconRegistry() {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	want := "arrow-left,arrow-right,brand-mark,brand-wordmark,check,clock,danger,empty,info,ledger,person,spark,success,warning"
	if got := strings.Join(ids, ","); got != want {
		t.Fatalf("icon ID golden = %q, want %q", got, want)
	}
	// Keep the golden proof tied to rendered artwork, not only registry keys.
	// These stable path bytes catch accidental aliasing or geometry drift.
	for id, path := range map[IconID]string{
		IconBrandMark:     "M9 22v-5h4.5v5H9",
		IconBrandWordmark: "M7 10h18M7 16h12M7 22h18",
		IconArrowLeft:     "m11 18-6-6 6-6",
		IconArrowRight:    "m13 6 6 6-6 6",
		IconDanger:        "m15 9-6 6",
	} {
		out := renderNode(t, RenderIcon(id, "golden", nil))
		if !strings.Contains(out, path) {
			t.Errorf("%q golden artwork is missing path bytes %q: %s", id, path, out)
		}
	}
}

func TestTodo_UIPOLISH_010_Markup(t *testing.T) {
	for id := range IconRegistry() {
		out, err := ui.RenderToString(RenderIcon(id, "governed-icon", nil))
		if err != nil {
			t.Fatalf("render %q: %v", id, err)
		}
		if !strings.Contains(out, `aria-hidden="true"`) || !strings.Contains(out, `focusable="false"`) {
			t.Errorf("%q is not silent and non-focusable in browser markup: %s", id, out)
		}
	}
}

func TestTodo_UIPOLISH_010_Accessibility(t *testing.T) {
	for id, spec := range IconRegistry() {
		if spec.Decorative && spec.AccessibleName != "" {
			t.Errorf("decorative %q must not expose a competing accessible name", id)
		}
	}
}

func TestTodo_UIPOLISH_010_Security(t *testing.T) {
	pack := IconPack{IconBrandMark: IconBrandWordmark, IconArrowRight: IconArrowLeft}
	for _, allowed := range []IconID{IconBrandMark, IconArrowRight} {
		if got := renderNode(t, RenderIcon(allowed, "allowed", pack)); !strings.Contains(got, "<svg ") {
			t.Errorf("allowed icon %q did not resolve to a built-in SVG", allowed)
		}
	}
	invalid := IconPack{IconArrowRight: IconDanger}
	if got := renderNode(t, RenderIcon(IconArrowRight, "safe", invalid)); strings.Contains(got, "m15 9-6 6") {
		t.Fatal("customer substitution crossed from navigation into protected status semantics")
	}
}

func TestTodo_UIPOLISH_010_SecurityProtectedMappingsIgnored(t *testing.T) {
	protected := []IconID{IconCheck, IconInfo, IconSuccess, IconWarning, IconDanger, IconLedger, IconEmpty, IconClock, IconPerson, IconSpark}
	for _, id := range protected {
		t.Run(string(id), func(t *testing.T) {
			baseline := renderNode(t, RenderIcon(id, "protected", nil))
			pack := IconPack{id: IconBrandWordmark}
			got := renderNode(t, RenderIcon(id, "protected", pack))
			if got != baseline {
				t.Fatalf("protected mapping changed rendered artwork: baseline=%s got=%s", baseline, got)
			}
		})
	}
}

func TestTodo_UIPOLISH_010_NavigationSubstitutionRendersVariant(t *testing.T) {
	base := renderNode(t, RenderIcon(IconArrowRight, "nav", nil))
	variant := renderNode(t, RenderIcon(IconArrowRight, "nav", IconPack{IconArrowRight: IconArrowLeft}))
	if strings.Contains(variant, "m13 6 6 6-6 6") {
		t.Fatalf("navigation substitution retained the right-arrow path: %s", variant)
	}
	if !strings.Contains(variant, "m11 18-6-6 6-6") || variant == base {
		t.Fatalf("navigation substitution did not render the governed left-arrow variant: %s", variant)
	}
}

func TestTodo_UIPOLISH_010_BrandSubstitutionRendersVariant(t *testing.T) {
	base := renderNode(t, RenderIcon(IconBrandMark, "brand", nil))
	variant := renderNode(t, RenderIcon(IconBrandMark, "brand", IconPack{IconBrandMark: IconBrandWordmark}))
	if variant == base {
		t.Fatal("brand substitution did not change rendered artwork bytes")
	}
	if !strings.Contains(variant, "M7 10h18M7 16h12M7 22h18") {
		t.Fatalf("brand substitution did not render the governed wordmark variant: %s", variant)
	}
}

func TestTodo_UIPOLISH_010_Regression(t *testing.T) {
	registry := IconRegistry()
	registry[IconDanger] = IconSpec{}
	if fresh := IconRegistry()[IconDanger]; fresh.ID != IconDanger {
		t.Fatal("mutating a returned registry changed the governed registry")
	}
	if got := renderNode(t, RenderIcon("unknown", "unknown", nil)); !strings.Contains(got, "M12 11v5") {
		t.Fatal("unknown semantic ID did not safely fall back to the neutral info icon")
	}
	if got := renderNode(t, RenderIcon(IconDanger, "danger", nil)); !strings.Contains(got, "m15 9-6 6") {
		t.Fatalf("danger semantic ID no longer resolves to the governed danger glyph: %s", got)
	}
	if got := renderNode(t, RenderIcon(IconArrowRight, "arrow", nil)); !strings.Contains(got, `class="arrow"`) {
		t.Fatalf("arrow semantic ID dropped its styling hook: %s", got)
	}
}
