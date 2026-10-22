package chatui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATLANG-006 on the administration page: what is measured about each language
// pair, the readers' reports of wrong translations as a list an administrator
// resolves, and the glossary's version and editor per language. None of it holds
// message text.

// TranslationAdminPair is one language pair's monitoring over the period.
type TranslationAdminPair struct {
	Source         string `json:"source"`
	Target         string `json:"target"`
	Translations   int64  `json:"translations"`
	Failures       int64  `json:"failures"`
	LowConfidence  int64  `json:"low_confidence"`
	LatencyMsTotal int64  `json:"latency_ms_total"`
	LatencyMsMax   int64  `json:"latency_ms_max"`
	Reports        int64  `json:"reports"`
}

// TranslationAdminReport is one reader's report of a wrong translation: the
// pair, the reader's suggestion and what was done about it.
type TranslationAdminReport struct {
	ID         string `json:"id"`
	Source     string `json:"source"`
	Target     string `json:"target"`
	Suggestion string `json:"suggestion"`
	At         string `json:"at"`
	State      string `json:"state"`
	Note       string `json:"note,omitempty"`
}

// TranslationAdminGlossaryLanguage is a language's glossary terms and who
// edited them last.
type TranslationAdminGlossaryLanguage struct {
	Language   string `json:"language"`
	Terms      int    `json:"terms"`
	LastEditor string `json:"last_editor,omitempty"`
}

var translationAdminW10Copy = map[string][3]string{
	"mon_title":        {"Translation quality, last 30 days", "Übersetzungsqualität, letzte 30 Tage", "جودة الترجمة، آخر 30 يوماً"},
	"mon_hint":         {"Counts and times for each pair of languages. No message text is read or shown here.", "Anzahlen und Zeiten für jedes Sprachpaar. Hier wird kein Nachrichtentext gelesen oder angezeigt.", "أعداد وأزمنة لكل زوج من اللغات. لا يُقرأ أو يُعرض أي نص رسائل هنا."},
	"mon_empty":        {"Nothing has been translated yet.", "Es wurde noch nichts übersetzt.", "لم تُترجم أي رسالة بعد."},
	"mon_pair":         {"Languages", "Sprachen", "اللغات"},
	"mon_translations": {"Translated", "Übersetzt", "المترجمة"},
	"mon_failures":     {"Failed", "Fehlgeschlagen", "الفاشلة"},
	"mon_low":          {"Low confidence", "Geringe Sicherheit", "ثقة منخفضة"},
	"mon_avg":          {"Average time", "Durchschnittliche Zeit", "متوسط الزمن"},
	"mon_max":          {"Slowest", "Langsamste", "الأبطأ"},
	"mon_reports":      {"Reported wrong", "Als falsch gemeldet", "بلاغات الخطأ"},
	"mon_ms":           {"{n} ms", "{n} ms", "{n} مللي ثانية"},
	"mon_pair_text":    {"{from} to {to}", "{from} nach {to}", "من {from} إلى {to}"},
	"rep_title":        {"Reports of wrong translations", "Gemeldete falsche Übersetzungen", "بلاغات الترجمات الخاطئة"},
	"rep_hint":         {"Readers report a translation from the message itself. You see the languages and what they suggested, not the message.", "Leser melden eine Übersetzung direkt an der Nachricht. Sie sehen die Sprachen und den Vorschlag, nicht die Nachricht.", "يبلّغ القراء عن الترجمة من الرسالة نفسها. ترى اللغات وما اقترحوه، لا الرسالة."},
	"rep_empty":        {"No reports.", "Keine Meldungen.", "لا توجد بلاغات."},
	"rep_none":         {"No suggestion given", "Kein Vorschlag angegeben", "لم يُقدَّم اقتراح"},
	"rep_suggested":    {"Suggested:", "Vorschlag:", "الاقتراح:"},
	"rep_open":         {"Open", "Offen", "مفتوح"},
	"rep_resolved":     {"Resolved", "Erledigt", "تمت المعالجة"},
	"rep_dismissed":    {"Dismissed", "Verworfen", "تم التجاهل"},
	"rep_resolve":      {"Mark resolved", "Als erledigt markieren", "حدّد كمعالَج"},
	"rep_dismiss":      {"Dismiss", "Verwerfen", "تجاهل"},
	"rep_resolve_a":    {"Mark this report resolved", "Diese Meldung als erledigt markieren", "حدّد هذا البلاغ كمعالَج"},
	"rep_dismiss_a":    {"Dismiss this report", "Diese Meldung verwerfen", "تجاهل هذا البلاغ"},
	"glossary_version": {"Glossary version {n}", "Glossarversion {n}", "إصدار المسرد {n}"},
	"glossary_editor":  {"{language}: {n} terms, last edited by {person}", "{language}: {n} Begriffe, zuletzt bearbeitet von {person}", "{language}: {n} مصطلحات، آخر تعديل بواسطة {person}"},
	"glossary_nobody":  {"{language}: {n} terms", "{language}: {n} Begriffe", "{language}: {n} مصطلحات"},
}

