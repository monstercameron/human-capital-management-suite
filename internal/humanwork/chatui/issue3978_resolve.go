package chatui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Help me resolve this (#3978). A requester whose workforce-change request is
// blocked gets a deterministic, private reading of the workflow's own checks:
// what stops the request, what is only for information, which revision they
// belong to, and who can act. It reads nothing itself, edits nothing and
// submits nothing; every correction goes through the request and its governed
// workflow, which checks again before anything is approved.

// JourneyResolveFinding is one preflight finding reduced to what the panel may
// use: the severity token and the stable code. The engine's prose is never
// carried, because it can hold pay figures and employee details.
type JourneyResolveFinding struct{ Severity, Code string }

const (
	resolveFieldSep  = "\x1f"
	resolveRecordSep = "\x1e"
)

// EncodeJourneyResolveFindings packs findings into a comparable string so a
// JourneyPreview stays comparable with ==.
func EncodeJourneyResolveFindings(in []JourneyResolveFinding) string {
	var parts []string
	for _, f := range in {
		code := strings.TrimSpace(f.Code)
		if code == "" || isResolveWaitCode(code) {
			continue
		}
		clean := func(s string) string {
			return strings.NewReplacer(resolveFieldSep, " ", resolveRecordSep, " ").Replace(strings.TrimSpace(s))
		}
		parts = append(parts, clean(f.Severity)+resolveFieldSep+clean(code))
	}
	return strings.Join(parts, resolveRecordSep)
}

// DecodeJourneyResolveFindings is the inverse of EncodeJourneyResolveFindings.
func DecodeJourneyResolveFindings(s string) []JourneyResolveFinding {
	if s == "" {
		return nil
	}
	var out []JourneyResolveFinding
	for _, record := range strings.Split(s, resolveRecordSep) {
		fields := strings.SplitN(record, resolveFieldSep, 2)
		if len(fields) == 2 && fields[1] != "" {
			out = append(out, JourneyResolveFinding{Severity: fields[0], Code: fields[1]})
		}
	}
	return out
}

// isResolveWaitCode skips the effective-date wait explanation findings, which
// describe a healthy wait and are shown by the journey page itself.
func isResolveWaitCode(code string) bool { return strings.HasPrefix(code, "WAIT_") }

// JourneyResolveSeverity groups the wire's severity and status tokens:
// blocking (including a blocked status), needs-data, denied, or info.
func JourneyResolveSeverity(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "BLOCKING", "BLOCKED", "ERROR", "FATAL", "CRITICAL":
		return "blocking"
	case "NEEDS_DATA", "NEEDS-DATA", "NEEDS DATA":
		return "needs-data"
	case "DENIED":
		return "denied"
	}
	return "info"
}

// journeyResolveOffered: only the person who started a blocked (or just
// rechecking) request is offered help; the owners of other steps are not told
// to fix someone else's proposal.
func journeyResolveOffered(p JourneyPreview) bool {
	return p.RequestedByViewer && (p.Stage == "JOURNEY_STAGE_BLOCKED" || p.Stage == "JOURNEY_STAGE_REVALIDATION")
}

// resolveTopic is a finding code's plain-language title and who can act on it.
type resolveTopic struct {
	title [3]string
	owner string // you, hr, comp, finance, review, open
}

var resolveTopicPay = resolveTopic{
	[3]string{"The pay details on this request are incomplete or not accepted.", "Die Gehaltsangaben in diesem Antrag sind unvollständig oder werden nicht akzeptiert.", "تفاصيل الراتب في هذا الطلب ناقصة أو غير مقبولة."}, "you"}

