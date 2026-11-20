package chatui

import (
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3959: Prepare an analysis. A message that asks for workforce figures
// offers a private request builder. The builder reads the message for the
// metrics it names (by words only, no model), asks the requester to confirm the
// metrics, population, reporting period and grouping, lists what is still
// undecided and what each metric leaves ambiguous, and then hands the partly or
// fully filled request to the governed analytics surface (the report catalog)
// as opaque tokens. Chat never runs a query and never shows a figure: the
// analytics surface decides what the person may see, applies small-group
// suppression and presents definitions, cohort and as-of time. Nothing is sent
// to a channel; the state lives only in the page, so it is private.

// AnalysisRequest is the request as the requester has filled it so far. Every
// value is a token of the fixed choices below, never free text.
type AnalysisRequest struct {
	Metrics    []string
	Population string
	Period     string
	Grouping   string
}

// AnalysisState is the page's transient state of the builder. The page sets
// Enabled only for a viewer who may open the analytics surface.
type AnalysisState struct {
	Enabled bool
	// PostID is the message whose builder is open ("" when none is).
	PostID     string
	Metrics    map[string]bool
	Population string
	Period     string
	Grouping   string

	Open    func(postID string)
	Close   func()
	Metric  func(metricID string)
	Choose  func(field, value string)
	HandOff func(conversationID, postID string, request AnalysisRequest)
}

type analysisMetric struct {
	ID    string
	Terms []string // lower case, hyphens written as spaces
}

var analysisMetrics = []analysisMetric{
	{"time_to_fill", []string{"time to fill", "time to hire", "besetzungsdauer", "مدة شغل", "مدة التوظيف"}},
	{"offer_acceptance", []string{"offer acceptance", "offers accepted", "acceptance rate", "angebotsannahme", "annahmequote", "قبول العروض", "معدل قبول"}},
	{"attrition", []string{"attrition", "turnover", "fluktuation", "abgangsquote", "ترك العمل", "معدل الدوران", "الاستنزاف"}},
	{"headcount", []string{"headcount", "head count", "personalbestand", "mitarbeiterzahl", "عدد الموظفين"}},
}

var analysisChoices = map[string][]string{
	"population": {"all", "mine", "other"},
	"period":     {"last_quarter", "last_12_months", "year_to_date", "custom"},
	"grouping":   {"none", "department", "location", "month"},
}

func analysisKnownMetric(id string) bool {
	for _, metric := range analysisMetrics {
		if metric.ID == id {
			return true
		}
	}
	return false
}

func analysisKnownChoice(field, value string) bool {
	for _, choice := range analysisChoices[field] {
		if choice == value {
			return true
		}
	}
	return false
}

// AnalysisMetricsIn returns the metrics a message names, in catalog order.
func AnalysisMetricsIn(body string) []string {
	text := strings.ToLower(strings.NewReplacer("-", " ", "_", " ").Replace(body))
	var out []string
	for _, metric := range analysisMetrics {
		for _, term := range metric.Terms {
			if strings.Contains(text, term) {
				out = append(out, metric.ID)
				break
			}
		}
	}
	return out
}

const analysisReportsRoute = "/workspace/app/reports/catalog"

// AnalysisHandoffHref is the address of the governed analytics surface with the
// conversation and message as opaque ids and the requester's choices as fixed
// tokens. Message text never travels, and an unknown token is dropped. It is
// empty when there is no conversation id.
func AnalysisHandoffHref(locale, conversationID, messageID string, request AnalysisRequest) string {
	conversationID, messageID = strings.TrimSpace(conversationID), strings.TrimSpace(messageID)
	if conversationID == "" || len(conversationID) > workflowHandoffIDLimit || len(messageID) > workflowHandoffIDLimit {
		return ""
	}
	query := url.Values{"from_conversation": {conversationID}}
	if messageID != "" {
		query.Set("from_message", messageID)
	}
	for _, id := range request.Metrics {
		if analysisKnownMetric(id) {
			query.Add("metric", id)
		}
	}
	for field, value := range map[string]string{"population": request.Population, "period": request.Period, "grouping": request.Grouping} {
		if analysisKnownChoice(field, value) {
			query.Set(field, value)
		}
	}
	if resolved := chatEmojiLocale(locale); resolved != "en-US" {
		query.Set("locale", resolved)
	}
	return analysisReportsRoute + "?" + query.Encode()
}

func (s AnalysisState) value(field string) string {
	switch field {
	case "population":
		return s.Population
	case "period":
		return s.Period
	case "grouping":
		return s.Grouping
	}
	return ""
}

func (s AnalysisState) chosenMetrics() []string {
	var out []string
	for _, metric := range analysisMetrics {
		if s.Metrics[metric.ID] {
			out = append(out, metric.ID)
		}
	}
	return out
}

func (s AnalysisState) request() AnalysisRequest {
	return AnalysisRequest{Metrics: s.chosenMetrics(), Population: s.Population, Period: s.Period, Grouping: s.Grouping}
}

const (
	keyIssue3959Menu       = "chat.issue3959.menu"
	keyIssue3959Title      = "chat.issue3959.title"
	keyIssue3959Private    = "chat.issue3959.private"
	keyIssue3959NoFigures  = "chat.issue3959.no_figures"
	keyIssue3959From       = "chat.issue3959.from"
	keyIssue3959Metrics    = "chat.issue3959.metrics"
	keyIssue3959MetricNote = "chat.issue3959.metrics_note"
	keyIssue3959Population = "chat.issue3959.population"
	keyIssue3959Period     = "chat.issue3959.period"
	keyIssue3959Grouping   = "chat.issue3959.grouping"
	keyIssue3959Pending    = "chat.issue3959.pending"
	keyIssue3959Complete   = "chat.issue3959.complete"
	keyIssue3959Decide     = "chat.issue3959.decide"
	keyIssue3959HandOff    = "chat.issue3959.handoff"
	keyIssue3959HandOffAid = "chat.issue3959.handoff_hint"
	keyIssue3959Close      = "chat.issue3959.close"
	keyIssue3959GapMetric  = "chat.issue3959.gap.metric"
	keyIssue3959GapField   = "chat.issue3959.gap.population"
	keyIssue3959GapPeriod  = "chat.issue3959.gap.period"
	keyIssue3959GapGroup   = "chat.issue3959.gap.grouping"
)

func issue3959MetricKey(id string) string       { return "chat.issue3959.metric." + id }
func issue3959MetricDecideKey(id string) string { return "chat.issue3959.decide." + id }
func issue3959ChoiceKey(field, value string) string {
	return "chat.issue3959.choice." + field + "." + value
}

var issue3959Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3959Menu:                               "Prepare an analysis",
		keyIssue3959Title:                              "Prepare an analysis",
		keyIssue3959Private:                            "Only you see this request. Nothing is sent or run until you open the analytics page.",
		keyIssue3959NoFigures:                          "Chat does not run the analysis or show figures. The analytics page runs governed reports, shows the definitions, group and as-of time, and decides what you may see, including hiding groups that are too small.",
		keyIssue3959From:                               "From the message",
		keyIssue3959Metrics:                            "Metrics named in the message",
		keyIssue3959MetricNote:                         "Only metrics the message names are listed. The analytics page confirms which of them exist in the governed catalog.",
		keyIssue3959Population:                         "Population",
		keyIssue3959Period:                             "Reporting period",
		keyIssue3959Grouping:                           "Grouping",
		keyIssue3959Pending:                            "Still to decide",
		keyIssue3959Complete:                           "Every field is chosen. The analytics page still checks availability, your access and small groups.",
		keyIssue3959Decide:                             "Decide: {text}",
		keyIssue3959HandOff:                            "Open in analytics",
		keyIssue3959HandOffAid:                         "Fields you have not chosen are carried over as open.",
		keyIssue3959Close:                              "Close",
		keyIssue3959GapMetric:                          "Choose at least one metric.",
		keyIssue3959GapField:                           "Choose a population.",
		keyIssue3959GapPeriod:                          "Choose a reporting period.",
		keyIssue3959GapGroup:                           "Choose a grouping, or no grouping.",
		issue3959MetricKey("time_to_fill"):             "Time to fill",
		issue3959MetricKey("offer_acceptance"):         "Offer acceptance",
		issue3959MetricKey("attrition"):                "Attrition",
		issue3959MetricKey("headcount"):                "Headcount",
		issue3959MetricDecideKey("time_to_fill"):       "Say where the clock starts (requisition approved or posted) and where it stops (offer accepted or start date).",
		issue3959MetricDecideKey("offer_acceptance"):   "Say whether offers still open count, and whether a re-issued offer counts once.",
		issue3959MetricDecideKey("attrition"):          "Say whether voluntary, involuntary or all exits count, and the window, for example the first 90 days.",
		issue3959MetricDecideKey("headcount"):          "Say whether contractors count and whether it is a single day or an average.",
		issue3959ChoiceKey("population", "all"):        "Everyone I may see",
		issue3959ChoiceKey("population", "mine"):       "My organization",
		issue3959ChoiceKey("population", "other"):      "Another group, set on the analytics page",
		issue3959ChoiceKey("period", "last_quarter"):   "Last quarter",
		issue3959ChoiceKey("period", "last_12_months"): "Last 12 months",
		issue3959ChoiceKey("period", "year_to_date"):   "Year to date",
		issue3959ChoiceKey("period", "custom"):         "Another period, set on the analytics page",
		issue3959ChoiceKey("grouping", "none"):         "No grouping",
		issue3959ChoiceKey("grouping", "department"):   "By department",
		issue3959ChoiceKey("grouping", "location"):     "By location",
		issue3959ChoiceKey("grouping", "month"):        "By month",
	},
	"de-DE": {
		keyIssue3959Menu:                               "Analyse vorbereiten",
		keyIssue3959Title:                              "Analyse vorbereiten",
		keyIssue3959Private:                            "Nur Sie sehen diese Anfrage. Nichts wird gesendet oder ausgeführt, bis Sie die Analyseseite öffnen.",
		keyIssue3959NoFigures:                          "Der Chat führt die Analyse nicht aus und zeigt keine Zahlen. Die Analyseseite führt geprüfte Berichte aus, zeigt Definitionen, Gruppe und Stichtag und entscheidet, was Sie sehen dürfen, einschließlich des Ausblendens zu kleiner Gruppen.",
		keyIssue3959From:                               "Aus der Nachricht",
		keyIssue3959Metrics:                            "In der Nachricht genannte Kennzahlen",
		keyIssue3959MetricNote:                         "Aufgelistet sind nur Kennzahlen, die die Nachricht nennt. Die Analyseseite bestätigt, welche davon im geprüften Katalog vorhanden sind.",
		keyIssue3959Population:                         "Personengruppe",
		keyIssue3959Period:                             "Berichtszeitraum",
		keyIssue3959Grouping:                           "Gruppierung",
		keyIssue3959Pending:                            "Noch zu entscheiden",
		keyIssue3959Complete:                           "Alle Felder sind gewählt. Die Analyseseite prüft weiterhin Verfügbarkeit, Ihren Zugriff und kleine Gruppen.",
		keyIssue3959Decide:                             "Entscheiden: {text}",
		keyIssue3959HandOff:                            "In der Analyse öffnen",
		keyIssue3959HandOffAid:                         "Nicht gewählte Felder werden als offen übernommen.",
		keyIssue3959Close:                              "Schließen",
		keyIssue3959GapMetric:                          "Wählen Sie mindestens eine Kennzahl.",
		keyIssue3959GapField:                           "Wählen Sie eine Personengruppe.",
		keyIssue3959GapPeriod:                          "Wählen Sie einen Berichtszeitraum.",
		keyIssue3959GapGroup:                           "Wählen Sie eine Gruppierung oder keine Gruppierung.",
		issue3959MetricKey("time_to_fill"):             "Besetzungsdauer",
		issue3959MetricKey("offer_acceptance"):         "Angebotsannahme",
		issue3959MetricKey("attrition"):                "Fluktuation",
		issue3959MetricKey("headcount"):                "Personalbestand",
		issue3959MetricDecideKey("time_to_fill"):       "Legen Sie fest, wann die Zählung beginnt (Stelle genehmigt oder ausgeschrieben) und wann sie endet (Angebot angenommen oder Eintrittsdatum).",
		issue3959MetricDecideKey("offer_acceptance"):   "Legen Sie fest, ob noch offene Angebote zählen und ob ein erneut ausgestelltes Angebot nur einmal zählt.",
		issue3959MetricDecideKey("attrition"):          "Legen Sie fest, ob freiwillige, unfreiwillige oder alle Abgänge zählen und welcher Zeitraum gilt, zum Beispiel die ersten 90 Tage.",
		issue3959MetricDecideKey("headcount"):          "Legen Sie fest, ob externe Kräfte zählen und ob es ein einzelner Tag oder ein Durchschnitt ist.",
		issue3959ChoiceKey("population", "all"):        "Alle, die ich sehen darf",
		issue3959ChoiceKey("population", "mine"):       "Meine Organisation",
		issue3959ChoiceKey("population", "other"):      "Andere Gruppe, auf der Analyseseite festgelegt",
		issue3959ChoiceKey("period", "last_quarter"):   "Letztes Quartal",
		issue3959ChoiceKey("period", "last_12_months"): "Letzte 12 Monate",
		issue3959ChoiceKey("period", "year_to_date"):   "Seit Jahresbeginn",
		issue3959ChoiceKey("period", "custom"):         "Anderer Zeitraum, auf der Analyseseite festgelegt",
		issue3959ChoiceKey("grouping", "none"):         "Keine Gruppierung",
		issue3959ChoiceKey("grouping", "department"):   "Nach Abteilung",
		issue3959ChoiceKey("grouping", "location"):     "Nach Standort",
		issue3959ChoiceKey("grouping", "month"):        "Nach Monat",
	},
	"ar": {
		keyIssue3959Menu:                               "إعداد تحليل",
		keyIssue3959Title:                              "إعداد تحليل",
		keyIssue3959Private:                            "أنت وحدك ترى هذا الطلب. لا يُرسل شيء ولا يُنفَّذ حتى تفتح صفحة التحليلات.",
		keyIssue3959NoFigures:                          "لا تُجري الدردشة التحليل ولا تعرض أرقاماً. تُشغّل صفحة التحليلات تقارير معتمدة وتعرض التعريفات والمجموعة ووقت البيانات وتحدد ما يحق لك رؤيته، بما في ذلك إخفاء المجموعات الصغيرة جداً.",
		keyIssue3959From:                               "من الرسالة",
		keyIssue3959Metrics:                            "المؤشرات المذكورة في الرسالة",
		keyIssue3959MetricNote:                         "تُعرض فقط المؤشرات التي تذكرها الرسالة. تؤكد صفحة التحليلات أيّها موجود في الكتالوج المعتمد.",
		keyIssue3959Population:                         "الفئة",
		keyIssue3959Period:                             "فترة التقرير",
		keyIssue3959Grouping:                           "التجميع",
		keyIssue3959Pending:                            "ما زال يحتاج إلى قرار",
		keyIssue3959Complete:                           "تم اختيار كل الحقول. ما زالت صفحة التحليلات تتحقق من التوفر وصلاحياتك والمجموعات الصغيرة.",
		keyIssue3959Decide:                             "قرّر: {text}",
		keyIssue3959HandOff:                            "فتح في التحليلات",
		keyIssue3959HandOffAid:                         "تُنقل الحقول التي لم تختَرها على أنها مفتوحة.",
		keyIssue3959Close:                              "إغلاق",
		keyIssue3959GapMetric:                          "اختر مؤشراً واحداً على الأقل.",
		keyIssue3959GapField:                           "اختر الفئة.",
		keyIssue3959GapPeriod:                          "اختر فترة التقرير.",
		keyIssue3959GapGroup:                           "اختر التجميع أو بلا تجميع.",
		issue3959MetricKey("time_to_fill"):             "مدة شغل الوظيفة",
		issue3959MetricKey("offer_acceptance"):         "قبول العروض",
		issue3959MetricKey("attrition"):                "معدل ترك العمل",
		issue3959MetricKey("headcount"):                "عدد الموظفين",
		issue3959MetricDecideKey("time_to_fill"):       "حدّد متى تبدأ المدة (اعتماد الوظيفة أو نشرها) ومتى تنتهي (قبول العرض أو تاريخ المباشرة).",
		issue3959MetricDecideKey("offer_acceptance"):   "حدّد هل تُحتسب العروض المفتوحة وهل يُحتسب العرض المعاد إصداره مرة واحدة.",
		issue3959MetricDecideKey("attrition"):          "حدّد هل تُحتسب حالات المغادرة الطوعية أو غير الطوعية أو كلها، والفترة المعتمدة، مثل أول 90 يوماً.",
		issue3959MetricDecideKey("headcount"):          "حدّد هل يُحتسب المتعاقدون وهل هو يوم واحد أم متوسط.",
		issue3959ChoiceKey("population", "all"):        "كل من يحق لي رؤيته",
		issue3959ChoiceKey("population", "mine"):       "مؤسستي",
		issue3959ChoiceKey("population", "other"):      "فئة أخرى تُحدَّد في صفحة التحليلات",
		issue3959ChoiceKey("period", "last_quarter"):   "الربع الأخير",
		issue3959ChoiceKey("period", "last_12_months"): "آخر 12 شهراً",
		issue3959ChoiceKey("period", "year_to_date"):   "منذ بداية السنة",
		issue3959ChoiceKey("period", "custom"):         "فترة أخرى تُحدَّد في صفحة التحليلات",
		issue3959ChoiceKey("grouping", "none"):         "بلا تجميع",
		issue3959ChoiceKey("grouping", "department"):   "حسب القسم",
		issue3959ChoiceKey("grouping", "location"):     "حسب الموقع",
		issue3959ChoiceKey("grouping", "month"):        "حسب الشهر",
	},
}

