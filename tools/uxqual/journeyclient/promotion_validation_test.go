package journeyclient

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	journeytransport "github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func promotionValidationFixture(t *testing.T) (*journeyv1.Worker, *journeyv1.WorkforceOptions, *journeyv1.PromotionPathOption) {
	t.Helper()
	worker := &journeyv1.Worker{WorkerRef: "omar-reyes", JobCode: "OPS-HRBP2", Grade: "P2", BasePay: "100.03", Currency: "USD", PayZone: "US-EAST"}
	options := testWorkforceOptions()
	path := selectedPromotionPath(options, worker, "OPS-HRBP3", "P3")
	if path == nil {
		t.Fatal("promotion fixture has no published ladder path")
	}
	return worker, options, path
}

func promotionRangeRefusal(t *testing.T) error {
	return promotionRangeRefusalWithReference(t, "cor-private-123")
}

func promotionRangeRefusalWithReference(t *testing.T, reference string) error {
	t.Helper()
	st, err := status.New(codes.InvalidArgument, "private SQL, worker and pay-band details").WithDetails(&commonv1.ErrorDetail{
		CorrelationId: reference,
		FieldViolations: []*commonv1.FieldViolation{{
			FieldPath: "proposed_base", Description: "private pay band is 99999.99", RuleRef: "promotion.ladder.base_increase_out_of_range",
			PermittedMoneyRange: &commonv1.MoneyRange{Minimum: "105.04", Maximum: "115.03", Currency: "USD"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return st.Err()
}

func TestTodo_PROMOUX_007_Browser_CopyableSupportReference(t *testing.T) {
	const reference = "req:0123456789abcdef"
	worker, options, _ := promotionValidationFixture(t)
	presentation := mapProposalRefusal(promotionRangeRefusalWithReference(t, reference), productui.ResolveProductLocale("en-US"))
	if presentation.notice == nil || presentation.notice.SupportReference != reference {
		t.Fatalf("typed refusal lost server-issued reference: %+v", presentation.notice)
	}
	form := focusedProposalForm(map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3", FieldBase: "100.00"}, worker.GetWorkerRef(), options, worker)
	applyProposalErrors(&form, presentation.fields)
	doc, err := journey.RenderToString(journey.Page{Locale: "en-US", Notice: presentation.notice, Proposal: &journey.ProposalView{Form: form}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="jn-notice-support"`, "Support details", `id="jn-support-reference"`, `for="jn-support-reference"`, `value="` + reference + `"`, `href="#propose-base"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("support disclosure lacks %q", want)
		}
	}
	if !strings.Contains(strings.ToLower(doc), "readonly") {
		t.Error("support reference is not a selectable read-only input")
	}
	if strings.Contains(doc, `<details class="jn-notice-support" open`) || strings.Contains(presentation.notice.Detail, reference) || strings.Contains(presentation.notice.Title, reference) {
		t.Fatal("support reference entered ordinary notice copy or an open disclosure")
	}
	for _, secret := range []string{"private SQL", "private pay band", "99999.99"} {
		if strings.Contains(doc, secret) {
			t.Errorf("support disclosure leaked %q", secret)
		}
	}
}

func TestTodo_PROMOUX_007_I18N_SupportDisclosure(t *testing.T) {
	for _, tc := range []struct{ locale, summary, label string }{
		{"en-US", "Support details", "Support reference"},
		{"de-DE", "Supportangaben", "Supportreferenz"},
		{"ar", "تفاصيل الدعم", "مرجع الدعم"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			doc, err := journey.RenderToString(journey.Page{Locale: tc.locale, Notice: &journey.Notice{Tone: "warning", Title: "Correct the form", SupportReference: "req:0123456789abcdef"}})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(doc, tc.summary) || !strings.Contains(doc, tc.label) {
				t.Fatalf("%s support copy absent", tc.locale)
			}
		})
	}
}

func TestTodo_PROMOUX_007_Security_SupportReferenceAllowlist(t *testing.T) {
	for _, tc := range []struct {
		id   string
		want bool
	}{
		{"req:0123456789abcdef", true},
		{"cor_01JX6Y8B2C7D9EFG", true},
		{"req:short", false},
		{"req:0123456789abcdef<script>", false},
		{"cor-private-123", false},
		{"cor_01JX6Y8B2C7D9EFG\nprivate", false},
	} {
		if got := validSupportReference(tc.id); got != tc.want {
			t.Errorf("reference %q accepted=%t, want %t", tc.id, got, tc.want)
		}
	}
	if got := supportReference(promotionRangeRefusal(t)); got != "" {
		t.Fatalf("untrusted server reference rendered: %q", got)
	}
}

func TestTodo_PROMOUX_007(t *testing.T) {
	worker, options, path := promotionValidationFixture(t)
	rangeForWorker := proposalPayRangeFor(worker, options, path)
	if rangeForWorker == nil || rangeForWorker.minimum.Amount().String() != "105.04" || rangeForWorker.maximum.Amount().String() != "115.03" {
		t.Fatalf("exact published bounds = %+v", rangeForWorker)
	}
	got := proposalFieldErrorsLocale(promotionRangeRefusal(t), productui.ResolveProductLocale("en-US"))
	if got[FieldBase] != "Enter an amount from USD\u00a0105.04 to USD\u00a0115.03, inclusive." || len(got) != 1 {
		t.Fatalf("field-linked correction = %#v", got)
	}
}

func TestTodo_PROMOUX_007_Golden(t *testing.T) {
	worker, options, _ := promotionValidationFixture(t)
	form := focusedProposalForm(map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3"}, worker.GetWorkerRef(), options, worker)
	var help string
	for _, field := range form.Fields {
		if field.ID == FieldBase {
			help = field.Help
		}
	}
	const want = "For this role, base pay must increase by 5.00% to 15.00%. Benefit eligibility is reviewed separately; existing choices do not change with this request. Available base-pay range: USD\u00a0105.04 to USD\u00a0115.03 per year."
	if help != want {
		t.Fatalf("published pay guidance changed:\n%s", help)
	}
}

func TestTodo_PROMOUX_007_Browser(t *testing.T) {
	worker, options, _ := promotionValidationFixture(t)
	form := focusedProposalForm(map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3", FieldBase: "100.00"}, worker.GetWorkerRef(), options, worker)
	applyProposalCurrency(&form, "USD")
	applyProposalErrors(&form, proposalFieldErrorsLocale(promotionRangeRefusal(t), productui.ResolveProductLocale("en-US")))
	doc, err := journey.RenderToString(journey.Page{Locale: "en-US", Notice: &journey.Notice{Tone: "warning", Title: "Correct the form"}, Proposal: &journey.ProposalView{Form: form}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`href="#propose-base"`, "USD\u00a0105.04", "USD\u00a0115.03", `value="100.00"`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("rendered refusal lacks %q", want)
		}
	}
	for _, secret := range []string{"private SQL", "private pay band", "cor-private-123", "99999.99"} {
		if strings.Contains(doc, secret) {
			t.Fatalf("rendered refusal disclosed %q", secret)
		}
	}
}

func TestTodo_PROMOUX_007_Accessibility(t *testing.T) {
	worker, options, _ := promotionValidationFixture(t)
	form := focusedProposalForm(map[string]string{FieldJobCode: "OPS-HRBP3", FieldGrade: "P3"}, worker.GetWorkerRef(), options, worker)
	applyProposalCurrency(&form, "USD")
	applyProposalErrors(&form, proposalFieldErrorsLocale(promotionRangeRefusal(t), productui.ResolveProductLocale("en-US")))
	doc, err := journey.RenderToString(journey.Page{Locale: "en-US", Notice: &journey.Notice{Tone: "warning", Title: "Correct the form"}, Proposal: &journey.ProposalView{Form: form}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`href="#propose-base"`, `id="propose-base-error"`, `aria-invalid="true"`, `aria-describedby="propose-base-prefix propose-base-suffix propose-base-help propose-base-error"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("accessible pay correction lacks %q", want)
		}
	}
}

func TestTodo_PROMOUX_007_I18N(t *testing.T) {
	for _, tc := range []struct{ locale, prefix, minimum, maximum string }{
		{"en-US", "Enter an amount from USD", "105.04", "115.03"},
		{"de-DE", "Geben Sie einen Betrag von 105,04", "105,04", "115,03"},
		{"ar", "أدخل مبلغًا من USD", "١٠٥٫٠٤", "١١٥٫٠٣"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			copy := productui.ResolveProductLocale(tc.locale)
			message := proposalFieldErrorsLocale(promotionRangeRefusal(t), copy)[FieldBase]
			if !strings.HasPrefix(message, tc.prefix) || !strings.Contains(message, tc.minimum) || !strings.Contains(message, tc.maximum) {
				t.Fatalf("%s exact correction = %q", tc.locale, message)
			}
		})
	}
}

func TestTodo_PROMOUX_007_Security(t *testing.T) {
	st, err := status.New(codes.InvalidArgument, "private request").WithDetails(&commonv1.ErrorDetail{
		FieldViolations: []*commonv1.FieldViolation{{FieldPath: "desired_base_pay", Description: "secret=123", RuleRef: "attacker.range_override"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	message := proposalFieldErrorsLocale(st.Err(), productui.ResolveProductLocale("en-US"))[FieldBase]
	if message != "Enter an exact amount within the approved pay range for this role." || strings.Contains(message, "123") || strings.Contains(message, "105") {
		t.Fatalf("untrusted refusal changed published bounds: %q", message)
	}
	worker, options, path := promotionValidationFixture(t)
	path.MaximumBaseIncrease = "not-a-percentage"
	if proposalPayRangeFor(worker, options, path) != nil {
		t.Fatal("malformed promotion rule yielded a visible salary bound")
	}
}

func TestTodo_PROMOUX_007_Regression_ServerBoundsOverrideStaleClientProjection(t *testing.T) {
	worker, options, path := promotionValidationFixture(t)
	worker.BasePay = "200.03"
	stale := proposalPayRangeFor(worker, options, path)
	if stale == nil || stale.minimum.Amount().String() == "105.04" {
		t.Fatal("fixture did not create a stale client-side worker baseline")
	}
	message := proposalFieldErrorsLocale(promotionRangeRefusal(t), productui.ResolveProductLocale("en-US"))[FieldBase]
	if !strings.Contains(message, "105.04") || !strings.Contains(message, "115.03") || strings.Contains(message, stale.minimum.Amount().String()) {
		t.Fatalf("refusal used cached rather than server-owned bounds: %q", message)
	}
	for _, raw := range []*commonv1.MoneyRange{
		nil,
		{Minimum: "105.040", Maximum: "115.03", Currency: "USD"},
		{Minimum: "115.03", Maximum: "105.04", Currency: "USD"},
		{Minimum: "105.04", Maximum: "115.03", Currency: "BAD-CURRENCY"},
	} {
		st, err := status.New(codes.InvalidArgument, "private pay").WithDetails(&commonv1.ErrorDetail{FieldViolations: []*commonv1.FieldViolation{{
			FieldPath: "proposed_base", RuleRef: "promotion.ladder.base_increase_out_of_range", PermittedMoneyRange: raw,
		}}})
		if err != nil {
			t.Fatal(err)
		}
		got := proposalFieldErrorsLocale(st.Err(), productui.ResolveProductLocale("en-US"))[FieldBase]
		if got != "Enter an exact amount within the approved pay range for this role." {
			t.Fatalf("malformed or absent server range produced precise-looking guidance: %q", got)
		}
	}
}

func TestTodo_PROMOUX_007_Regression_AppRefusalPreservesInputs(t *testing.T) {
	h := newHarness(t)
	h.svc.workers[0].BasePay = "100.03"
	h.svc.workers[0].PayZone = "US-EAST"
	h.svc.workforce = testWorkforceOptions()
	h.svc.proposeErr = promotionRangeRefusal(t)
	h.app.Start(context.Background(), ProposalHref("omar-reyes"))
	h.awaitPage(t, "promotion form", proposalFor("omar-reyes"))
	h.app.Submit(ActionPropose, map[string]string{
		NameWorker: "omar-reyes", NameJobCode: "OPS-HRBP3", NameGrade: "P3",
		NameBase: "100.00", NameEffective: "2026-12-01", NameReason: "A test reason that must survive refusal",
	})
	page := h.awaitPage(t, "exact pay refusal", noticeTitled("That proposal is not valid"))
	base, ok := fieldByID(page.Proposal.Form.Fields, FieldBase)
	if !ok || base.Value != "100.00" || base.Error != "Enter an amount from USD\u00a0105.04 to USD\u00a0115.03, inclusive." || page.FocusInvalidRevision == 0 {
		t.Fatalf("live proposal correction = %+v, focus revision %d", base, page.FocusInvalidRevision)
	}
	reason, ok := fieldByID(page.Proposal.Form.Fields, FieldReason)
	if !ok || reason.Value != "A test reason that must survive refusal" {
		t.Fatalf("refusal lost the reason = %+v", reason)
	}
	if h.svc.called("ProposePromotion") != 1 {
		t.Fatal("test did not reach the service refusal")
	}
	page.OnFieldChange(FieldBase, "105.04")
	updated := h.store.Page()
	base, _ = fieldByID(updated.Proposal.Form.Fields, FieldBase)
	if base.Value != "105.04" || base.Error != "" || updated.Notice != nil {
		t.Fatalf("editing the rejected pay did not clear only its stale refusal: %+v notice=%+v", base, updated.Notice)
	}
}

func TestTodo_PROMOUX_007_Regression_LocalizedCorrectionClearsOnlyStaleNotice(t *testing.T) {
	for _, tc := range []struct {
		locale, title string
	}{
		{locale: "de-DE", title: "Antrag kann nicht eingereicht werden"},
		{locale: "ar", title: "لا يمكن تقديم الطلب"},
	} {
		for _, serverRefusal := range []bool{false, true} {
			name := tc.locale + "/required"
			if serverRefusal {
				name = tc.locale + "/server"
			}
			t.Run(name, func(t *testing.T) {
				h := newHarness(t)
				h.svc.workers[0].BasePay = "100.03"
				h.svc.workers[0].PayZone = "US-EAST"
				h.svc.workforce = testWorkforceOptions()
				if serverRefusal {
					h.svc.proposeErr = promotionRangeRefusal(t)
				}
				h.app.Start(context.Background(), ProposalHref("omar-reyes"))
				h.awaitPage(t, "promotion form", proposalFor("omar-reyes"))
				h.app.SetLocale(tc.locale)
				basePay := ""
				wantTitle := productui.ResolveProductLocale(tc.locale).Text("journey.required_fields_title")
				wantKey := "journey.required_fields_title"
				if serverRefusal {
					basePay = "100.00"
					wantTitle = tc.title
					wantKey = "journey.error_invalid_title"
				}
				h.app.Submit(ActionPropose, map[string]string{
					NameWorker: "omar-reyes", NameJobCode: "OPS-HRBP3", NameGrade: "P3",
					NameBase: basePay, NameEffective: "2026-12-01", NameReason: "Keep this reason",
				})
				page := h.awaitPage(t, "localized pay refusal", noticeTitled(wantTitle))
				if page.Notice.TitleKey != wantKey {
					t.Fatalf("notice key = %q, want %q", page.Notice.TitleKey, wantKey)
				}
				pay, ok := fieldByID(page.Proposal.Form.Fields, FieldBase)
				if !ok || pay.Error == "" {
					t.Fatalf("pay refusal not linked: %+v", pay)
				}
				page.OnFieldChange(FieldBase, "105.04")
				corrected := h.store.Page()
				pay, _ = fieldByID(corrected.Proposal.Form.Fields, FieldBase)
				reason, _ := fieldByID(corrected.Proposal.Form.Fields, FieldReason)
				if pay.Value != "105.04" || pay.Error != "" || corrected.Notice != nil || reason.Value != "Keep this reason" {
					t.Fatalf("localized correction left stale notice or lost input: pay=%+v reason=%+v notice=%+v", pay, reason, corrected.Notice)
				}
			})
		}
	}
}

func TestTodo_PROMOUX_007_I18N_LocaleSwitchRelocalizesActiveRefusal(t *testing.T) {
	h := newHarness(t)
	h.svc.workers[0].BasePay = "100.03"
	h.svc.workers[0].PayZone = "US-EAST"
	h.svc.workforce = testWorkforceOptions()
	h.svc.proposeErr = promotionRangeRefusalWithReference(t, "req:0123456789abcdef")
	h.app.Start(context.Background(), ProposalHref("omar-reyes"))
	h.awaitPage(t, "promotion form", proposalFor("omar-reyes"))
	h.app.Submit(ActionPropose, map[string]string{
		NameWorker: "omar-reyes", NameJobCode: "OPS-HRBP3", NameGrade: "P3",
		NameBase: "100.00", NameEffective: "2026-12-01", NameReason: "Keep this reason",
	})
	en := h.awaitPage(t, "English pay refusal", noticeTitled("That proposal is not valid"))
	enPay, _ := fieldByID(en.Proposal.Form.Fields, FieldBase)
	if !strings.Contains(enPay.Error, "105.04") || !strings.Contains(enPay.Error, "115.03") {
		t.Fatalf("English exact bounds missing: %q", enPay.Error)
	}
	for _, tc := range []struct {
		locale, title, sentence, minimum, maximum string
	}{
		{"de-DE", "Antrag kann nicht eingereicht werden", "Geben Sie einen Betrag von", "105,04", "115,03"},
		{"ar", "لا يمكن تقديم الطلب", "أدخل مبلغًا من", "١٠٥٫٠٤", "١١٥٫٠٣"},
	} {
		h.app.SetLocale(tc.locale)
		page := h.store.Page()
		pay, _ := fieldByID(page.Proposal.Form.Fields, FieldBase)
		reason, _ := fieldByID(page.Proposal.Form.Fields, FieldReason)
		if page.Locale != tc.locale || page.Notice == nil || page.Notice.Title != tc.title ||
			!strings.Contains(pay.Error, tc.sentence) || !strings.Contains(pay.Error, tc.minimum) || !strings.Contains(pay.Error, tc.maximum) ||
			pay.Value != "100.00" || reason.Value != "Keep this reason" ||
			page.Notice.SupportReference != "req:0123456789abcdef" {
			t.Fatalf("%s locale switch left stale correction or lost state: pay=%+v reason=%+v notice=%+v", tc.locale, pay, reason, page.Notice)
		}
	}
}

func TestTodo_PROMOUX_007_Regression_InFlightLocaleSwitchUsesCurrentLanguage(t *testing.T) {
	h := newHarness(t)
	h.svc.workers[0].BasePay = "100.03"
	h.svc.workers[0].PayZone = "US-EAST"
	h.svc.workforce = testWorkforceOptions()
	h.svc.proposeErr = promotionRangeRefusalWithReference(t, "req:0123456789abcdef")
	h.app.Start(context.Background(), ProposalHref("omar-reyes"))
	h.awaitPage(t, "promotion form", proposalFor("omar-reyes"))
	gate := make(chan struct{})
	h.svc.mu.Lock()
	h.svc.gate = gate
	h.svc.mu.Unlock()
	h.app.Submit(ActionPropose, map[string]string{
		NameWorker: "omar-reyes", NameJobCode: "OPS-HRBP3", NameGrade: "P3",
		NameBase: "100.00", NameEffective: "2026-12-01", NameReason: "Keep this reason",
	})
	deadline := time.Now().Add(3 * time.Second)
	for h.svc.called("ProposePromotion") == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if h.svc.called("ProposePromotion") == 0 {
		close(gate)
		t.Fatal("proposal request never reached the delayed service")
	}
	h.app.SetLocale("de-DE")
	close(gate)
	page := h.awaitPage(t, "German refusal after pending request", noticeTitled("Antrag kann nicht eingereicht werden"))
	pay, _ := fieldByID(page.Proposal.Form.Fields, FieldBase)
	if !strings.Contains(pay.Error, "Geben Sie einen Betrag von") || !strings.Contains(pay.Error, "105,04") ||
		!strings.Contains(pay.Error, "115,03") || pay.Value != "100.00" {
		t.Fatalf("in-flight locale change mixed correction languages: %+v", pay)
	}
}

func TestTodo_PROMOUX_007_Regression_SSRAndEnhancedProjection(t *testing.T) {
	h := newHarness(t)
	h.svc.workers[0].BasePay = "100.03"
	h.svc.workers[0].PayZone = "US-EAST"
	h.svc.workforce = testWorkforceOptions()
	h.svc.proposeErr = promotionRangeRefusalWithReference(t, "req:0123456789abcdef")
	h.app.Start(context.Background(), ProposalHref("omar-reyes"))
	h.awaitPage(t, "promotion form", proposalFor("omar-reyes"))
	h.app.Submit(ActionPropose, map[string]string{
		NameWorker: "omar-reyes", NameJobCode: "OPS-HRBP3", NameGrade: "P3",
		NameBase: "100.00", NameEffective: "2026-12-01", NameReason: "Keep this entered reason",
	})
	enhanced := h.awaitPage(t, "typed pay refusal", noticeTitled("That proposal is not valid"))
	if enhanced.Notice == nil || enhanced.Notice.SupportReference != "req:0123456789abcdef" {
		t.Fatalf("enhanced refusal lost support reference: %+v", enhanced.Notice)
	}
	ssr := enhanced
	form := *enhanced.Proposal
	form.Form.OnSubmit = nil
	ssr.Proposal = &form
	ssr.OnFieldChange = nil
	for name, page := range map[string]journey.Page{"SSR": ssr, "enhanced": enhanced} {
		doc, err := journey.RenderToString(page)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`href="#propose-base"`, "USD\u00a0105.04", "USD\u00a0115.03", `value="req:0123456789abcdef"`, `value="100.00"`, "Keep this entered reason"} {
			if !strings.Contains(doc, want) {
				t.Errorf("%s refusal omitted %q", name, want)
			}
		}
	}
}

func TestTodo_PROMOUX_007_Regression_ChangingOneFieldPreservesOtherRefusals(t *testing.T) {
	h := newHarness(t)
	h.svc.workers[0].BasePay = "100.03"
	h.svc.workers[0].PayZone = "US-EAST"
	h.svc.workforce = testWorkforceOptions()
	st, err := status.New(codes.InvalidArgument, "private details").WithDetails(&commonv1.ErrorDetail{FieldViolations: []*commonv1.FieldViolation{
		{FieldPath: "proposed_base", RuleRef: "promotion.ladder.base_increase_out_of_range"},
		{FieldPath: "reason", RuleRef: "promotion.reason.review_required"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	h.svc.proposeErr = st.Err()
	h.app.Start(context.Background(), ProposalHref("omar-reyes"))
	h.awaitPage(t, "promotion form", proposalFor("omar-reyes"))
	h.app.Submit(ActionPropose, map[string]string{
		NameWorker: "omar-reyes", NameJobCode: "OPS-HRBP3", NameGrade: "P3",
		NameBase: "100.00", NameEffective: "2026-12-01", NameReason: "Original reason",
	})
	page := h.awaitPage(t, "two field refusals", noticeTitled("That proposal is not valid"))
	base, _ := fieldByID(page.Proposal.Form.Fields, FieldBase)
	reason, _ := fieldByID(page.Proposal.Form.Fields, FieldReason)
	if base.Error == "" || reason.Error == "" {
		t.Fatalf("fixture did not project both refusals: base=%q reason=%q", base.Error, reason.Error)
	}
	page.OnFieldChange(FieldBase, "105.04")
	updated := h.store.Page()
	base, _ = fieldByID(updated.Proposal.Form.Fields, FieldBase)
	reason, _ = fieldByID(updated.Proposal.Form.Fields, FieldReason)
	if base.Error != "" || reason.Error == "" || updated.Notice == nil || reason.Value != "Original reason" {
		t.Fatalf("editing pay cleared another field's refusal: base=%+v reason=%+v notice=%+v", base, reason, updated.Notice)
	}
}

func TestTodo_PROMOUX_007_Regression_TypedCorrectionWinsRegardlessOfViolationOrder(t *testing.T) {
	generic := &commonv1.FieldViolation{FieldPath: "proposed_base", RuleRef: "journey.input.invalid"}
	typed := &commonv1.FieldViolation{FieldPath: "proposed_base", RuleRef: "promotion.ladder.base_increase_out_of_range", PermittedMoneyRange: &commonv1.MoneyRange{
		Minimum: "105.04", Maximum: "115.03", Currency: "USD",
	}}
	for _, violations := range [][]*commonv1.FieldViolation{{generic, typed}, {typed, generic}} {
		st, err := status.New(codes.InvalidArgument, "private details").WithDetails(&commonv1.ErrorDetail{FieldViolations: violations})
		if err != nil {
			t.Fatal(err)
		}
		got := proposalFieldErrorsLocale(st.Err(), productui.ResolveProductLocale("en-US"))[FieldBase]
		if got != "Enter an amount from USD\u00a0105.04 to USD\u00a0115.03, inclusive." {
			t.Fatalf("violation order %q then %q lost exact correction: %q", violations[0].GetRuleRef(), violations[1].GetRuleRef(), got)
		}
	}
}

type promotionRefusalWireVerifier struct{}

func (promotionRefusalWireVerifier) Verify(_ context.Context, cred trust.Credential) (*trust.Principal, error) {
	if cred.Token != "test-promotion-refusal" {
		return nil, trust.ErrInvalidCredential
	}
	now := time.Now()
	return trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "harborcare-demo", OrganizationScopeID: "org:harborcare-demo:people-ops", Subject: "manager-jane",
		SubjectKind: trust.SubjectKindHuman, Roles: []string{"intent_author"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-refusal", Purposes: []string{"hcm_operations"},
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "digest:refusal",
	})
}

// promotionRefusalRoleAccess supplies the exact durable page and feature
// grant this wire-level refusal fixture needs. The journey transport denies
// missing permission data, so leaving RoleAccess nil would test authorization
// failure instead of the typed engine refusal these tests are responsible for.
type promotionRefusalRoleAccess struct{}

func (promotionRefusalRoleAccess) Bootstrap(context.Context, values.TenantId, string) error {
	return nil
}
func (promotionRefusalRoleAccess) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	return roleaccess.Snapshot{
		PagePermissions:    []roleaccess.PagePermission{{RoleID: "intent_author", PageID: "journeys", View: true, Create: true}},
		FeaturePermissions: []roleaccess.FeaturePermission{{RoleID: "intent_author", PageID: "journeys", FeatureID: "promotion_request", View: true, Create: true}},
	}, nil
}
func (promotionRefusalRoleAccess) SaveRole(context.Context, values.TenantId, string, roleaccess.Role) (roleaccess.Role, error) {
	return roleaccess.Role{}, nil
}
func (promotionRefusalRoleAccess) SaveAssignment(context.Context, values.TenantId, string, roleaccess.Assignment) (roleaccess.Assignment, error) {
	return roleaccess.Assignment{}, nil
}
func (promotionRefusalRoleAccess) SaveVisibility(context.Context, values.TenantId, string, string, roleaccess.VisibilityPolicy) (roleaccess.VisibilityPolicy, error) {
	return roleaccess.VisibilityPolicy{}, nil
}
func (promotionRefusalRoleAccess) SavePagePermission(context.Context, values.TenantId, string, roleaccess.PagePermission) (roleaccess.PagePermission, error) {
	return roleaccess.PagePermission{}, nil
}
func (promotionRefusalRoleAccess) SaveFeaturePermission(context.Context, values.TenantId, string, roleaccess.FeaturePermission) (roleaccess.FeaturePermission, error) {
	return roleaccess.FeaturePermission{}, nil
}

type promotionRefusalWireEngine struct {
	workspace.JourneyEngine
	refusal error
}

func (e *promotionRefusalWireEngine) ProposePromotion(context.Context, *journeyv1.ProposePromotionRequest) (*journeyv1.ProposePromotionResponse, error) {
	return nil, e.refusal
}

type promotionRefusalWireService struct {
	Service
	wire Service
}

func (s promotionRefusalWireService) ProposePromotion(ctx context.Context, req *journeyv1.ProposePromotionRequest) (*journeyv1.ProposePromotionResponse, error) {
	return s.wire.ProposePromotion(ctx, req)
}

func TestTodo_PROMOUX_007_Integration_RealRefusalReachesRenderedClient(t *testing.T) {
	minimum, err := values.NewMoney("105.04", "USD", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	maximum, err := values.NewMoney("115.03", "USD", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	engine := &promotionRefusalWireEngine{refusal: &workspace.JourneyInputError{
		FieldPath: "proposed_base", ReasonRef: "promotion.ladder.base_increase_out_of_range",
		Detail: "private pay baseline 100.03", PayRange: &workspace.JourneyPayRange{Minimum: minimum, Maximum: maximum},
	}}
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(transport.Config{Verifier: promotionRefusalWireVerifier{}})))
	journeytransport.Register(srv, journeytransport.Dependencies{Engine: engine, RoleAccess: promotionRefusalRoleAccess{}})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { srv.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	h := newHarness(t)
	h.svc.workers[0].BasePay = "100.03"
	h.svc.workers[0].PayZone = "US-EAST"
	h.svc.workforce = testWorkforceOptions()
	h.app.svc = promotionRefusalWireService{Service: h.svc, wire: NewGRPCService(conn, "test-promotion-refusal")}
	h.app.Start(context.Background(), ProposalHref("omar-reyes"))
	h.awaitPage(t, "promotion form", proposalFor("omar-reyes"))
	h.app.Submit(ActionPropose, map[string]string{
		NameWorker: "omar-reyes", NameJobCode: "OPS-HRBP3", NameGrade: "P3",
		NameBase: "100.00", NameEffective: "2026-12-01", NameReason: "Keep this entered reason",
	})
	page := h.awaitPage(t, "real wire refusal", noticeTitled("That proposal is not valid"))
	base, ok := fieldByID(page.Proposal.Form.Fields, FieldBase)
	if !ok || base.Value != "100.00" || base.Error != "Enter an amount from USD\u00a0105.04 to USD\u00a0115.03, inclusive." {
		t.Fatalf("real wire refusal did not recover entered pay: %+v", base)
	}
	if page.Notice == nil || !validSupportReference(page.Notice.SupportReference) {
		t.Fatalf("server-issued request reference is not copyable: %+v", page.Notice)
	}
	doc, err := journey.RenderToString(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`href="#propose-base"`, `value="100.00"`, "Keep this entered reason", `value="` + page.Notice.SupportReference + `"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("real wire refusal omitted %q", want)
		}
	}
	for _, secret := range []string{"private pay baseline", "promotion.ladder", "manager-jane"} {
		if strings.Contains(doc, secret) {
			t.Errorf("real wire refusal leaked %q", secret)
		}
	}
}
