package journey

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-017's RED was measured on the running server: a terminal journey
// rendered "This proposal has already reached a terminal outcome. There is
// nothing left to withdraw, cancel or edit." once under each of Withdraw,
// Request cancellation and Edit proposal -- the same sentence three times,
// crowding out the one thing a reader could still do. The same page also
// renamed a control between states: "Review and withdraw" when it was
// offered, "Withdraw" when it was not.

const uxlive017Terminal = "This proposal has already reached a terminal outcome. There is nothing left to withdraw, cancel or edit."

func uxlive017Actions(reasons ...string) []Action {
	labels := []string{"Withdraw", "Request cancellation", "Edit proposal"}
	out := make([]Action, 0, len(reasons))
	for i, reason := range reasons {
		out = append(out, Action{
			ID: strings.ToLower(strings.ReplaceAll(labels[i], " ", "-")), Label: labels[i],
			Description: labels[i] + " description.", Disabled: true, DisabledReason: reason,
			Confirmation: []Fact{{Label: "Employee", Value: "Omar"}},
		})
	}
	return out
}

func uxlive017Render(t *testing.T, actions []Action) string {
	t.Helper()
	markup, err := ui.RenderToString(actionsSection(live{locale: "en-US"}, actions))
	if err != nil {
		t.Fatalf("render actions: %v", err)
	}
	return markup
}

// TestTodo_UXLIVE_017 is the primary red/green test: one reason shared by
// every action in the section is stated once.
func TestTodo_UXLIVE_017(t *testing.T) {
	shared := uxlive017Render(t, uxlive017Actions(uxlive017Terminal, uxlive017Terminal, uxlive017Terminal))
	if got := strings.Count(shared, uxlive017Terminal); got != 1 {
		t.Fatalf("the shared reason appears %d times, want 1:\n%s", got, shared)
	}

	// Every disabled control still points at a reason that exists in the
	// document, so the association a screen reader follows is unbroken.
	described := regexp.MustCompile(`aria-describedby="([^"]+)"`).FindAllStringSubmatch(shared, -1)
	if len(described) != 3 {
		t.Fatalf("%d of 3 disabled controls name a reason:\n%s", len(described), shared)
	}
	for _, match := range described {
		if !strings.Contains(shared, `id="`+match[1]+`"`) {
			t.Fatalf("control points at reason %q, which the document does not contain:\n%s", match[1], shared)
		}
	}

	// Reasons that genuinely differ stay on their own cards.
	distinct := uxlive017Render(t, uxlive017Actions(uxlive017Terminal, "Use Withdraw instead of Cancel.", uxlive017Terminal))
	for _, want := range []string{uxlive017Terminal, "Use Withdraw instead of Cancel."} {
		if !strings.Contains(distinct, want) {
			t.Fatalf("a distinct reason was dropped: %q\n%s", want, distinct)
		}
	}
	if got := strings.Count(distinct, uxlive017Terminal); got != 2 {
		t.Fatalf("two cards share a reason with a third that differs; it appears %d times, want 2:\n%s", got, distinct)
	}
}

// TestTodo_UXLIVE_017_Browser keeps a control's name stable across states:
// the same action must not be called one thing when it is offered and
// another when it is not.
func TestTodo_UXLIVE_017_Browser(t *testing.T) {
	offered := Action{ID: "withdraw", Label: "Withdraw", Description: "Stops this proposal.",
		Confirmation: []Fact{{Label: "Employee", Value: "Omar"}}}
	refused := offered
	refused.Disabled = true
	refused.DisabledReason = uxlive017Terminal

	offeredMarkup := uxlive017Render(t, []Action{offered})
	refusedMarkup := uxlive017Render(t, []Action{refused})

	name := regexp.MustCompile(`>(Review and withdraw|Withdraw)<`)
	offeredNames := name.FindAllStringSubmatch(offeredMarkup, -1)
	refusedNames := name.FindAllStringSubmatch(refusedMarkup, -1)
	if len(offeredNames) == 0 || len(refusedNames) == 0 {
		t.Fatalf("the control has no name in one of its states:\noffered:\n%s\nrefused:\n%s", offeredMarkup, refusedMarkup)
	}
	if offeredNames[0][1] != refusedNames[0][1] {
		t.Fatalf("the control is called %q when offered and %q when refused",
			offeredNames[0][1], refusedNames[0][1])
	}
}
