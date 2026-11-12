package projectui

import (
	"strings"
	"testing"
)

// TestTodo_66_NarrowTicketsLayout pins the phone-width rules for the Tickets
// tab: the filter bar may not keep its 20rem flex basis (which overflows a
// 390 px or 320 px viewport), and the empty-state boxes must size to their
// container rather than their text. Reverting any rule fails the test.
func TestTodo_66_NarrowTicketsLayout(t *testing.T) {
	pins := []string{
		"@media (max-width:40rem){.projectui-tickets .projectui-tickets-filters{flex-wrap:wrap;overflow-x:visible;padding-block-end:.25rem;inline-size:100%;flex:1 1 100%;min-inline-size:0;max-inline-size:100%}}",
		".projectui-tickets-toolbar{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:.5rem 1rem;min-inline-size:0;max-inline-size:100%}",
		".projectui-tickets-empty{display:grid;grid-template-columns:minmax(0,1fr);",
		".project-my-tickets-empty{display:grid;grid-template-columns:minmax(0,1fr);",
	}
	for _, pin := range pins {
		if !strings.Contains(projectUIShareStyles, pin) {
			t.Fatalf("stylesheet lost narrow-layout rule: %s", pin)
		}
	}
}
