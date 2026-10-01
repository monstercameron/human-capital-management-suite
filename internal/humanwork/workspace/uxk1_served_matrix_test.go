package workspace

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/presentation"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/forms"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/wcag"
)

// TestTodo_UX_001_Integration proves the server-resolved contract reaches the
// served workspace renderer without reintroducing masked fields or actions.
func TestTodo_UX_001_Integration(t *testing.T) {
	doc, err := Render(ux002Contract(), "csrf-fixture", "worker-fixture", false)
	if err != nil {
		t.Fatalf("Render served Promotion workspace: %v", err)
	}
	for _, want := range []string{"Adrian Cole", "Senior Engineer", "Approve", "people.promote@v1"} {
		if !strings.Contains(doc, want) {
			t.Errorf("served Promotion document missing %q", want)
		}
	}
	for _, masked := range []string{"proposedCompensation", "128000.00", "reject", "Reject"} {
		if strings.Contains(doc, masked) {
			t.Errorf("served Promotion document leaked masked value %q", masked)
		}
	}
}

// TestTodo_UX_001_Browser checks the browser-facing document preserves the
// server's ordered field and action projection as HTML controls.
func TestTodo_UX_001_Browser(t *testing.T) {
	doc, err := Render(ux002Contract(), "csrf-fixture", "worker-fixture", true)
	if err != nil {
		t.Fatalf("Render enhanced workspace: %v", err)
	}
	if strings.Index(doc, `name="proposedJobTitle"`) > strings.Index(doc, `name="businessReason"`) {
		t.Fatal("served field order does not match the server contract")
	}
	if !strings.Contains(doc, `value="approve"`) || strings.Contains(doc, `value="reject"`) {
		t.Fatal("served action projection does not match the authorized contract")
	}
}

// TestTodo_UX_003_Integration ties the WCAG checks to the document produced
// by the shipped workspace renderer, rather than only to a qualification
// fixture outside the serving binary.
func TestTodo_UX_003_Integration(t *testing.T) {
	doc, err := Render(forms.FixtureWithValidationError(), "csrf-fixture", "worker-fixture", false)
	if err != nil {
		t.Fatalf("Render served Promotion workspace: %v", err)
	}
	for _, criterion := range []struct {
		name   string
		pass   bool
		detail string
	}{
		{"focus/names", wcag.CheckFocusAndNames(doc).Pass, wcag.CheckFocusAndNames(doc).Detail},
		{"zoom/reflow", wcag.CheckZoomReflow(doc).Pass, wcag.CheckZoomReflow(doc).Detail},
		{"reduced-motion", wcag.CheckReducedMotion(doc).Pass, wcag.CheckReducedMotion(doc).Detail},
		{"auth", servedAccessibleAuth(doc), "served transition controls are authorized and keyboard-reachable"},
	} {
		if !criterion.pass {
			t.Errorf("served WCAG criterion failed: %s: %s", criterion.name, criterion.detail)
		}
	}
}

func servedAccessibleAuth(doc string) bool {
	for _, transition := range []string{"approve", "reject", "request_more_information"} {
		if !strings.Contains(doc, `name="transition"`) || !strings.Contains(doc, `value="`+transition+`"`) {
			return false
		}
	}
	for _, masked := range []string{"force_execute", "Force execute", "nationalId", "555-11-2222"} {
		if strings.Contains(doc, masked) {
			return false
		}
	}
	return true
}

// TestTodo_UX_003_Browser asserts the browser document carries the same
// keyboard order and authorization projection as the server contract.
func TestTodo_UX_003_Browser(t *testing.T) {
	doc, err := Render(forms.FixtureWithValidationError(), "csrf-fixture", "worker-fixture", true)
	if err != nil {
		t.Fatalf("Render enhanced workspace: %v", err)
	}
	if strings.Index(doc, `name="proposedJobTitle"`) > strings.Index(doc, `name="businessReason"`) {
		t.Fatal("browser field order differs from the server projection")
	}
	if !strings.Contains(doc, `name="proposedCompensation"`) {
		t.Fatal("browser document omitted the authorized compensation field")
	}
	if strings.Contains(doc, "force_execute") || strings.Contains(doc, "nationalId") {
		t.Fatal("browser document exposed a privileged or masked value")
	}
}

// TestTodo_UX_005_Integration proves the recovery projection is attached to
// a served Page and carries only safe, server-resolved actions.
func TestTodo_UX_005_Integration(t *testing.T) {
	page := Page{
		Contract: ux002Contract(),
		Reading:  Reading{EvidenceIDs: []string{"evidence:served-1"}},
		Locale:   ResolveLocale(DefaultLocale),
	}
	projection := pageRecovery(page)
	if projection.State != presentation.StateSimulationReady {
		t.Fatalf("served recovery state=%s, want %s", projection.State, presentation.StateSimulationReady)
	}
	if len(projection.EvidenceRefs) != 1 || projection.EvidenceRefs[0] != "evidence:served-1" {
		t.Fatalf("served evidence=%v", projection.EvidenceRefs)
	}
	doc, err := RenderPage(page, "csrf-fixture", "worker-fixture", false)
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	for _, want := range []string{`data-recovery-state="SIMULATION_READY"`, `data-recovery-actions="SUBMIT,REVIEW"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("served recovery metadata missing %q", want)
		}
	}
}
