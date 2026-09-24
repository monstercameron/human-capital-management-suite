package docsdiagram

import (
	"math"
	"strconv"
	"strings"
)

// locale carries the few words and number conventions a diagram generates
// itself; everything else on a diagram is the author's text.
type locale struct {
	tag      string
	decimal  string
	group    string
	phrases  map[string]string
	months   [12]string
	listJoin string
}

var enPhrases = map[string]string{
	"flowchart":        "Flowchart",
	"flow.summary":     "Flowchart with {n} steps and {e} connections: {list}",
	"flow.connects":    "{from} → {to}",
	"flow.in":          "in {group}",
	"flow.alone":       "{node} (no connections)",
	"flow.list":        "Connections",
	"sequence":         "Sequence diagram",
	"seq.summary":      "Sequence diagram between {list} with {n} messages.",
	"seq.list":         "Messages in order",
	"seq.note":         "Note over {who}: {text}",
	"seq.block":        "{kind}: {text}",
	"seq.end":          "End of {kind}",
	"pie":              "Pie chart",
	"pie.summary":      "Pie chart of {n} parts totalling {total}: {list}",
	"xy":               "Chart",
	"xy.summary":       "Chart with {n} categories and {s} series: {list}. Values range from {min} to {max}.",
	"xy.bar":           "bars",
	"xy.line":          "line",
	"xy.line.right":    "line, right axis",
	"xy.series":        "Series {n}",
	"gantt":            "Gantt chart",
	"gantt.summary":    "Gantt chart with {n} tasks in {s} sections from {start} to {end}.",
	"timeline":         "Timeline",
	"tl.summary":       "Timeline with {n} periods: {list}",
	"journey":          "User journey",
	"journey.summary":  "User journey with {n} tasks in {s} sections. Average score {avg} of 5.",
	"col.label":        "Label",
	"col.value":        "Value",
	"col.percent":      "Percent",
	"col.category":     "Category",
	"col.section":      "Section",
	"col.task":         "Task",
	"col.start":        "Start",
	"col.end":          "End",
	"col.status":       "Status",
	"col.period":       "Period",
	"col.events":       "Events",
	"col.score":        "Score",
	"col.actors":       "Actors",
	"status.done":      "done",
	"status.active":    "active",
	"status.crit":      "critical",
	"status.milestone": "milestone",
	"status.planned":   "planned",
	"more":             "and {n} more",
	"data":             "Data",
}

var dePhrases = map[string]string{
	"flowchart":        "Flussdiagramm",
	"flow.summary":     "Flussdiagramm mit {n} Schritten und {e} Verbindungen: {list}",
	"flow.connects":    "{from} → {to}",
	"flow.in":          "in {group}",
	"flow.alone":       "{node} (keine Verbindungen)",
	"flow.list":        "Verbindungen",
	"sequence":         "Sequenzdiagramm",
	"seq.summary":      "Sequenzdiagramm zwischen {list} mit {n} Nachrichten.",
	"seq.list":         "Nachrichten in Reihenfolge",
	"seq.note":         "Notiz zu {who}: {text}",
	"seq.block":        "{kind}: {text}",
	"seq.end":          "Ende von {kind}",
	"pie":              "Kreisdiagramm",
	"pie.summary":      "Kreisdiagramm mit {n} Teilen, gesamt {total}: {list}",
	"xy":               "Diagramm",
	"xy.summary":       "Diagramm mit {n} Kategorien und {s} Reihen: {list}. Werte von {min} bis {max}.",
	"xy.bar":           "Balken",
	"xy.line":          "Linie",
	"xy.line.right":    "Linie, rechte Achse",
	"xy.series":        "Reihe {n}",
	"gantt":            "Gantt-Diagramm",
	"gantt.summary":    "Gantt-Diagramm mit {n} Aufgaben in {s} Abschnitten von {start} bis {end}.",
	"timeline":         "Zeitleiste",
	"tl.summary":       "Zeitleiste mit {n} Zeiträumen: {list}",
	"journey":          "Nutzerreise",
	"journey.summary":  "Nutzerreise mit {n} Aufgaben in {s} Abschnitten. Durchschnittliche Bewertung {avg} von 5.",
	"col.label":        "Bezeichnung",
	"col.value":        "Wert",
	"col.percent":      "Prozent",
	"col.category":     "Kategorie",
	"col.section":      "Abschnitt",
	"col.task":         "Aufgabe",
	"col.start":        "Beginn",
	"col.end":          "Ende",
	"col.status":       "Status",
	"col.period":       "Zeitraum",
	"col.events":       "Ereignisse",
	"col.score":        "Bewertung",
	"col.actors":       "Beteiligte",
	"status.done":      "erledigt",
	"status.active":    "aktiv",
	"status.crit":      "kritisch",
	"status.milestone": "Meilenstein",
	"status.planned":   "geplant",
	"more":             "und {n} weitere",
	"data":             "Daten",
}

