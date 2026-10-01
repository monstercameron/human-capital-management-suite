package productui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// These are presentation projections, not copies of the owning services. The
// request adapter supplies only the authorized, bounded facts that a page may
// show. A route remains unavailable until its service marks the projection
// ready.

type IntegrationOperationProjection struct {
	ID          string
	Connector   string
	Operation   string
	State       string
	StartedAt   string
	FinishedAt  string
	EvidenceRef string
}

type IntegrationOperationsProjection struct {
	Ready           bool
	ContractVersion int
	ServiceVersion  string
	AsOf            string
	Operations      []IntegrationOperationProjection
}

type ReconciliationFindingProjection struct {
	ID             string
	Kind           string
	Severity       string
	Resource       string
	TargetCount    int
	EvidenceDigest string
	Actions        []string
}

type ReconciliationWorkbenchProjection struct {
	Ready           bool
	ContractVersion int
	Slice           string
	Truncated       bool
	Items           []ReconciliationFindingProjection
}

type PrivacyTelemetryProjection struct {
	Ready                  bool
	PolicyVersion          int
	PolicyDigest           string
	SuccessSampleRate      string
	MetricCardinalityLimit int
	CriticalFailureClasses []string
	EvidenceRef            string
}

type PerformanceBudgetProjection struct {
	ID       string
	Name     string
	Scope    string
	Target   string
	Observed string
	Status   string
	Version  string
}

type PerformanceBudgetsProjection struct {
	Ready           bool
	ContractVersion int
	ServiceVersion  string
	AsOf            string
	Budgets         []PerformanceBudgetProjection
}

var adminOperationsCopy = map[string]map[string]string{
	"en": {
		"service_state":        "Live service projection",
		"integration_intro":    "Review authorized connector operations and their retained evidence.",
		"operations":           "Connector operations",
		"no_operations":        "No connector operations are available for this scope.",
		"reconciliation_intro": "Review observed discrepancies and the governed repair actions admitted for each finding.",
		"findings":             "Repair findings",
		"no_findings":          "No repair findings are available for this scope.",
		"truncated":            "Only the bounded set of findings is shown.",
		"telemetry_intro":      "Review the privacy-safe telemetry policy applied to frontend signals.",
		"policy":               "Telemetry policy",
		"sample_rate":          "Success sample rate",
		"cardinality":          "Metric cardinality limit",
		"critical_failures":    "Retained failure classes",
		"no_failure_classes":   "No critical failure classes are published.",
		"performance_intro":    "Review the server-published budgets and the measurements reported against them.",
		"budgets":              "Frontend budgets",
		"no_budgets":           "No frontend budgets are published for this scope.",
		"version":              "Version",
		"as_of":                "As of",
		"evidence":             "Evidence",
	},
	"de": {
		"service_state":        "Live-Projektion des Dienstes",
		"integration_intro":    "Autorisierte Konnektorvorgänge und ihre aufbewahrten Nachweise prüfen.",
		"operations":           "Konnektorvorgänge",
		"no_operations":        "Für diesen Bereich sind keine Konnektorvorgänge verfügbar.",
		"reconciliation_intro": "Beobachtete Abweichungen und die zugelassenen gesteuerten Reparaturaktionen prüfen.",
		"findings":             "Reparaturbefunde",
		"no_findings":          "Für diesen Bereich sind keine Reparaturbefunde verfügbar.",
		"truncated":            "Es wird nur die begrenzte Anzahl an Befunden angezeigt.",
		"telemetry_intro":      "Die auf Frontendsignale angewendete datenschutzsichere Telemetrierichtlinie prüfen.",
		"policy":               "Telemetrierichtlinie",
		"sample_rate":          "Stichprobenrate für Erfolge",
		"cardinality":          "Kardinalitätsgrenze für Metriken",
		"critical_failures":    "Aufbewahrte Fehlerklassen",
		"no_failure_classes":   "Keine kritischen Fehlerklassen veröffentlicht.",
		"performance_intro":    "Veröffentlichte Budgets und die dazu gemeldeten Messwerte prüfen.",
		"budgets":              "Frontend-Budgets",
		"no_budgets":           "Für diesen Bereich sind keine Frontend-Budgets veröffentlicht.",
		"version":              "Version",
		"as_of":                "Stand",
		"evidence":             "Nachweis",
	},
	"ar": {
		"service_state":        "إسقاط مباشر من الخدمة",
		"integration_intro":    "راجع عمليات الموصلات المصرح بها وأدلتها المحتفظ بها.",
		"operations":           "عمليات الموصلات",
		"no_operations":        "لا تتوفر عمليات موصلات لهذا النطاق.",
		"reconciliation_intro": "راجع التباينات المرصودة وإجراءات الإصلاح المحكومة المسموح بها لكل نتيجة.",
		"findings":             "نتائج الإصلاح",
		"no_findings":          "لا تتوفر نتائج إصلاح لهذا النطاق.",
		"truncated":            "يتم عرض المجموعة المحدودة من النتائج فقط.",
		"telemetry_intro":      "راجع سياسة القياس الآمنة للخصوصية المطبقة على إشارات الواجهة الأمامية.",
		"policy":               "سياسة القياس",
		"sample_rate":          "معدل عينات النجاح",
		"cardinality":          "حد تعدد أبعاد المقياس",
		"critical_failures":    "فئات الفشل المحتفظ بها",
		"no_failure_classes":   "لم تُنشر فئات فشل حرجة.",
		"performance_intro":    "راجع الميزانيات المنشورة من الخادم والقياسات المبلغ عنها مقابلها.",
		"budgets":              "ميزانيات الواجهة الأمامية",
		"no_budgets":           "لا توجد ميزانيات واجهة أمامية منشورة لهذا النطاق.",
		"version":              "الإصدار",
		"as_of":                "حتى",
		"evidence":             "الدليل",
	},
}