func init() {
	// The administration's copy table is one map; these keys join it.
	for key, row := range translationAdminW10Copy {
		translationAdminCopy[key] = row
	}
}

func translationPairName(t func(string) string, source, target string) string {
	from, to := t(source), t(target)
	if from == "" {
		from = strings.ToUpper(source)
	}
	if to == "" {
		to = strings.ToUpper(target)
	}
	return strings.NewReplacer("{from}", from, "{to}", to).Replace(t("mon_pair_text"))
}

func translationMs(t func(string) string, n int64) string {
	return strings.ReplaceAll(t("mon_ms"), "{n}", strconv.FormatInt(n, 10))
}

// translationMonitoring is the table of language pairs: what was translated,
// what failed, what the engine was unsure of, how long it took and how often
// readers reported it.
func translationMonitoring(m TranslationAdminModel, t func(string) string) ui.Node {
	d := m.Data
	children := []ui.Node{html.H3(html.Props{ID: "chatlangadmin-monitor-title", Text: t("mon_title")}), html.P(html.Props{Class: "field-hint", Text: t("mon_hint")})}
	if len(d.Pairs) == 0 {
		children = append(children, html.P(html.Props{Text: t("mon_empty")}))
		return html.Section(html.Props{Class: "chatlangadmin-monitor", Data: map[string]string{"chatlangadmin": "monitor"}}, children...)
	}
	head := html.Tr(html.Props{},
		html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("mon_pair")}), html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("mon_translations")}), html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("mon_failures")}),
		html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("mon_low")}), html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("mon_avg")}), html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("mon_max")}), html.Th(html.Props{Raw: map[string]any{"scope": "col"}, Text: t("mon_reports")}))
	rows := []ui.Node{}
	for _, pair := range d.Pairs {
		attempts := pair.Translations + pair.Failures
		average := int64(0)
		if attempts > 0 {
			average = pair.LatencyMsTotal / attempts
		}
		num := func(n int64) ui.Node { return html.Td(html.Props{Class: "chatlangadmin-num", Text: m.number(n)}) }
		rows = append(rows, html.Tr(html.Props{Data: map[string]string{"pair": pair.Source + "-" + pair.Target}},
			html.Th(html.Props{Raw: map[string]any{"scope": "row"}, Dir: "auto", Text: translationPairName(t, pair.Source, pair.Target)}),
			num(pair.Translations), num(pair.Failures), num(pair.LowConfidence),
			html.Td(html.Props{Class: "chatlangadmin-num", Text: translationMs(t, average)}), html.Td(html.Props{Class: "chatlangadmin-num", Text: translationMs(t, pair.LatencyMsMax)}), num(pair.Reports)))
	}
	children = append(children, html.Div(html.Props{Class: "chatlangadmin-table-wrap"},
		html.Table(html.Props{Class: "chatlangadmin-table", Aria: map[string]string{"labelledby": "chatlangadmin-monitor-title"}}, html.Thead(html.Props{}, head), html.Tbody(html.Props{}, rows...))))
	return html.Section(html.Props{Class: "chatlangadmin-monitor", Data: map[string]string{"chatlangadmin": "monitor"}}, children...)
}

// number writes a count in the person's digits.
func (m TranslationAdminModel) number(n int64) string {
	return chatNumeral(m.Locale, strconv.FormatInt(n, 10))
}