var arPhrases = map[string]string{
	"flowchart":        "مخطط انسيابي",
	"flow.summary":     "مخطط انسيابي من {n} خطوات و{e} روابط: {list}",
	"flow.connects":    "{from} ← {to}",
	"flow.in":          "ضمن {group}",
	"flow.alone":       "{node} (بلا روابط)",
	"flow.list":        "الروابط",
	"sequence":         "مخطط تسلسلي",
	"seq.summary":      "مخطط تسلسلي بين {list} يضم {n} رسائل.",
	"seq.list":         "الرسائل بالترتيب",
	"seq.note":         "ملاحظة على {who}: {text}",
	"seq.block":        "{kind}: {text}",
	"seq.end":          "نهاية {kind}",
	"pie":              "مخطط دائري",
	"pie.summary":      "مخطط دائري من {n} أجزاء مجموعها {total}: {list}",
	"xy":               "مخطط",
	"xy.summary":       "مخطط بـ {n} فئات و{s} سلاسل: {list}. القيم من {min} إلى {max}.",
	"xy.bar":           "أعمدة",
	"xy.line":          "خط",
	"xy.line.right":    "خط، المحور الأيمن",
	"xy.series":        "السلسلة {n}",
	"gantt":            "مخطط جانت",
	"gantt.summary":    "مخطط جانت يضم {n} مهام في {s} أقسام من {start} إلى {end}.",
	"timeline":         "خط زمني",
	"tl.summary":       "خط زمني من {n} فترات: {list}",
	"journey":          "رحلة المستخدم",
	"journey.summary":  "رحلة مستخدم من {n} مهام في {s} أقسام. متوسط التقييم {avg} من 5.",
	"col.label":        "التسمية",
	"col.value":        "القيمة",
	"col.percent":      "النسبة",
	"col.category":     "الفئة",
	"col.section":      "القسم",
	"col.task":         "المهمة",
	"col.start":        "البداية",
	"col.end":          "النهاية",
	"col.status":       "الحالة",
	"col.period":       "الفترة",
	"col.events":       "الأحداث",
	"col.score":        "التقييم",
	"col.actors":       "المشاركون",
	"status.done":      "منجزة",
	"status.active":    "نشطة",
	"status.crit":      "حرجة",
	"status.milestone": "معلم",
	"status.planned":   "مخططة",
	"more":             "و{n} أخرى",
	"data":             "البيانات",
}

var (
	enMonths = [12]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	deMonths = [12]string{"Jan.", "Feb.", "März", "Apr.", "Mai", "Juni", "Juli", "Aug.", "Sept.", "Okt.", "Nov.", "Dez."}
	arMonths = [12]string{"يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو", "يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر", "ديسمبر"}
)

func localeFor(tag string) locale {
	base := strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexAny(base, "-_"); i >= 0 {
		base = base[:i]
	}
	switch base {
	case "de":
		return locale{tag: "de", decimal: ",", group: ".", phrases: dePhrases, months: deMonths, listJoin: ", "}
	case "ar":
		return locale{tag: "ar", decimal: "٫", group: "٬", phrases: arPhrases, months: arMonths, listJoin: "، "}
	}
	return locale{tag: "en", decimal: ".", group: ",", phrases: enPhrases, months: enMonths, listJoin: ", "}
}

// t fills a phrase's {name} slots from pairs of name, value.
func (l locale) t(key string, pairs ...string) string {
	s, ok := l.phrases[key]
	if !ok {
		s = enPhrases[key]
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		s = strings.ReplaceAll(s, "{"+pairs[i]+"}", pairs[i+1])
	}
	return s
}

func (l locale) more(n int) string { return l.t("more", "n", strconv.Itoa(n)) }

// number formats v with up to maxDecimals decimals and grouped thousands.
func (l locale) number(v float64, maxDecimals int) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	neg := v < 0
	v = math.Abs(v)
	s := strconv.FormatFloat(v, 'f', maxDecimals, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	intPart, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteString(l.group)
		}
		b.WriteRune(r)
	}
	out := b.String()
	if frac != "" {
		out += l.decimal + frac
	}
	if neg && out != "0" {
		out = "-" + out
	}
	return out
}

func (l locale) percent(v float64) string {
	return l.number(v, 1) + "%"
}
