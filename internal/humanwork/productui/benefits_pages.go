package productui

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/benefits"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func benefitsText(view View, key string) string { return view.Locale.Text("benefits_ui." + key) }

func benefitsUnavailable(view View, prefix string) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title: view.Locale.Text(prefix + ".unavailable_title"), Description: view.Locale.Text(prefix + ".unavailable_detail"), Role: "status",
		Action: &ActionLinkProps{Label: view.Locale.Text(prefix + ".return_home"), Href: statefulHref(view, PageHome), Class: "button primary", Navigate: view.Navigate},
	})
}

func benefitsStateFallback(view View, prefix, state string) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title: view.Locale.Text(prefix + "." + state + "_title"), Description: view.Locale.Text(prefix + "." + state + "_detail"), Role: "status",
		Action: &ActionLinkProps{Label: view.Locale.Text(prefix + ".return_home"), Href: statefulHref(view, PageHome), Class: "button primary", Navigate: view.Navigate},
	})
}

func benefitEligibilityLabel(view View, status benefits.EligibilityStatus) string {
	switch status {
	case benefits.StatusEligible:
		return benefitsText(view, "eligibility_eligible")
	case benefits.StatusConditional:
		return benefitsText(view, "eligibility_conditional")
	case benefits.StatusIneligible:
		return benefitsText(view, "eligibility_ineligible")
	case benefits.StatusUnknown:
		return benefitsText(view, "eligibility_unknown")
	default:
		return string(status)
	}
}

func benefitWindowLabel(view View, status benefits.WindowStatus) string {
	switch status {
	case benefits.WindowOpen:
		return benefitsText(view, "window_open")
	case benefits.WindowClosed:
		return benefitsText(view, "window_closed")
	case benefits.WindowConditional:
		return benefitsText(view, "window_conditional")
	case benefits.WindowUnknown:
		return benefitsText(view, "window_unknown")
	default:
		return string(status)
	}
}

func benefitOutcomeLabel(view View, outcome benefits.ReconOutcome) string {
	switch outcome {
	case benefits.ReconMatch:
		return benefitsText(view, "outcome_match")
	case benefits.ReconMismatch:
		return benefitsText(view, "outcome_mismatch")
	case benefits.ReconPartial:
		return benefitsText(view, "outcome_partial")
	case benefits.ReconStale:
		return benefitsText(view, "outcome_stale")
	case benefits.ReconUnknown:
		return benefitsText(view, "outcome_unknown")
	default:
		return string(outcome)
	}
}

func benefitTierLabel(view View, tier benefits.TierCode) string {
	switch tier {
	case benefits.TierEmployeeOnly:
		return benefitsText(view, "tier_employee_only")
	case benefits.TierEmployeeSpouse:
		return benefitsText(view, "tier_employee_spouse")
	case benefits.TierEmployeeChildren:
		return benefitsText(view, "tier_employee_children")
	case benefits.TierFamily:
		return benefitsText(view, "tier_family")
	default:
		return string(tier)
	}
}

// benefitLocalDate renders a domain calendar date in the viewer's locale. An
// unset date is an explicit unknown fact and renders as nothing.
func benefitLocalDate(view View, date values.LocalDate) string {
	if date.Validate() != nil || !date.IsSet() {
		return ""
	}
	return view.Locale.FormatDate(time.Date(int(date.Year()), date.Month(), int(date.Day()), 0, 0, 0, 0, time.UTC))
}

// benefitWindowRange renders the resolved enrollment window as a localized
// date range. A window without published dates renders only its status.
func benefitWindowRange(view View, window benefits.EnrollmentWindowResolution) string {
	start, end := benefitLocalDate(view, window.Start), benefitLocalDate(view, window.End)
	switch {
	case start != "" && end != "":
		return start + " – " + end
	case start != "":
		return start
	case end != "":
		return end
	default:
		return ""
	}
}

func benefitFactRow(label, value string) ui.Node {
	return html.Div(html.Props{Class: "benefit-fact"}, html.Tag("dt", html.Props{}, ui.Text(label)), html.Tag("dd", html.Props{}, ui.Text(value)))
}

func benefitStringList(items []string) ui.Node {
	if len(items) == 0 {
		return nil
	}
	entries := make([]ui.Node, 0, len(items))
	for _, item := range items {
		entries = append(entries, html.Li(html.Props{}, ui.Text(item)))
	}
	return html.Ul(html.Props{}, entries...)
}

