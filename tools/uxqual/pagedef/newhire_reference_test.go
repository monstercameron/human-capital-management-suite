package pagedef

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// TestTodo_WFPAGE_034_Reference pins the shipped New hire reference page:
// the definition validates, carries exactly the seven journey controls and
// the pinned hiring SOP slot, and its company rules refuse an over-band
// salary and a pre-policy start date when the client is bypassed.
func TestTodo_WFPAGE_034_Reference(t *testing.T) {
	page := NewHireReferencePage()
	if violations := page.Validate(); len(violations) != 0 {
		t.Fatalf("shipped reference page is invalid: %v", violations)
	}
	if page.WorkflowKey != NewHireWorkflowKey || page.WorkflowVersion != NewHireWorkflowVersion || page.PageVersion != NewHireReferencePageVersion || page.PageID != NewHireReferencePageID {
		t.Fatalf("shipped reference identity drifted: %+v", page)
	}
	var bindings []string
	for _, widget := range page.Widgets {
		bindings = append(bindings, widget.Binding)
	}
	want := []string{"person", "position", "cost_centre", "salary", "start_date", "contact", "attachment"}
	if len(bindings) != len(want) {
		t.Fatalf("shipped reference controls = %v, want %v", bindings, want)
	}
	for i := range want {
		if bindings[i] != want[i] {
			t.Fatalf("shipped reference controls = %v, want %v", bindings, want)
		}
	}
	pinned := false
	for _, slot := range page.LinkSlots {
		if slot.ID == "hiring-sop" && slot.Mode == "pinned" && slot.DocumentRef == "document:hiring-sop@docv-7" {
			pinned = true
		}
	}
	if !pinned {
		t.Fatalf("shipped reference has no pinned hiring SOP slot: %+v", page.LinkSlots)
	}
	digest := sha256.Sum256(page.Canonical())
	if got, want := hex.EncodeToString(digest[:]), "968ea64018fff2855823c39775a4ecdf5b717ece804a8ab72dc9cb986d0c9c98"; got != want {
		t.Fatalf("shipped reference canonical digest = %s, want %s", got, want)
	}

	rules := NewHireCompanyRules()
	if len(rules) != 2 || rules[0].Code != "SALARY_BAND" || rules[1].Code != "START_DATE" {
		t.Fatalf("shipped company rules = %+v", rules)
	}
	salary, err := workflow.CompilePageRule(rules[0])
	if err != nil {
		t.Fatal(err)
	}
	start, err := workflow.CompilePageRule(rules[1])
	if err != nil {
		t.Fatal(err)
	}
	max, _ := workflow.NewMoneyValue("USD", "100000")
	tooHigh, _ := workflow.NewMoneyValue("USD", "120000")
	if result, err := salary.Evaluate(workflow.PageRuleInput{Fields: map[string]any{"salary": tooHigh}, Parameters: map[string]any{"salary_band_max": max}}); err != nil || result.Valid || len(result.Findings) != 1 || result.Findings[0].Code != "SALARY_BAND" {
		t.Fatalf("salary-band bypass was accepted: result=%+v err=%v", result, err)
	}
	if result, err := start.Evaluate(workflow.PageRuleInput{Fields: map[string]any{"start_date": time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}}); err != nil || result.Valid || len(result.Findings) != 1 || result.Findings[0].Code != "START_DATE" {
		t.Fatalf("start-date bypass was accepted: result=%+v err=%v", result, err)
	}
	if result, err := salary.Evaluate(workflow.PageRuleInput{Fields: map[string]any{"salary": max}, Parameters: map[string]any{"salary_band_max": max}}); err != nil || !result.Valid {
		t.Fatalf("in-band salary was refused: result=%+v err=%v", result, err)
	}
	if result, err := start.Evaluate(workflow.PageRuleInput{Fields: map[string]any{"start_date": time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}}); err != nil || !result.Valid {
		t.Fatalf("policy start date was refused: result=%+v err=%v", result, err)
	}
}