var resolveTopics = map[string]resolveTopic{
	"compensation.current_amount_invalid":           resolveTopicPay,
	"compensation.proposed_amount_invalid":          resolveTopicPay,
	"compensation.currency_required":                resolveTopicPay,
	"compensation.pay_basis_required":               resolveTopicPay,
	"compensation.currency_change_not_supported_v0": resolveTopicPay,
	"compensation.not_a_raise":                      {[3]string{"The proposed pay is not higher than the current pay.", "Das vorgeschlagene Gehalt liegt nicht über dem aktuellen Gehalt.", "الراتب المقترح ليس أعلى من الراتب الحالي."}, "you"},
	"compensation.business_reason_required":         {[3]string{"A business reason is needed.", "Eine geschäftliche Begründung fehlt.", "مطلوب مبرر عمل."}, "you"},
	"compensation.effective_at_required":            {[3]string{"The effective date is missing or too far in the past.", "Das Wirksamkeitsdatum fehlt oder liegt zu weit in der Vergangenheit.", "تاريخ السريان مفقود أو بعيد جدًا في الماضي."}, "you"},
	"compensation.effective_at_too_far_in_past":     {[3]string{"The effective date is missing or too far in the past.", "Das Wirksamkeitsdatum fehlt oder liegt zu weit in der Vergangenheit.", "تاريخ السريان مفقود أو بعيد جدًا في الماضي."}, "you"},
	"compensation.increase_over_ten_percent":        {[3]string{"The pay increase is above the review threshold, so approvers will look at it closely.", "Die Gehaltserhöhung liegt über der Prüfschwelle, daher schauen die Genehmigenden genau hin.", "زيادة الراتب أعلى من حد المراجعة، لذا سيدقق فيها المعتمدون."}, "review"},
	"promotion.target_job_required":                 {[3]string{"The new job or grade is missing, or is the same as the current one.", "Die neue Stelle oder Stufe fehlt oder entspricht der aktuellen.", "الوظيفة أو الدرجة الجديدة مفقودة أو مطابقة للحالية."}, "you"},
	"promotion.target_grade_required":               {[3]string{"The new job or grade is missing, or is the same as the current one.", "Die neue Stelle oder Stufe fehlt oder entspricht der aktuellen.", "الوظيفة أو الدرجة الجديدة مفقودة أو مطابقة للحالية."}, "you"},
	"promotion.same_grade":                          {[3]string{"The new job or grade is missing, or is the same as the current one.", "Die neue Stelle oder Stufe fehlt oder entspricht der aktuellen.", "الوظيفة أو الدرجة الجديدة مفقودة أو مطابقة للحالية."}, "you"},
	"promotion.effective_date_before_hire_date":     {[3]string{"The effective date is before the person's start date.", "Das Wirksamkeitsdatum liegt vor dem Eintrittsdatum der Person.", "تاريخ السريان يسبق تاريخ بدء الشخص."}, "you"},
	"promotion.worker_not_active":                   {[3]string{"This person is not active right now.", "Diese Person ist derzeit nicht aktiv.", "هذا الشخص غير نشط حاليًا."}, "hr"},
	"promotion.pay_band_not_found":                  {[3]string{"No pay range was found for the new job and grade.", "Für die neue Stelle und Stufe wurde keine Gehaltsspanne gefunden.", "لم يُعثر على نطاق راتب للوظيفة والدرجة الجديدتين."}, "comp"},
	"promotion.pay_below_band_minimum":              {[3]string{"The proposed pay is outside the pay range for the new job and grade.", "Das vorgeschlagene Gehalt liegt außerhalb der Gehaltsspanne für die neue Stelle und Stufe.", "الراتب المقترح خارج نطاق الراتب للوظيفة والدرجة الجديدتين."}, "you"},
	"promotion.pay_above_band_maximum":              {[3]string{"The proposed pay is outside the pay range for the new job and grade.", "Das vorgeschlagene Gehalt liegt außerhalb der Gehaltsspanne für die neue Stelle und Stufe.", "الراتب المقترح خارج نطاق الراتب للوظيفة والدرجة الجديدتين."}, "you"},
	"promotion.budget_authority_missing":            {[3]string{"Budget could not be confirmed for this change.", "Das Budget konnte für diese Änderung nicht bestätigt werden.", "تعذّر تأكيد الميزانية لهذا التغيير."}, "finance"},
	"promotion.budget_observed_insufficient":        {[3]string{"Budget could not be confirmed for this change.", "Das Budget konnte für diese Änderung nicht bestätigt werden.", "تعذّر تأكيد الميزانية لهذا التغيير."}, "finance"},
	"promotion.budget_authority_observation_only":   {[3]string{"Budget was only looked at, not set aside. Nothing is held yet.", "Das Budget wurde nur geprüft, nicht reserviert. Noch ist nichts zurückgelegt.", "جرى الاطلاع على الميزانية فقط ولم تُحجز. لا شيء محجوز بعد."}, "review"},
	"promotion.required_field_unavailable":          {[3]string{"Some information needed for the checks could not be read.", "Einige für die Prüfungen nötige Angaben konnten nicht gelesen werden.", "تعذّرت قراءة بعض المعلومات اللازمة للفحوصات."}, "hr"},
	// Position and reporting-line checks. The wording never says whether a
	// position or manager exists, only that the choice cannot be used.
	"promotion.target_position_not_found":            {[3]string{"The selected position cannot be used for this request.", "Die gewählte Position kann für diesen Antrag nicht verwendet werden.", "لا يمكن استخدام المنصب المحدد لهذا الطلب."}, "you"},
	"promotion.target_position_incompatible":         {[3]string{"The selected position does not match the new job or organization.", "Die gewählte Position passt nicht zur neuen Stelle oder Organisation.", "المنصب المحدد لا يطابق الوظيفة أو الجهة الجديدة."}, "you"},
	"promotion.target_position_not_effective":        {[3]string{"The selected position is not valid on the effective date.", "Die gewählte Position ist am Wirksamkeitsdatum nicht gültig.", "المنصب المحدد غير صالح في تاريخ السريان."}, "you"},
	"promotion.target_position_at_capacity":          {[3]string{"The selected position has no room left on that date.", "Die gewählte Position hat an diesem Datum keinen freien Platz mehr.", "لا يوجد مكان متاح في المنصب المحدد في ذلك التاريخ."}, "hr"},
	"promotion.target_position_reservation_conflict": {[3]string{"The selected position is already set aside for that date.", "Die gewählte Position ist für dieses Datum bereits reserviert.", "المنصب المحدد محجوز بالفعل لذلك التاريخ."}, "hr"},
	"promotion.target_manager_not_found":             {[3]string{"The selected manager cannot be used for this request.", "Die gewählte Führungskraft kann für diesen Antrag nicht verwendet werden.", "لا يمكن استخدام المدير المحدد لهذا الطلب."}, "you"},
	"promotion.manager_relationship_cycle":           {[3]string{"This change would make someone report to themselves, directly or through others.", "Diese Änderung würde dazu führen, dass jemand direkt oder indirekt an sich selbst berichtet.", "سيجعل هذا التغيير شخصًا يتبع لنفسه، مباشرة أو عبر آخرين."}, "you"},
	"promotion.manager_chain_unresolved":             {[3]string{"The reporting line above the new manager could not be fully checked.", "Die Berichtslinie oberhalb der neuen Führungskraft konnte nicht vollständig geprüft werden.", "تعذّر فحص خط التبعية فوق المدير الجديد بالكامل."}, "hr"},
}

