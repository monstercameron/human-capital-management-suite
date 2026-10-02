package productui

import (
	"math"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AgentSpendLimitClient saves one agent's daily limits. The server decides
// whether the signed-in person owns the agent and whether the change raises
// the limit; the card only asks.
type AgentSpendLimitClient interface {
	SaveSpendLimit(agentID string, maxRunsPerDay, maxSpendMicrosPerDay int64) error
}

// AgentSpendLimitsProps are the inputs of the spend limits card. A zero value
// means "no limit" on that line.
type AgentSpendLimitsProps struct {
	I18nProps
	AgentID              string
	AgentName            string
	MaxRunsPerDay        int64
	MaxSpendMicrosPerDay int64
	// Reached is the sentence shown when today's limit has been reached.
	Reached string
	Client  AgentSpendLimitClient
	// Status is what the last save did (saved, failed or invalid), for a card
	// that is drawn again after the page's script saved it.
	Status string
	// Denied is true when the viewer may read this agent but not change it.
	Denied bool
}

// AgentSpendLimitsCard is the owner's card for one agent's limits: runs per
// day and spend per day, labels above the fields, one Save action.
func AgentSpendLimitsCard(props AgentSpendLimitsProps) ui.Node {
	return ui.CreateElement(agentSpendLimitsCard, props)
}

func agentSpendLimitsCard(props AgentSpendLimitsProps) ui.Node {
	locale := props.Locale
	runs := ui.UseState(agentCostFormatCount(props.MaxRunsPerDay))
	spend := ui.UseState(agentCostFormatDecimal(props.MaxSpendMicrosPerDay))
	status := ui.UseState(props.Status)
	idBase := "agent-spend-" + safeAgentDOMToken(props.AgentID)
	submit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		if props.Client == nil {
			return
		}
		maxRuns, runsOK := parseAgentCostCount(runs.Get())
		micros, spendOK := parseAgentCostMicros(spend.Get())
		if !runsOK || !spendOK {
			status.Set("invalid")
			return
		}
		if err := props.Client.SaveSpendLimit(props.AgentID, maxRuns, micros); err != nil {
			status.Set("failed")
			return
		}
		status.Set("saved")
	})
	children := []ui.Node{
		html.H3(html.Props{ID: idBase + "-title"}, ui.Text(agentCostText(locale, "limits_title"))),
		html.P(html.Props{Class: "muted"}, ui.Text(agentCostText(locale, "limits_detail"))),
	}
	if strings.TrimSpace(props.Reached) != "" {
		children = append(children, html.P(html.Props{Class: "agent-spend-reached", Role: "status", Raw: map[string]any{"data-limit-reached": "true"}}, ui.Text(props.Reached)))
	} else if props.MaxRunsPerDay == 0 && props.MaxSpendMicrosPerDay == 0 {
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(agentCostText(locale, "no_limits"))))
	}
	field := func(id, key, value string, set func(string), mode string) ui.Node {
		return html.Div(html.Props{Class: "field"},
			html.Label(html.Props{For: id}, ui.Text(agentCostText(locale, key))),
			html.Input(html.Props{ID: id, Name: id, Type: "text", Value: value, Disabled: props.Denied, Raw: map[string]any{"inputmode": mode, "autocomplete": "off", "data-spend-field": key}, OnInput: ui.UseEvent(func(event ui.InputEvent) { set(event.GetValue()); status.Set("") })}),
			html.Small(html.Props{Class: "muted"}, ui.Text(agentCostText(locale, key+"_help"))),
		)
	}
	children = append(children,
		field(idBase+"-runs", "runs_per_day", runs.Get(), runs.Set, "numeric"),
		field(idBase+"-spend", "spend_per_day", spend.Get(), spend.Set, "decimal"),
	)
	save := html.Props{Class: "button primary", Type: "submit"}
	if props.Client == nil || props.Denied {
		save.Disabled = true
	}
	if props.Denied {
		children = append(children, html.P(html.Props{Class: "muted", Raw: map[string]any{"data-spend-denied": "true"}}, ui.Text(agentCostText(locale, "denied"))))
	}
	children = append(children, html.Div(html.Props{Class: "action-row"}, html.Button(save, ui.Text(agentCostText(locale, "save_limits")))))
	if message := status.Get(); message != "" {
		children = append(children, html.P(html.Props{Class: "muted", Role: "status", Raw: map[string]any{"data-spend-status": message}}, ui.Text(agentCostText(locale, "status_"+message))))
	}
	return html.Form(html.Props{Class: "surface agent-spend-limits", Aria: map[string]string{"labelledby": idBase + "-title"}, Raw: map[string]any{"data-agent-spend-form": props.AgentID}, OnSubmit: submit}, children...)
}