func benefitOptionCard(view View, option BenefitOptionView) ui.Node {
	window := benefitWindowLabel(view, option.Window.Status)
	if span := benefitWindowRange(view, option.Window); span != "" {
		window += " · " + span
	}
	electable := benefitsText(view, "electable_no")
	if option.CanElect {
		electable = benefitsText(view, "electable_yes")
	}
	body := []ui.Node{
		html.H3(html.Props{}, ui.Text(option.PlanName)),
		html.Tag("dl", html.Props{Class: "benefit-facts"},
			benefitFactRow(benefitsText(view, "year_label"), strconv.Itoa(int(option.PlanYear))),
			benefitFactRow(benefitsText(view, "eligibility_label"), benefitEligibilityLabel(view, option.Eligibility.Status)),
			benefitFactRow(benefitsText(view, "window_label"), window),
			benefitFactRow(benefitsText(view, "electable_label"), electable),
		),
		html.H4(html.Props{}, ui.Text(benefitsText(view, "tiers_label"))),
		benefitStringList(option.CoverageTiers),
		html.H4(html.Props{}, ui.Text(benefitsText(view, "options_label"))),
		benefitStringList(option.Options),
	}
	if option.CanElect {
		body = append(body, ui.CreateElement(ActionLink, ActionLinkProps{
			Label: benefitsText(view, "enroll_link"), Href: statefulHref(view, PageBenefitsEnroll),
			Class: "button secondary", Navigate: view.Navigate,
		}))
	}
	return html.Article(html.Props{Class: "benefit-option"}, body...)
}

// benefitElectableOptions keeps only the options the domain resolved as
// eligible inside an open window. The enroll form offers nothing else.
func benefitElectableOptions(options []BenefitOptionView) []BenefitOptionView {
	electable := make([]BenefitOptionView, 0, len(options))
	for _, option := range options {
		if option.CanElect {
			electable = append(electable, option)
		}
	}
	return electable
}

// benefitElectionTiers is the closed BEN-005 tier vocabulary the enroll form
// offers. The domain, not the worker, decides which tier a plan accepts.
func benefitElectionTiers() []benefits.TierCode {
	return []benefits.TierCode{benefits.TierEmployeeOnly, benefits.TierEmployeeSpouse, benefits.TierEmployeeChildren, benefits.TierFamily}
}

// benefitsOverviewContent renders one card per resolved plan option.
func benefitsOverviewContent(view View, options []BenefitOptionView) ui.Node {
	cards := make([]ui.Node, 0, len(options))
	for _, option := range options {
		cards = append(cards, benefitOptionCard(view, option))
	}
	return html.Section(html.Props{Class: "surface benefits-overview", Raw: map[string]any{"aria-labelledby": "benefits-overview-title"}},
		html.H2(html.Props{ID: "benefits-overview-title"}, ui.Text(benefitsText(view, "overview_title"))),
		html.Div(html.Props{Class: "benefit-options"}, cards...),
	)
}

// benefitsCompareContent renders every resolved option side by side: one
// column per plan, one row per attribute.
func benefitsCompareContent(view View, options []BenefitOptionView) ui.Node {
	header := []ui.Node{html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(benefitsText(view, "plan_label")))}
	for _, option := range options {
		header = append(header, html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(option.PlanName)))
	}
	body := []ui.Node{
		benefitCompareRow(view, "year_label", options, func(option BenefitOptionView) string { return strconv.Itoa(int(option.PlanYear)) }),
		benefitCompareRow(view, "tiers_label", options, func(option BenefitOptionView) string { return strings.Join(option.CoverageTiers, ", ") }),
		benefitCompareRow(view, "options_label", options, func(option BenefitOptionView) string { return strings.Join(option.Options, ", ") }),
		benefitCompareRow(view, "eligibility_label", options, func(option BenefitOptionView) string { return benefitEligibilityLabel(view, option.Eligibility.Status) }),
		benefitCompareRow(view, "window_label", options, func(option BenefitOptionView) string {
			window := benefitWindowLabel(view, option.Window.Status)
			if span := benefitWindowRange(view, option.Window); span != "" {
				window += " · " + span
			}
			return window
		}),
		benefitCompareRow(view, "electable_label", options, func(option BenefitOptionView) string {
			if option.CanElect {
				return benefitsText(view, "electable_yes")
			}
			return benefitsText(view, "electable_no")
		}),
	}
	return html.Section(html.Props{Class: "surface benefits-compare", Raw: map[string]any{"aria-labelledby": "benefits-compare-title"}},
		html.H2(html.Props{ID: "benefits-compare-title"}, ui.Text(benefitsText(view, "compare_title"))),
		html.Table(html.Props{}, html.Thead(html.Props{}, html.Tr(html.Props{}, header...)), html.Tbody(html.Props{}, body...)),
	)
}