var (
	resolveFallbackStop = resolveTopic{[3]string{"A check on this request did not pass.", "Eine Prüfung dieses Antrags wurde nicht bestanden.", "لم ينجح أحد الفحوصات على هذا الطلب."}, "open"}
	resolveFallbackInfo = resolveTopic{[3]string{"Something to be aware of on this request.", "Etwas, das Sie bei diesem Antrag beachten sollten.", "أمر يجدر الانتباه إليه في هذا الطلب."}, "open"}
	resolveDenied       = resolveTopic{[3]string{"Details of one check are not available to you.", "Details zu einer Prüfung sind für Sie nicht verfügbar.", "تفاصيل أحد الفحوصات غير متاحة لك."}, "withheld"}
)

var resolveOwnerWords = map[string][3]string{
	"you":      {"You can correct this on the request, then submit it again.", "Sie können das im Antrag korrigieren und ihn dann erneut einreichen.", "يمكنك تصحيح هذا في الطلب ثم إرساله مرة أخرى."},
	"hr":       {"Ask your HR administrator; you cannot change this yourself.", "Wenden Sie sich an Ihre Personaladministration; Sie können das nicht selbst ändern.", "اطلب من مسؤول الموارد البشرية؛ لا يمكنك تغيير هذا بنفسك."},
	"comp":     {"Ask the compensation team; you cannot change this yourself.", "Wenden Sie sich an das Vergütungsteam; Sie können das nicht selbst ändern.", "اطلب من فريق التعويضات؛ لا يمكنك تغيير هذا بنفسك."},
	"finance":  {"Ask Finance; you cannot change this yourself.", "Wenden Sie sich an das Finanzteam; Sie können das nicht selbst ändern.", "اطلب من قسم المالية؛ لا يمكنك تغيير هذا بنفسك."},
	"review":   {"Approvers will see this. Nothing is needed from you.", "Die Genehmigenden sehen das. Von Ihnen ist nichts nötig.", "سيراه المعتمدون. لا يلزم منك أي إجراء."},
	"open":     {"Open the request to see more.", "Öffnen Sie den Antrag, um mehr zu sehen.", "افتح الطلب لمعرفة المزيد."},
	"withheld": {"Ask the person who owns this request or your HR administrator.", "Wenden Sie sich an die Person, der dieser Antrag gehört, oder an Ihre Personaladministration.", "اطلب من صاحب هذا الطلب أو من مسؤول الموارد البشرية."},
}

