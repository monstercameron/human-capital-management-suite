package journey

import (
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
)

func TestBlockedDetailShowsActualCheckNearStatusWithoutFakeRevisionAction(t *testing.T) {
	detail := DetailView{
		Journey:  JourneyCard{Stage: "BLOCKED"},
		Findings: []Finding{{Severity: "blocking", Message: "Proposed pay exceeds the approved band."}},
	}
	markup := renderNode(t, blockedFindingBannerLocale("en-US", detail))
	for _, want := range []string{"Proposed pay exceeds the approved band.", "Blocked"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("blocked summary lacks %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "Start approval workflow") {
		t.Fatalf("blocked summary invented an action: %s", markup)
	}
	if strings.Contains(markup, "#findings-heading") {
		t.Fatalf("blocked summary used document-fragment scrolling outside the main pane: %s", markup)
	}
	if node := blockedFindingBannerLocale("en-US", DetailView{Journey: JourneyCard{Stage: "PROPOSED"}, Findings: detail.Findings}); node != nil {
		t.Fatal("unblocked detail showed a blocking summary")
	}
}

func TestBlockedPayFindingUsesBusinessCopyWhilePreservingExactChecks(t *testing.T) {
	for _, tc := range []struct {
		locale string
		code   string
		want   string
	}{
		{"en-US", "promotion.pay_below_band_minimum", "below the approved minimum"},
		{"de-DE", "promotion.pay_below_band_minimum", "unter der genehmigten Untergrenze"},
		{"ar", "promotion.pay_above_band_maximum", "أعلى من الحد الأقصى"},
	} {
		detail := DetailView{Journey: JourneyCard{Stage: "BLOCKED"}, Findings: []Finding{{Severity: "blocking", Code: tc.code, Message: "annualized base 75000.00 USD is below the band minimum (compa-ratio 0.7653)"}}}
		markup := renderNode(t, blockedFindingBannerLocale(tc.locale, detail))
		if !strings.Contains(markup, tc.want) || strings.Contains(markup, "compa-ratio") {
			t.Fatalf("%s blocked banner should use localized business copy: %s", tc.locale, markup)
		}
	}
}

func TestTodo_PROMOUX_014_EffectiveDateWaitExplanationRendersTypedFacts(t *testing.T) {
	explanation := &wait.EffectiveDateWait{
		EffectiveInstant: values.NewInstant(time.Date(2026, 6, 17, 14, 0, 0, 0, time.UTC)),
		Timezone:         "America/New_York@2026a", Owner: "promotion owner",
		ScheduledAction: "revalidate and commit promotion", RemainingChecks: "approvals and references",
		NotificationBehavior: "notify owner when effective", AuthorizedIntervention: "cancel with an authorized reason",
		ReviewRequired: true, ReviewReason: "calendar changed",
	}
	markup, err := ui.RenderToString(effectiveDateWaitLocale("en-US", explanation))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2026-06-17T14:00:00Z", "America/New_York@2026a", "promotion owner", "revalidate and commit promotion", "approvals and references", "notify owner when effective", "cancel with an authorized reason", "calendar changed"} {
		if !strings.Contains(markup, want) {
			t.Errorf("wait explanation omitted %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "countdown") || strings.Contains(markup, "Advance") {
		t.Error("wait explanation exposed an untyped countdown or timer bypass")
	}
}

func TestUXBLIND007EmbeddedJourneyListHasOneResponsibility(t *testing.T) {
	view := ListView{People: &PeopleView{DirectoryLink: NavLink{Href: "/workspace/app/people"}}}
	markup, err := ui.RenderToString(embeddedListView(Page{}, view))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`id="people-heading"`, `id="new-employee-heading"`, `id="propose-heading"`} {
		if strings.Contains(markup, forbidden) {
			t.Fatalf("unrelated task remains: %s", forbidden)
		}
	}
	if !strings.Contains(markup, "Choose an employee to promote") || !strings.Contains(markup, `id="journeys-heading"`) {
		t.Fatal("journey list lost its review or employee entry point")
	}
}

// TestPrincipalChipPairsPurposeWithItsExit is UXAUDIT-007's render-level
// proof of the pairing GREEN actually requires: not merely that the purpose
// explanation renders somewhere, and not merely that an exit control renders
// somewhere, but that the masthead emits both together, the exit control
// names the same purpose the explanation just named, and it points at the
// session's real logout destination. Asserting only the purpose text (as
// this package's other coverage already did) is exactly what let a masthead
// ship with an explanation and no paired way to act on it.
func TestPrincipalChipPairsPurposeWithItsExit(t *testing.T) {
	markup, err := ui.RenderToString(principalChip(Principal{
		Subject:    "avery.okafor@northwind.example",
		Roles:      []string{"hr.business_partner"},
		Purpose:    "compensation_review",
		LogoutHref: "/workspace/logout",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Purpose: ") || !strings.Contains(markup, "compensation_review") {
		t.Fatalf("masthead lost the purpose explanation: %s", markup)
	}
	exit := regexp.MustCompile(`<a [^>]*href="/workspace/logout"[^>]*>([^<]*)</a>`).FindStringSubmatch(markup)
	if exit == nil {
		t.Fatalf("masthead has no exit control pointing at the session's logout destination: %s", markup)
	}
	if !strings.Contains(exit[1], "compensation_review") {
		t.Fatalf("exit control text %q does not name the purpose it exits, want it to say so explicitly rather than a bare generic control", exit[1])
	}
	if !strings.Contains(strings.ToLower(exit[1]), "exit") {
		t.Fatalf("exit control text %q does not read as leaving the reviewing context", exit[1])
	}

	// A purposeless session (no reviewing context to exit) keeps the plain,
	// generic control: this pairing must not invent a "purpose" that was
	// never there.
	plainMarkup, err := ui.RenderToString(principalChip(Principal{
		Subject:    "avery.okafor@northwind.example",
		LogoutHref: "/workspace/logout",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plainMarkup, "compensation_review") || !strings.Contains(plainMarkup, ">Sign out<") {
		t.Fatalf("purposeless session did not keep the plain Sign out control: %s", plainMarkup)
	}

	// No logout destination at all (no dev browser login): the explanation
	// still renders, honestly, with nothing pretending to be an exit link.
	noExitMarkup, err := ui.RenderToString(principalChip(Principal{
		Subject: "avery.okafor@northwind.example",
		Purpose: "compensation_review",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(noExitMarkup, "compensation_review") {
		t.Fatal("purpose explanation disappeared once there was no logout destination")
	}
	if strings.Contains(noExitMarkup, "<a ") {
		t.Fatalf("an exit link rendered with no LogoutHref to back it: %s", noExitMarkup)
	}
}

func mustRender(t *testing.T, p Page) string {
	t.Helper()
	out, err := RenderToString(p)
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	return out
}

func TestJourneyNextActionsPrecedeSupportingDetails(t *testing.T) {
	markup, err := ui.RenderToString(detailView(Page{}, DetailView{Actions: []Action{{}}, Proposal: []Fact{{Label: "Reason", Value: "Promotion"}}}))
	if err != nil {
		t.Fatal(err)
	}
	actions := strings.Index(markup, `id="actions-heading"`)
	proposal := strings.Index(markup, `id="proposal-heading"`)
	if actions < 0 || proposal < 0 || actions >= proposal || strings.Count(markup, `id="actions-heading"`) != 1 {
		t.Fatal("next actions must appear once before supporting proposal details")
	}
}

func TestJourneyHeroTechnicalIdentifiersAreCollapsed(t *testing.T) {
	markup, err := ui.RenderToString(heroSection(JourneyCard{WorkerName: "Jane", WorkerRef: "worker-test", IntentID: "intent-test", InstanceID: "instance-test", EffectiveDate: "date-test", Updated: "updated-test", DiagnosticsAuthorized: true}, true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `<details class="jn-journey-technical">`) || !strings.Contains(markup, "Technical details") {
		t.Fatal("technical identifiers need a closed native disclosure")
	}
	start, end := strings.Index(markup, "<details"), strings.Index(markup, "</details>")
	// PROMOUX-008: the disclosure redacts every identifier it shows
	// (maskIdentifier), so the raw values never appear in the markup at
	// all -- only their masked form, and only inside the disclosure.
	for _, identifier := range []string{"worker-test", "intent-test", "instance-test"} {
		if strings.Contains(markup, identifier) {
			t.Fatalf("raw identifier %q must not appear in the markup at all; the disclosure must redact it", identifier)
		}
	}
	for _, masked := range []string{maskIdentifier("worker-test"), maskIdentifier("intent-test"), maskIdentifier("instance-test")} {
		at := strings.Index(markup, masked)
		if at < start || at > end {
			t.Fatalf("redacted identifier outside disclosure: %s", masked)
		}
	}
	for _, value := range []string{"date-test", "updated-test"} {
		at := strings.Index(markup, value)
		if at < 0 || at >= start {
			t.Fatalf("primary date hidden: %s", value)
		}
	}
}

func TestTodo_UXAUDIT_006_JourneyHeroLocale(t *testing.T) {
	for _, tc := range []struct {
		locale, title, effective, updated, technical string
	}{
		{"en-US", "Promotion journey", "Effective", "Updated", "Technical details"},
		{"de-DE", "Beförderungsantrag", "Wirksam", "Aktualisiert", "Technische Details"},
		{"ar", "طلب الترقية", "السريان", "آخر تحديث", "التفاصيل التقنية"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			markup, err := ui.RenderToString(heroSectionLocale(tc.locale, JourneyCard{WorkerName: "Omar Reyes", WorkerRef: "worker-ref", EffectiveDate: "date-value", Updated: "updated-value", DiagnosticsAuthorized: true}, true))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{tc.title, tc.effective, tc.updated, tc.technical} {
				if !strings.Contains(markup, want) {
					t.Fatalf("%s hero missing %q: %s", tc.locale, want, markup)
				}
			}
		})
	}
}

func TestTodo_UXAUDIT_006_PromotionStepperLocale(t *testing.T) {
	for _, tc := range []struct{ locale, heading, state string }{
		{"en-US", "Stages", "Current stage"},
		{"de-DE", "Schritte", "Aktueller Schritt"},
		{"ar", "مراحل الطلب", "المرحلة الحالية"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			markup, err := ui.RenderToString(stepperSectionLocale(tc.locale, []Step{{ID: "finance-review", Label: "finance", State: stepActive}}))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(markup, tc.heading) || !strings.Contains(markup, tc.state) || !strings.Contains(markup, `aria-current="step"`) {
				t.Fatalf("localized stepper lost heading, state or current semantics: %s", markup)
			}
		})
	}
}

func TestTodo_UXAUDIT_006_I18N_PromotionDetailSections(t *testing.T) {
	for _, tc := range []struct {
		locale string
		wants  []string
	}{
		{"de-DE", []string{"Aktuell und vorgeschlagen", "Geschäftliche Begründung", "Grundgehalt", "Prüfungen und Zeitplan", "Angaben fehlen", "Zeitlicher Ablauf", "Erfasstes Ergebnis", "Aktionen und Verlauf", "Beförderung erfasst"}},
		{"ar", []string{"الحالي والمقترح", "مبرر العمل", "الأجر الأساسي", "الفحوص والتوقيت", "تنقص معلومات", "الفترة الفعالة", "النتيجة المسجلة", "الإجراءات والسجل", "سُجلت الترقية"}},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			markup, err := ui.RenderToString(detailView(Page{Locale: tc.locale}, DetailView{
				Journey:         JourneyCard{WorkerName: "Omar Reyes", Stage: "recorded"},
				Proposal:        []Fact{{Label: tc.wants[1], Value: "reason-value"}},
				Comparison:      []ComparisonRow{{Label: tc.wants[2], Current: "93", Proposed: "98", Changed: true}},
				Findings:        []Finding{{Severity: "needs-data", Message: "finding-value"}},
				EffectiveWindow: &EffectiveWindow{Start: "start-value", EffectiveDate: "date-value", KnownAt: "known-value"},
				Ledger:          &LedgerCard{EffectiveAt: "date-value", RecordedAt: "recorded-value"},
				Timeline:        []TimelineEvent{{At: "time-value", Title: tc.wants[8], Actor: "actor-value"}},
			}))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.wants {
				if !strings.Contains(markup, want) {
					t.Errorf("%s detail missing %q", tc.locale, want)
				}
			}
			for _, forbidden := range []string{"⟦", ">Current<", ">Changed<", ">Outcome<", ">Timeline<"} {
				if strings.Contains(markup, forbidden) {
					t.Errorf("%s detail leaked unresolved or English copy %q", tc.locale, forbidden)
				}
			}
		})
	}
}

func TestTodo_UXAUDIT_006_Browser_RTLMoneyIsDirectionallyIsolated(t *testing.T) {
	markup, err := ui.RenderToString(detailView(Page{Locale: "ar"}, DetailView{
		Journey:    JourneyCard{WorkerName: "Naomi", PayLine: "USD 125,000.00 → 135,000.00 (+8.0%)"},
		Comparison: []ComparisonRow{{Label: "الأجر الأساسي", Current: "USD 125,000.00", Proposed: "USD 135,000.00", Delta: "+USD 10,000.00 (+8.0%)", Changed: true}},
		Proposal:   []Fact{{Label: "مبرر العمل", Value: "Expanded enterprise responsibilities."}},
		Timeline:   []TimelineEvent{{Title: "قُدم طلب الترقية", Detail: "Expanded enterprise responsibilities."}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="jn-hero-pay"><span dir="ltr"`, `class="jn-delta" dir="ltr"`, `class="jn-num" dir="auto"`, `<dd dir="auto">Expanded enterprise responsibilities.</dd>`, `class="jn-tldetail" dir="auto"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("RTL money lost directional isolation %q: %s", want, markup)
		}
	}
}

func TestTodo_UXAUDIT_006_Browser_TimelineSpineFollowsTextDirection(t *testing.T) {
	css := Stylesheet()
	if !regexp.MustCompile(`\.jn-tl::before\{[^}]*inset-inline-start:\.4375rem`).MatchString(css) {
		t.Error("timeline spine must follow inline start in both LTR and RTL")
	}
}

func TestTodo_UXAUDIT_006_Regression_PendingIsNotCalledRecorded(t *testing.T) {
	for _, tc := range []struct{ locale, want, recorded string }{
		{"en-US", "What happens next", "Recorded outcome"},
		{"de-DE", "Nächste Schritte", "Erfasstes Ergebnis"},
		{"ar", "ما التالي", "النتيجة المسجلة"},
	} {
		markup, err := ui.RenderToString(outcomeSectionLocale(tc.locale, nil, "pending-value"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, tc.want) || strings.Contains(markup, tc.recorded) {
			t.Errorf("%s pending outcome is mislabeled: %s", tc.locale, markup)
		}
	}
}

func TestTodo_UXAUDIT_006_Accessibility_DetailNavigationAndDiagnosticsLocale(t *testing.T) {
	for _, tc := range []struct{ locale, context, back, all, diagnostics string }{
		{"de-DE", "Kontext des Antrags", "Zurück zum Mitarbeiterprofil", "Alle Beförderungsanträge anzeigen", "Systemdiagnose"},
		{"ar", "التنقل بين الطلبات", "العودة إلى ملف الموظف", "عرض جميع طلبات الترقية", "تشخيص النظام"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			markup, err := ui.RenderToString(detailView(Page{Locale: tc.locale}, DetailView{
				Journey: JourneyCard{WorkerName: "Naomi", DiagnosticsAuthorized: true}, Diagnostics: true,
				BackLink: NavLink{Href: "/employee"}, JourneysLink: NavLink{Href: "/journeys"},
				Engine: []Fact{{Label: "Instance", Value: "diagnostic-only", Mono: true}},
			}))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{`aria-label="` + tc.context + `"`, tc.back, tc.all, tc.diagnostics} {
				if !strings.Contains(markup, want) {
					t.Errorf("%s detail lacks localized control %q", tc.locale, want)
				}
			}
		})
	}
}

// ----------------------------------------------------------------------
// Sections render for every fixture
// ----------------------------------------------------------------------

// TestEverySectionRendersForEveryFixture is the coarse net: it asserts each
// section's own heading or landmark actually reaches the output for the
// fixtures that should contain it. A section function that silently
// returned nil would still let the page render, and only this catches it.
func TestEverySectionRendersForEveryFixture(t *testing.T) {
	// The list view is workforce-first: People, New employee, the proposal
	// (named for whoever is selected), then the journeys grid.
	listWants := []string{
		`id="people-heading"`, `>People<`,
		`id="new-employee-heading"`, `>New employee<`,
		`class="jn-table jn-zebra jn-people"`,
		`id="propose-heading"`, `>Propose a promotion for Omar Reyes<`,
		`id="journeys-heading"`, `>Journeys<`,
		`class="jn-grid"`, `class="jn-card jn-journey"`,
		`method="post"`, `class="jn-btn"`,
	}
	detailWants := []string{
		`id="journey-heading"`, `class="jn-panel jn-hero"`,
		`id="stages-heading"`, `class="jn-stepper"`,
		`id="proposal-heading"`, `class="jn-facts"`, `class="jn-table"`,
		`id="findings-heading"`, `class="jn-board"`, `class="jn-checkpill"`,
		`class="jn-band"`, `class="jn-meter"`, `class="jn-strip"`,
		`id="workflow-heading"`, `class="jn-workitems"`, `jn-zebra`,
		`id="outcome-heading"`,
		`id="evidence-heading"`, `class="jn-evidence"`,
		`id="actions-heading"`, `class="jn-actions"`,
		`id="timeline-heading"`, `class="jn-timeline"`,
		`class="jn-col jn-rail"`,
	}
	cases := map[string][]string{
		"list":      listWants,
		"detail":    detailWants,
		"completed": append(append([]string{}, detailWants...), `>Recorded<`),
	}
	for _, name := range sampleNames() {
		out := mustRender(t, samplePages()[name])
		t.Run(name, func(t *testing.T) {
			for _, want := range cases[name] {
				if !strings.Contains(out, want) {
					t.Errorf("rendered %s page does not contain %q", name, want)
				}
			}
		})
	}
}

func TestFocusedProposalShowsOnlyItsSubject(t *testing.T) {
	p := Page{
		Title: "Promote Jane", Brand: "Human Capital Management Suite",
		Proposal: &ProposalView{
			Subject: &PromotionSubject{
				Ref: "jane-doe", Name: "Jane Doe", Number: "W-1001", Title: "Software Engineer III",
				JobCode: "ENG-SWE3", Grade: "P3", OrgUnit: "Engineering", Location: "New York", PayLine: "USD 120,000.00",
			},
			BackHref:     "/workspace/app/person?person=jane-doe",
			JourneysLink: NavLink{Label: "View all promotion journeys", Href: "#/journeys"},
			Form: ProposalForm{Action: "#/journeys/new?worker=jane-doe", Submit: "Propose and simulate", Fields: []Field{
				{ID: "worker", Name: "worker_ref", Kind: fieldKindHidden, Value: "jane-doe"},
				{ID: "job", Name: "target_job_code", Label: "Target job code", Kind: fieldKindText},
			}},
		},
	}
	out := mustRender(t, p)
	for _, want := range []string{"Promote Jane Doe", "Employee", "Employee details ready", `name="worker_ref" type="hidden" value="jane-doe"`, "Promotion details", "View all promotion requests", "Software Engineer III · P3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("focused proposal missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "ENG-SWE3 · P3") {
		t.Fatal("the ordinary current-role fact exposed the job code despite an available title")
	}
	for _, unrelated := range []string{"Omar Reyes", "Priya Raghunathan", `id="people-heading"`, `id="journeys-heading"`, `for="worker">Worker`} {
		if strings.Contains(out, unrelated) {
			t.Fatalf("focused proposal leaked unrelated/global context %q", unrelated)
		}
	}
}

func TestTodo_UXAUDIT_006_Browser_PromotionFormLocale(t *testing.T) {
	for _, tc := range []struct {
		locale, direction, heading, submitHelp string
	}{
		{"en-US", "ltr", "Promote Jane Doe", "We check this request before submission"},
		{"de-DE", "ltr", "Jane Doe befördern", "Wir prüfen diesen Antrag vor der Einreichung"},
		{"ar", "rtl", "ترقية Jane Doe", "نتحقق من الطلب قبل تقديمه"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			p := Page{Locale: tc.locale, Proposal: &ProposalView{
				Subject: &PromotionSubject{Name: "Jane Doe", Number: "W-1001"},
				Form:    ProposalForm{Submit: "Continue", Fields: []Field{{ID: "reason", Name: "reason", Label: "Reason", Required: true}}},
			}}
			out, err := Document(p)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{`lang="` + tc.locale + `"`, `dir="` + tc.direction + `"`, tc.heading, tc.submitHelp} {
				if !strings.Contains(out, want) {
					t.Errorf("document missing %q", want)
				}
			}
			if strings.Contains(out, "Pinned to the selected ladder edge") || strings.Contains(out, "⟦journey.") {
				t.Fatal("ordinary promotion form contains internal language or unresolved catalog key")
			}
		})
	}
}

func TestTodo_UXAUDIT_006_Browser_EnhancedPromotionUsesLocalizedValidation(t *testing.T) {
	form := ProposalForm{Fields: []Field{{ID: "propose-position", Name: "target_position", Label: "Target position", Required: true}}}
	page := Page{Proposal: &ProposalView{Form: form}}
	plain := mustRender(t, page)
	if strings.Contains(strings.ToLower(plain), "novalidate") {
		t.Fatal("plain POST lost native constraint validation")
	}
	page.Proposal.Form.OnSubmit = func(map[string]string) {}
	if got := proposalFormProps(live{}, page.Proposal.Form).Raw["noValidate"]; got != true {
		t.Fatalf("live form did not set the browser's noValidate property: %v", got)
	}
	enhanced := mustRender(t, page)
	if !strings.Contains(strings.ToLower(enhanced), "novalidate") {
		t.Fatal("enhanced promotion form still blocks localized field validation")
	}
}

func TestTodo_UXAUDIT_006_Accessibility_LocalizedSeverityNames(t *testing.T) {
	page := Page{Locale: "de-DE", Notice: &Notice{Tone: toneWarning, Title: "Pflichtfelder ausfüllen"}, Proposal: &ProposalView{Form: ProposalForm{Fields: []Field{{ID: "propose-base", Name: "proposed_base", Label: "Gehalt", Error: "Füllen Sie dieses Feld aus."}}}}}
	out := mustRender(t, page)
	if !strings.Contains(out, "Warnung: ") || !strings.Contains(out, "Fehler: ") {
		t.Fatalf("localized visible copy had English-only assistive severity names: %s", out)
	}
}

func TestTodo_PROMOUX_007_Accessibility_ErrorSummaryLinksToInvalidFields(t *testing.T) {
	for _, tc := range []struct{ locale, heading string }{
		{"en-US", "Fields to correct"},
		{"de-DE", "Zu korrigierende Felder"},
		{"ar", "الحقول المطلوب تصحيحها"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			page := Page{Locale: tc.locale, Notice: &Notice{Tone: toneWarning, Title: "Correct the form"}, Proposal: &ProposalView{Form: ProposalForm{Fields: []Field{
				{ID: "propose-position", Name: "target_position", Label: "Target position", Error: "Required"},
				{ID: "propose-base", Name: "proposed_base", Label: "Proposed base pay", Error: "Required"},
				{ID: "propose-reason", Name: "reason", Label: "Reason"},
			}}}}
			out := mustRender(t, page)
			for _, want := range []string{tc.heading, `href="#propose-position"`, `href="#propose-base"`, "Target position", "Proposed base pay"} {
				if !strings.Contains(out, want) {
					t.Errorf("error summary missing %q", want)
				}
			}
			if strings.Contains(out, `href="#propose-reason"`) {
				t.Error("valid field was included in error summary")
			}
		})
	}
}

func TestListShowsItsEmptyStateWhenThereAreNoJourneys(t *testing.T) {
	p := SampleListPage()
	p.List.Journeys = nil
	p.List.Empty = "No promotion has been proposed in this tenant yet."
	out := mustRender(t, p)
	if !strings.Contains(out, "jn-empty") {
		t.Error("the empty panel did not render")
	}
	if !strings.Contains(out, p.List.Empty) {
		t.Error("the empty message did not render")
	}
	if strings.Contains(out, `class="jn-grid"`) {
		t.Error("the card grid rendered alongside the empty state")
	}
	if !strings.Contains(out, ">0 requests<") {
		t.Error("the section count did not fall to zero")
	}
}

func TestJourneyCardsKeepTechnicalIdentifiersOutOfTheOverview(t *testing.T) {
	page := SampleListPage()
	for index := range page.List.Journeys {
		page.List.Journeys[index].DiagnosticsAuthorized = false
	}
	out := mustRender(t, page)
	for _, forbidden := range []string{`class="jn-journey-technical"`, ">Technical details<", ">Instance<"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("journey overview disclosed technical context through %q", forbidden)
		}
	}
	if got := readableTenantLabel("harborcare-demo"); got != "Harborcare Demo" {
		t.Fatalf("readableTenantLabel = %q, want Harborcare Demo", got)
	}
}

func TestListExplainsWhenTheEngineIsNotComposed(t *testing.T) {
	p := SampleListPage()
	p.List.EngineAvailable = false
	p.List.EngineNotice = "This cell was started without -execution-authority=true."
	p.List.Form.Disabled = true
	p.List.Form.DisabledReason = "Nothing can be executed here."
	// A cell with no execution authority cannot record a worker either, so
	// the People form is refused alongside the proposal: the control sweep
	// below covers every editable control on the page, both forms included.
	p.List.People.Form.Disabled = true
	p.List.People.Form.DisabledReason = "Nothing can be recorded here."
	out := mustRender(t, p)

	if !strings.Contains(out, "jn-callout") || strings.Contains(out, p.List.EngineNotice) || !strings.Contains(out, "Your request has not changed") {
		t.Error("the engine notice leaked its technical cause or lost task guidance")
	}
	if !strings.Contains(out, `id="proposal-disabled"`) || !strings.Contains(out, p.List.Form.DisabledReason) {
		t.Error("the disabled reason did not render")
	}
	if !strings.Contains(out, `aria-describedby="proposal-disabled"`) {
		t.Error("the disabled submit button is not described by its reason")
	}
	if !regexp.MustCompile(`<button[^>]*disabled`).MatchString(out) {
		t.Error("the submit button is not disabled")
	}
	for _, control := range regexp.MustCompile(`<(input|select|textarea)[^>]*>`).FindAllString(out, -1) {
		if strings.Contains(control, `type="hidden"`) {
			continue
		}
		if !strings.Contains(control, "disabled") {
			t.Errorf("control is still editable while the form is disabled: %s", control)
		}
	}
}

func TestEngineCalloutIsAbsentWhenTheEngineIsComposed(t *testing.T) {
	out := mustRender(t, SampleListPage())
	if strings.Contains(out, "jn-callout") {
		t.Error("the engine callout rendered on a cell that has the execution authority")
	}
}

// ----------------------------------------------------------------------
// Forms: routes, hidden inputs, ordering
// ----------------------------------------------------------------------

var formPattern = regexp.MustCompile(`(?s)<form\b[^>]*>.*?</form>`)

func formsIn(html string) []string { return formPattern.FindAllString(html, -1) }

func formAction(form string) string {
	m := regexp.MustCompile(`action="([^"]*)"`).FindStringSubmatch(form)
	if m == nil {
		return ""
	}
	return m[1]
}

// hiddenPairs returns the (name, value) of every hidden input in a form, in
// document order.
func hiddenPairs(form string) [][2]string {
	var out [][2]string
	for _, tag := range regexp.MustCompile(`<input[^>]*type="hidden"[^>]*>`).FindAllString(form, -1) {
		name := regexp.MustCompile(`name="([^"]*)"`).FindStringSubmatch(tag)
		value := regexp.MustCompile(`value="([^"]*)"`).FindStringSubmatch(tag)
		if name == nil {
			continue
		}
		v := ""
		if value != nil {
			v = value[1]
		}
		out = append(out, [2]string{name[1], v})
	}
	return out
}

// formWithAction returns the single rendered form posting to action. The
// list page now carries two -- New employee and the proposal -- so a test
// about one of them has to name which.
func formWithAction(t *testing.T, page, action string) string {
	t.Helper()
	var found []string
	for _, form := range formsIn(page) {
		if formAction(form) == action {
			found = append(found, form)
		}
	}
	if len(found) != 1 {
		t.Fatalf("found %d forms posting to %q, want exactly 1", len(found), action)
	}
	return found[0]
}

func TestProposalFormPostsToItsRouteWithEveryHiddenInput(t *testing.T) {
	p := SampleListPage()
	out := mustRender(t, p)
	forms := formsIn(out)
	if len(forms) != 2 {
		t.Fatalf("the list page rendered %d forms, want exactly 2 (New employee and the proposal)", len(forms))
	}
	form := formWithAction(t, out, p.List.Form.Action)
	if !strings.Contains(form, `method="post"`) {
		t.Error("the form is not a POST; a GET would put the proposal in the URL")
	}
	assertHiddenInputs(t, form, p.List.Form.Hidden)
}

func TestEveryActionFormPostsToItsOwnRouteWithEveryHiddenInput(t *testing.T) {
	p := SampleDetailPage()
	forms := formsIn(mustRender(t, p))
	if len(forms) != len(p.Detail.Actions) {
		t.Fatalf("rendered %d forms for %d actions", len(forms), len(p.Detail.Actions))
	}
	for i, a := range p.Detail.Actions {
		form := forms[i]
		if got := formAction(form); got != a.Action {
			t.Errorf("action %q posts to %q, want %q", a.ID, got, a.Action)
		}
		if !strings.Contains(form, `method="post"`) {
			t.Errorf("action %q is not a POST", a.ID)
		}
		assertHiddenInputs(t, form, a.Hidden)
	}
}

func assertHiddenInputs(t *testing.T, form string, want map[string]string) {
	t.Helper()
	got := hiddenPairs(form)
	if len(got) != len(want) {
		t.Fatalf("form emitted %d hidden inputs, want %d (%v)", len(got), len(want), got)
	}
	for _, pair := range got {
		w, ok := want[pair[0]]
		if !ok {
			t.Errorf("form emitted an unexpected hidden input %q", pair[0])
			continue
		}
		if pair[1] != w {
			t.Errorf("hidden %q = %q, want %q", pair[0], pair[1], w)
		}
	}
	for name := range want {
		found := false
		for _, pair := range got {
			if pair[0] == name {
				found = true
			}
		}
		if !found {
			t.Errorf("form is missing the hidden input %q", name)
		}
	}
}

// TestHiddenInputsAreEmittedInSortedKeyOrder is not cosmetic. Go randomises
// map iteration, so an unsorted range would make two renders of the same
// page different documents -- and on the live client it would make the
// reconciler replace every hidden input on every re-render.
func TestHiddenInputsAreEmittedInSortedKeyOrder(t *testing.T) {
	hidden := map[string]string{
		"zeta": "1", "alpha": "2", "mike": "3", "csrf_token": "4", "Beta": "5", "_leading": "6",
	}
	p := SampleListPage()
	p.List.Form.Hidden = hidden

	// Render repeatedly: a single pass can agree with sorted order by luck.
	for i := 0; i < 12; i++ {
		form := formWithAction(t, mustRender(t, p), p.List.Form.Action)
		var names []string
		for _, pair := range hiddenPairs(form) {
			names = append(names, pair[0])
		}
		want := []string{"Beta", "_leading", "alpha", "csrf_token", "mike", "zeta"}
		if strings.Join(names, ",") != strings.Join(want, ",") {
			t.Fatalf("pass %d: hidden inputs in order %v, want %v", i, names, want)
		}
	}
}

func TestActionsCarryTheirVariantDescriptionAndActsAsLine(t *testing.T) {
	p := SampleDetailPage()
	out := mustRender(t, p)
	for _, a := range p.Detail.Actions {
		if a.Description != "" && !strings.Contains(out, a.Description) {
			t.Errorf("action %q lost its description", a.ID)
		}
		if a.ActsAs != "" && !strings.Contains(out, "Acts as "+a.ActsAs) {
			t.Errorf("action %q does not say whose authority it uses", a.ID)
		}
		if !strings.Contains(out, `data-variant="`+a.Variant+`"`) {
			t.Errorf("action %q lost its %q variant", a.ID, a.Variant)
		}
		if a.Disabled && !strings.Contains(out, `id="action-`+a.ID+`-blocked"`) {
			t.Errorf("action %q is disabled but names no reason", a.ID)
		}
	}
}

func TestTodo_PROMOUX_010_Localized(t *testing.T) {
	p := SampleDetailPage()
	p.Detail.Actions = []Action{{
		ID: "approve", Label: "Approve", Variant: "primary", Action: "/approve",
		Confirmation:     []Fact{{Label: "Employee", Value: "Priya"}, {Label: "Effective date", Value: "1 Dec 2026"}},
		ConfirmationNote: "Approval records the governed promotion fact.",
		Fields:           []Field{{ID: "approve-reason", Name: "reason", Label: "Reason", Kind: fieldKindTextarea}},
	}}
	out := mustRender(t, p)
	for _, want := range []string{`class="jn-confirm"`, `class="jn-confirm-body"`, "Review and approve", "Cancel review", "Confirm approval", "Priya", "1 Dec 2026", "Approval records the governed promotion fact.", `id="approve-reason"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("confirmation markup is missing %q\n%s", want, out)
		}
	}
	if start := strings.Index(out, `<details class="jn-confirm"`); start < 0 || !strings.Contains(out[start:strings.Index(out[start:], `</details>`)+start], `id="approve-reason"`) {
		t.Fatal("decision reason is not contained in the shared review disclosure")
	}
}

func TestTodo_PROMOUX_010_Browser_Localized(t *testing.T) {
	p := SampleDetailPage()
	p.Detail.Actions = []Action{
		{ID: "execute", Label: "Start", Action: "/start", ConfirmationNote: "Start the reviews."},
		{ID: "approve", Label: "Approve", Action: "/approve", ConfirmationNote: "Record your approval."},
		{ID: "reject", Label: "Reject", Action: "/reject", ConfirmationNote: "End the request."},
		{ID: "withdraw", Label: "Withdraw", Action: "/withdraw", ConfirmationNote: "Withdraw the request."},
		{ID: "cancel", Label: "Cancel", Action: "/cancel", ConfirmationNote: "Cancel the request."},
	}
	out := mustRender(t, p)
	if got := strings.Count(out, `class="jn-confirm"`); got != len(p.Detail.Actions) {
		t.Fatalf("shared review surfaces = %d, want %d", got, len(p.Detail.Actions))
	}
	for _, action := range p.Detail.Actions {
		id := "action-" + action.ID + "-review-heading"
		if !strings.Contains(out, `id="`+id+`"`) {
			t.Errorf("%s does not use the shared labeled review surface", action.ID)
		}
	}
	if strings.Count(out, `class="jn-confirm-open-label"`) != len(p.Detail.Actions) {
		t.Fatal("a consequential action bypasses the shared review trigger")
	}
}

func TestTodo_PROMOUX_010_Accessibility_Localized(t *testing.T) {
	p := SampleDetailPage()
	p.Detail.Actions = []Action{{ID: "approve", Label: "Approve", Action: "/approve", Confirmation: []Fact{{Label: "Employee", Value: "Priya"}}}}
	out := mustRender(t, p)
	for _, want := range []string{`<details`, `<summary`, `id="action-approve-review-heading"`, `class="jn-btn jn-confirm-cancel"`, "Cancel review", `type="button"`, `role="status"`} {
		if !strings.Contains(out, want) {
			t.Errorf("modal review lacks %q", want)
		}
	}
}

func TestTodo_PROMOUX_010_Performance_Localized(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{`.jn-confirm-surface{`, "position:fixed", "max-height:calc(100vh - 2rem)", "overflow-y:auto", ".jn-confirm-actionbar{"} {
		if !strings.Contains(css, want) {
			t.Errorf("bounded, non-reflowing review surface lacks %q", want)
		}
	}
	for _, want := range []string{".jn-confirm-backdrop{", ".jn-confirm-status{", "position:sticky"} {
		if !strings.Contains(css, want) {
			t.Errorf("narrow confirmation controls lack %q", want)
		}
	}
	p := SampleDetailPage()
	p.Detail.Actions = []Action{
		{ID: "execute", Label: "Start", Action: "/start", ConfirmationNote: "Start the reviews."},
		{ID: "approve", Label: "Approve", Action: "/approve", ConfirmationNote: "Record your approval."},
		{ID: "reject", Label: "Reject", Action: "/reject", ConfirmationNote: "End the request."},
		{ID: "withdraw", Label: "Withdraw", Action: "/withdraw", ConfirmationNote: "Withdraw the request."},
		{ID: "cancel", Label: "Cancel", Action: "/cancel", ConfirmationNote: "Cancel the request."},
	}
	if _, err := RenderToString(p); err != nil {
		t.Fatal(err)
	}
	durations := make([]time.Duration, 40)
	for i := range durations {
		start := time.Now()
		if _, err := RenderToString(p); err != nil {
			t.Fatal(err)
		}
		durations[i] = time.Since(start)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	if p95 := durations[37]; p95 > 50*time.Millisecond {
		t.Errorf("five-action review p95 render = %s, budget 50ms", p95)
	}
}

func TestTodo_UXAUDIT_006_I18N_ConfirmationControls(t *testing.T) {
	for _, tc := range []struct {
		locale, heading, review, confirm, cancel, rejectHelp string
	}{
		{"en-US", "Actions", "Review and approve", "Confirm approval", "Cancel review", "Required. The requester sees this exactly as written."},
		{"de-DE", "Aktionen", "Genehmigung prüfen", "Genehmigung bestätigen", "Prüfung abbrechen", "Erforderlich. Die antragstellende Person sieht diesen Text unverändert."},
		{"ar", "الإجراءات", "مراجعة الموافقة", "تأكيد الموافقة", "إلغاء المراجعة", "مطلوب. يرى مقدم الطلب هذا النص كما كتبته."},
	} {
		p := SampleDetailPage()
		p.Locale = tc.locale
		p.Detail.Actions = []Action{{ID: "approve", Label: tc.confirm, Action: "/approve", Confirmation: []Fact{{Label: "Employee", Value: "Priya"}}, ConfirmTitle: tc.confirm, ReviewLabel: tc.review}}
		out := mustRender(t, p)
		for _, want := range []string{tc.heading, tc.review, tc.confirm, tc.cancel} {
			if !strings.Contains(out, want) {
				t.Errorf("%s confirmation missing %q", tc.locale, want)
			}
		}
		if strings.Contains(out, "⟦") {
			t.Errorf("%s confirmation uses a missing catalog key", tc.locale)
		}
		if got := productui.ResolveProductLocale(tc.locale).Text("journey.action_reject_help"); got != tc.rejectHelp {
			t.Errorf("%s rejection audience = %q, want %q", tc.locale, got, tc.rejectHelp)
		}
	}
}

func TestPromotionComparisonPrecedesRequestMetadata(t *testing.T) {
	p := SampleDetailPage()
	out := mustRender(t, p)
	comparison := strings.Index(out, `id="comparison-heading"`)
	request := strings.Index(out, `>Request</h3>`)
	if comparison < 0 || request < 0 || comparison >= request {
		t.Fatal("promotion review must show the change comparison before request metadata")
	}
}

// TestUnknownActionVariantFallsBackToSecondary keeps a projection that
// invents a variant from rendering an unstyled button.
func TestUnknownActionVariantFallsBackToSecondary(t *testing.T) {
	p := SampleDetailPage()
	p.Detail.Actions = []Action{{ID: "x", Label: "Do it", Variant: "chartreuse", Action: "/x"}}
	out := mustRender(t, p)
	if !strings.Contains(out, `data-variant="secondary"`) {
		t.Error("an unrecognised variant did not fall back to secondary")
	}
	if strings.Contains(out, "chartreuse") {
		t.Error("an unrecognised variant reached the markup")
	}
}

// ----------------------------------------------------------------------
// Fields
// ----------------------------------------------------------------------

func TestFieldNodeRendersEveryKind(t *testing.T) {
	cases := []struct {
		name  string
		field Field
		want  []string
		avoid []string
	}{
		{
			name:  "text",
			field: Field{ID: "f", Name: "n", Label: "L", Kind: fieldKindText, Value: "v", Placeholder: "p"},
			want:  []string{`<input`, `type="text"`, `id="f"`, `name="n"`, `value="v"`, `placeholder="p"`},
		},
		{
			name:  "number carries its step, minimum and a decimal keypad",
			field: Field{ID: "f", Name: "n", Label: "L", Kind: fieldKindNumber, Value: "1.50", Step: "0.01", Min: "0"},
			want:  []string{`type="number"`, `step="0.01"`, `min="0"`, `inputmode="decimal"`},
		},
		{
			name:  "date",
			field: Field{ID: "f", Name: "n", Label: "L", Kind: fieldKindDate, Value: "2026-06-01", Min: "2026-05-13"},
			want:  []string{`type="date"`, `value="2026-06-01"`, `min="2026-05-13"`},
		},
		{
			name: "select marks the chosen option",
			field: Field{ID: "f", Name: "n", Label: "L", Kind: fieldKindSelect, Options: []Option{
				{Value: "a", Label: "A"}, {Value: "b", Label: "B", Selected: true},
			}},
			want: []string{`<select`, `<option value="a">A</option>`, `selected value="b"`},
		},
		{
			name:  "textarea holds its value as content and spans the grid",
			field: Field{ID: "f", Name: "n", Label: "L", Kind: fieldKindTextarea, Value: "long text"},
			want:  []string{`<textarea`, `>long text</textarea>`, `data-span="full"`},
		},
		{
			name:  "hidden renders bare, with no label or wrapper",
			field: Field{ID: "f", Name: "n", Label: "L", Kind: fieldKindHidden, Value: "v"},
			want:  []string{`type="hidden"`, `name="n"`, `value="v"`},
			avoid: []string{`<label`, `jn-field`},
		},
		{
			name:  "an unknown kind degrades to a text input",
			field: Field{ID: "f", Name: "n", Label: "L", Kind: "colour-picker", Value: "v"},
			want:  []string{`type="text"`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := renderNode(t, fieldNode(live{}, tc.field, false))
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q in:\n%s", want, out)
				}
			}
			for _, avoid := range tc.avoid {
				if strings.Contains(out, avoid) {
					t.Errorf("unexpected %q in:\n%s", avoid, out)
				}
			}
		})
	}
}

func TestPromotionSelectPlaceholderHasExplicitEmptyValue(t *testing.T) {
	field := Field{ID: "propose-job", Name: "job_code", Label: "Next role", Kind: fieldKindSelect, Required: true, Options: []Option{
		{Value: "", Label: "Select a next role", Selected: true},
		{Value: "FIN-DIR", Label: "Finance Director"},
	}}
	markup := renderNode(t, fieldNode(live{values: map[string]string{}, onFieldChange: func(string, string) {}}, field, false))
	if !strings.Contains(markup, `<option selected value="">Select a next role</option>`) ||
		!strings.Contains(markup, `<option value="FIN-DIR">Finance Director</option>`) {
		t.Fatalf("required promotion select lost its explicit empty choice: %s", markup)
	}
}

// TestFieldNodeWiresLabelHelpErrorAndAdornments checks the whole
// accessibility contract of one control in one place: the label points at
// the control, the description order is prefix, suffix, help, error, and a
// rejected value is announced as invalid rather than merely tinted red.
func TestFieldNodeWiresLabelHelpErrorAndAdornments(t *testing.T) {
	f := Field{
		ID: "base", Name: "proposed_base", Label: "Proposed base pay", Kind: fieldKindNumber,
		Required: true, Prefix: "USD", Suffix: "per year",
		Help:  "In the workers current currency.",
		Error: "Above the approved envelope for this org unit.",
	}
	out := renderNode(t, fieldNode(live{}, f, false))

	for _, want := range []string{
		`<label class="jn-label" for="base"`,
		`aria-describedby="base-prefix base-suffix base-help base-error"`,
		`aria-invalid="true"`,
		`aria-required="true"`,
		`required`,
		`id="base-prefix"`, `>USD</span>`,
		`id="base-suffix"`, `>per year</span>`,
		`id="base-help"`, f.Help,
		`id="base-error"`, f.Error,
		`data-invalid="true"`,
		`(required)`,
		`Error: `,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestOptionalFieldIsNeitherRequiredNorInvalid(t *testing.T) {
	out := renderNode(t, fieldNode(live{}, Field{ID: "f", Name: "n", Label: "L", Kind: fieldKindText}, false))
	for _, avoid := range []string{"aria-required", "aria-invalid", "aria-describedby", "required", "(required)"} {
		if strings.Contains(out, avoid) {
			t.Errorf("unexpected %q on an optional field with no help or error:\n%s", avoid, out)
		}
	}
}

// ----------------------------------------------------------------------
// Tone and severity normalisation
// ----------------------------------------------------------------------

func TestToneAndSeverityVocabulariesAreClosed(t *testing.T) {
	toneCases := map[string]string{
		toneInfo: toneInfo, toneSuccess: toneSuccess, toneWarning: toneWarning,
		toneDanger: toneDanger, toneNeutral: toneNeutral,
		"": toneNeutral, "PENDING": toneNeutral, "danger ": toneNeutral,
	}
	for in, want := range toneCases {
		if got := toneOf(in); got != want {
			t.Errorf("toneOf(%q) = %q, want %q", in, got, want)
		}
	}
	severityCases := map[string]string{
		severityBlocking: severityBlocking, severityWarning: severityWarning, severityNeedsData: severityNeedsData,
		severitySuccess: severitySuccess, severityInfo: severityInfo,
		"": severityInfo, "FATAL": severityInfo,
	}
	for in, want := range severityCases {
		if got := severityOf(in); got != want {
			t.Errorf("severityOf(%q) = %q, want %q", in, got, want)
		}
	}
	stepCases := map[string]string{
		stepDone: stepDone, stepActive: stepActive, stepFailed: stepFailed,
		stepUpcoming: stepUpcoming, "": stepUpcoming, "skipped": stepUpcoming,
	}
	for in, want := range stepCases {
		if got := stepStateOf(in); got != want {
			t.Errorf("stepStateOf(%q) = %q, want %q", in, got, want)
		}
	}
	toneForSeverity := map[string]string{
		severityBlocking: toneDanger, severityWarning: toneWarning, severityNeedsData: toneWarning,
		severitySuccess: toneSuccess, severityInfo: toneInfo, "unknown": toneInfo,
	}
	for in, want := range toneForSeverity {
		if got := severityTone(in); got != want {
			t.Errorf("severityTone(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNothingMeansAnythingByColorAlone is WCAG 1.4.1 as an assertion: every
// toned surface is accompanied by a word, either its own visible label or a
// visually-hidden state name.
func TestNothingMeansAnythingByColorAlone(t *testing.T) {
	p := SampleDetailPage()
	out := mustRender(t, p)

	for _, s := range p.Detail.Steps {
		want := s.Label + `<span class="jn-visually-hidden"> — ` + stepStateWord(stepStateOf(s.State))
		if !strings.Contains(out, want) {
			t.Errorf("step %q does not spell out its state; looked for %q", s.ID, want)
		}
	}
	for _, f := range p.Detail.Findings {
		want := `<span class="jn-checkpill-label">` + severityWord(severityOf(f.Severity)) + `</span>`
		if !strings.Contains(out, want) {
			t.Errorf("finding %q does not spell out its severity in its pill", f.Code)
		}
	}
	for _, r := range p.Detail.Comparison {
		if !r.Changed {
			continue
		}
		if !strings.Contains(out, ">Changed</span>") {
			t.Errorf("changed row %q is marked only by its tint", r.Label)
		}
	}
	if !strings.Contains(out, ">No change</span>") {
		t.Error("unchanged comparison rows say nothing at all in the change column")
	}
}

func TestNeedsDataFindingIsNotPresentedAsHarmlessInformation(t *testing.T) {
	p := SampleDetailPage()
	p.Detail.Findings = []Finding{{Severity: severityNeedsData, Code: "pay_band.missing", Message: "A pay band is required."}}
	out := mustRender(t, p)
	if !strings.Contains(out, `<span class="jn-checkpill-label">Needs information</span>`) {
		t.Fatal("missing governed pay-band data was not named as a prerequisite")
	}
	if !strings.Contains(out, "A pay band is required.") {
		t.Fatal("missing-data finding lost its explanation")
	}
}

// TestChangedRowWithNoDeltaStillSaysChanged: a job code changes without any
// arithmetic to report. Saying "No change" there would be flatly wrong.
func TestChangedRowWithNoDeltaStillSaysChanged(t *testing.T) {
	out := renderNode(t, comparisonRow(ComparisonRow{
		Label: "Job code", Current: "A", Proposed: "B", Changed: true,
	}))
	if !strings.Contains(out, ">Changed</span>") {
		t.Errorf("changed row does not say so: %s", out)
	}
	if strings.Contains(out, "No change") {
		t.Errorf("changed row claims no change: %s", out)
	}
}

// ----------------------------------------------------------------------
// The preflight instruments
// ----------------------------------------------------------------------

// TestPreflightBoardStaggersWithoutInlineStyles: the row animation is
// delayed by position, and the only way to express that under a CSP that
// forbids style attributes is a data attribute the stylesheet keys off.
func TestPreflightBoardStaggersWithoutInlineStyles(t *testing.T) {
	findings := make([]Finding, 12)
	for i := range findings {
		findings[i] = Finding{Severity: severityInfo, Code: "c", Message: "m"}
	}
	out := renderNode(t, preflightSection(DetailView{Findings: findings}))
	for i := 0; i <= 7; i++ {
		if !strings.Contains(out, `data-row="`+string(rune('0'+i))+`"`) {
			t.Errorf("no row carries data-row=%d", i)
		}
	}
	if strings.Contains(out, `data-row="8"`) {
		t.Error("the stagger is not clamped; the stylesheet only declares delays up to row 7")
	}
	if strings.Contains(out, "style=") {
		t.Error("the board used an inline style attribute, which the content-security-policy blocks")
	}
}

// TestPayBandGaugeIsGeometryNotStyle: the marker positions are data, so
// they have to be SVG presentation attributes. This asserts both that they
// are, and that the drawing is labelled rather than decorative.
func TestPayBandGaugeIsGeometryNotStyle(t *testing.T) {
	out := renderNode(t, payBandGauge(&PayBand{
		Min: "USD 84,000.00", Mid: "USD 94,000.00", Max: "USD 108,000.00",
		Current: "USD 93,000.00", Proposed: "USD 98,000.00",
		CurrentPct: 37.5, ProposedPct: 58.3,
		Note: "Above the midpoint.",
	}))
	for _, want := range []string{
		`role="img"`, `aria-label="Pay band for the target grade: current base USD 93,000.00`,
		`class="jn-band-track"`, `class="jn-band-fill"`,
		`data-which="current"`, `data-which="proposed"`,
		`Min USD 84,000.00`, `Mid USD 94,000.00`, `Max USD 108,000.00`,
		`Above the midpoint.`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "style=") {
		t.Error("the gauge positioned something with an inline style attribute")
	}
	// 37.5% of the 288-unit span plus the 6-unit pad: 114.00.
	if !strings.Contains(out, `x1="114.00"`) {
		t.Errorf("the current marker is not at the computed position:\n%s", out)
	}
}

func TestGaugePercentagesAreClampedNotDrawnOffTheTrack(t *testing.T) {
	out := renderNode(t, payBandGauge(&PayBand{CurrentPct: -40, ProposedPct: 180}))
	if !strings.Contains(out, `x1="6.00"`) {
		t.Errorf("a negative percentage was not clamped to the left end:\n%s", out)
	}
	if !strings.Contains(out, `x1="294.00"`) {
		t.Errorf("a percentage above 100 was not clamped to the right end:\n%s", out)
	}
	for _, in := range []float64{-1, 0, 50, 100, 101, 1e9} {
		if got := clampPct(in); got < 0 || got > 100 {
			t.Errorf("clampPct(%v) = %v, outside [0,100]", in, got)
		}
	}
}

// TestBudgetMeterTonesFollowTheNumber: the meter's color is derived, not
// supplied, so this pins the thresholds rather than trusting the fixture.
func TestBudgetMeterTonesFollowTheNumber(t *testing.T) {
	cases := []struct {
		pct  float64
		tone string
	}{
		{0, toneSuccess}, {79.9, toneSuccess}, {80, toneWarning}, {94.9, toneWarning},
		{95, toneDanger}, {100, toneDanger}, {140, toneDanger},
	}
	for _, tc := range cases {
		out := renderNode(t, budgetMeter(&Budget{UsedPct: tc.pct}))
		if !strings.Contains(out, `data-tone="`+tc.tone+`"`) {
			t.Errorf("UsedPct %.1f did not render the %q tone:\n%s", tc.pct, tc.tone, out)
		}
		if !strings.Contains(out, `role="img"`) {
			t.Error("the meter is not labelled for assistive technology")
		}
	}
	// The fill width is the percentage of the 300-unit track.
	out := renderNode(t, budgetMeter(&Budget{UsedPct: 50}))
	if !strings.Contains(out, `width="150.00"`) {
		t.Errorf("the fill is not half the track:\n%s", out)
	}
}

func TestEffectiveWindowNamesTheAsKnownAtInstant(t *testing.T) {
	out := renderNode(t, effectiveWindow(&EffectiveWindow{
		Start: "1 Apr 2026", EffectiveDate: "1 Jun 2026", KnownAt: "12 May 2026, 09:12 UTC",
		Note: "As known at that instant.",
	}))
	for _, want := range []string{"Cycle opens", "1 Apr 2026", "Takes effect", "1 Jun 2026",
		"Information current as of", "12 May 2026, 09:12 UTC", `data-which="effective"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestPreflightSectionIsAbsentRatherThanEmpty: a simulation that produced
// nothing should leave no heading behind for the reader to wonder about.
func TestPreflightSectionIsAbsentRatherThanEmpty(t *testing.T) {
	if node := preflightSection(DetailView{}); node != nil {
		t.Error("the preflight section rendered with nothing to show")
	}
	if node := payBandGauge(nil); node != nil {
		t.Error("payBandGauge(nil) rendered something")
	}
	if node := budgetMeter(nil); node != nil {
		t.Error("budgetMeter(nil) rendered something")
	}
	if node := effectiveWindow(nil); node != nil {
		t.Error("effectiveWindow(nil) rendered something")
	}
}

// ----------------------------------------------------------------------
// The live region and the notice
// ----------------------------------------------------------------------

// TestLiveRegionIsPresentEvenWithNoNotice: a role="status" element inserted
// at the same moment as its text is not reliably announced, and on the live
// client the notice appears and disappears as RPCs answer.
func TestLiveRegionIsPresentEvenWithNoNotice(t *testing.T) {
	p := SampleListPage()
	p.Notice = nil
	out := mustRender(t, p)
	if !strings.Contains(out, `<div aria-live="polite" class="jn-live" role="status"></div>`) {
		t.Errorf("the empty live region is missing or not empty:\n%s", firstN(out, 1200))
	}
}

func TestNoticeRendersInsideTheLiveRegionWithItsTone(t *testing.T) {
	for _, tone := range []string{toneInfo, toneSuccess, toneWarning, toneDanger} {
		t.Run(tone, func(t *testing.T) {
			p := SampleListPage()
			p.Notice = &Notice{Tone: tone, Title: "A title", Detail: "A detail."}
			out := mustRender(t, p)
			if !strings.Contains(out, `role="status"`) || !strings.Contains(out, `aria-live="polite"`) {
				t.Error("the notice is not inside a live region")
			}
			if !strings.Contains(out, `data-tone="`+tone+`"`) {
				t.Error("the notice lost its tone")
			}
			if !strings.Contains(out, severityLabelForTone(tone)+": ") {
				t.Error("the notice does not name its severity in words")
			}
			if !strings.Contains(out, "A title") || !strings.Contains(out, "A detail.") {
				t.Error("the notice lost its text")
			}
		})
	}
}

// ----------------------------------------------------------------------
// Escaping
// ----------------------------------------------------------------------

// hostileString is placed in every value the page renders. If any of it
// reaches the document unescaped, the page is an injection sink -- and on
// the live client, where these values arrive over a gRPC stream from the
// engine, that is the whole attack surface.
const hostileString = `<script>alert("x")</script> & <img src=x onerror=alert(1)> "quoted" 'single'`

func TestUserControlledStringsAreEscapedEverywhere(t *testing.T) {
	p := hostilePage()
	doc, err := Document(p)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}

	assertNothingExecutable(t, doc)
	if !strings.Contains(doc, "&lt;script&gt;") {
		t.Error("the hostile string was dropped rather than escaped; nothing was actually tested")
	}
	if !strings.Contains(doc, "&amp;") {
		t.Error("an ampersand was not escaped")
	}
	// Attribute positions: an unescaped quote would break out of the value.
	for _, attr := range regexp.MustCompile(`(href|action|value|id|aria-label)="([^"]*)"`).FindAllStringSubmatch(doc, -1) {
		if strings.Contains(attr[2], `"`) {
			t.Errorf("attribute %s escaped its own quoting: %q", attr[1], attr[2])
		}
	}
}

func TestTitleIsEscapedInTheHead(t *testing.T) {
	p := SampleListPage()
	p.Title = `Fish & Chips <script>alert(1)</script>`
	doc, err := Document(p)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	want := "<title>Fish &amp; Chips &lt;script&gt;alert(1)&lt;/script&gt;</title>"
	if !strings.Contains(doc, want) {
		t.Errorf("title is not escaped; want %q", want)
	}
}

// hostilePage puts hostileString into every string the detail view renders
// from, so one assertion pass covers every sink on that view.
func hostilePage() Page {
	h := hostileString
	return Page{
		Title:       h,
		Brand:       h,
		TenantLabel: h,
		Principal:   Principal{Subject: h, Roles: []string{h}, Purpose: h, LogoutHref: "/out?a=1&b=" + h},
		Nav:         []NavLink{{Label: h, Href: "/nav?q=" + h, Current: true}},
		Notice:      &Notice{Tone: h, Title: h, Detail: h},
		Detail: &DetailView{
			Journey: JourneyCard{IntentID: h, Href: "/j?id=" + h, WorkerName: h, WorkerRef: h,
				Headline: h, PayLine: h, EffectiveDate: h, Stage: h, StageLabel: h, StageTone: h,
				Updated: h, InstanceID: h},
			Steps:      []Step{{ID: h, Label: h, Detail: h, State: h, At: h}},
			Proposal:   []Fact{{Label: h, Value: h, Mono: true, Tone: h}},
			Comparison: []ComparisonRow{{Label: h, Current: h, Proposed: h, Delta: h, Changed: true}},
			Findings:   []Finding{{Severity: h, Code: h, Message: h}},
			Engine:     []Fact{{Label: h, Value: h}},
			Nodes:      []NodeRow{{NodeID: h, StepType: h, Status: h, Attempt: h, Started: h, Completed: h, Tone: h}},
			WorkItems:  []WorkItemCard{{ID: h, Kind: h, Status: h, Owner: h, NodeID: h, Deadline: h, Claimed: h, Completed: h, Tone: h}},
			Ledger: &LedgerCard{StreamKey: h, Sequence: h, SchemaRef: h, Digest: h,
				IdempotencyKey: h, RecordedAt: h, EffectiveAt: h},
			Evidence: []string{h},
			Timeline: []TimelineEvent{{At: h, Actor: h, Title: h, Detail: h, Ref: h, Tone: h}},
			Actions: []Action{{
				ID: "a", Label: h, Variant: h, Description: h, Action: "/act?x=" + h,
				Hidden: map[string]string{"csrf_token": h, "q&a": h},
				Fields: []Field{{ID: "hf", Name: h, Label: h, Kind: fieldKindTextarea, Value: h,
					Placeholder: h, Help: h, Error: h, Required: true}},
				ActsAs: h, DisabledReason: h,
			}},
			// The gauges take hostile strings straight into an SVG
			// aria-label, which is the one attribute on this page assembled
			// from several engine-supplied values at once.
			PayBand: &PayBand{Min: h, Mid: h, Max: h, Current: h, Proposed: h,
				Currency: h, CurrentPct: 30, ProposedPct: 70, Note: h},
			Budget:          &Budget{Available: h, Committed: h, Requested: h, UsedPct: 50, Note: h},
			EffectiveWindow: &EffectiveWindow{Start: h, EffectiveDate: h, KnownAt: h, Note: h},
		},
		Footer: Footer{PolicyVersion: h, CellID: h, BuildRef: h, Lines: []string{h}},
	}
}

// TestHostileListPageIsAlsoEscaped covers the list view's own sinks, which
// hostilePage cannot reach because a Page shows one view at a time.
func TestHostileListPageIsAlsoEscaped(t *testing.T) {
	h := hostileString
	p := hostilePage()
	p.Detail = nil
	p.List = &ListView{
		Journeys: []JourneyCard{{IntentID: h, Href: "/j?id=" + h, WorkerName: h, WorkerRef: h,
			Headline: h, PayLine: h, EffectiveDate: h, StageLabel: h, StageTone: h, Updated: h, InstanceID: h}},
		Empty: h,
		Form: ProposalForm{
			Action: "/propose?x=" + h,
			Hidden: map[string]string{"csrf_token": h},
			Submit: h,
			Fields: []Field{
				{ID: "a", Name: h, Label: h, Kind: fieldKindText, Value: h, Placeholder: h, Help: h, Error: h},
				{ID: "b", Name: h, Label: h, Kind: fieldKindSelect, Options: []Option{{Value: h, Label: h, Selected: true}}},
			},
			Disabled: true, DisabledReason: h,
		},
		EngineAvailable: false,
		EngineNotice:    h,
		People: &PeopleView{
			Workers: []WorkerCard{{
				Ref: h, Name: h, Number: h, Title: h, JobCode: h, Grade: h,
				OrgUnit: h, Location: h, PayLine: h, HireDate: h,
				Source: sourceCreated, SourceLabel: h, Tone: h, Selected: true,
				OpenJourneys: 3, ProposeHref: "/new?worker=" + h,
			}},
			Empty:       h,
			SelectedRef: h,
			Note:        h,
			Form: WorkerForm{
				Action: "/people?x=" + h,
				Hidden: map[string]string{"csrf_token": h},
				Submit: h,
				Fields: []Field{
					{ID: "wa", Name: h, Label: h, Kind: fieldKindText, Value: h, Help: h, Error: h},
					{ID: "wb", Name: h, Label: h, Kind: fieldKindNumber, Value: h, Prefix: h, Suffix: h},
				},
			},
		},
	}
	doc, err := Document(p)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	assertNothingExecutable(t, doc)
	if !strings.Contains(doc, "&lt;script&gt;") {
		t.Error("the hostile string was dropped rather than escaped")
	}
}

// quotedAttributeValue matches a double-quoted attribute value, and
// inlineHandlerPattern an event-handler attribute NAME on a real tag.
//
// The two go together. The hostile fixture puts `+"`"+`onerror=alert(1)`+"`"+` inside
// values that legitimately end up in an href, where it is escaped, inert
// prose. Scanning the raw document for the substring would fail on
// correctly escaped text -- strict-looking and meaningless -- so the values
// are emptied first and only attribute names are examined.
var (
	quotedAttributeValue = regexp.MustCompile(`="[^"]*"`)
	inlineHandlerPattern = regexp.MustCompile(`(?i)<[a-z][^>]*\son[a-z]+\s*=`)
)

// assertNothingExecutable is the whole escaping contract in one place: no
// real tag that the content-security-policy would have to block, and no
// inline handler on any tag that survived.
func assertNothingExecutable(t *testing.T, doc string) {
	t.Helper()
	skeleton := quotedAttributeValue.ReplaceAllString(doc, `=""`)
	for _, tag := range []string{"<script", "<img", "<iframe", "<object", "<embed"} {
		if strings.Contains(strings.ToLower(skeleton), tag) {
			t.Errorf("%q reached the document as a real tag", tag)
		}
	}
	if m := inlineHandlerPattern.FindString(skeleton); m != "" {
		t.Errorf("an inline event handler reached the document: %q", m)
	}
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