func benefitCompareRow(view View, label string, options []BenefitOptionView, project func(BenefitOptionView) string) ui.Node {
	cells := []ui.Node{html.Th(html.Props{Raw: map[string]any{"scope": "row"}}, ui.Text(benefitsText(view, label)))}
	for _, option := range options {
		cells = append(cells, html.Td(html.Props{}, ui.Text(project(option))))
	}
	return html.Tr(html.Props{}, cells...)
}

// benefitsReconContent renders one BEN-008 outcome record per election.
func benefitsReconContent(view View, records []BenefitReconView) ui.Node {
	items := make([]ui.Node, 0, len(records))
	for _, record := range records {
		items = append(items, benefitReconRecord(view, record))
	}
	return html.Section(html.Props{Class: "surface pay-benefit-recon", Raw: map[string]any{"aria-labelledby": "pay-benefit-recon-title"}},
		html.H2(html.Props{ID: "pay-benefit-recon-title"}, ui.Text(benefitsText(view, "recon_title"))),
		html.Ul(html.Props{Class: "recon-records"}, items...),
	)
}

func benefitReconRecord(view View, record BenefitReconView) ui.Node {
	facts := []ui.Node{benefitFactRow(benefitsText(view, "outcome_label"), benefitOutcomeLabel(view, record.Outcome))}
	if len(record.Fields) > 0 {
		facts = append(facts, html.Div(html.Props{Class: "benefit-fact"},
			html.Tag("dt", html.Props{}, ui.Text(benefitsText(view, "fields_label"))),
			html.Tag("dd", html.Props{}, benefitStringList(record.Fields)),
		))
	}
	if record.Repair != nil {
		repair := []ui.Node{benefitFactRow(benefitsText(view, "repair_label"), record.Repair.Scope)}
		if len(record.Repair.Fields) > 0 {
			repair = append(repair, html.Div(html.Props{Class: "benefit-fact"},
				html.Tag("dt", html.Props{}, ui.Text(benefitsText(view, "fields_label"))),
				html.Tag("dd", html.Props{}, benefitStringList(record.Repair.Fields)),
			))
		}
		facts = append(facts, html.Tag("dl", html.Props{Class: "benefit-repair"}, repair...))
	}
	return html.Li(html.Props{Class: "recon-record"}, html.Tag("dl", html.Props{Class: "benefit-facts"}, facts...))
}

