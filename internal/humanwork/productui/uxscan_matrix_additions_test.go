package productui

import (
	"fmt"
	"strings"
	"testing"
)

func TestTodo_UXSCAN_005_Performance(t *testing.T) {
	view := NewView(PagePeople, "HarborCare", "viewer", "scope")
	const peopleCount = 100
	view.PeoplePageSize = peopleCount
	view.People = make([]Person, peopleCount)
	for i := range view.People {
		view.People[i] = Person{ID: fmt.Sprintf("worker-%03d", i), Name: fmt.Sprintf("Worker %03d", i), WorkerNumber: fmt.Sprintf("HC-%03d", i)}
	}

	markup, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(markup, `people-row-item people-row`); got != peopleCount {
		t.Fatalf("rendered %d people rows, want %d", got, peopleCount)
	}
	// A directory row should contribute bounded markup; this catches accidental
	// repeated page rendering or runaway nested controls without a flaky timer.
	const maxBytesPerRow = 10000
	if limit := peopleCount * maxBytesPerRow; len(markup) > limit {
		t.Fatalf("%d-row directory rendered %d bytes, over the %d-byte bound", peopleCount, len(markup), limit)
	}
}

func TestTodo_UXSCAN_006(t *testing.T) {
	for _, preset := range palettePresets {
		modes, err := ResolveThemeModes(preset.Overrides)
		if err != nil {
			t.Fatalf("resolve %s palette: %v", preset.Option.ID, err)
		}
		assertPreviewContrast(t, preset.Option.ID, modes)
	}
	css := Stylesheet()
	for _, want := range []string{
		`.appearance-preview-bar{`,
		`:root[data-hcm-palette="evergreen"] .appearance-preview-window[data-hcm-preview-color-mode="dark"]`,
		`:root[data-hcm-palette="evergreen"] .appearance-preview-window[data-hcm-preview-color-mode="light"]`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("preview theme contract missing %q", want)
		}
	}
}

func TestTodo_UXSCAN_006_Golden(t *testing.T) {
	// Pin the neutral header's complete declarations: the original defect was
	// near-black brand ink on a dark preview header.
	const want = `align-items:center;background-color:var(--surface);border-bottom:1px solid var(--line);color:var(--ink);display:flex;gap:7px;padding-bottom:10px;padding-left:12px;padding-right:12px;padding-top:10px;`
	blocks := cssRuleBlocks(t, Stylesheet(), ".appearance-preview-bar")
	if len(blocks) != 1 || blocks[0] != want {
		t.Fatalf("appearance preview header rule changed from its readable surface/ink golden: %q", blocks)
	}
}

func TestTodo_UXSCAN_006_Accessibility(t *testing.T) {
	for _, preset := range palettePresets {
		modes, err := ResolveThemeModes(preset.Overrides)
		if err != nil {
			t.Fatalf("resolve %s palette: %v", preset.Option.ID, err)
		}
		for _, mode := range []ThemeMode{ThemeModeLight, ThemeModeDark} {
			theme := modes[mode]
			for _, pair := range [][3]string{
				{"color.text.primary", "color.surface", "text/surface"},
				{"color.on.brand", "color.brand.primary", "on-brand/brand"},
				{"color.focus", "color.surface", "focus/surface"},
				{"color.control.border", "color.surface", "control/surface"},
			} {
				foreground, _ := theme.Value(pair[0])
				background, _ := theme.Value(pair[1])
				ratio, err := contrastRatio(foreground, background)
				minimum := 4.5
				if pair[2] == "focus/surface" || pair[2] == "control/surface" {
					minimum = 3
				}
				if err != nil || ratio < minimum {
					t.Errorf("%s %s %s contrast %.2f:1, want >= %.1f:1 (%v)", preset.Option.ID, mode, pair[2], ratio, minimum, err)
				}
			}
		}
	}
}

func TestTodo_UXSCAN_006_Regression(t *testing.T) {
	css := Stylesheet()
	if strings.Contains(css, ".wordmark-mark,.appearance-preview-bar") ||
		strings.Contains(css, ".button.primary,.wordmark-mark,.appearance-preview-bar") {
		t.Fatal("preview header inherited foreground intended for brand-colored controls")
	}
	for _, mode := range []string{"light", "dark"} {
		selector := `:root[data-hcm-palette="evergreen"] .appearance-preview-window[data-hcm-preview-color-mode="` + mode + `"]`
		if !strings.Contains(css, selector) {
			t.Errorf("%s appearance preview lost palette-scoped token resolution", mode)
		}
	}
}
