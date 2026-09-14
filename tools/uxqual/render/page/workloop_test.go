package page

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

func renderWorkLoopFixture(t *testing.T, state pagedef.WorkLoopState) string {
	t.Helper()
	node, err := RenderProductionWorkLoop(state)
	if err != nil {
		t.Fatalf("RenderProductionWorkLoop(%q): %v", state, err)
	}
	out, err := ui.RenderToString(node)
	if err != nil {
		t.Fatalf("ui.RenderToString(%q): %v", state, err)
	}
	return out
}

// TestTodo_ALIGN_043 proves the production work-loop fixture resolves and
// renders its assigned-work, evidence, and recovery regions.
func TestTodo_ALIGN_043(t *testing.T) {
	out := renderWorkLoopFixture(t, pagedef.WorkLoopReady)
	for _, want := range []string{
		`id="page-work.my-work"`,
		`data-work-state="ready"`,
		"Promotion review",
		"Awaiting approval",
		"authorized work projection",
		"No action is required.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("ready work-loop output missing %q\n%s", want, out)
		}
	}
}

// TestTodo_ALIGN_043_Property proves transient and degraded states do not
// render the ready fixture item or claim a successful outcome.
func TestTodo_ALIGN_043_Property(t *testing.T) {
	for _, state := range []pagedef.WorkLoopState{
		pagedef.WorkLoopLoading,
		pagedef.WorkLoopEmpty,
		pagedef.WorkLoopStale,
		pagedef.WorkLoopDenied,
		pagedef.WorkLoopError,
		pagedef.WorkLoopRetrying,
	} {
		out := renderWorkLoopFixture(t, state)
		if strings.Contains(out, "Promotion review") || strings.Contains(out, "Awaiting approval") {
			t.Errorf("%q state rendered ready work as if it were current", state)
		}
		if strings.Contains(strings.ToLower(out), "success") || strings.Contains(strings.ToLower(out), "completed") {
			t.Errorf("%q state made an unsupported success claim", state)
		}
	}
}

// TestTodo_ALIGN_043_Golden pins the work-loop live region and source
// explanation, both of which are publication properties.
func TestTodo_ALIGN_043_Golden(t *testing.T) {
	out := renderWorkLoopFixture(t, pagedef.WorkLoopReady)
	if !strings.Contains(out, `id="live-region"`) || !strings.Contains(out, `aria-live="polite"`) {
		t.Fatalf("ready work-loop has no polite live region: %s", out)
	}
	if !strings.Contains(out, `data-source="authorized-work-projection"`) {
		t.Fatalf("ready work-loop omitted source authority marker: %s", out)
	}
}

// TestTodo_ALIGN_043_Security confirms fixture text is escaped by the normal
// GWC writer and that state projection emits only fixed data attributes.
func TestTodo_ALIGN_043_Security(t *testing.T) {
	reg := ProductionWorkLoopRegistry(pagedef.WorkLoopReady)
	ctx := WidgetContext{PageID: "work.my-work", PageVersion: 1, Slot: pagedef.WidgetSlot{WidgetRef: "widget.work-loop.state.v1"}}
	node := regWidget(t, reg, ctx)
	out, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"style=", "onerror", "onclick", "javascript:"} {
		if strings.Contains(strings.ToLower(out), forbidden) {
			t.Errorf("state widget emitted forbidden %q: %s", forbidden, out)
		}
	}
}

