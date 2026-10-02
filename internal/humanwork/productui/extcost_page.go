package productui

import (
	"fmt"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/extcost"
	"net/url"
	"sort"
	"strings"
	"time"
)

const ExtcostPageID PageID = "external-usage"
const ExtcostPagePath = "/workspace/app/admin/external-usage"

type ExtcostPageProps struct {
	View                    View
	Allowed, Loading, Error bool
	Report                  extcost.Report
	Filter                  extcost.Filter
	Names                   map[string]string
}
type ExtcostPageRenderer struct{ Project func(View) ExtcostPageProps }

func (r ExtcostPageRenderer) Render(v View) ui.Node {
	if r.Project == nil {
		return BuildExternalUsagePage(ExtcostPageProps{View: v})
	}
	p := r.Project(v)
	p.View = v
	return BuildExternalUsagePage(p)
}
func ExtcostPageModule(r ExtcostPageRenderer) PageModule {
	return PageModule{Definition: PageDefinition{ID: ExtcostPageID, Route: ExtcostPagePath, Label: "External usage", Title: "External usage", ParentNav: PageAdmin, RenderOrder: 179, OwnsHeading: true, Admitted: true, NavigationPublished: true}, Render: r, Access: PageAccessPolicy{Audience: PageAudienceDenied}, RouteProfile: RouteProfileAdmin, DataProfile: DataProfileNone}
}
func BuildExternalUsagePage(p ExtcostPageProps) ui.Node {
	locale := p.View.Locale
	if locale.Resolved == "" {
		locale = ResolveProductLocale(DefaultProductLocale)
	}
	text := func(k string) string { return extcostText(locale, k) }
	state := "ready"
	var nodes []ui.Node
	switch {
	case !p.Allowed:
		state = "denied"
		nodes = append(nodes, html.Section(html.Props{Role: "status"}, html.H2(html.Props{}, ui.Text(text("denied"))), html.P(html.Props{}, ui.Text(text("denied_help")))))
	case p.Loading:
		state = "loading"
		nodes = append(nodes, html.P(html.Props{Role: "status", Raw: map[string]any{"aria-live": "polite"}}, ui.Text(text("loading"))), html.A(html.Props{Href: ExtcostHref(locale, p.Filter, false)}, ui.Text(text("refresh"))))
	case p.Error:
		state = "error"
		nodes = append(nodes, html.P(html.Props{Role: "alert"}, ui.Text(text("error"))), html.A(html.Props{Class: "button primary", Href: ExtcostHref(locale, p.Filter, false)}, ui.Text(text("refresh"))))
	default:
		nodes = append(nodes, extcostFilters(locale, p.Filter))
		if p.Report.Truncated {
			nodes = append(nodes, html.P(html.Props{Role: "status"}, ui.Text(text("truncated"))))
		}
		if len(p.Report.Lines) == 0 && len(p.Report.Pending) == 0 {
			state = "empty"
			nodes = append(nodes, html.Section(html.Props{Role: "status"}, html.H2(html.Props{}, ui.Text(text("empty"))), html.P(html.Props{}, ui.Text(text("empty_help")))))
		}
		totals := []ui.Node{html.H2(html.Props{}, ui.Text(text("total")))}
		for _, g := range p.Report.Groups {
			if g.Kind == "total" {
				totals = append(totals, html.P(html.Props{}, ui.Text(extcostAmount(locale, g.CostMicros, g.Currency)+" · "+locale.FormatNumber(fmt.Sprint(g.Calls), 0)+" "+text("calls"))))
			}
		}
		nodes = append(nodes, html.Section(html.Props{Class: "surface"}, totals...))
		budgets := []ui.Node{html.H2(html.Props{}, ui.Text(text("budget")))}
		if len(p.Report.Budgets) == 0 {
			budgets = append(budgets, html.P(html.Props{}, ui.Text(text("budget_missing"))))
		}
		for _, b := range p.Report.Budgets {
			budgets = append(budgets, html.P(html.Props{}, ui.Text(text(b.Budget.Scope.Kind)+": "+extcostAmount(locale, b.SpentMicros, b.Budget.Currency)+" / "+extcostAmount(locale, b.Budget.LimitMicros, b.Budget.Currency)+" · "+text("reserved")+": "+extcostAmount(locale, b.ReservedMicros, b.Budget.Currency))))
			if b.Warning {
				budgets = append(budgets, html.P(html.Props{Role: "status"}, ui.Text(text("warning"))))
			}
		}
		nodes = append(nodes, html.Section(html.Props{Class: "surface"}, budgets...))
		for _, kind := range []string{"day", "provider", "feature", "purpose", "agent", "workflow", "person"} {
			groupNodes := []ui.Node{html.H2(html.Props{}, ui.Text(text("by_"+kind)))}
			for _, g := range p.Report.Groups {
				if g.Kind != kind {
					continue
				}
				f := p.Filter
				switch kind {
				case "provider":
					f.Provider = g.Name
				case "feature":
					f.Feature = g.Name
				case "purpose":
					f.Purpose = g.Name
				case "agent":
					f.Agent = g.Name
				case "workflow":
					f.Workflow = g.Name
				case "person":
					f.Person = g.Name
				case "day":
					f.From, _ = time.Parse("2006-01-02", g.Name)
					f.Until = f.From.AddDate(0, 0, 1)
				}
				label := extcostName(locale, p, kind, g.Name)
				groupNodes = append(groupNodes, html.Div(html.Props{Class: "extcost-row"}, html.A(html.Props{Href: ExtcostHref(locale, f, false), Aria: map[string]string{"label": text("view_calls") + ": " + label}}, ui.Text(label)), html.Span(html.Props{}, ui.Text(extcostAmount(locale, g.CostMicros, g.Currency))), html.Span(html.Props{}, ui.Text(locale.FormatNumber(fmt.Sprint(g.Calls), 0)+" "+text("calls")))))
			}
			if len(groupNodes) > 1 {
				nodes = append(nodes, html.Section(html.Props{Class: "surface"}, groupNodes...))
			}
		}
		nodes = append(nodes, extcostCalls(locale, p))
		if len(p.Report.Pending) > 0 {
			rows := []ui.Node{html.H2(html.Props{}, ui.Text(text("pending")))}
			for _, r := range p.Report.Pending {
				f := p.Filter
				f.Key = r.Call.Key
				rows = append(rows, html.Article(html.Props{Class: "extcost-call"}, html.A(html.Props{Href: ExtcostHref(locale, f, false)}, ui.Text(text("view_call")+" · "+r.Call.Provider)), html.P(html.Props{}, ui.Text(text("pending_help")+" · "+text("maximum")+": "+extcostAmount(locale, r.MaximumMicros, r.Currency)))))
			}
			nodes = append(nodes, html.Section(html.Props{Class: "surface"}, rows...))
		}

		for _, s := range p.Report.Statistics {
			label := extcostName(locale, p, "workflow", s.Workflow)
			if s.Node != "" {
				label += " / " + extcostName(locale, p, "node", s.Node)
			}
			nodes = append(nodes, html.P(html.Props{}, ui.Text(label+" · "+text("median")+": "+extcostAmount(locale, s.MedianMicros, s.Currency)+" · "+text("p95")+": "+extcostAmount(locale, s.P95Micros, s.Currency))))
		}
		nodes = append(nodes, html.A(html.Props{Href: ExtcostHref(locale, p.Filter, true), Raw: map[string]any{"download": "external-usage.csv"}}, ui.Text(text("export"))))
	}
	return ProductPageFrame(ProductPageFrameProps{Class: "extcost-page", Dir: string(locale.Direction), Title: text("title"), TitleID: "extcost-title", Aria: map[string]string{"labelledby": "extcost-title"}, Raw: map[string]any{"data-extcost-state": state}, Body: []ui.Node{html.Div(html.Props{Class: "extcost-content"}, nodes...)}})
}
func extcostFilters(locale LocaleContext, f extcost.Filter) ui.Node {
	fields := []ui.Node{html.Input(html.Props{Type: "hidden", Raw: map[string]any{"name": "locale", "value": locale.Resolved}})}
	values := [][2]string{{"provider", f.Provider}, {"operation", f.Operation}, {"feature", f.Feature}, {"purpose", f.Purpose}, {"agent", f.Agent}, {"workflow", f.Workflow}, {"person", f.Person}, {"search", f.Search}, {"from", extcostDateInput(f.From)}, {"until", extcostDateInput(f.Until)}}
	for _, v := range values {
		typ := "text"
		if v[0] == "from" || v[0] == "until" {
			typ = "date"
		}
		id := "extcost-filter-" + v[0]
		fields = append(fields, html.Div(html.Props{Class: "extcost-field"}, html.Label(html.Props{For: id}, ui.Text(extcostText(locale, v[0]))), html.Input(html.Props{ID: id, Type: typ, Raw: map[string]any{"name": v[0], "value": v[1], "maxlength": "256"}})))
	}
	fields = append(fields, html.Button(html.Props{Type: "submit", Class: "button primary"}, ui.Text(extcostText(locale, "apply"))))
	return html.Form(html.Props{Class: "extcost-filters", Raw: map[string]any{"method": "get", "action": ExtcostPagePath, "aria-label": extcostText(locale, "filters")}}, fields...)
}
func extcostDateInput(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format("2006-01-02")
}
func ExtcostHref(locale LocaleContext, f extcost.Filter, export bool) string {
	q := url.Values{"locale": {locale.Resolved}}
	for k, v := range map[string]string{"provider": f.Provider, "operation": f.Operation, "feature": f.Feature, "purpose": f.Purpose, "agent": f.Agent, "workflow": f.Workflow, "person": f.Person, "search": f.Search, "from": extcostDateInput(f.From), "until": extcostDateInput(f.Until), "key": f.Key, "cause": f.Cause} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if export {
		q.Set("export", "csv")
	}
	return ExtcostPagePath + "?" + q.Encode()
}
func extcostAmount(locale LocaleContext, micros int64, currency string) string {
	negative := micros < 0
	whole, part := micros/1_000_000, micros%1_000_000
	if whole < 0 {
		whole = -whole
	}
	if part < 0 {
		part = -part
	}
	decimal := fmt.Sprintf("%d.%06d", whole, part)
	if negative {
		decimal = "-" + decimal
	}
	return locale.FormatMoney(decimal, currency, 6)
}
func extcostName(locale LocaleContext, p ExtcostPageProps, kind, id string) string {
	if n := strings.TrimSpace(p.Names[kind+":"+id]); n != "" {
		return n
	}
	if kind == "day" {
		if at, err := time.Parse("2006-01-02", id); err == nil {
			return locale.FormatDate(at)
		}
	}
	if kind == "provider" {
		return id
	}
	if kind == "purpose" || kind == "feature" {
		if text := extcostText(locale, id); text != "" {
			return text
		}
	}
	return extcostText(locale, kind)
}
func extcostCalls(locale LocaleContext, p ExtcostPageProps) ui.Node {
	if len(p.Report.Lines) == 0 {
		return nil
	}
	nodes := []ui.Node{html.H2(html.Props{}, ui.Text(extcostText(locale, "calls")))}
	for _, l := range p.Report.Lines {
		f := p.Filter
		f.Key = l.Call.Key
		amount := extcostAmount(locale, l.CostMicros, l.Currency)
		if l.ScheduleVersion == "" || l.Currency == "" {
			amount = extcostText(locale, "unpriced")
		}
		quality := "measured"
		if l.Measurement.Estimated {
			quality = "estimated"
		}
		keys := make([]string, 0, len(l.Measurement.Units))
		for unit := range l.Measurement.Units {
			keys = append(keys, string(unit))
		}
		sort.Strings(keys)
		detail := []ui.Node{html.Summary(html.Props{}, ui.Text(extcostText(locale, "view_units")))}
		for _, unit := range keys {
			detail = append(detail, html.P(html.Props{}, ui.Text(extcostText(locale, unit)+": "+locale.FormatNumber(fmt.Sprint(l.Measurement.Units[extcost.Unit(unit)]), 0))))
		}
		reconciled := false
		if at := p.Report.ReconciledAt[l.Call.Key]; !at.IsZero() {
			detail = append(detail, html.P(html.Props{}, ui.Text(extcostText(locale, "reconciled")+": "+locale.FormatDate(at))))
			reconciled = true
		}
		for _, r := range p.Report.Reconciliations {
			if reconciled {
				break
			}
			if r.Provider == l.Call.Provider && r.Operation == l.Call.Operation && r.Currency == l.Currency && r.Day.Equal(extcost.PeriodStart(l.At, "day")) {
				detail = append(detail, html.P(html.Props{}, ui.Text(extcostText(locale, "reconciled")+": "+locale.FormatDate(r.At))))
				reconciled = true
			}
		}
		if !reconciled {
			detail = append(detail, html.P(html.Props{}, ui.Text(extcostText(locale, "unreconciled"))))
		}
		if l.Finding != "" {
			detail = append(detail, html.P(html.Props{Role: "status"}, ui.Text(extcostText(locale, "finding"))))
		}
		nodes = append(nodes, html.Article(html.Props{Class: "extcost-call"}, html.A(html.Props{Href: ExtcostHref(locale, f, false)}, ui.Text(extcostText(locale, "view_call")+" · "+l.Call.Provider)), html.P(html.Props{}, ui.Text(locale.FormatDate(l.At)+" · "+amount+" · "+extcostText(locale, quality)+" · "+extcostText(locale, l.Measurement.Outcome))), html.Details(html.Props{}, detail...)))
	}
	return html.Section(html.Props{Class: "surface"}, nodes...)
}
func ExtcostStylesheet() string {
	return `.extcost-page{min-width:0;max-width:100%;color:var(--hcm-color-text)}.extcost-content{display:grid;gap:var(--hcm-space-2);min-width:0}.extcost-content .surface{min-width:0;padding:var(--hcm-space-2);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-surface);background:var(--hcm-color-surface);box-shadow:var(--hcm-shadow-resting)}.extcost-filters{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,12rem),1fr));gap:var(--hcm-space-2)}.extcost-field{display:grid;gap:var(--hcm-space-1);min-width:0}.extcost-field input{min-width:0;width:100%;box-sizing:border-box;color:var(--hcm-color-text);background:var(--hcm-color-surface);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-control);padding:var(--hcm-space-1);min-height:var(--hcm-control-height)}.extcost-row{display:grid;grid-template-columns:minmax(0,1fr) auto auto;gap:var(--hcm-space-2);padding:.75rem 0;border-block-end:1px solid var(--hcm-color-border)}.extcost-call{padding:.75rem 0;border-block-end:1px solid var(--hcm-color-border)}.extcost-content a,.extcost-content p,.extcost-row span{overflow-wrap:anywhere}.extcost-content a:focus-visible,.extcost-content button:focus-visible,.extcost-content input:focus-visible,.extcost-content summary:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:var(--hcm-focus-ring-offset)}@media(max-width:800px){.extcost-row{grid-template-columns:minmax(0,1fr) auto}.extcost-row span:last-child{grid-column:1/-1}}@media(max-width:390px){.extcost-row{grid-template-columns:minmax(0,1fr)}}`
}
func extcostText(locale LocaleContext, k string) string {
	copy := map[string][3]string{
		"pending":      {"Calls awaiting reconciliation", "Aufrufe warten auf Abgleich", "اتصالات تنتظر المطابقة"},
		"pending_help": {"The provider outcome is unknown. Review the provider report before retrying.", "Das Anbieterergebnis ist unbekannt. Prüfen Sie den Anbieterbericht vor einem erneuten Versuch.", "نتيجة المزوّد غير معروفة. راجع تقرير المزوّد قبل إعادة المحاولة."},
		"maximum":      {"Estimated maximum", "Geschätztes Maximum", "الحد الأقصى التقديري"},
		"unpriced":     {"Price unavailable", "Preis nicht verfügbar", "السعر غير متاح"},
		"title":        {"External usage", "Externe Nutzung", "الاستخدام الخارجي"}, "denied": {"Administrator access required", "Administratorzugriff erforderlich", "يلزم وصول المسؤول"}, "denied_help": {"Ask an administrator for access to external usage.", "Bitten Sie eine Administration um Zugriff auf die externe Nutzung.", "اطلب من المسؤول الوصول إلى الاستخدام الخارجي."},
		"loading": {"Loading usage. You can refresh this page.", "Nutzung wird geladen. Sie können diese Seite aktualisieren.", "جارٍ تحميل الاستخدام. يمكنك تحديث هذه الصفحة."}, "refresh": {"Refresh usage", "Nutzung aktualisieren", "تحديث الاستخدام"}, "error": {"Usage could not be loaded. Refresh to try again.", "Nutzung konnte nicht geladen werden. Aktualisieren Sie die Seite und versuchen Sie es erneut.", "تعذر تحميل الاستخدام. حدّث الصفحة للمحاولة مجددًا."},
		"empty": {"No external calls in this period", "Keine externen Aufrufe in diesem Zeitraum", "لا توجد اتصالات خارجية في هذه الفترة"}, "empty_help": {"Choose another date or remove a filter to see more usage.", "Wählen Sie ein anderes Datum oder entfernen Sie einen Filter.", "اختر تاريخًا آخر أو أزل أحد عوامل التصفية."}, "truncated": {"The call list is limited. Totals include all matching calls. Narrow the filters to export every call.", "Die Aufrufliste ist begrenzt. Summen enthalten alle passenden Aufrufe. Schränken Sie die Filter für den Export ein.", "قائمة الاتصالات محدودة. تشمل المجاميع جميع الاتصالات المطابقة. ضيّق التصفية لتصدير كل اتصال."},
		"filters": {"Filter external usage", "Externe Nutzung filtern", "تصفية الاستخدام الخارجي"}, "provider": {"Provider", "Anbieter", "المزوّد"}, "operation": {"Operation", "Vorgang", "العملية"}, "feature": {"Feature", "Funktion", "الميزة"}, "purpose": {"Purpose", "Zweck", "الغرض"}, "agent": {"Agent", "Agent", "الوكيل"}, "workflow": {"Workflow", "Workflow", "سير العمل"}, "person": {"Person", "Person", "الشخص"}, "search": {"Search usage", "Nutzung suchen", "البحث في الاستخدام"}, "from": {"From date", "Ab Datum", "من تاريخ"}, "until": {"Until date (exclusive)", "Bis Datum (ausschließlich)", "حتى تاريخ (غير شامل)"}, "apply": {"Apply filters", "Filter anwenden", "تطبيق التصفية"}, "export": {"Export usage", "Nutzung exportieren", "تصدير الاستخدام"},
		"total": {"Spend in this period", "Ausgaben in diesem Zeitraum", "الإنفاق في هذه الفترة"}, "budget": {"Budget", "Budget", "الميزانية"}, "tenant": {"Workspace", "Arbeitsbereich", "مساحة العمل"}, "run": {"Run", "Lauf", "التشغيل"}, "node": {"Workflow step", "Workflow-Schritt", "خطوة سير العمل"}, "step": {"Agent step", "Agentenschritt", "خطوة الوكيل"}, "reserved": {"Reserved", "Reserviert", "محجوز"}, "budget_missing": {"No budget is visible for these filters. Ask an administrator to review spending limits.", "Für diese Filter ist kein Budget sichtbar. Bitten Sie eine Administration, die Ausgabenlimits zu prüfen.", "لا توجد ميزانية ظاهرة لهذه التصفية. اطلب من المسؤول مراجعة حدود الإنفاق."}, "warning": {"Spending is approaching the limit. Review the budget before making more calls.", "Die Ausgaben nähern sich dem Limit. Prüfen Sie das Budget vor weiteren Aufrufen.", "الإنفاق يقترب من الحد. راجع الميزانية قبل إجراء مزيد من الاتصالات."},
		"by_day": {"Spend by day", "Ausgaben nach Tag", "الإنفاق حسب اليوم"}, "by_provider": {"Spend by provider", "Ausgaben nach Anbieter", "الإنفاق حسب المزوّد"}, "by_feature": {"Spend by feature", "Ausgaben nach Funktion", "الإنفاق حسب الميزة"}, "by_purpose": {"Spend by purpose", "Ausgaben nach Zweck", "الإنفاق حسب الغرض"}, "by_agent": {"Spend by agent", "Ausgaben nach Agent", "الإنفاق حسب الوكيل"}, "by_workflow": {"Spend by workflow", "Ausgaben nach Workflow", "الإنفاق حسب سير العمل"}, "by_person": {"Top callers", "Häufigste Aufrufer", "أكثر المتصلين إنفاقًا"}, "day": {"Day", "Tag", "اليوم"},
		"answers": {"Answers", "Antworten", "الإجابات"}, "announcements": {"Announcements", "Ankündigungen", "الإعلانات"}, "screening": {"Screening", "Prüfung", "الفحص"}, "indexing": {"Indexing", "Indexierung", "الفهرسة"}, "transcription": {"Transcription", "Transkription", "النسخ الصوتي"}, "delivery": {"Delivery", "Zustellung", "التسليم"}, "storage": {"Storage", "Speicherung", "التخزين"}, "translation": {"Translation", "Übersetzung", "الترجمة"},
		"calls": {"Calls", "Aufrufe", "الاتصالات"}, "view_calls": {"View calls", "Aufrufe ansehen", "عرض الاتصالات"}, "view_call": {"View call", "Aufruf ansehen", "عرض الاتصال"}, "view_units": {"View measured units", "Gemessene Einheiten ansehen", "عرض الوحدات المقاسة"}, "measured": {"Measured", "Gemessen", "مقاس"}, "estimated": {"Estimated", "Geschätzt", "تقديري"}, "succeeded": {"Succeeded", "Erfolgreich", "نجح"}, "failed": {"Failed", "Fehlgeschlagen", "فشل"}, "refused": {"Refused", "Abgelehnt", "مرفوض"}, "unknown": {"Outcome unknown", "Ergebnis unbekannt", "النتيجة غير معروفة"}, "reconciled": {"Last reconciled", "Zuletzt abgeglichen", "آخر مطابقة"}, "unreconciled": {"Not yet reconciled with the provider", "Noch nicht mit dem Anbieter abgeglichen", "لم يُطابق مع المزوّد بعد"}, "finding": {"Usage needs administrator review.", "Die Nutzung muss von einer Administration geprüft werden.", "يتطلب الاستخدام مراجعة المسؤول."}, "median": {"Median per run", "Median je Lauf", "الوسيط لكل تشغيل"}, "p95": {"95th percentile per run", "95. Perzentil je Lauf", "المئين ٩٥ لكل تشغيل"},
		"input_tokens": {"Input tokens", "Eingabetokens", "رموز الإدخال"}, "cached_input_tokens": {"Cached input tokens", "Zwischengespeicherte Eingabetokens", "رموز الإدخال المخزنة مؤقتًا"}, "output_tokens": {"Output tokens", "Ausgabetokens", "رموز الإخراج"}, "seconds": {"Seconds", "Sekunden", "ثوانٍ"}, "characters": {"Characters", "Zeichen", "أحرف"}, "requests": {"Requests", "Anfragen", "طلبات"}, "messages": {"Messages", "Nachrichten", "رسائل"}, "byte_months": {"Byte-months", "Byte-Monate", "بايتات شهريًا"}, "bytes_transferred": {"Bytes transferred", "Übertragene Bytes", "بايتات منقولة"}, "minutes_relayed": {"Minutes relayed", "Weitergeleitete Minuten", "دقائق مرحّلة"},
	}
	v, ok := copy[k]
	if !ok {
		return ""
	}
	return v[agentRPLocaleIndex(locale)]
}