func agentCostFormatCount(value int64) string {
	if value <= 0 {
		return ""
	}
	return strconv.FormatInt(value, 10)
}

func agentCostFormatDecimal(micros int64) string {
	if micros <= 0 {
		return ""
	}
	return strconv.FormatFloat(float64(micros)/1e6, 'f', 2, 64)
}

// parseAgentCostCount reads a whole number of runs; empty means no limit.
func parseAgentCostCount(text string) (int64, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(text, 10, 64)
	return value, err == nil && value >= 0
}

// parseAgentCostMicros reads a dollar amount such as "5" or "12.50"; empty
// means no limit.
func parseAgentCostMicros(text string) (int64, bool) {
	text = strings.TrimPrefix(strings.TrimSpace(text), "$")
	if text == "" {
		return 0, true
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || value < 0 || math.IsInf(value, 0) || math.IsNaN(value) || value > 1e9 {
		return 0, false
	}
	return int64(math.Round(value * 1e6)), true
}

// AgentCostDay is one day of spend.
type AgentCostDay struct {
	Day    string
	Micros int64
	Runs   int64
}

// AgentCostAgentDay is one agent's spend on one day.
type AgentCostAgentDay struct {
	Agent  string
	Day    string
	Micros int64
}

// AgentCostRun is one run's cost.
type AgentCostRun struct {
	Agent  string
	When   string
	Kind   string
	Micros int64
}

// AgentCostReport is the owner's cost figures for Agent operations.
type AgentCostReport struct {
	PerAnsweredQuestionMicros int64
	MonthToDateMicros         int64
	ForecastMonthMicros       int64
	PerHundredMessagesMicros  int64
	QuietSharePercent         int
	// Notices are the 80 percent warnings the owner has not yet seen.
	Notices     []string
	Trend       []AgentCostDay
	PerAgentDay []AgentCostAgentDay
	Runs        []AgentCostRun
}

// AgentCostPanelProps are the inputs of the cost panel.
type AgentCostPanelProps struct {
	I18nProps
	State   AgentAccessLoadState
	Report  AgentCostReport
	OnRetry func()
}

// AgentCostPanel shows what the owner's agents cost: the headline figures,
// then the thirty-day trend, each agent per day and the latest runs as tables.
func AgentCostPanel(props AgentCostPanelProps) ui.Node {
	locale := props.Locale
	switch props.State {
	case AgentAccessStateLoading:
		return html.Section(html.Props{Class: "surface agent-cost-panel", Role: "status", Raw: map[string]any{"data-agent-cost-state": "loading", "aria-live": "polite"}},
			html.H3(html.Props{}, ui.Text(agentCostText(locale, "cost_title"))), AgentLoadingFrame(AgentLoadingProps{Locale: locale, Shape: AgentLoadingTable, Rows: 3, Status: agentCostText(locale, "loading")}))
	case AgentAccessStateUnavailable:
		retry := html.Button(html.Props{Class: "button primary", Type: "button", Raw: map[string]any{"data-agent-cost-action": "retry"}, OnClick: ui.UseEvent(func(ui.MouseEvent) {
			if props.OnRetry != nil {
				props.OnRetry()
			}
		})}, ui.Text(agentCostText(locale, "try_again")))
		return html.Section(html.Props{Class: "surface agent-cost-panel", Role: "alert", Raw: map[string]any{"data-agent-cost-state": "failed"}},
			html.H3(html.Props{}, ui.Text(agentCostText(locale, "cost_title"))), html.P(html.Props{Class: "muted"}, ui.Text(agentCostText(locale, "failed"))), html.Div(html.Props{Class: "action-row"}, retry))
	}
	report := props.Report
	if len(report.Runs) == 0 && len(report.PerAgentDay) == 0 {
		return html.Section(html.Props{Class: "surface agent-cost-panel", Raw: map[string]any{"data-agent-cost-state": "empty"}},
			html.H3(html.Props{}, ui.Text(agentCostText(locale, "cost_title"))), html.P(html.Props{Class: "muted"}, ui.Text(agentCostText(locale, "empty"))))
	}
	figure := func(key string, value string) ui.Node {
		return html.Li(html.Props{Class: "agent-cost-figure"}, html.Span(html.Props{Class: "muted"}, ui.Text(agentCostText(locale, key))), html.Strong(html.Props{}, ui.Text(value)))
	}
	figures := []ui.Node{
		figure("month_to_date", agentCostMoney(locale, report.MonthToDateMicros)),
		figure("forecast", agentCostMoney(locale, report.ForecastMonthMicros)),
		figure("per_question", agentCostMoney(locale, report.PerAnsweredQuestionMicros)),
		figure("quiet_share", locale.FormatNumber(strconv.Itoa(report.QuietSharePercent), 0)+"%"),
	}
	if report.PerHundredMessagesMicros > 0 {
		figures = append(figures, figure("per_hundred", agentCostMoney(locale, report.PerHundredMessagesMicros)))
	}
	trendRows := make([]ui.Node, 0, len(report.Trend))
	for _, day := range report.Trend {
		trendRows = append(trendRows, html.Tr(html.Props{}, html.Td(html.Props{}, ui.Text(day.Day)), html.Td(html.Props{Class: "numeric"}, ui.Text(locale.FormatNumber(strconv.FormatInt(day.Runs, 10), 0))), html.Td(html.Props{Class: "numeric"}, ui.Text(agentCostMoney(locale, day.Micros)))))
	}
	agentRows := make([]ui.Node, 0, len(report.PerAgentDay))
	for _, row := range report.PerAgentDay {
		agentRows = append(agentRows, html.Tr(html.Props{}, html.Td(html.Props{}, ui.Text(row.Agent)), html.Td(html.Props{}, ui.Text(row.Day)), html.Td(html.Props{Class: "numeric"}, ui.Text(agentCostMoney(locale, row.Micros)))))
	}
	runRows := make([]ui.Node, 0, len(report.Runs))
	for _, run := range report.Runs {
		runRows = append(runRows, html.Tr(html.Props{}, html.Td(html.Props{}, ui.Text(run.Agent)), html.Td(html.Props{}, ui.Text(run.When)), html.Td(html.Props{}, ui.Text(agentCostText(locale, "kind_"+run.Kind))), html.Td(html.Props{Class: "numeric"}, ui.Text(agentCostMoney(locale, run.Micros)))))
	}
	table := func(titleKey string, heads []string, rows []ui.Node) ui.Node {
		if len(rows) == 0 {
			return nil
		}
		head := make([]ui.Node, 0, len(heads))
		for _, key := range heads {
			head = append(head, html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(agentCostText(locale, key))))
		}
		return html.Div(html.Props{Class: "agent-cost-table"}, html.H4(html.Props{}, ui.Text(agentCostText(locale, titleKey))),
			html.Table(html.Props{Class: "data-table"}, html.Thead(html.Props{}, html.Tr(html.Props{}, head...)), html.Tbody(html.Props{}, rows...)))
	}
	children := []ui.Node{
		html.H3(html.Props{}, ui.Text(agentCostText(locale, "cost_title"))),
		html.P(html.Props{Class: "muted"}, ui.Text(agentCostText(locale, "cost_detail"))),
		html.Ul(html.Props{Class: "agent-cost-figures"}, figures...),
	}
	if len(report.Notices) > 0 {
		items := make([]ui.Node, 0, len(report.Notices))
		for _, notice := range report.Notices {
			items = append(items, html.Li(html.Props{}, ui.Text(notice)))
		}
		children = append(children, html.Ul(html.Props{Class: "agent-cost-notices", Role: "status", Raw: map[string]any{"data-agent-cost-notices": "true"}}, items...))
	}
	for _, node := range []ui.Node{
		table("trend_title", []string{"col_day", "col_runs", "col_cost"}, trendRows),
		table("per_agent_title", []string{"col_agent", "col_day", "col_cost"}, agentRows),
		table("runs_title", []string{"col_agent", "col_when", "col_kind", "col_cost"}, runRows),
	} {
		if node != nil {
			children = append(children, node)
		}
	}
	return html.Section(html.Props{Class: "surface agent-cost-panel", Dir: string(locale.Direction), Raw: map[string]any{"data-agent-cost-state": "ready"}}, children...)
}