// Issue3959Text is the copy of the analysis builder in a locale.
func Issue3959Text(locale, key string) string {
	return chatbug039Text(key, issue3959Copy[chatEmojiLocale(locale)][key], issue3959Copy["en-US"][key])
}

// analysisAvailable says whether this conversation may offer the builder: the
// page reaches the analytics surface, the viewer is signed in and the room is
// served by this company, so another company's room is never handed to this
// company's analytics.
func analysisAvailable(m Model) bool {
	if !m.Analysis.Enabled || m.CurrentUser == "" || m.SelectedID == "" {
		return false
	}
	c := m.selected()
	return c.HostTenantID == "" || c.HostTenantID == m.CurrentTenantID
}

// analysisMenuItem is the Prepare an analysis row of a message's menu. It is
// offered only where the page may reach the analytics surface, for a message
// with words that names at least one metric, outside a restricted case
// conversation.
func analysisMenuItem(m Model, msg Message) []ui.Node {
	if !analysisAvailable(m) || m.Analysis.Open == nil || msg.ID == "" || msg.Body == chat.RemovedByAdministrator ||
		strings.TrimSpace(msg.Body) == "" || issue3812Restricted(m) || len(AnalysisMetricsIn(msg.Body)) == 0 {
		return nil
	}
	return []ui.Node{chatux022Item("menu-item analysis-menu-item", "analysis-open", msg.ID, "insights", laneText(m, issue3959Copy, keyIssue3959Menu), false)}
}

