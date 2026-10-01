package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PolicyRuleProjection is a redaction-safe rule row selected by the governed
// policy service. The page renders only values the service supplied; it never
// derives a rule from a role, route, or browser input.
type PolicyRuleProjection struct {
	ID         string
	Effect     string
	Subject    string
	Capability string
	Resource   string
	DataDomain string
	Purpose    string
	Condition  string
	Version    string
}

// PolicyStudioProjection is the request-scoped answer for Policy Studio.
// Ready is meaningful even when Rules is empty: an authorized empty result is
// different from an unavailable service and is presented as such.
type PolicyStudioProjection struct {
	Ready       bool
	PolicyKey   string
	Version     string
	PublishedAt string
	Rules       []PolicyRuleProjection
	CanEdit     bool
	EditHref    string
}

// ConfigurationSectionProjection is a server-owned entry in the
// configuration center. Href is already authorization-filtered; the browser
// never manufactures an administrative destination.
type ConfigurationSectionProjection struct {
	ID          string
	Label       string
	Description string
	State       string
	Version     string
	UpdatedAt   string
	Href        string
}

// ConfigurationCenterProjection is the request-scoped answer for the
// configuration center. A ready empty list is an honest no-configurations
// result, not a reason to synthesize settings.
type ConfigurationCenterProjection struct {
	Ready    bool
	Sections []ConfigurationSectionProjection
}

var adminPageProjectionCopy = map[string]map[string]string{
	"en": {
		"service_state":    "Live service projection",
		"policy_intro":     "Author versioned authorization policy through the governed policy service.",
		"policy_published": "Published policy",
		"policy_key":       "Policy key",
		"version":          "Version",
		"published_at":     "Published",
		"rules":            "Rules",
		"rule_id":          "ID", "effect": "Effect", "subject": "Subject", "capability": "Capability", "resource": "Resource", "data_domain": "Data domain", "purpose": "Purpose", "condition": "Condition",
		"no_rules":            "No rules are published for this policy.",
		"edit":                "Open policy editor",
		"simulation_intro":    "Evaluate an authorization request against a server-selected policy snapshot. This view grants no authority.",
		"configuration_intro": "Open governed configuration areas published for this organization.",
		"configuration_areas": "Configuration areas",
		"no_configuration":    "No configuration areas are published for this organization.",
	},
	"de": {
		"service_state":    "Live-Projektion des Dienstes",
		"policy_intro":     "Versionierte Autorisierungsrichtlinien über den gesteuerten Richtliniendienst bearbeiten.",
		"policy_published": "Veröffentlichte Richtlinie",
		"policy_key":       "Richtlinienschlüssel",
		"version":          "Version",
		"published_at":     "Veröffentlicht",
		"rules":            "Regeln",
		"rule_id":          "ID", "effect": "Wirkung", "subject": "Subjekt", "capability": "Fähigkeit", "resource": "Ressource", "data_domain": "Datenbereich", "purpose": "Zweck", "condition": "Bedingung",
		"no_rules":            "Für diese Richtlinie sind keine Regeln veröffentlicht.",
		"edit":                "Richtlinieneditor öffnen",
		"simulation_intro":    "Eine Autorisierungsanfrage anhand eines serverseitig ausgewählten Richtliniensnapshots auswerten. Diese Ansicht erteilt keine Berechtigung.",
		"configuration_intro": "Vom Unternehmen veröffentlichte, gesteuerte Konfigurationsbereiche öffnen.",
		"configuration_areas": "Konfigurationsbereiche",
		"no_configuration":    "Für dieses Unternehmen sind keine Konfigurationsbereiche veröffentlicht.",
	},
	"ar": {
		"rule_id": "المعرّف", "effect": "التأثير", "subject": "الموضوع", "capability": "القدرة", "resource": "المورد", "data_domain": "نطاق البيانات", "purpose": "الغرض", "condition": "الشرط",
		"service_state":       "إسقاط مباشر من الخدمة",
		"policy_intro":        "أنشئ سياسة تفويض بإصدارات عبر خدمة السياسات الخاضعة للحوكمة.",
		"policy_published":    "السياسة المنشورة",
		"policy_key":          "مفتاح السياسة",
		"version":             "الإصدار",
		"published_at":        "تاريخ النشر",
		"rules":               "القواعد",
		"no_rules":            "لا توجد قواعد منشورة لهذه السياسة.",
		"edit":                "فتح محرر السياسة",
		"simulation_intro":    "قيّم طلب تفويض مقابل لقطة سياسة يحددها الخادم. لا تمنح هذه الصفحة أي صلاحية.",
		"configuration_intro": "افتح مجالات التكوين الخاضعة للحوكمة والمنشورة لهذه المؤسسة.",
		"configuration_areas": "مجالات التكوين",
		"no_configuration":    "لا توجد مجالات تكوين منشورة لهذه المؤسسة.",
	},
}

func adminPageProjectionText(locale LocaleContext, key string) string {
	language := strings.ToLower(locale.normalized().Resolved)
	if index := strings.IndexByte(language, '-'); index > 0 {
		language = language[:index]
	}
	if value := adminPageProjectionCopy[language][key]; value != "" {
		return value
	}
	return adminPageProjectionCopy["en"][key]
}