// benefitEnrollForm offers the electable plans through the server-composed
// election adapter. The submitted choice is validated against the same
// domain resolution the form was rendered from.
func benefitEnrollForm(view View, projection BenefitsProjection, options, electable []BenefitOptionView) ui.Node {
	planRef := ui.UseState("")
	planRevision := ui.UseState("")
	windowID := ui.UseState("")
	tier := ui.UseState("")
	waive := ui.UseState(false)
	evidence := ui.UseState("")
	busy := ui.UseState(false)
	submitted := ui.UseState(false)
	failed := ui.UseState(false)
	planProps := html.Props{ID: "benefits-enroll-plan", Name: "plan", Required: true}
	planProps.OnChange = ui.UseEvent(func(event ui.InputEvent) {
		selected := event.GetValue()
		for _, option := range electable {
			if selected == option.PlanRef+"\x00"+option.PlanRevision+"\x00"+option.Window.WindowID {
				planRef.Set(option.PlanRef)
				planRevision.Set(option.PlanRevision)
				windowID.Set(option.Window.WindowID)
				return
			}
		}
		planRef.Set("")
		planRevision.Set("")
		windowID.Set("")
	})
	tierProps := html.Props{ID: "benefits-enroll-tier", Name: "tier", Required: true}
	tierProps.OnChange = ui.UseEvent(func(event ui.InputEvent) { tier.Set(event.GetValue()) })
	choiceProps := html.Props{ID: "benefits-enroll-choice", Name: "choice", Required: true}
	choiceProps.OnChange = ui.UseEvent(func(event ui.InputEvent) { waive.Set(event.GetValue() == "waive") })
	evidenceProps := html.Props{ID: "benefits-enroll-evidence", Name: "waiver_evidence", Rows: 3, MaxLength: 2000, Raw: map[string]any{"aria-describedby": "benefits-enroll-evidence-help"}}
	evidenceProps.OnInput = ui.UseEvent(func(event ui.InputEvent) { evidence.Set(event.GetValue()) })
	status := ui.Node(nil)
	if failed.Get() {
		status = html.P(html.Props{ID: "benefits-enroll-status", Raw: map[string]any{"role": "alert"}}, ui.Text(benefitsText(view, "submit_failed")))
	} else if busy.Get() {
		status = html.P(html.Props{ID: "benefits-enroll-status", Raw: map[string]any{"role": "status"}}, ui.Text(benefitsText(view, "submitting")))
	} else if submitted.Get() {
		status = html.P(html.Props{ID: "benefits-enroll-status", Raw: map[string]any{"role": "status"}}, ui.Text(benefitsText(view, "submitted")))
	}
	planOptions := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}}, ui.Text(benefitsText(view, "choose_plan")))}
	for _, option := range electable {
		planOptions = append(planOptions, html.Option(
			html.Props{Value: option.PlanRef + "\x00" + option.PlanRevision + "\x00" + option.Window.WindowID},
			ui.Text(option.PlanName),
		))
	}
	tierOptions := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}}, ui.Text(benefitsText(view, "choose_tier")))}
	for _, code := range benefitElectionTiers() {
		tierOptions = append(tierOptions, html.Option(html.Props{Value: string(code)}, ui.Text(benefitTierLabel(view, code))))
	}
	submit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		if busy.Get() {
			return
		}
		busy.Set(true)
		failed.Set(false)
		submitted.Set(false)
		request := BenefitElectionRequest{
			PlanRef: planRef.Get(), PlanRevision: planRevision.Get(), WindowID: windowID.Get(),
			Tier: benefits.TierCode(tier.Get()), Waiver: waive.Get(), WaiverEvidence: strings.TrimSpace(evidence.Get()),
		}
		if !submitBenefitElection(projection, options, request, func(err error) {
			busy.Set(false)
			if err != nil {
				failed.Set(true)
				return
			}
			submitted.Set(true)
		}) {
			busy.Set(false)
			return
		}
	})
	return html.Section(html.Props{Class: "surface benefits-enroll", Raw: map[string]any{"aria-labelledby": "benefits-enroll-form-title"}},
		html.H2(html.Props{ID: "benefits-enroll-form-title"}, ui.Text(benefitsText(view, "enroll_title"))),
		html.P(html.Props{ID: "benefits-enroll-help", Class: "muted"}, ui.Text(benefitsText(view, "enroll_detail"))),
		html.Form(html.Props{Class: "benefits-enroll-form", OnSubmit: submit},
			html.Label(html.Props{For: planProps.ID}, ui.Text(benefitsText(view, "plan_label"))), html.Select(planProps, planOptions...),
			html.Label(html.Props{For: tierProps.ID}, ui.Text(benefitsText(view, "tier_label"))), html.Select(tierProps, tierOptions...),
			html.Label(html.Props{For: choiceProps.ID}, ui.Text(benefitsText(view, "choice_label"))),
			html.Select(choiceProps,
				html.Option(html.Props{Value: "enroll"}, ui.Text(benefitsText(view, "choice_enroll"))),
				html.Option(html.Props{Value: "waive"}, ui.Text(benefitsText(view, "choice_waive"))),
			),
			html.Label(html.Props{For: evidenceProps.ID}, ui.Text(benefitsText(view, "waiver_evidence_label"))),
			html.P(html.Props{ID: "benefits-enroll-evidence-help", Class: "muted"}, ui.Text(benefitsText(view, "waiver_evidence_help"))),
			html.Textarea(evidenceProps),
			status,
			html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: busy.Get()}, ui.Text(benefitsText(view, "submit_label"))),
		),
	)
}