func issue3959ChoiceGroup(m Model, field, headingKey string) ui.Node {
	s := m.Analysis
	heading := laneText(m, issue3959Copy, headingKey)
	nodes := []ui.Node{html.Strong(html.Props{Class: "analysis-heading", Text: heading})}
	for _, value := range analysisChoices[field] {
		label := laneText(m, issue3959Copy, issue3959ChoiceKey(field, value))
		nodes = append(nodes, html.Button(html.Props{Class: "button secondary small analysis-choice", Type: "button", Disabled: s.Choose == nil, Title: label,
			Data: map[string]string{"action": "analysis-choose", "id": field + ":" + value},
			Aria: map[string]string{"pressed": boolString(s.value(field) == value)}}, ui.Text(label)))
	}
	return html.Div(html.Props{Class: "analysis-group", Role: "group", Aria: map[string]string{"label": heading}}, nodes...)
}

// analysisBlock is the open builder under its message, or nil.
func analysisBlock(m Model, msg Message) []ui.Node {
	s := m.Analysis
	if !analysisAvailable(m) || s.PostID == "" || s.PostID != msg.ID {
		return nil
	}
	t := func(key string) string { return laneText(m, issue3959Copy, key) }

	metricNodes := []ui.Node{html.Strong(html.Props{Class: "analysis-heading", Text: t(keyIssue3959Metrics)})}
	for _, id := range AnalysisMetricsIn(msg.Body) {
		label := t(issue3959MetricKey(id))
		metricNodes = append(metricNodes, html.Button(html.Props{Class: "button secondary small analysis-choice", Type: "button", Disabled: s.Metric == nil, Title: label,
			Data: map[string]string{"action": "analysis-metric", "id": id},
			Aria: map[string]string{"pressed": boolString(s.Metrics[id])}}, ui.Text(label)))
	}
	children := []ui.Node{
		html.Strong(html.Props{Class: "analysis-title", Text: t(keyIssue3959Title)}),
		html.P(html.Props{Class: "analysis-private", Text: t(keyIssue3959Private)}),
		html.P(html.Props{Class: "analysis-nofigures", Text: t(keyIssue3959NoFigures)}),
		html.Div(html.Props{Class: "analysis-source"},
			html.Strong(html.Props{Class: "analysis-heading", Text: t(keyIssue3959From)}),
			html.P(html.Props{Class: "analysis-quote", Dir: "auto", Text: analysisQuote(msg.Body)})),
		html.Div(html.Props{Class: "analysis-group", Role: "group", Aria: map[string]string{"label": t(keyIssue3959Metrics)}}, metricNodes...),
		html.P(html.Props{Class: "analysis-note", Text: t(keyIssue3959MetricNote)}),
	}
	for _, id := range s.chosenMetrics() {
		children = append(children, html.P(html.Props{Class: "analysis-decide", Text: strings.ReplaceAll(t(keyIssue3959Decide), "{text}", t(issue3959MetricDecideKey(id)))}))
	}
	children = append(children,
		issue3959ChoiceGroup(m, "population", keyIssue3959Population),
		issue3959ChoiceGroup(m, "period", keyIssue3959Period),
		issue3959ChoiceGroup(m, "grouping", keyIssue3959Grouping))

	var gaps []string
	if len(s.chosenMetrics()) == 0 {
		gaps = append(gaps, keyIssue3959GapMetric)
	}
	for _, g := range []struct{ field, key string }{{"population", keyIssue3959GapField}, {"period", keyIssue3959GapPeriod}, {"grouping", keyIssue3959GapGroup}} {
		if !analysisKnownChoice(g.field, s.value(g.field)) {
			gaps = append(gaps, g.key)
		}
	}
	status := []ui.Node{}
	if len(gaps) == 0 {
		status = append(status, html.P(html.Props{Class: "analysis-complete", Text: t(keyIssue3959Complete)}))
	} else {
		items := make([]ui.Node, 0, len(gaps))
		for _, key := range gaps {
			items = append(items, html.Li(html.Props{Text: t(key)}))
		}
		status = append(status, html.Strong(html.Props{Class: "analysis-heading", Text: t(keyIssue3959Pending)}), html.Ul(html.Props{Class: "analysis-gaps"}, items...))
	}
	children = append(children,
		html.Div(html.Props{Class: "analysis-status", Role: "status", Aria: map[string]string{"live": "polite"}}, status...),
		html.P(html.Props{Class: "analysis-note", Text: t(keyIssue3959HandOffAid)}),
		html.Div(html.Props{Class: "analysis-actions"},
			actionButton("button primary small", "analysis-handoff", msg.ID, t(keyIssue3959HandOff), s.HandOff == nil, ui.Text(t(keyIssue3959HandOff))),
			actionButton("button secondary small", "analysis-close", msg.ID, t(keyIssue3959Close), s.Close == nil, ui.Text(t(keyIssue3959Close)))))
	return []ui.Node{html.Div(html.Props{ID: "analysis-" + msg.ID, Class: "analysis-builder", Role: "region", TabIndex: -1, Aria: map[string]string{"label": t(keyIssue3959Title)}}, children...)}
}