var resolveWords = map[string][3]string{
	"summary":     {"Help me resolve this", "Hilfe bei der Lösung", "ساعدني في حل هذا"},
	"private":     {"Only you see this. Nothing is changed or sent from here.", "Nur Sie sehen das. Von hier aus wird nichts geändert oder gesendet.", "أنت وحدك ترى هذا. لا يتم تغيير أو إرسال أي شيء من هنا."},
	"source":      {"From the workflow's preflight and simulation checks · Revision {rev}", "Aus den Vorabprüfungen und der Simulation des Workflows · Revision {rev}", "من فحوصات التحقق المسبق والمحاكاة في المسار · المراجعة {rev}"},
	"norev":       {"From the workflow's preflight and simulation checks · Revision not reported", "Aus den Vorabprüfungen und der Simulation des Workflows · Revision nicht angegeben", "من فحوصات التحقق المسبق والمحاكاة في المسار · لم تُذكر المراجعة"},
	"stale":       {"This request is being rechecked. These results may be out of date.", "Dieser Antrag wird gerade erneut geprüft. Diese Ergebnisse sind möglicherweise veraltet.", "تجري إعادة فحص هذا الطلب. قد تكون هذه النتائج قديمة."},
	"unavailable": {"The checks for this request could not be read right now. Open the request to see them; nothing here is guessed.", "Die Prüfungen für diesen Antrag konnten gerade nicht gelesen werden. Öffnen Sie den Antrag, um sie zu sehen; hier wird nichts geraten.", "تعذّرت قراءة فحوصات هذا الطلب الآن. افتح الطلب لمشاهدتها؛ لا شيء هنا مبني على التخمين."},
	"stops":       {"Stopping this request ({n})", "Hält diesen Antrag auf ({n})", "تعطّل هذا الطلب ({n})"},
	"info":        {"For information ({n})", "Zur Information ({n})", "للعلم ({n})"},
	"nextstep":    {"Next step: correct the request. That step is yours.", "Nächster Schritt: den Antrag korrigieren. Dieser Schritt liegt bei Ihnen.", "الخطوة التالية: تصحيح الطلب. هذه الخطوة عليك."},
	"recheck":     {"After you correct it and submit again, the checks run again before anything is approved.", "Nachdem Sie ihn korrigiert und erneut eingereicht haben, werden die Prüfungen vor jeder Genehmigung erneut durchgeführt.", "بعد أن تصحّحه وتعيد الإرسال، تُجرى الفحوصات مرة أخرى قبل اعتماد أي شيء."},
	"open":        {"Open the request for {name}", "Antrag für {name} öffnen", "افتح الطلب الخاص بـ {name}"},
}

func resolveText(m Model, key string, vars map[string]string) string {
	words := resolveWords[key]
	text := chatbug039Text(key, words[chatbug039LocaleIndex(m.Locale)], words[0])
	for k, v := range vars {
		text = strings.ReplaceAll(text, "{"+k+"}", v)
	}
	return text
}

func resolveTopicText(m Model, t resolveTopic) string {
	return chatbug039Text("resolve.topic", t.title[chatbug039LocaleIndex(m.Locale)], t.title[0])
}

func resolveOwnerText(m Model, owner string) string {
	words := resolveOwnerWords[owner]
	return chatbug039Text(owner, words[chatbug039LocaleIndex(m.Locale)], words[0])
}

func resolveItem(m Model, f JourneyResolveFinding, severity string) ui.Node {
	topic, known := resolveTopics[f.Code]
	switch {
	case severity == "denied":
		// A refused check is never explained: not its code, not its topic.
		topic = resolveDenied
	case !known && severity == "info":
		topic = resolveFallbackInfo
	case !known:
		topic = resolveFallbackStop
	}
	return html.Li(html.Props{Class: "chat-journey-resolve-item", Data: map[string]string{"severity": severity}},
		html.Strong(html.Props{Dir: "auto", Text: resolveTopicText(m, topic)}),
		html.Span(html.Props{Class: "chat-journey-resolve-owner", Dir: "auto", Text: resolveOwnerText(m, topic.owner)}))
}

