package wcag

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/tokens"
)

func TestTodo_UIPOLISH_005(t *testing.T) {
	for _, theme := range []Theme{DefaultTheme(), DarkTheme()} {
		report := QualifyTheme(theme)
		if !report.Passed {
			t.Errorf("%s failed: %+v", theme.Name, report.Pairs)
		}
		if len(report.Pairs) < 10 {
			t.Fatalf("%s checks too few semantic pairs", theme.Name)
		}
	}
	theme, report, err := GenerateTenantTheme("acme", "#7c3aed")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || theme.Colors["accent"] == "#7c3aed" && report.Preview == "" {
		t.Fatalf("tenant preview missing: %+v", report)
	}
	modes, reports, err := GenerateTenantThemeModes("acme", "#ffffff")
	if err != nil || len(modes) != 2 || !reports["light"].Passed || !reports["dark"].Passed {
		t.Fatalf("tenant mode qualification failed: %v %#v", err, reports)
	}
}

func TestTodo_UIPOLISH_005_Property(t *testing.T) {
	for _, color := range []string{"#000000", "#ffffff", "#123456", "#abcdef", "#ff0000", "#00ff00", "#0000ff"} {
		theme, report, err := GenerateTenantTheme("property", color)
		if err != nil {
			t.Fatal(err)
		}
		ratio, err := tokens.ContrastRatio(theme.Colors["accent-text"], theme.Colors["accent"])
		if err != nil || ratio < tokens.MinRatioNormalText {
			t.Fatalf("%s generated unsafe accent %s: %.2f (%v)", color, theme.Colors["accent"], ratio, err)
		}
		if !report.Passed {
			t.Fatalf("generated theme %s failed: %s", color, report.Preview)
		}
	}
}

func TestTodo_UIPOLISH_005_Golden(t *testing.T) {
	r := QualifyTheme(DefaultTheme())
	if got, want := PairNames(r), []string{"accent-text/accent", "border/surface", "danger-text/danger", "divider/surface", "focus/background", "hover-text/hover", "info/surface", "muted/background", "muted/surface", "selection-text/selection", "success/surface", "text/background", "text/surface", "warning-text/warning"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("semantic pair names = %v, want %v", got, want)
	}
}

func TestTodo_UIPOLISH_005_Accessibility_EssentialBoundaryUsesThreeToOne(t *testing.T) {
	theme := DefaultTheme()
	theme.Colors["border"] = tokens.Palette.Border.Hex
	report := QualifyTheme(theme)
	var essential, divider PairResult
	for _, pair := range report.Pairs {
		switch pair.Pair.Name {
		case "border/surface":
			essential = pair
		case "divider/surface":
			divider = pair
		}
	}
	if essential.Pass || essential.Pair.MinRatio != 3 {
		t.Fatalf("essential boundary accepted below 3:1: %+v", essential)
	}
	if !divider.Pass || divider.Pair.MinRatio != 1.4 {
		t.Fatalf("decorative divider lost its lower floor: %+v", divider)
	}
}

func TestTodo_UIPOLISH_005_Accessibility(t *testing.T) {
	bad := DefaultTheme()
	bad.Colors["text-muted"] = "#eeeeee"
	if QualifyTheme(bad).Passed {
		t.Fatal("unreadable muted text was accepted")
	}
	bad = DefaultTheme()
	bad.Colors["border"] = bad.Colors["surface"]
	if QualifyTheme(bad).Passed {
		t.Fatal("invisible border was accepted")
	}
}

func TestTodo_UIPOLISH_005_Regression(t *testing.T) {
	if _, _, err := GenerateTenantTheme("bad", "purple"); err == nil {
		t.Fatal("malformed tenant brand accepted")
	}
	if _, _, err := GenerateTenantTheme("bad", "#123"); err == nil {
		t.Fatal("short tenant brand accepted")
	}
}

func TestTodo_UIPOLISH_005_Property_AllTenantModesQualify(t *testing.T) {
	rng := rand.New(rand.NewSource(005))
	for i := 0; i < 128; i++ {
		brand := fmt.Sprintf("#%06x", rng.Intn(1<<24))
		modes, reports, err := GenerateTenantThemeModes("property", brand)
		if err != nil {
			t.Fatalf("brand %s rejected: %v", brand, err)
		}
		for _, mode := range []string{"light", "dark"} {
			if !reports[mode].Passed {
				t.Fatalf("brand %s mode %s failed: %+v", brand, mode, reports[mode].Pairs)
			}
			accentRatio, err := tokens.ContrastRatio(modes[mode].Colors["accent-text"], modes[mode].Colors["accent"])
			if err != nil || accentRatio < tokens.MinRatioNormalText {
				t.Fatalf("brand %s mode %s accent ratio %.2f (%v)", brand, mode, accentRatio, err)
			}
		}
	}
}

func TestTodo_UIPOLISH_005_AdmissionRejectsMissingSemanticColor(t *testing.T) {
	for _, key := range []string{"text", "text-muted", "background", "surface", "border", "accent", "accent-text", "success", "warning", "warning-text", "danger", "danger-text", "info", "focus", "selection", "selection-text", "hover", "hover-text"} {
		theme := DefaultTheme()
		delete(theme.Colors, key)
		report := QualifyTheme(theme)
		if report.Passed {
			t.Fatalf("missing semantic color %q was accepted", key)
		}
		found := false
		for _, pair := range report.Pairs {
			if !pair.Pass && strings.Contains(pair.Error, "expected #rrggbb") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing semantic color %q did not produce a parse failure: %+v", key, report.Pairs)
		}
	}
}

func TestTodo_UIPOLISH_005_Regression_DarkBrandGetsReadableDarkModeText(t *testing.T) {
	modes, reports, err := GenerateTenantThemeModes("dark-brand", "#000000")
	if err != nil || !reports["dark"].Passed {
		t.Fatalf("dark brand was not admitted safely: %v %+v", err, reports["dark"])
	}
	if modes["dark"].Colors["accent-text"] != "#ffffff" {
		t.Fatalf("dark accent text = %s, want protected white text", modes["dark"].Colors["accent-text"])
	}
}

func TestTodo_UIPOLISH_005_GenericQualifierDoesNotClaimProductAdmission(t *testing.T) {
	_, report, err := GenerateTenantTheme("reference", "#1d4f91")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(report.Preview), "production") || strings.Contains(strings.ToLower(report.Preview), "admitted") {
		t.Fatalf("generic preview made a product-admission claim: %q", report.Preview)
	}
}

// TestTodo_UIPOLISH_005_SemanticContract validates the qualified semantic
// pairs used by browser-facing themes. It is not live browser automation;
// the UIPOLISH-005 browser matrix remains open.
func TestTodo_UIPOLISH_005_SemanticContract(t *testing.T) {
	for _, theme := range []Theme{DefaultTheme(), DarkTheme()} {
		for _, pair := range QualifyTheme(theme).Pairs {
			if !pair.Pass {
				t.Fatalf("browser semantic pair %s failed", pair.Pair.Name)
			}
		}
	}
}