// analysisQuote is the message as plain text, bounded so a long message does
// not fill the builder.
func analysisQuote(body string) string {
	runes := []rune(strings.TrimSpace(chatDisplayBody(body)))
	if len(runes) > 400 {
		return string(runes[:400]) + "…"
	}
	return string(runes)
}

// analysisAction runs a control of the builder.
func (m Model) analysisAction(action, id string) {
	s := m.Analysis
	switch action {
	case "analysis-open":
		if !analysisAvailable(m) || s.Open == nil || id == "" {
			return
		}
		if s.PostID == id && s.Close != nil {
			s.Close()
			return
		}
		s.Open(id)
	case "analysis-close":
		if s.Close != nil {
			s.Close()
		}
	case "analysis-metric":
		if s.Metric != nil && s.PostID != "" && analysisKnownMetric(id) {
			s.Metric(id)
		}
	case "analysis-choose":
		field, value, ok := strings.Cut(id, ":")
		if s.Choose != nil && s.PostID != "" && ok && analysisKnownChoice(field, value) {
			s.Choose(field, value)
		}
	case "analysis-handoff":
		if analysisAvailable(m) && s.HandOff != nil && s.PostID != "" && id == s.PostID {
			s.HandOff(m.SelectedID, id, s.request())
		}
	}
}