// TestTodo_ALIGN_043_Conformance proves all production fixture slots are
// registered and every constructor returns a non-empty deterministic node.
func TestTodo_ALIGN_043_Conformance(t *testing.T) {
	reg := ProductionWorkLoopRegistry(pagedef.WorkLoopReady)
	for _, pd := range []pagedef.PageDefinition{pagedef.WorkLoopPageDefinition(), pagedef.FailureRecoveryPageDefinition()} {
		for _, region := range pd.Regions {
			for _, slot := range region.Widgets {
				ctor, ok := reg.Lookup(slot.WidgetRef)
				if !ok {
					t.Errorf("fixture slot %q has no registered widget %q", slot.ID, slot.WidgetRef)
					continue
				}
				ctx := WidgetContext{PageID: pd.PageID, PageVersion: pd.Version, Region: region, Slot: slot}
				first, err := ui.RenderToString(ctor(ctx))
				if err != nil || first == "" {
					t.Errorf("widget %q rendered invalid output: %q, %v", slot.WidgetRef, first, err)
				}
				second, _ := ui.RenderToString(ctor(ctx))
				if first != second {
					t.Errorf("widget %q is nondeterministic", slot.WidgetRef)
				}
			}
		}
	}
}

// TestTodo_ALIGN_044 proves denied, stale, and failed reads expose a safe
// explanation and an operable retry/read route without protected identifiers.
func TestTodo_ALIGN_044(t *testing.T) {
	for _, tc := range []struct {
		state  pagedef.WorkLoopState
		text   string
		action string
	}{
		{pagedef.WorkLoopDenied, "You don’t have access to this work.", "Access is required"},
		{pagedef.WorkLoopStale, "may be out of date", "Refresh safely"},
		{pagedef.WorkLoopError, "No changes were saved.", "Try again"},
	} {
		out := renderFailureFixture(t, tc.state)
		if !strings.Contains(out, tc.text) || !strings.Contains(out, tc.action) {
			t.Errorf("failure state %q omitted explanation/action: %s", tc.state, out)
		}
		if strings.Contains(out, "fixture-review") || strings.Contains(out, "intent-") {
			t.Errorf("failure state %q disclosed a protected resource identifier", tc.state)
		}
	}
}

// TestTodo_ALIGN_044_Property ensures recovery actions are real buttons with
// explicit retry semantics, not links that pretend a server operation ran.
func TestTodo_ALIGN_044_Property(t *testing.T) {
	for _, state := range []pagedef.WorkLoopState{pagedef.WorkLoopStale, pagedef.WorkLoopError} {
		out := renderFailureFixture(t, state)
		if !strings.Contains(out, `<button`) || !strings.Contains(out, `data-work-action="retry-read"`) {
			t.Errorf("%q recovery output is not an operable retry button: %s", state, out)
		}
		if strings.Contains(out, "ExecuteIntent") || strings.Contains(out, "completed") {
			t.Errorf("%q recovery output implies material success", state)
		}
	}
}

// TestTodo_ALIGN_044_Golden pins the assertive announcement used when the
// primary work projection is unavailable.
func TestTodo_ALIGN_044_Golden(t *testing.T) {
	out := renderFailureFixture(t, pagedef.WorkLoopError)
	if !strings.Contains(out, `aria-live="assertive"`) || !strings.Contains(out, `role="alert"`) {
		t.Fatalf("error recovery page is not announced assertively: %s", out)
	}
}

// TestTodo_ALIGN_044_Security ensures the denied fallback contains no
// resource or tenant identifiers, even though it remains useful.
func TestTodo_ALIGN_044_Security(t *testing.T) {
	out := renderFailureFixture(t, pagedef.WorkLoopDenied)
	for _, forbidden := range []string{"tenant-", "worker-", "intent-", "policy-", "permission-ref"} {
		if strings.Contains(strings.ToLower(out), forbidden) {
			t.Errorf("denied page leaked %q: %s", forbidden, out)
		}
	}
}

func renderFailureFixture(t *testing.T, state pagedef.WorkLoopState) string {
	t.Helper()
	node, err := RenderFailureRecovery(state)
	if err != nil {
		t.Fatalf("RenderFailureRecovery(%q): %v", state, err)
	}
	out, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func regWidget(t *testing.T, reg *Registry, ctx WidgetContext) ui.Node {
	t.Helper()
	ctor, ok := reg.Lookup(ctx.Slot.WidgetRef)
	if !ok {
		t.Fatalf("missing widget %q", ctx.Slot.WidgetRef)
	}
	return ctor(ctx)
}