func adminOperationsText(locale LocaleContext, key string) string {
	language := strings.ToLower(locale.normalized().Resolved)
	if index := strings.IndexByte(language, '-'); index > 0 {
		language = language[:index]
	}
	if value := adminOperationsCopy[language][key]; value != "" {
		return value
	}
	return adminOperationsCopy["en"][key]
}

func adminOperationsServiceHeader(view View, title, description, serviceVersion, asOf string) []ui.Node {
	copy := func(key string) string { return adminOperationsText(view.Locale, key) }
	children := []ui.Node{
		html.H2(html.Props{ID: "admin-operations-page-title"}, ui.Text(title)),
		html.P(html.Props{Class: "muted"}, ui.Text(description)),
		html.P(html.Props{Class: "admin-live-state", Raw: map[string]any{"role": "status"}}, ui.Text(copy("service_state"))),
	}
	meta := make([]string, 0, 2)
	if strings.TrimSpace(serviceVersion) != "" {
		meta = append(meta, copy("version")+": "+strings.TrimSpace(serviceVersion))
	}
	if strings.TrimSpace(asOf) != "" {
		meta = append(meta, copy("as_of")+": "+strings.TrimSpace(asOf))
	}
	if len(meta) > 0 {
		children = append(children, html.P(html.Props{Class: "admin-live-meta"}, ui.Text(strings.Join(meta, " · "))))
	}
	return children
}

func integrationOperationsLivePage(view View, projection IntegrationOperationsProjection) ui.Node {
	copy := func(key string) string { return adminOperationsText(view.Locale, key) }
	children := adminOperationsServiceHeader(view, view.Locale.Text("page.integration_operations.title"), copy("integration_intro"), projection.ServiceVersion, projection.AsOf)
	children = append(children, html.H3(html.Props{}, ui.Text(copy("operations"))))
	rows := make([]ui.Node, 0, len(projection.Operations))
	for _, operation := range projection.Operations {
		if strings.TrimSpace(operation.ID) == "" || strings.TrimSpace(operation.Connector) == "" {
			continue
		}
		values := []string{operation.ID, operation.Connector, operation.Operation, operation.State, operation.StartedAt, operation.FinishedAt, operation.EvidenceRef}
		cells := make([]ui.Node, 0, len(values))
		for _, value := range values {
			cells = append(cells, html.Tag("td", html.Props{}, ui.Text(strings.TrimSpace(value))))
		}
		rows = append(rows, html.Tag("tr", html.Props{}, cells...))
	}
	if len(rows) == 0 {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(copy("no_operations"))))
	} else {
		headings := []string{"ID", "Connector", "Operation", "State", "Started", "Finished", copy("evidence")}
		cells := make([]ui.Node, 0, len(headings))
		for _, heading := range headings {
			cells = append(cells, html.Tag("th", html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(heading)))
		}
		children = append(children, html.Tag("table", html.Props{Class: "admin-live-table"}, html.Tag("thead", html.Props{}, html.Tag("tr", html.Props{}, cells...)), html.Tag("tbody", html.Props{}, rows...)))
	}
	return html.Section(html.Props{ID: "integration-operations-page", Class: "admin-live-page", Raw: map[string]any{"aria-labelledby": "admin-operations-page-title", "data-service-state": "ready"}}, children...)
}