// translationReports is the queue of readers' reports: open ones first, each
// with the pair, the suggestion, and two actions that close it.
func translationReports(m TranslationAdminModel, t func(string) string) ui.Node {
	d := m.Data
	children := []ui.Node{html.H3(html.Props{ID: "chatlangadmin-reports-title", Text: t("rep_title")}), html.P(html.Props{Class: "field-hint", Text: t("rep_hint")})}
	if len(d.Reports) == 0 {
		children = append(children, html.P(html.Props{Text: t("rep_empty")}))
		return html.Section(html.Props{Class: "chatlangadmin-reports", Data: map[string]string{"chatlangadmin": "reports"}}, children...)
	}
	items := []ui.Node{}
	for _, report := range d.Reports {
		suggestion := report.Suggestion
		if suggestion == "" || suggestion == ChatlangReportNoSuggestion {
			suggestion = t("rep_none")
		}
		state := t("rep_" + report.State)
		line := []ui.Node{
			html.Strong(html.Props{Dir: "auto", Text: translationPairName(t, report.Source, report.Target)}),
			html.Span(html.Props{Class: "chatlangadmin-report-state", Data: map[string]string{"state": report.State}, Text: state}),
			html.P(html.Props{Class: "chatlangadmin-report-text", Dir: "auto"}, html.Span(html.Props{Text: t("rep_suggested") + " "}), html.Span(html.Props{Dir: "auto", Text: suggestion})),
		}
		if report.State == "open" {
			line = append(line, html.Div(html.Props{Class: "chatlangadmin-actions"},
				html.Button(html.Props{Type: "button", Class: "button secondary", Text: t("rep_resolve"), Disabled: m.Loading || m.ResolveReport.Value() == nil, OnClick: m.ResolveReport, Data: map[string]string{"report": report.ID, "state": "resolved"}, Aria: map[string]string{"label": t("rep_resolve_a")}}),
				html.Button(html.Props{Type: "button", Class: "button secondary", Text: t("rep_dismiss"), Disabled: m.Loading || m.ResolveReport.Value() == nil, OnClick: m.ResolveReport, Data: map[string]string{"report": report.ID, "state": "dismissed"}, Aria: map[string]string{"label": t("rep_dismiss_a")}})))
		}
		items = append(items, html.Li(html.Props{Class: "chatlangadmin-report", Data: map[string]string{"report": report.ID}}, line...))
	}
	children = append(children, html.Ul(html.Props{Class: "chatlangadmin-terms"}, items...))
	return html.Section(html.Props{Class: "chatlangadmin-reports", Data: map[string]string{"chatlangadmin": "reports"}}, children...)
}

// translationGlossaryOwners says the glossary's version and, per language, how
// many terms there are and who edited them last.
func translationGlossaryOwners(m TranslationAdminModel, t func(string) string) ui.Node {
	d := m.Data
	if d.GlossaryVersion == 0 && len(d.GlossaryLanguages) == 0 {
		return nil
	}
	lines := []ui.Node{html.P(html.Props{Class: "field-hint", Data: map[string]string{"chatlangadmin": "glossary-version"}, Text: strings.ReplaceAll(t("glossary_version"), "{n}", m.number(d.GlossaryVersion))})}
	for _, language := range d.GlossaryLanguages {
		key := "glossary_editor"
		if language.LastEditor == "" {
			key = "glossary_nobody"
		}
		text := strings.NewReplacer("{language}", t(language.Language), "{n}", m.number(int64(language.Terms)), "{person}", language.LastEditor).Replace(t(key))
		lines = append(lines, html.P(html.Props{Class: "field-hint", Dir: "auto", Data: map[string]string{"chatlangadmin-glossary-language": language.Language}, Text: text}))
	}
	return html.Div(html.Props{Class: "chatlangadmin-glossary-owners"}, lines...)
}

// ChatlangMonitorStyles is the monitoring table, the report queue and the
// glossary lines, from the administration's own tokens.
const ChatlangMonitorStyles = `.chatlangadmin-table-wrap{overflow-x:auto;max-inline-size:100%}.chatlangadmin-table{border-collapse:collapse;inline-size:100%;font-size:.875rem}.chatlangadmin-table th,.chatlangadmin-table td{padding:6px 8px;border-block-end:1px solid var(--hcm-color-border);text-align:start;white-space:nowrap}.chatlangadmin-table thead th{font-weight:600;color:var(--hcm-color-text-muted)}.chatlangadmin-num{font-variant-numeric:tabular-nums}` +
	`.chatlangadmin-monitor,.chatlangadmin-reports{display:grid;gap:8px}.chatlangadmin-report{display:grid;gap:4px;padding:8px 0;border-block-end:1px solid var(--hcm-color-border)}.chatlangadmin-report-text{margin:0}.chatlangadmin-report-state{color:var(--hcm-color-text-muted);font-size:.8125rem}.chatlangadmin-report-state[data-state=open]{color:var(--hcm-color-text);font-weight:600}`
