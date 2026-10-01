package preferences

import "testing"

func TestTodo_UXBLIND_067_ColorMode(t *testing.T) {
	value := NormalizeUser(User{Accessibility: Accessibility{ColorMode: " DARK "}})
	if value.Accessibility.ColorMode != "dark" {
		t.Fatalf("normalized personal color mode = %q, want dark", value.Accessibility.ColorMode)
	}
	for _, invalid := range []string{"organization", "system", "sepia"} {
		value := NormalizeUser(User{Accessibility: Accessibility{ColorMode: invalid}})
		if value.Accessibility.ColorMode != "" {
			t.Errorf("invalid personal color mode %q normalized to %q, want inherit", invalid, value.Accessibility.ColorMode)
		}
	}
}
