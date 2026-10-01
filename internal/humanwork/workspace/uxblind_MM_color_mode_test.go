package workspace

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_UXBLIND_067_ColorMode_Integration(t *testing.T) {
	h, token := newShellHandler(t, false)
	snapshot := preferences.DefaultSnapshot()
	snapshot.Theme.ColorMode = "light"
	snapshot.User.Accessibility.ColorMode = "dark"
	h.preferences = uxblindTPreferenceStore{snapshot: snapshot}
	recorder := uxblindTRequest(t, h, token, PathProductHome)
	if recorder.Code != 200 {
		t.Fatalf("GET product shell = %d: %s", recorder.Code, recorder.Body.String())
	}
	root := firstPaintRoot(t, recorder.Body.String())
	if !strings.Contains(root, `data-hcm-color-mode="dark"`) {
		t.Fatalf("personal dark mode did not win on first paint: %s", root)
	}
	if !strings.Contains(root, `data-hcm-organization-color-mode="light"`) {
		t.Fatalf("organization fallback was not retained for reset: %s", root)
	}
}

func TestTodo_UXBLIND_067_ColorMode_Browser(t *testing.T) {
	theme := productui.DefaultCustomerTheme()
	theme.ColorMode = "light"
	doc, err := productShellDocumentForRouteStateWithPreferences(JourneyConfig{}, false,
		productui.ResolveProductLocale("en-US"), productui.PageHome, "", "", theme,
		productui.AccessibilityPreferences{ColorMode: "dark"}, productStylesheet())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(firstPaintRoot(t, doc), `data-hcm-color-mode="dark"`) || !strings.Contains(doc, `content="dark"`) {
		t.Fatalf("browser first paint did not declare the personal dark mode")
	}
}