func policyStudioLivePage(view View, projection PolicyStudioProjection) ui.Node {
	copy := func(key string) string { return adminPageProjectionText(view.Locale, key) }
	children := []ui.Node{
		html.H2(html.Props{ID: "policy-studio-page-title"}, ui.Text(view.Locale.Text("page.policy_studio.title"))),
		html.P(html.Props{Class: "muted"}, ui.Text(copy("policy_intro"))),
		html.P(html.Props{Class: "admin-live-state", Raw: map[string]any{"role": "status"}}, ui.Text(copy("service_state"))),
	}

	facts := []ui.Node{}
	appendFact := func(label, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		facts = append(facts, html.Tag("dt", html.Props{}, ui.Text(label)), html.Tag("dd", html.Props{}, ui.Text(strings.TrimSpace(value))))
	}
	appendFact(copy("policy_key"), projection.PolicyKey)
	appendFact(copy("version"), projection.Version)
	appendFact(copy("published_at"), projection.PublishedAt)
	if len(facts) > 0 {
		children = append(children, html.Tag("dl", html.Props{Class: "admin-live-facts"}, facts...))
	}

	rows := make([]ui.Node, 0, len(projection.Rules))
	for _, rule := range projection.Rules {
		if strings.TrimSpace(rule.ID) == "" {
			continue
		}
		cells := []string{rule.ID, rule.Effect, rule.Subject, rule.Capability, rule.Resource, rule.DataDomain, rule.Purpose, rule.Condition, rule.Version}
		row := make([]ui.Node, 0, len(cells))
		for _, cell := range cells {
			row = append(row, html.Tag("td", html.Props{}, ui.Text(strings.TrimSpace(cell))))
		}
		rows = append(rows, html.Tag("tr", html.Props{}, row...))
	}
	if len(rows) == 0 {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(copy("no_rules"))))
	} else {
		headings := []string{copy("rule_id"), copy("effect"), copy("subject"), copy("capability"), copy("resource"), copy("data_domain"), copy("purpose"), copy("condition"), copy("version")}
		header := make([]ui.Node, 0, len(headings))
		for _, heading := range headings {
			header = append(header, html.Tag("th", html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(heading)))
		}
		children = append(children, html.Tag("h3", html.Props{}, ui.Text(copy("rules"))), html.Tag("table", html.Props{Class: "admin-live-table"},
			html.Tag("thead", html.Props{}, html.Tag("tr", html.Props{}, header...)), html.Tag("tbody", html.Props{}, rows...)))
	}
	if projection.CanEdit && validRecoveryHref(projection.EditHref) {
		children = append(children, softwareLink(view.Navigate, html.Props{Class: "button primary"}, projection.EditHref, ui.Text(copy("edit"))))
	}
	return html.Section(html.Props{ID: "policy-studio-page", Class: "admin-live-page", Raw: map[string]any{"aria-labelledby": "policy-studio-page-title", "data-service-state": "ready"}}, children...)
}

func policySimulationLivePage(view View) ui.Node {
	return html.Section(html.Props{ID: "policy-simulation-page", Class: "admin-live-page", Raw: map[string]any{"aria-labelledby": "policy-simulation-page-title", "data-service-state": "ready"}},
		html.H2(html.Props{ID: "policy-simulation-page-title"}, ui.Text(view.Locale.Text("page.policy_simulation.title"))),
		html.P(html.Props{Class: "muted"}, ui.Text(adminPageProjectionText(view.Locale, "simulation_intro"))),
		policySimulation(view),
	)
}

func configurationCenterLivePage(view View, projection ConfigurationCenterProjection) ui.Node {
	copy := func(key string) string { return adminPageProjectionText(view.Locale, key) }
	children := []ui.Node{
		html.H2(html.Props{ID: "configuration-center-page-title"}, ui.Text(view.Locale.Text("page.configuration_center.title"))),
		html.P(html.Props{Class: "muted"}, ui.Text(copy("configuration_intro"))),
		html.P(html.Props{Class: "admin-live-state", Raw: map[string]any{"role": "status"}}, ui.Text(copy("service_state"))),
		html.H3(html.Props{}, ui.Text(copy("configuration_areas"))),
	}
	items := make([]ui.Node, 0, len(projection.Sections))
	for _, section := range projection.Sections {
		if strings.TrimSpace(section.ID) == "" || strings.TrimSpace(section.Label) == "" {
			continue
		}
		content := []ui.Node{html.Strong(html.Props{}, ui.Text(strings.TrimSpace(section.Label)))}
		if strings.TrimSpace(section.Description) != "" {
			content = append(content, html.P(html.Props{Class: "muted"}, ui.Text(strings.TrimSpace(section.Description))))
		}
		meta := []string{section.State, section.Version, section.UpdatedAt}
		facts := make([]string, 0, len(meta))
		for _, value := range meta {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				facts = append(facts, trimmed)
			}
		}
		if len(facts) > 0 {
			content = append(content, html.Small(html.Props{Class: "admin-live-meta"}, ui.Text(strings.Join(facts, " · "))))
		}
		if validRecoveryHref(section.Href) {
			content = append(content, html.Div(html.Props{}, softwareLink(view.Navigate, html.Props{Class: "button secondary"}, section.Href, ui.Text(section.Label))))
		}
		items = append(items, html.Tag("li", html.Props{Class: "admin-live-card", Data: map[string]string{"configuration-id": section.ID}}, content...))
	}
	if len(items) == 0 {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(copy("no_configuration"))))
	} else {
		children = append(children, html.Tag("ul", html.Props{Class: "admin-live-list"}, items...))
	}
	return html.Section(html.Props{ID: "configuration-center-page", Class: "admin-live-page", Raw: map[string]any{"aria-labelledby": "configuration-center-page-title", "data-service-state": "ready"}}, children...)
}
