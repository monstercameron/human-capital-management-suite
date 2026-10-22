package workspace

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_CHATUX_044: a person who has not chosen a colour mode gets the
// system setting, on the server-rendered document (so there is no flash of the
// other theme), and a stored choice wins over it.
func TestTodo_CHATUX_044(t *testing.T) {
	h, token := newShellHandler(t, false)
	cases := []struct {
		name         string
		organization string
		personal     string
		mode, scheme string
	}{
		{"a new person follows the system", "system", "", "system", `content="light dark"`},
		{"a stored light choice wins", "system", "light", "light", `content="light"`},
		{"a stored dark choice wins", "system", "dark", "dark", `content="dark"`},
		{"an organization's dark mode applies when the person has not chosen", "dark", "", "dark", `content="dark"`},
	}
	for _, c := range cases {
		snapshot := preferences.DefaultSnapshot()
		snapshot.Theme.ColorMode = c.organization
		snapshot.User.Accessibility.ColorMode = c.personal
		h.preferences = uxblindTPreferenceStore{snapshot: snapshot}
		recorder := uxblindTRequest(t, h, token, PathProductHome)
		if recorder.Code != 200 {
			t.Fatalf("%s: GET product shell = %d", c.name, recorder.Code)
		}
		body := recorder.Body.String()
		if root := firstPaintRoot(t, body); !strings.Contains(root, `data-hcm-color-mode="`+c.mode+`"`) {
			t.Errorf("%s: the document's root is %s, want mode %q", c.name, root, c.mode)
		}
		if !strings.Contains(body, `<meta name="color-scheme" `+c.scheme) {
			t.Errorf("%s: the colour-scheme meta is not %s, so the canvas would paint before the stylesheet", c.name, c.scheme)
		}
	}
}

// TestTodo_CHATUX_044_Browser renders the document without a server for a
// person with no stored preference, and for one whose store could not be read:
// both start on the system mode, never on light.
func TestTodo_CHATUX_044_Browser(t *testing.T) {
	for _, theme := range []productui.CustomerTheme{productui.DefaultCustomerTheme(), {}} {
		doc := firstPaintDocument(t, theme, productui.DefaultAccessibilityPreferences())
		if root := firstPaintRoot(t, doc); !strings.Contains(root, `data-hcm-color-mode="system"`) {
			t.Errorf("a person with no choice starts on %s, want the system mode", root)
		}
		if !strings.Contains(doc, `<meta name="color-scheme" content="light dark">`) {
			t.Errorf("the colour-scheme meta does not follow the device")
		}
	}
}
