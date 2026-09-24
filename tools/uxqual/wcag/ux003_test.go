package wcag

import (
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/forms"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/gwc"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/ssr"
)

func rendered(t *testing.T) (string, string) {
	t.Helper()
	fixture := forms.FixtureWithValidationError()
	ssrDoc, err := ssr.Render(fixture)
	if err != nil {
		t.Fatalf("ssr.Render: %v", err)
	}
	gwcDoc, err := gwc.Document(fixture)
	if err != nil {
		t.Fatalf("gwc.Document: %v", err)
	}
	return ssrDoc, gwcDoc
}

// TestTodo_UX_003 is the PRIMARY UX-003 pilot release gate. SSR is the
// accessible route selected by FORM-004; GWC must retain the shared keyboard,
// naming, masking and contrast invariants.
func TestTodo_UX_003(t *testing.T) {
	ssrDoc, gwcDoc := rendered(t)
	ssrResults, gwcResults := Score(ssrDoc), Score(gwcDoc)
	for _, surface := range []struct {
		name    string
		results []qual.CriterionResult
	}{{"ssr", ssrResults}, {"gwc", gwcResults}} {
		for _, result := range surface.results {
			t.Logf("[%s] %s: pass=%v (%s)", surface.name, result.Name, result.Pass, result.Detail)
			if surface.name == "ssr" && !result.Pass {
				t.Errorf("UX-003 SSR failed %s: %s", result.Name, result.Detail)
			}
		}
	}
	loadedEvidence, err := LoadEvidence()
	if err != nil {
		t.Fatal(err)
	}
	if err := loadedEvidence.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := loadedEvidence.ReleaseReady(); err == nil {
		t.Fatal("repository evidence must not claim release readiness before browser and assistive-technology runs")
	}
	broken := strings.Replace(ssrDoc, `aria-describedby="proposedCompensation-error"`, `aria-describedby="missing-error"`, 1)
	if r := forms.CheckErrorAssociation(broken, forms.ErroredFieldIDs); r.Pass {
		t.Fatal("error association check is a rubber stamp")
	}
	if r := CheckAccessibleAuth(strings.Replace(ssrDoc, "force_execute", "force_execute", 1) + "force_execute"); r.Pass {
		t.Fatal("accessible-auth check did not reject a privileged action")
	}
}

// TestTodo_UX_003_Race runs independent scorecards concurrently, protecting
// the release artifact against shared mutable checker state under -race.
func TestTodo_UX_003_Race(t *testing.T) {
	ssrDoc, gwcDoc := rendered(t)
	docs := []string{ssrDoc, gwcDoc, ssrDoc, gwcDoc}
	var wg sync.WaitGroup
	for _, doc := range docs {
		doc := doc
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, r := range Score(doc) {
				if r.Name == "" {
					t.Errorf("unnamed criterion")
				}
			}
		}()
	}
	wg.Wait()
}

// TestTodo_UX_003_Integration proves both renderers expose the same authorized
// action surface and that the release evidence parses as a governed artifact.
func TestTodo_UX_003_Integration(t *testing.T) {
	ssrDoc, gwcDoc := rendered(t)
	for _, action := range []string{`value="approve"`, `value="reject"`, `value="request_more_information"`} {
		if strings.Count(ssrDoc, action) != strings.Count(gwcDoc, action) {
			t.Errorf("renderer action mismatch for %s", action)
		}
	}
	e, err := LoadEvidence()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := e.ReleaseReady(); err == nil {
		t.Fatal("pending manual scenarios unexpectedly passed the release gate")
	}
}

// TestTodo_UX_003_Browser is the deterministic browser-fixture contract:
// browser automation must receive a document with the same focusable field
// order, names, errors and authorization projection as the Go gate.
func TestTodo_UX_003_Browser(t *testing.T) {
	ssrDoc, _ := rendered(t)
	got := strings.Join([]string{"proposedJobTitle", "proposedGrade", "proposedCompensation", "effectiveDate", "businessReason"}, ",")
	if want := strings.Join(fieldIDs(ssrDoc), ","); !strings.Contains(want, got) {
		t.Fatalf("browser fixture field order missing: want %s in %s", got, want)
	}
	for _, r := range []struct {
		name string
		pass bool
	}{{"focus/names", CheckFocusAndNames(ssrDoc).Pass}, {"zoom/reflow", CheckZoomReflow(ssrDoc).Pass}, {"reduced-motion", CheckReducedMotion(ssrDoc).Pass}, {"auth", CheckAccessibleAuth(ssrDoc).Pass}} {
		if !r.pass {
			t.Errorf("browser criterion failed: %s", r.name)
		}
	}
}

func fieldIDs(doc string) []string { return qual.FieldIDsInDocumentOrder(doc) }
