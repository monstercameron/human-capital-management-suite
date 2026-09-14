package productui

import (
	"fmt"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func TestTodo_UXAUDIT_006_MergedUX(t *testing.T) {
	forbidden := []string{
		"journeyservice", "canonical grpc", "authenticated cell",
		"server-enforced boundary", "tenant appearance", "credential role fallback",
		"worker projection", "governed service", "server authority",
		"this cell", "live cell", "live workspace", "execution authority gate",
		"requisition service unavailable", "position service unavailable", "organization allowlist",
		"journey-dienst", "diese zelle", "geregelten dienst",
		"gowebcomponents", "live-quelle", "authorized workflow projection", "employee records", "governed",
		"خدمة الرحلات", "الخلية",
	}
	var issues []string
	for _, locale := range SupportedProductLocales() {
		for _, definition := range PageDefinitions() {
			for _, mode := range []string{"ready", "empty", "loading", "error"} {
				view := testView(definition.ID)
				view.Locale = ResolveProductLocale(locale)
				switch mode {
				case "empty":
					view.Work = nil
					view.People = nil
					view.PersonWorkflows = nil
					view.LauncherActions = nil
					view.SelectedWork = ""
					view.SelectedPerson = ""
				case "loading":
					view.Loading = true
				case "error":
					view.LoadError = "rpc error: JourneyService unavailable in authenticated cell"
				}
				doc, err := Render(view)
				if err != nil {
					t.Fatalf("render %s in %s/%s: %v", definition.ID, locale, mode, err)
				}
				visible := strings.ToLower(ordinaryVisibleText(t, doc))
				if mode == "error" && !strings.Contains(visible, strings.ToLower(view.Locale.Text("shell.load_recovery"))) {
					issues = append(issues, fmt.Sprintf("%s/%s/%s: missing recovery step", locale, definition.ID, mode))
				}
				for _, phrase := range forbidden {
					if strings.Contains(visible, phrase) {
						issues = append(issues, fmt.Sprintf("%s/%s/%s: %s", locale, definition.ID, mode, phrase))
					}
				}
			}
		}
	}
	if len(issues) > 0 {
		limit := min(len(issues), 35)
		t.Fatalf("%d ordinary page copy violations:\n%s", len(issues), strings.Join(issues[:limit], "\n"))
	}
}

func TestTodo_UXAUDIT_006_Golden_MergedUX(t *testing.T) {
	var actual strings.Builder
	for _, language := range SupportedProductLocales() {
		locale := ResolveProductLocale(language)
		fmt.Fprintf(&actual, "%s|%s|%s|%s\n", language,
			locale.Text("admin.roles_action"), locale.Text("shell.page_unavailable"), locale.Text("journey.startup_title"))
	}
	const want = "en-US|Manage roles|Page unavailable|This page could not start\n" +
		"de-DE|Rollen verwalten|Seite nicht verfügbar|Diese Seite konnte nicht gestartet werden\n" +
		"ar|إدارة الأدوار|الصفحة غير متاحة|تعذر بدء هذه الصفحة\n"
	if actual.String() != want {
		t.Fatalf("reviewed action and recovery copy changed:\n%s", actual.String())
	}
}

func TestTodo_UXAUDIT_006_Browser_MergedUX(t *testing.T) {
	for _, language := range SupportedProductLocales() {
		view := ApplyRoleVisibility(testView(PageAdmin), []string{RoleHCMAdmin})
		view = ApplyLocale(view, ResolveProductLocale(language))
		view.LoadError = "rpc error: JourneyService unavailable in authenticated cell"
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(doc, `lang="`+language+`"`) || !strings.Contains(doc, `dir="`+string(view.Locale.Direction)+`"`) {
			t.Errorf("%s document lost language or writing direction", language)
		}
		visible := ordinaryVisibleText(t, doc)
		for _, want := range []string{
			view.Locale.Text("admin.roles_action"), view.Locale.Text("admin.promotion_title"),
			view.Locale.Text("shell.load_recovery"), view.Locale.Text("admin.journeys_unavailable_reason"),
			view.Locale.Text("admin.hero_description"), view.Locale.Text("shell.live_source"),
		} {
			if !strings.Contains(visible, want) {
				t.Errorf("%s browser-facing Admin error state lost %q", language, want)
			}
		}
		if strings.Contains(visible, "JourneyService") || strings.Contains(visible, "authenticated cell") {
			t.Errorf("%s browser-facing Admin error state leaked its internal cause", language)
		}
	}
}

func TestTodo_UXAUDIT_006_I18N_MergedUX(t *testing.T) {
	keys := []string{
		"settings.access_context_description",
		"settings.signed_in_as",
		"insights.open_work", "insights.visible_label", "insights.visible_note", "insights.in_progress_label", "insights.in_progress_note",
		"insights.closed_label", "insights.closed_note", "insights.attention_title", "insights.attention_description",
		"insights.no_data_note", "insights.no_data_title", "insights.no_data_description", "insights.needs_attention",
		"insights.context_title", "insights.time_range_label", "insights.freshness_label", "insights.source_label",
		"insights.time_range_value", "insights.freshness_value", "insights.source_value",
		"admin.eyebrow", "admin.hero_description", "admin.roles_description", "admin.roles_action",
		"admin.visibility_description", "admin.visibility_action",
		"admin.worker_ids_description", "admin.worker_ids_action",
		"admin.appearance_description", "admin.appearance_action",
		"admin.promotion_title", "admin.promotion_description", "admin.promotion_action",
		"admin.custom_title", "admin.custom_description", "admin.available", "admin.unavailable",
		"admin.journeys_unavailable_reason", "admin.studio_unavailable_reason",
		"shell.load_recovery", "shell.page_unavailable", "shell.page_recovery", "shell.live_source", "shell.myself_unidentified",
		"journey.startup_title", "journey.startup_detail", "journey.startup_footer",
		"journey.actions_unavailable_title", "journey.actions_unavailable_detail",
		"journey.promotion_eyebrow", "journey.promote_person", "journey.promotion_lead", "journey.profile_link", "journey.all_link",
		"journey.loading_employee_title", "journey.loading_employee_detail", "journey.subject_unavailable_title", "journey.subject_unavailable_detail",
		"journey.employee_label", "journey.employee_verified", "journey.worker_number", "journey.current_job", "journey.organization", "journey.location", "journey.current_base", "journey.form_heading",
		"journey.form_submit_help", "journey.required", "journey.form_submit", "journey.form_worker", "journey.form_worker_help", "journey.form_worker_option", "journey.form_next_role", "journey.form_next_role_help", "journey.form_next_role_option",
		"journey.form_grade", "journey.form_grade_help", "journey.form_grade_option", "journey.form_position", "journey.form_position_help", "journey.form_base", "journey.form_base_help", "journey.form_base_year", "journey.form_effective", "journey.form_effective_help", "journey.form_reason", "journey.form_reason_placeholder", "journey.form_reason_help", "journey.form_no_path", "journey.form_no_access", "journey.form_no_employee", "journey.form_base_rule", "journey.form_benefit_rule", "journey.form_choose_role_rule", "journey.form_base_rule_unavailable", "journey.list_eyebrow", "journey.list_title", "journey.list_lead", "journey.list_choose_employee", "journey.list_empty", "journey.footer", "journey.section_title", "journey.count_one", "journey.count_many", "journey.open_request", "journey.effective", "journey.updated", "journey.empty_title",
		"shell.resource_history", "shell.history_back", "shell.history_forward",
		"journey.required_fields_title", "journey.required_fields_detail", "journey.required_field", "journey.severity_info", "journey.severity_success", "journey.severity_warning", "journey.severity_error",
		"journey.invalid_fields",
		"workflow.choose", "appearance.brand_help", "appearance.company_logo", "appearance.company_logo_help",
		"appearance.logo_upload", "appearance.logo_preview", "appearance.logo_remove", "appearance.logo_undo",
		"appearance.logo_link_label", "appearance.logo_approval_help", "appearance.logo_link_help",
		"workflow.promotion_name", "workflow.promotion_category", "workflow.promotion_description",
		"headcount.unavailable_title", "position.unavailable_title",
		"headcount.return_home", "report_export.return_home", "release_gate.return_home",
		"people.scope", "work.past", "nav.support", "nav.favorite_add", "nav.favorite_remove", "nav.admin_overview", "nav.work_queue",
		"studio.back_admin", "studio.unavailable_badge", "studio.unavailable_title", "studio.unavailable_description", "studio.return_home",
		"page.governed_feedback.label", "page.governed_feedback.title", "governed_feedback.unavailable_title",
		"page.skills_profile.title", "page.governed_population.label", "page.governed_population.title", "governed_population.unavailable_title",
		"page.report_export.title", "report_export.unavailable_title",
	}
	for _, kind := range []string{"denied", "signed_out", "not_found", "invalid", "precondition", "unavailable", "deadline", "canceled", "exists", "exhausted", "other"} {
		keys = append(keys, "journey.error_"+kind+"_title", "journey.error_"+kind+"_detail")
	}
	keys = append(keys,
		"journey.action_start", "journey.action_start_description", "journey.action_start_note", "journey.action_start_blocked_description", "journey.action_start_blocked_reason",
		"journey.action_saving",
		"journey.action_preparing", "journey.action_preparing_description", "journey.action_preparing_reason",
		"journey.action_approve", "journey.action_approve_description", "journey.action_approve_note", "journey.action_approve_reason", "journey.action_approve_placeholder", "journey.action_approve_help",
		"journey.action_reject", "journey.action_reject_description", "journey.action_reject_note", "journey.action_reject_reason", "journey.action_reject_help",
		"journey.action_employee", "journey.action_placement", "journey.action_base", "journey.action_effective", "journey.action_finance_review", "journey.action_manager_review", "journey.action_updated_review", "journey.action_comp_review",
		"journey.actions_heading", "journey.action_acts_as", "journey.action_cancel_review", "journey.action_confirm_execute", "journey.action_review_execute", "journey.action_confirm_approve", "journey.action_review_approve", "journey.action_confirm_reject", "journey.action_review_reject", "journey.action_confirm_generic", "journey.action_review_generic",
		"journey.busy_title", "journey.busy_list", "journey.busy_detail", "journey.busy_proposal", "journey.busy_employee", "journey.busy_start", "journey.busy_approve", "journey.busy_reject",
		"journey.notice_started_title", "journey.notice_started_detail", "journey.notice_declined_title", "journey.notice_declined_detail", "journey.notice_recorded_title", "journey.notice_recorded_detail", "journey.notice_approval_title", "journey.notice_finance_detail", "journey.notice_manager_detail", "journey.notice_reapproval_detail", "journey.notice_waiting_title", "journey.notice_waiting_detail", "journey.notice_next_detail",
		"journey.refusal_busy_title", "journey.refusal_busy_detail", "journey.refusal_unknown_action_title", "journey.refusal_unknown_action_detail",
		"journey.refusal_worker_title", "journey.refusal_worker_detail", "journey.refusal_target_title", "journey.refusal_target_detail",
		"journey.refusal_stale_title", "journey.refusal_stale_detail", "journey.refusal_reason_title", "journey.refusal_reason_detail",
		"journey.refusal_employee_invalid_title",
		"journey.route_unavailable_title", "journey.route_unavailable_detail", "journey.watch_stopped_title", "journey.watch_stopped_detail",
	)
	keys = append(keys, "journey.error_invalid_unlinked_detail")
	for _, field := range []string{"worker", "job", "grade", "position", "base", "effective", "reason"} {
		keys = append(keys, "journey.field_"+field+"_error")
	}
	for _, language := range SupportedProductLocales() {
		locale := ResolveProductLocale(language)
		for _, key := range keys {
			result, err := locale.Resolve(key)
			if err != nil || result.Locale != language || strings.TrimSpace(result.Text) == "" || result.FallbackPath != language {
				t.Errorf("%s %s used missing or fallback copy: %+v, %v", language, key, result, err)
			}
		}
	}
}

func TestTodo_UXAUDIT_006_Regression_MergedUX(t *testing.T) {
	wantReturnHome := map[string]string{
		"en-US": "Back to Home",
		"de-DE": "Zurück zur Startseite",
		"ar":    "العودة إلى الرئيسية",
	}
	for _, language := range SupportedProductLocales() {
		locale := ResolveProductLocale(language)
		view := testView(PageAppearance)
		view.Locale = locale
		appearance, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		for _, technical := range []string{"/workspace/assets/", "organization allowlist", "Organisationsfreigabe", "Asset-Referenzen"} {
			if strings.Contains(appearance, technical) {
				t.Errorf("%s Appearance exposed %q outside diagnostics", language, technical)
			}
		}
		for _, key := range []string{"headcount.return_home", "report_export.return_home", "release_gate.return_home"} {
			copy := locale.Text(key)
			if copy != wantReturnHome[language] {
				t.Errorf("%s %s recovery label = %q, want %q", language, key, copy, wantReturnHome[language])
			}
		}
	}
	for _, locale := range SupportedProductLocales() {
		view := testView(PageHome)
		view.Locale = ResolveProductLocale(locale)
		view.LoadError = "rpc error: code = Unavailable desc = tenant abc: JourneyService refused"
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		ordinary := ordinaryVisibleText(t, doc)
		if strings.Contains(ordinary, "JourneyService") || strings.Contains(ordinary, "tenant abc") || !strings.Contains(ordinary, view.Locale.Text("shell.load_recovery")) {
			t.Fatalf("%s leaked an internal load failure or lost recovery guidance", locale)
		}
		view.LoadError = ""
		view.Page = PageID("missing")
		doc, err = Render(view)
		if err != nil {
			t.Fatal(err)
		}
		ordinary = ordinaryVisibleText(t, doc)
		if strings.Contains(ordinary, "productui: unknown page") || !strings.Contains(ordinary, view.Locale.Text("shell.page_recovery")) {
			t.Fatalf("%s leaked internal route details or lost recovery guidance", locale)
		}
	}
	view := testView(PageInsights)
	view.Work = nil
	view.LoadError = "rpc error: JourneyService unavailable"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	visible := ordinaryVisibleText(t, doc)
	if !strings.Contains(visible, view.Locale.Text("shell.load_recovery")) || strings.Contains(visible, view.Locale.Text("insights.no_data_title")) {
		t.Fatal("a failed Insights read was presented as an empty result")
	}
	settingsView := testView(PageSettings)
	settingsView.Principal = "principal:secret-123"
	settingsView.Viewer = ViewerProfile{}
	settings, err := Render(settingsView)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(settings, "Authorization scope") || strings.Contains(settings, "Authoritative source") {
		t.Fatal("Settings exposes internal access or source labels instead of a useful session summary")
	}
	if strings.Contains(settings, "principal:secret-123") {
		t.Fatal("Settings exposes a raw principal when no authorized viewer profile is available")
	}
	personView := testView(PagePerson)
	profile, err := Render(personView)
	if err != nil {
		t.Fatal(err)
	}
	personCopy := ordinaryVisibleText(t, profile)
	for _, technical := range []string{"Source · CREATED", "Record source", "Record created", "Authoritative · v9", "PRESENT", "MISSING", "WITHHELD"} {
		if strings.Contains(personCopy, technical) {
			t.Errorf("person profile exposed %q outside diagnostics", technical)
		}
	}
	peopleView := testView(PagePeople)
	peopleView.Scope = "compensation_review"
	directory, err := Render(peopleView)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ordinaryVisibleText(t, directory), "compensation_review") {
		t.Fatal("People exposed an internal scope code")
	}
}

func ordinaryVisibleText(t *testing.T, doc string) string {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			switch node.Data {
			case "script", "style", "template":
				return
			case "details":
				for _, attr := range node.Attr {
					if attr.Key == "class" && (strings.Contains(attr.Val, "diagnostic") || strings.Contains(attr.Val, "technical")) {
						return
					}
				}
			}
		}
		if node.Type == xhtml.TextNode {
			text.WriteString(node.Data)
			text.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return text.String()
}