// Issue3959Styles is the builder's layout. Logical properties only, so it holds
// in right-to-left; targets are 44px on a phone.
const Issue3959Styles = `
.analysis-builder{margin-block-start:8px;padding:10px;border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);min-inline-size:0;display:grid;gap:8px}
.analysis-title{font-size:.875rem}
.analysis-private,.analysis-nofigures,.analysis-note,.analysis-decide,.analysis-complete,.analysis-quote{margin:0;font-size:.8125rem;overflow-wrap:anywhere}
.analysis-note,.analysis-nofigures{color:var(--hcm-color-text-muted)}
.analysis-quote{padding-inline-start:8px;border-inline-start:3px solid var(--hcm-color-border);white-space:pre-wrap}
.analysis-group{display:flex;flex-wrap:wrap;gap:6px;align-items:center;min-inline-size:0}
.analysis-heading{font-size:.8125rem;flex:1 0 100%}
.analysis-choice[aria-pressed="true"]{font-weight:600;outline:2px solid var(--hcm-color-focus,currentColor);outline-offset:-2px}
.analysis-gaps{margin:0;padding-inline-start:20px;font-size:.8125rem}
.analysis-actions{display:flex;flex-wrap:wrap;gap:8px}
@media(max-width:767px){.analysis-builder button{min-block-size:44px;min-inline-size:44px}}
`