func reconciliationWorkbenchLivePage(view View, projection ReconciliationWorkbenchProjection) ui.Node {
	copy := func(key string) string { return adminOperationsText(view.Locale, key) }
	children := adminOperationsServiceHeader(view, view.Locale.Text("page.reconciliation_workbench.title"), copy("reconciliation_intro"), "", "")
	if strings.TrimSpace(projection.Slice) != "" {
		children = append(children, html.P(html.Props{Class: "admin-live-meta"}, ui.Text(projection.Slice)))
	}
	children = append(children, html.H3(html.Props{}, ui.Text(copy("findings"))))
	rows := make([]ui.Node, 0, len(projection.Items))
	for _, item := range projection.Items {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Resource) == "" {
			continue
		}
		actions := strings.Join(nonEmptyStrings(item.Actions), ", ")
		values := []string{item.ID, item.Kind, item.Severity, item.Resource, strconv.Itoa(item.TargetCount), actions, item.EvidenceDigest}
		cells := make([]ui.Node, 0, len(values))
		for _, value := range values {
			cells = append(cells, html.Tag("td", html.Props{}, ui.Text(strings.TrimSpace(value))))
		}
		rows = append(rows, html.Tag("tr", html.Props{}, cells...))
	}
	if len(rows) == 0 {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(copy("no_findings"))))
	} else {
		headings := []string{"ID", "Kind", "Severity", "Resource", "Targets", "Actions", copy("evidence")}
		cells := make([]ui.Node, 0, len(headings))
		for _, heading := range headings {
			cells = append(cells, html.Tag("th", html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(heading)))
		}
		children = append(children, html.Tag("table", html.Props{Class: "admin-live-table"}, html.Tag("thead", html.Props{}, html.Tag("tr", html.Props{}, cells...)), html.Tag("tbody", html.Props{}, rows...)))
	}
	if projection.Truncated {
		children = append(children, html.P(html.Props{Class: "muted", Role: "status"}, ui.Text(copy("truncated"))))
	}
	return html.Section(html.Props{ID: "reconciliation-workbench-page", Class: "admin-live-page", Raw: map[string]any{"aria-labelledby": "admin-operations-page-title", "data-service-state": "ready"}}, children...)
}

func privacyTelemetryLivePage(view View, projection PrivacyTelemetryProjection) ui.Node {
	copy := func(key string) string { return adminOperationsText(view.Locale, key) }
	children := adminOperationsServiceHeader(view, view.Locale.Text("page.privacy_telemetry.title"), copy("telemetry_intro"), "", "")
	children = append(children, html.H3(html.Props{}, ui.Text(copy("policy"))))
	facts := make([]ui.Node, 0, 8)
	appendFact := func(label, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		facts = append(facts, html.Tag("dt", html.Props{}, ui.Text(label)), html.Tag("dd", html.Props{}, ui.Text(strings.TrimSpace(value))))
	}
	if projection.PolicyVersion > 0 {
		appendFact(copy("version"), strconv.Itoa(projection.PolicyVersion))
	}
	appendFact(copy("sample_rate"), projection.SuccessSampleRate)
	if projection.MetricCardinalityLimit > 0 {
		appendFact(copy("cardinality"), strconv.Itoa(projection.MetricCardinalityLimit))
	}
	appendFact(copy("evidence"), projection.EvidenceRef)
	appendFact("Policy digest", projection.PolicyDigest)
	if len(facts) > 0 {
		children = append(children, html.Tag("dl", html.Props{Class: "admin-live-facts"}, facts...))
	}
	classes := nonEmptyStrings(projection.CriticalFailureClasses)
	children = append(children, html.H4(html.Props{}, ui.Text(copy("critical_failures"))))
	if len(classes) == 0 {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(copy("no_failure_classes"))))
	} else {
		items := make([]ui.Node, 0, len(classes))
		for _, class := range classes {
			items = append(items, html.Tag("li", html.Props{}, ui.Text(class)))
		}
		children = append(children, html.Tag("ul", html.Props{Class: "admin-live-list"}, items...))
	}
	return html.Section(html.Props{ID: "privacy-telemetry-page", Class: "admin-live-page", Raw: map[string]any{"aria-labelledby": "admin-operations-page-title", "data-service-state": "ready"}}, children...)
}

func performanceBudgetsLivePage(view View, projection PerformanceBudgetsProjection) ui.Node {
	copy := func(key string) string { return adminOperationsText(view.Locale, key) }
	children := adminOperationsServiceHeader(view, view.Locale.Text("page.performance_budgets.title"), copy("performance_intro"), projection.ServiceVersion, projection.AsOf)
	children = append(children, html.H3(html.Props{}, ui.Text(copy("budgets"))))
	rows := make([]ui.Node, 0, len(projection.Budgets))
	for _, budget := range projection.Budgets {
		if strings.TrimSpace(budget.ID) == "" || strings.TrimSpace(budget.Name) == "" {
			continue
		}
		values := []string{budget.Name, budget.Scope, budget.Target, budget.Observed, budget.Status, budget.Version}
		cells := make([]ui.Node, 0, len(values))
		for _, value := range values {
			cells = append(cells, html.Tag("td", html.Props{}, ui.Text(strings.TrimSpace(value))))
		}
		rows = append(rows, html.Tag("tr", html.Props{}, cells...))
	}
	if len(rows) == 0 {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(copy("no_budgets"))))
	} else {
		headings := []string{"Name", "Scope", "Target", "Observed", "Status", copy("version")}
		cells := make([]ui.Node, 0, len(headings))
		for _, heading := range headings {
			cells = append(cells, html.Tag("th", html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(heading)))
		}
		children = append(children, html.Tag("table", html.Props{Class: "admin-live-table"}, html.Tag("thead", html.Props{}, html.Tag("tr", html.Props{}, cells...)), html.Tag("tbody", html.Props{}, rows...)))
	}
	return html.Section(html.Props{ID: "performance-budgets-page", Class: "admin-live-page", Raw: map[string]any{"aria-labelledby": "admin-operations-page-title", "data-service-state": "ready"}}, children...)
}

func nonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