func agentCostMoney(locale LocaleContext, micros int64) string {
	return "$" + locale.FormatNumber(strconv.FormatFloat(float64(micros)/1e6, 'f', 2, 64), 2)
}

func agentCostText(locale LocaleContext, key string) string {
	labels, ok := agentCostCopy[key]
	if !ok {
		return key
	}
	language := strings.ToLower(locale.Resolved)
	switch {
	case strings.HasPrefix(language, "de"):
		return labels[1]
	case strings.HasPrefix(language, "ar"):
		return labels[2]
	}
	return labels[0]
}

var agentCostCopy = map[string][3]string{
	"limits_title":       {"Spend limits", "Ausgabengrenzen", "حدود الإنفاق"},
	"limits_detail":      {"Limits count each day, midnight to midnight. When one is reached the agent stops answering until the day ends.", "Die Grenzen gelten je Tag, von Mitternacht bis Mitternacht. Ist eine erreicht, antwortet der Agent bis zum Tagesende nicht mehr.", "تُحتسب الحدود لكل يوم من منتصف الليل إلى منتصف الليل. عند بلوغ أحدها يتوقف الوكيل عن الإجابة حتى نهاية اليوم."},
	"no_limits":          {"No limits set. Leave a field empty for no limit.", "Keine Grenzen festgelegt. Ein leeres Feld bedeutet keine Grenze.", "لم يتم تحديد حدود. اترك الحقل فارغاً لعدم وضع حد."},
	"runs_per_day":       {"Runs per day", "Läufe pro Tag", "عدد التشغيلات يومياً"},
	"runs_per_day_help":  {"A whole number. Counts every answer and every screening check.", "Eine ganze Zahl. Zählt jede Antwort und jede Prüfung.", "عدد صحيح. يشمل كل إجابة وكل فحص."},
	"spend_per_day":      {"Spend per day (US dollars)", "Ausgaben pro Tag (US-Dollar)", "الإنفاق يومياً (دولار أمريكي)"},
	"spend_per_day_help": {"For example 5.00. You are told at 80 percent.", "Zum Beispiel 5.00. Sie werden bei 80 Prozent benachrichtigt.", "مثال: 5.00. سيتم إعلامك عند بلوغ 80 بالمئة."},
	"save_limits":        {"Save limits", "Grenzen speichern", "حفظ الحدود"},
	"status_saved":       {"Limits saved.", "Grenzen gespeichert.", "تم حفظ الحدود."},
	"status_failed":      {"The limits could not be saved. Only an owner of this agent can raise a limit. Try again.", "Die Grenzen konnten nicht gespeichert werden. Nur die Verantwortlichen dieses Agenten können eine Grenze erhöhen. Versuchen Sie es erneut.", "تعذر حفظ الحدود. يمكن لمالكي هذا الوكيل فقط رفع الحد. حاول مرة أخرى."},
	"denied":             {"Only an owner of this agent can raise or remove its limits.", "Nur die Verantwortlichen dieses Agenten können Grenzen erhöhen oder entfernen.", "يمكن لمالكي هذا الوكيل فقط رفع حدوده أو إزالتها."},
	"status_invalid":     {"Enter whole numbers for runs and an amount like 5.00 for spend.", "Geben Sie ganze Zahlen für Läufe und einen Betrag wie 5.00 für die Ausgaben ein.", "أدخل أعداداً صحيحة للتشغيلات ومبلغاً مثل 5.00 للإنفاق."},
	"cost_title":         {"What your agents cost", "Was Ihre Agenten kosten", "تكلفة وكلائك"},
	"cost_detail":        {"Only agents you own are counted.", "Es werden nur Agenten gezählt, die Ihnen gehören.", "تُحتسب الوكلاء الذين تملكهم فقط."},
	"limits_failed":      {"This agent's limits could not be read.", "Die Grenzen dieses Agenten konnten nicht gelesen werden.", "تعذرت قراءة حدود هذا الوكيل."},
	"loading_limits":     {"Loading this agent's limits…", "Grenzen dieses Agenten werden geladen…", "جارٍ تحميل حدود هذا الوكيل…"},
	"loading":            {"Loading costs…", "Kosten werden geladen…", "جارٍ تحميل التكاليف…"},
	"failed":             {"Costs could not be loaded.", "Die Kosten konnten nicht geladen werden.", "تعذر تحميل التكاليف."},
	"try_again":          {"Try again", "Erneut versuchen", "حاول مرة أخرى"},
	"empty":              {"No agent runs yet. Costs appear here after an agent answers.", "Noch keine Agentenläufe. Kosten erscheinen hier, sobald ein Agent geantwortet hat.", "لا توجد تشغيلات بعد. تظهر التكاليف هنا بعد أن يجيب وكيل."},
	"month_to_date":      {"This month so far", "Dieser Monat bisher", "هذا الشهر حتى الآن"},
	"forecast":           {"Forecast for the month", "Prognose für den Monat", "توقع الشهر"},
	"per_question":       {"Per answered question", "Pro beantworteter Frage", "لكل سؤال مُجاب"},
	"quiet_share":        {"Spent on screening and decisions", "Für Prüfungen und Entscheidungen ausgegeben", "المنفق على الفحص والقرارات"},
	"per_hundred":        {"Per 100 messages read", "Pro 100 gelesene Nachrichten", "لكل 100 رسالة مقروءة"},
	"trend_title":        {"Last 30 days", "Letzte 30 Tage", "آخر 30 يوماً"},
	"per_agent_title":    {"Each agent per day", "Jeder Agent pro Tag", "كل وكيل يومياً"},
	"runs_title":         {"Latest runs", "Letzte Läufe", "آخر التشغيلات"},
	"col_day":            {"Day", "Tag", "اليوم"},
	"col_runs":           {"Runs", "Läufe", "التشغيلات"},
	"col_cost":           {"Cost", "Kosten", "التكلفة"},
	"col_agent":          {"Agent", "Agent", "الوكيل"},
	"col_when":           {"When", "Wann", "الوقت"},
	"col_kind":           {"What for", "Wofür", "الغرض"},
	"kind_answer":        {"Answer", "Antwort", "إجابة"},
	"kind_screening":     {"Screening", "Prüfung", "فحص"},
	"kind_decision":      {"Decision", "Entscheidung", "قرار"},
}

// ParseAgentSpendLimitFields reads the two fields of the spend limits card: a
// whole number of runs and a dollar amount, either empty for no limit. The page
// script uses it so the card and the script agree on what is valid.
func ParseAgentSpendLimitFields(runs, spend string) (maxRuns, maxSpendMicros int64, ok bool) {
	maxRuns, runsOK := parseAgentCostCount(runs)
	maxSpendMicros, spendOK := parseAgentCostMicros(spend)
	return maxRuns, maxSpendMicros, runsOK && spendOK
}
