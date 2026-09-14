package productui

import (
	"os"
	"strings"
	"testing"
)

func TestTodo_UIPOLISH_011_Regression_DrawerEscapeFromTrigger(t *testing.T) {
	if !drawerEscapeCloses("Escape") || drawerEscapeCloses("Enter") {
		t.Fatal("drawer trigger no longer shares the shell Escape predicate")
	}
	source, err := os.ReadFile("shell.go")
	if err != nil {
		t.Fatal(err)
	}
	trigger := string(source)
	start := strings.Index(trigger, `ID: "nav-drawer-trigger"`)
	if start < 0 {
		t.Fatal("mobile drawer trigger is absent")
	}
	trigger = trigger[start:]
	end := strings.Index(trigger, `navIcon("menu")`)
	if end < 0 {
		t.Fatal("mobile drawer trigger boundary changed")
	}
	trigger = trigger[:end]
	for _, want := range []string{`OnKeyDown: ui.UseEvent`, `drawerOpen && drawerEscapeCloses(event.GetKey())`, `event.PreventDefault()`, `drawerToggle()`} {
		if !strings.Contains(trigger, want) {
			t.Errorf("mobile drawer trigger lost Escape handling %q", want)
		}
	}
}
