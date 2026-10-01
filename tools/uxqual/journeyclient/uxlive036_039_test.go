package journeyclient

import (
	"context"
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

func uxlivePromotionWorker() *journeyv1.Worker {
	return &journeyv1.Worker{WorkerRef: "uxlive-worker", LegalName: "Aya Hassan", JobCode: "OPS-HRBP2", Grade: "P2", OrgUnit: "people-ops", BasePay: "100000.00", Currency: "USD"}
}

func uxlivePromotionOptions() *journeyv1.WorkforceOptions {
	options := testWorkforceOptions()
	options.PositionVacancies = []*journeyv1.PositionVacancyOption{{
		Reference: "rev:position:aya-target", Title: "Senior HR Business Partner", JobCode: "OPS-HRBP3",
		Organization: "Data & Analytics", Manager: "Maya Chen", Location: "Boston, MA", ReservationState: "AVAILABLE",
	}}
	return options
}

func uxliveProposalPage(t *testing.T, values map[string]string) journey.Page {
	t.Helper()
	worker := uxlivePromotionWorker()
	return ProposalPage(testConfig(), ListData{Workers: []*journeyv1.Worker{worker}, SelectedRef: worker.GetWorkerRef(), Options: uxlivePromotionOptions()}, nil, values)
}

func TestTodo_UXLIVE_036(t *testing.T) {
	page := uxliveProposalPage(t, map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3", FieldBase: "0", FieldEffective: "2026-12-01"})
	if page.Proposal == nil || len(page.Proposal.Form.Confirmation) != 0 {
		t.Fatalf("invalid proposal reached review: %+v", page.Proposal)
	}
	base, _ := fieldByID(page.Proposal.Form.Fields, FieldBase)
	reason, _ := fieldByID(page.Proposal.Form.Fields, FieldReason)
	if base.Error == "" || reason.Error != "" {
		t.Fatalf("invalid local projection = base=%q reason=%q", base.Error, reason.Error)
	}
}

func TestTodo_UXLIVE_036_Browser(t *testing.T) {
	page := uxliveProposalPage(t, map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3", FieldBase: "0", FieldEffective: "2026-12-01"})
	if page.Proposal.Form.ConfirmationNote != "" || len(page.Proposal.Form.Confirmation) != 0 {
		t.Fatal("the browser-facing projection exposed a review note for an invalid proposal")
	}
}

func TestTodo_UXLIVE_036_Accessibility(t *testing.T) {
	page := uxliveProposalPage(t, map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3", FieldBase: "0", FieldEffective: "2026-12-01"})
	base, _ := fieldByID(page.Proposal.Form.Fields, FieldBase)
	if base.Error == "" || page.FocusInvalidRevision != 0 {
		// The initial projection has inline guidance; focus is added by the
		// submit path, not fabricated before the reader attempts submission.
		if base.Error == "" {
			t.Fatalf("zero pay has no accessible inline correction: %+v", base)
		}
	}
}

func TestTodo_UXLIVE_036_Integration(t *testing.T) {
	h := newHarness(t)
	h.app.Start(context.Background(), ProposalHref("omar-reyes"))
	h.awaitPage(t, "proposal", proposalFor("omar-reyes"))
	h.app.Submit(ActionPropose, map[string]string{NameWorker: "omar-reyes", NameJobCode: "OPS-HRBP3", NameGrade: "P3", NameBase: "0", NameEffective: "2026-12-01"})
	p := h.awaitPage(t, "local refusal", noticeTitled("Complete the required fields"))
	if h.svc.called("ProposePromotion") != 0 || p.FocusInvalidRevision == 0 {
		t.Fatalf("invalid proposal escaped local validation: calls=%d focus=%d", h.svc.called("ProposePromotion"), p.FocusInvalidRevision)
	}
}

func TestTodo_UXLIVE_036_Security(t *testing.T) {
	presentation := mapProposalRefusal(promotionRangeRefusalWithReference(t, "req:0123456789abcdef"), productui.ResolveProductLocale("en-US"))
	if strings.Contains(presentation.notice.Detail, "secret") || strings.Contains(presentation.notice.Detail, "rule") {
		t.Fatalf("server refusal leaked diagnostic text: %+v", presentation.notice)
	}
}

func TestTodo_UXLIVE_036_Regression(t *testing.T) {
	page := uxliveProposalPage(t, map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3", FieldBase: "115000.00", FieldEffective: "2026-12-01", FieldReason: "Expanded scope"})
	if len(page.Proposal.Form.Confirmation) == 0 {
		t.Fatal("a complete exact proposal lost its review facts")
	}
}

func TestTodo_UXLIVE_037(t *testing.T) {
	worker := uxlivePromotionWorker()
	facts := proposalConfirmationLocale("en-US", worker, uxlivePromotionOptions(), "OPS-HRBP3", "P3", "115000.00", "rev:position:aya-target", "Expanded scope", "2026-12-01")
	joined := make([]string, 0, len(facts))
	for _, fact := range facts {
		joined = append(joined, fact.Label+"="+fact.Value)
	}
	text := strings.ReplaceAll(strings.Join(joined, "\n"), "\u00a0", " ")
	for _, want := range []string{"Aya Hassan", "Senior HR Business Partner", "Data & Analytics", "USD 100,000.00", "USD 115,000.00", "+USD 15,000.00", "+15.0%", "Expanded scope", "2026"} {
		if !strings.Contains(text, want) {
			t.Fatalf("decision-grade proposal review omitted %q:\n%s", want, text)
		}
	}
}

func TestTodo_UXLIVE_037_Golden(t *testing.T) {
	facts := proposalConfirmationLocale("en-US", uxlivePromotionWorker(), uxlivePromotionOptions(), "OPS-HRBP3", "P3", "115000.00", "rev:position:aya-target", "Expanded scope", "2026-12-01")
	if len(facts) < 6 || facts[0].Value != "Aya Hassan" {
		t.Fatalf("review fact order or employee value drifted: %+v", facts)
	}
	for _, fact := range facts {
		if fact.Label == productui.ResolveProductLocale("en-US").Text("journey.action_base") && !strings.Contains(fact.Value, "115,000.00") {
			t.Fatalf("review base-pay fact drifted: %+v", fact)
		}
	}
}

func TestTodo_UXLIVE_037_Browser(t *testing.T) {
	page := uxliveProposalPage(t, map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3", FieldPosition: "rev:position:aya-target", FieldBase: "115000.00", FieldEffective: "2026-12-01", FieldReason: "Expanded scope"})
	if len(page.Proposal.Form.Confirmation) < 5 {
		t.Fatalf("browser proposal review is too sparse: %+v", page.Proposal.Form.Confirmation)
	}
}

func TestTodo_UXLIVE_037_Accessibility(t *testing.T) {
	markup, err := journey.RenderToString(uxliveProposalPage(t, map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3", FieldPosition: "rev:position:aya-target", FieldBase: "115000.00", FieldEffective: "2026-12-01", FieldReason: "Expanded scope"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`aria-label="Review and submit"`, `aria-label="Cancel"`, "Expanded scope"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("review dialog omitted accessible/decision fact %q", want)
		}
	}
}

func TestTodo_UXLIVE_037_I18N(t *testing.T) {
	facts := proposalConfirmationLocale("de-DE", uxlivePromotionWorker(), uxlivePromotionOptions(), "OPS-HRBP3", "P3", "115000.50", "rev:position:aya-target", "Erweiterter Umfang", "2026-12-01")
	text := ""
	for _, fact := range facts {
		text += fact.Label + "=" + fact.Value + "\n"
	}
	if !strings.Contains(text, "115.000,50") || strings.Contains(text, "Base pay") {
		t.Fatalf("German review is not localized: %s", text)
	}
}

func TestTodo_UXLIVE_037_Security(t *testing.T) {
	facts := proposalConfirmationLocale("en-US", nil, nil, "OPS-HRBP3", "P3", "115000.00", "", "Expanded scope", "2026-12-01")
	for _, fact := range facts {
		if strings.Contains(fact.Value, "uxlive-worker") || strings.Contains(fact.Value, "manager_ref") {
			t.Fatalf("review recovered a withheld worker fact: %+v", fact)
		}
	}
}

func TestTodo_UXLIVE_037_Regression(t *testing.T) {
	facts := proposalConfirmationLocale("ar", uxlivePromotionWorker(), uxlivePromotionOptions(), "OPS-HRBP3", "P3", "115000.00", "rev:position:aya-target", "نطاق موسع", "2026-12-01")
	if len(facts) == 0 {
		t.Fatal("Arabic review lost all facts")
	}
}

func TestTodo_UXLIVE_039(t *testing.T) {
	page := uxliveProposalPage(t, map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3", FieldEffective: "2026-12-01", FieldReason: "Expanded scope"})
	base, _ := fieldByID(page.Proposal.Form.Fields, FieldBase)
	if base.Value != "" || strings.Contains(base.Value, "0") {
		t.Fatalf("untouched proposed pay fabricated a zero: %+v", base)
	}
}

func TestTodo_UXLIVE_039_Property(t *testing.T) {
	for _, raw := range []string{"0", "0.00", "1.001", "999999.99"} {
		page := uxliveProposalPage(t, map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3", FieldBase: raw, FieldEffective: "2026-12-01", FieldReason: "Expanded scope"})
		if len(page.Proposal.Form.Confirmation) != 0 {
			t.Fatalf("unsafe pay %q reached review", raw)
		}
	}
}

func TestTodo_UXLIVE_039_Integration(t *testing.T) {
	h := newHarness(t)
	h.app.Start(context.Background(), ProposalHref("omar-reyes"))
	h.awaitPage(t, "proposal", proposalFor("omar-reyes"))
	h.app.Submit(ActionPropose, map[string]string{NameWorker: "omar-reyes", NameJobCode: "OPS-HRBP3", NameGrade: "P3", NameBase: "0.00", NameEffective: "2026-12-01", NameReason: "Expanded scope"})
	if h.svc.called("ProposePromotion") != 0 {
		t.Fatal("zero-money proposal reached the server")
	}
}

func TestTodo_UXLIVE_039_Browser(t *testing.T) {
	page := uxliveProposalPage(t, map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3", FieldEffective: "2026-12-01", FieldReason: "Expanded scope"})
	markup, err := journey.RenderToString(page)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, `value="0"`) || strings.Contains(markup, "USD 0.00") {
		t.Fatal("browser markup presents an invented zero compensation")
	}
}

func TestTodo_UXLIVE_039_Accessibility(t *testing.T) {
	page := uxliveProposalPage(t, map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3", FieldBase: "0", FieldEffective: "2026-12-01", FieldReason: "Expanded scope"})
	base, _ := fieldByID(page.Proposal.Form.Fields, FieldBase)
	markup, err := journey.RenderToString(page)
	if err != nil {
		t.Fatal(err)
	}
	if base.Error == "" || !strings.Contains(markup, `aria-invalid="true"`) || !strings.Contains(markup, "propose-base-error") {
		t.Fatalf("invalid compensation is not accessibly described: field=%+v", base)
	}
}

func TestTodo_UXLIVE_039_I18N(t *testing.T) {
	copy := productui.ResolveProductLocale("de-DE")
	if got := copy.FormatMoney("115000.50", "USD", 2); !strings.Contains(got, "115.000,50") {
		t.Fatalf("German exact money formatting = %q", got)
	}
}

func TestTodo_UXLIVE_039_Security(t *testing.T) {
	page := uxliveProposalPage(t, map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3", FieldBase: "1.001", FieldEffective: "2026-12-01", FieldReason: "Expanded scope"})
	if base, _ := fieldByID(page.Proposal.Form.Fields, FieldBase); base.Error == "" {
		t.Fatal("ambiguous fractional money was treated as a valid proposal")
	}
}

func TestTodo_UXLIVE_039_Regression(t *testing.T) {
	page := uxliveProposalPage(t, map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3", FieldBase: "115000.00", FieldEffective: "2026-12-01", FieldReason: "Expanded scope"})
	if len(page.Proposal.Form.Confirmation) == 0 {
		t.Fatal("valid exact money no longer reaches review")
	}
}