func resolveGroup(m Model, key string, items []ui.Node) ui.Node {
	return html.Div(html.Props{Class: "chat-journey-resolve-group"},
		html.P(html.Props{Class: "chat-journey-resolve-heading", Text: resolveText(m, key, map[string]string{"n": strconv.Itoa(len(items))})}),
		html.Ul(html.Props{Class: "chat-journey-resolve-list"}, items...))
}

// journeyResolvePanel is the private, deterministic help disclosure for a
// blocked request, or nil when the viewer is not offered it.
func journeyResolvePanel(m Model, p JourneyPreview) ui.Node {
	if !journeyResolveOffered(p) {
		return nil
	}
	var stops, notes []ui.Node
	for _, f := range DecodeJourneyResolveFindings(p.ResolveFindings) {
		switch severity := JourneyResolveSeverity(f.Severity); severity {
		case "info":
			notes = append(notes, resolveItem(m, f, severity))
		default:
			stops = append(stops, resolveItem(m, f, severity))
		}
	}
	source := resolveText(m, "norev", nil)
	if rev := strings.TrimSpace(p.Revision); rev != "" {
		source = resolveText(m, "source", map[string]string{"rev": rev})
	}
	body := []ui.Node{
		html.P(html.Props{Class: "chat-journey-resolve-note", Text: resolveText(m, "private", nil)}),
		html.P(html.Props{Class: "chat-journey-resolve-note", Dir: "auto", Text: source}),
	}
	if p.Stage == "JOURNEY_STAGE_REVALIDATION" {
		body = append(body, html.P(html.Props{Class: "chat-journey-resolve-note chat-journey-resolve-stale", Role: "status", Text: resolveText(m, "stale", nil)}))
	}
	if len(stops) == 0 && p.Stage == "JOURNEY_STAGE_BLOCKED" {
		// A blocked request with nothing that explains it: say so rather than
		// reassure or invent a reason.
		body = append(body, html.P(html.Props{Class: "chat-journey-resolve-note chat-journey-resolve-unavailable", Role: "status", Text: resolveText(m, "unavailable", nil)}))
	}
	if len(stops) > 0 {
		body = append(body, resolveGroup(m, "stops", stops))
	}
	if len(notes) > 0 {
		body = append(body, resolveGroup(m, "info", notes))
	}
	if p.NextStep == "JOURNEY_NEXT_STEP_CORRECT_PROPOSAL" {
		body = append(body, html.P(html.Props{Class: "chat-journey-resolve-note", Text: resolveText(m, "nextstep", nil)}))
	}
	body = append(body,
		html.P(html.Props{Class: "chat-journey-resolve-note", Text: resolveText(m, "recheck", nil)}),
		html.A(html.Props{Class: "button secondary small chat-journey-resolve-open", Href: JourneyReferenceURL(p.IntentID), Data: map[string]string{"action": "open-journey-reference", "id": p.IntentID}, Dir: "auto", Text: resolveText(m, "open", map[string]string{"name": p.Worker})}))
	return html.Details(html.Props{Class: "chat-journey-resolve", Data: map[string]string{"journey-resolve": p.IntentID}},
		html.Summary(html.Props{Class: "chat-journey-resolve-summary", Text: resolveText(m, "summary", nil)}),
		html.Div(html.Props{Class: "chat-journey-resolve-body"}, body...))
}

const journeyResolveStyles = `.chat-journey-embed-wrap{display:flex;flex-direction:column;gap:6px;min-width:0}` +
	`.chat-journey-resolve{border:1px solid var(--line);border-radius:10px;background:var(--panel,transparent);min-width:0}` +
	`.chat-journey-resolve-summary{cursor:pointer;min-height:44px;display:flex;align-items:center;padding-block:0;padding-inline:12px;font-weight:600;font-size:.875rem}` +
	`.chat-journey-resolve-body{display:flex;flex-direction:column;gap:8px;padding-block:0 12px;padding-inline:12px;font-size:.8125rem;overflow-wrap:anywhere}` +
	`.chat-journey-resolve-note{margin:0;color:var(--muted)}` +
	`.chat-journey-resolve-heading{margin:0;font-weight:600}` +
	`.chat-journey-resolve-list{margin:4px 0 0;padding-block:0;padding-inline:18px;display:flex;flex-direction:column;gap:6px}` +
	`.chat-journey-resolve-item{display:flex;flex-direction:column;gap:2px}` +
	`.chat-journey-resolve-owner{color:var(--muted)}` +
	`.chat-journey-resolve-open{align-self:flex-start;min-height:44px;display:inline-flex;align-items:center}`
