package chatui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/agentapproval"
)

// Issue #3971. The "What changed since I last reviewed?" section of the
// invoker-only persona approval card. It prints the deterministic comparison
// the agentapproval package computed for this viewer; it never decides,
// approves, runs or notifies anything, and the only route it offers is the
// governed task view.

// issue3971Copy rows are English, German, Arabic.
var issue3971Copy = map[string][3]string{
	"heading":            {"What changed since I last reviewed?", "Was hat sich seit meiner letzten Prüfung geändert?", "ما الذي تغيّر منذ آخر مراجعة لي؟"},
	"summary.changed":    {"Something you reviewed has changed. Your earlier approval no longer applies to this version.", "Seit Ihrer Prüfung hat sich etwas geändert. Ihre frühere Genehmigung gilt für diese Version nicht mehr.", "تغيّر شيء راجعته. موافقتك السابقة لم تعد تنطبق على هذه النسخة."},
	"summary.unchanged":  {"The details you reviewed have not changed.", "Die von Ihnen geprüften Angaben haben sich nicht geändert.", "لم تتغيّر التفاصيل التي راجعتها."},
	"summary.incomplete": {"This comparison is incomplete, so no changes are shown. Open the task view to review the current version safely.", "Dieser Vergleich ist unvollständig, deshalb werden keine Änderungen angezeigt. Öffnen Sie die Aufgabenansicht, um die aktuelle Version sicher zu prüfen.", "هذه المقارنة غير مكتملة، لذلك لا تُعرض أي تغييرات. افتح عرض المهام لمراجعة النسخة الحالية بأمان."},
	"unknown.previous":   {"The version you last reviewed is not available.", "Die Version, die Sie zuletzt geprüft haben, ist nicht verfügbar.", "النسخة التي راجعتها آخر مرة غير متاحة."},
	"unknown.current":    {"The current version is not available for review.", "Die aktuelle Version steht nicht zur Prüfung bereit.", "النسخة الحالية غير متاحة للمراجعة."},
	"unknown.access":     {"Your access to one of the versions has changed.", "Ihr Zugriff auf eine der Versionen hat sich geändert.", "تغيّرت صلاحية وصولك إلى إحدى النسختين."},
	"previous":           {"Previously reviewed", "Zuvor geprüft", "ما سبقت مراجعته"},
	"now":                {"Now", "Jetzt", "الآن"},
	"not_set":            {"Not set", "Nicht festgelegt", "غير محدد"},
	"kind.changed":       {"changed", "geändert", "تغيّر"},
	"kind.added":         {"added", "hinzugefügt", "أُضيف"},
	"kind.removed":       {"removed", "entfernt", "أُزيل"},
	"hidden.one":         {"1 more change is in details you cannot see here.", "1 weitere Änderung betrifft Angaben, die Sie hier nicht sehen können.", "هناك تغيير واحد آخر في تفاصيل لا يمكنك رؤيتها هنا."},
	"hidden.many":        {"{n} more changes are in details you cannot see here.", "{n} weitere Änderungen betreffen Angaben, die Sie hier nicht sehen können.", "هناك {n} تغييرات أخرى في تفاصيل لا يمكنك رؤيتها هنا."},
	"invalid.heading":    {"No longer valid", "Nicht mehr gültig", "لم يعد صالحًا"},
	"invalid.approval":   {"Your earlier approval covered only the version you reviewed.", "Ihre frühere Genehmigung galt nur für die Version, die Sie geprüft haben.", "اقتصرت موافقتك السابقة على النسخة التي راجعتها."},
	"invalid.step_up":    {"A new identity check is required in the task view.", "In der Aufgabenansicht ist eine neue Identitätsbestätigung erforderlich.", "يلزم تحقق جديد من الهوية في عرض المهام."},
	"note":               {"Nothing is approved, run or sent from here. You decide in the task view.", "Von hier aus wird nichts genehmigt, ausgeführt oder gesendet. Sie entscheiden in der Aufgabenansicht.", "لا يُعتمد أو يُنفَّذ أو يُرسَل أي شيء من هنا. القرار يكون في عرض المهام."},
	"fresh":              {"Open fresh review", "Neue Prüfung öffnen", "فتح مراجعة جديدة"},
	"source.heading":     {"Original message", "Ursprüngliche Nachricht", "الرسالة الأصلية"},
	"source.edited":      {"The message this came from was edited after the proposal was prepared. Check that the proposal still matches what you meant.", "Die Nachricht, aus der dies entstand, wurde nach der Erstellung des Vorschlags bearbeitet. Prüfen Sie, ob der Vorschlag noch dem entspricht, was Sie gemeint haben.", "عُدّلت الرسالة التي نشأ عنها هذا الاقتراح بعد إعداده. تحقق من أن الاقتراح ما زال يطابق ما قصدته."},
	"source.deleted":     {"The message this came from was deleted. Check that the proposal still matches what you meant.", "Die Nachricht, aus der dies entstand, wurde gelöscht. Prüfen Sie, ob der Vorschlag noch dem entspricht, was Sie gemeint haben.", "حُذفت الرسالة التي نشأ عنها هذا الاقتراح. تحقق من أن الاقتراح ما زال يطابق ما قصدته."},
	"source.unavailable": {"You can no longer open the message this came from. Check that the proposal still matches what you meant.", "Sie können die Nachricht, aus der dies entstand, nicht mehr öffnen. Prüfen Sie, ob der Vorschlag noch dem entspricht, was Sie gemeint haben.", "لم يعد بإمكانك فتح الرسالة التي نشأ عنها هذا الاقتراح. تحقق من أن الاقتراح ما زال يطابق ما قصدته."},
	"source.captured":    {"As first captured", "Wie ursprünglich erfasst", "كما التُقطت أولًا"},
	"source.current":     {"Current text", "Aktueller Text", "النص الحالي"},
	"field.item":         {"Item", "Eintrag", "العنصر"},
	"field.tier":         {"Approval level", "Genehmigungsstufe", "مستوى الموافقة"},
	"field.risk":         {"Risk level", "Risikostufe", "مستوى المخاطرة"},
	"field.definition":   {"Action", "Aktion", "الإجراء"},
	"field.operation":    {"Operation", "Vorgang", "العملية"},
	"field.uncertainty":  {"Open questions", "Offene Fragen", "أسئلة مفتوحة"},
	"field.agent":        {"Agent", "Agent", "الوكيل"},
	"field.expires":      {"Approval expires", "Genehmigung läuft ab", "تنتهي الموافقة"},
	"field.task_expires": {"Task expires", "Aufgabe läuft ab", "تنتهي المهمة"},
	"field.base":         {"Starting value of {name}", "Ausgangswert von {name}", "القيمة الابتدائية لـ {name}"},
	"field.source":       {"Source {name}", "Quelle {name}", "المصدر {name}"},
	"field.subject":      {"Affected record {name}", "Betroffener Datensatz {name}", "السجل المتأثر {name}"},
}

func issue3971Text(model Model, key string) string {
	row, ok := issue3971Copy[key]
	if !ok {
		return ""
	}
	return chatbug039Text(key, row[chatbug039LocaleIndex(model.Locale)], row[0])
}

func issue3971FieldLabel(model Model, field string) string {
	named := func(prefix, key string) (string, bool) {
		if !strings.HasPrefix(field, prefix) {
			return "", false
		}
		return strings.ReplaceAll(issue3971Text(model, key), "{name}", strings.TrimPrefix(field, prefix)), true
	}
	for _, p := range []struct{ prefix, key string }{
		{agentapproval.ReReviewFieldBasePrefix, "field.base"},
		{agentapproval.ReReviewFieldSourcePrefix, "field.source"},
		{agentapproval.ReReviewFieldSubjectPrefix, "field.subject"},
	} {
		if label, ok := named(p.prefix, p.key); ok {
			return label
		}
	}
	switch field {
	case agentapproval.ReReviewFieldItem, agentapproval.ReReviewFieldTier, agentapproval.ReReviewFieldRisk, agentapproval.ReReviewFieldDefinition,
		agentapproval.ReReviewFieldOperation, agentapproval.ReReviewFieldUncertainty, agentapproval.ReReviewFieldAgent,
		agentapproval.ReReviewFieldExpires, agentapproval.ReReviewFieldTaskExpires:
		return issue3971Text(model, "field."+field)
	}
	return field
}

// issue3971ReReviewView returns the comparison to render for the card, or nil.
// A comparison for a different digest than the card shows is stale and is
// shown as incomplete rather than as a speculative explanation.
func issue3971ReReviewView(card PersonaApprovalCard) *agentapproval.ReReview {
	rr := card.ReReview
	if rr == nil {
		return nil
	}
	if rr.State != agentapproval.ReReviewIncomplete && (rr.ViewerID != card.InvokerID || rr.CurrentDigest != card.Digest) {
		return &agentapproval.ReReview{State: agentapproval.ReReviewIncomplete, Unknown: []agentapproval.ReReviewUnknown{agentapproval.ReReviewUnknownCurrent}}
	}
	return rr
}

// issue3971FreshReview reports whether the card's task link should read
// "Open fresh review": only when the exact digest differs from the reviewed one.
func issue3971FreshReview(card PersonaApprovalCard) bool {
	rr := issue3971ReReviewView(card)
	return rr != nil && rr.State == agentapproval.ReReviewChanged
}

func issue3971Section(model Model, card PersonaApprovalCard) ui.Node {
	rr := issue3971ReReviewView(card)
	if rr == nil || !rr.NeedsAttention() {
		return ui.Fragment()
	}
	heading := issue3971Text(model, "heading")
	children := []ui.Node{html.H4(html.Props{Class: "persona-rereview-heading"}, ui.Text(heading))}
	summary := "summary.changed"
	switch rr.State {
	case agentapproval.ReReviewIncomplete:
		summary = "summary.incomplete"
	case agentapproval.ReReviewUnchanged:
		summary = "summary.unchanged"
	}
	children = append(children, html.P(html.Props{Class: "persona-rereview-summary", Role: "status", Aria: map[string]string{"live": "polite"}}, ui.Text(issue3971Text(model, summary))))
	if len(rr.Unknown) > 0 {
		items := make([]ui.Node, 0, len(rr.Unknown))
		for _, unknown := range rr.Unknown {
			key := "unknown.access"
			switch unknown {
			case agentapproval.ReReviewUnknownPrevious:
				key = "unknown.previous"
			case agentapproval.ReReviewUnknownCurrent:
				key = "unknown.current"
			}
			items = append(items, html.Li(html.Props{}, ui.Text(issue3971Text(model, key))))
		}
		children = append(children, html.Ul(html.Props{Class: "persona-rereview-unknown"}, items...))
	}
	if rr.State == agentapproval.ReReviewChanged && len(rr.Changes) > 0 {
		rows := make([]ui.Node, 0, len(rr.Changes))
		for _, change := range rr.Changes {
			rows = append(rows, issue3971ChangeRow(model, change))
		}
		children = append(children, html.Tag("dl", html.Props{Class: "persona-rereview-changes", Aria: map[string]string{"label": heading}}, rows...))
	}
	if rr.State == agentapproval.ReReviewChanged && rr.HiddenChanges > 0 {
		text := issue3971Text(model, "hidden.many")
		if rr.HiddenChanges == 1 {
			text = issue3971Text(model, "hidden.one")
		}
		children = append(children, html.P(html.Props{Class: "persona-rereview-hidden"}, ui.Text(strings.ReplaceAll(text, "{n}", strconv.Itoa(rr.HiddenChanges)))))
	}
	if rr.State == agentapproval.ReReviewChanged && len(rr.Invalidated) > 0 {
		items := make([]ui.Node, 0, len(rr.Invalidated))
		for _, check := range rr.Invalidated {
			key := "invalid.approval"
			if check == agentapproval.ReReviewInvalidStepUp {
				key = "invalid.step_up"
			}
			items = append(items, html.Li(html.Props{}, ui.Text(issue3971Text(model, key))))
		}
		children = append(children, html.Strong(html.Props{Class: "persona-rereview-invalid-heading"}, ui.Text(issue3971Text(model, "invalid.heading"))),
			html.Ul(html.Props{Class: "persona-rereview-invalid"}, items...))
	}
	if rr.Source != nil && rr.Source.ConfirmIntent {
		children = append(children, issue3971Source(model, *rr.Source))
	}
	children = append(children, html.P(html.Props{Class: "persona-rereview-note"}, ui.Text(issue3971Text(model, "note"))))
	return html.Section(html.Props{Class: "persona-rereview", Role: "region", Aria: map[string]string{"label": heading}, Data: map[string]string{"agent-rereview-state": string(rr.State)}}, children...)
}

func issue3971ChangeRow(model Model, change agentapproval.ReReviewChange) ui.Node {
	value := func(text string) string {
		if strings.TrimSpace(text) == "" {
			return issue3971Text(model, "not_set")
		}
		return text
	}
	label := issue3971FieldLabel(model, change.Field) + " (" + issue3971Text(model, "kind."+string(change.Kind)) + ")"
	return html.Div(html.Props{Class: "persona-rereview-change", Data: map[string]string{"agent-rereview-kind": string(change.Kind)}},
		html.Tag("dt", html.Props{Dir: "auto"}, ui.Text(label)),
		html.Tag("dd", html.Props{Class: "persona-rereview-before", Dir: "auto"}, html.Span(html.Props{Class: "muted"}, ui.Text(issue3971Text(model, "previous")+": ")), ui.Text(value(change.Before))),
		html.Tag("dd", html.Props{Class: "persona-rereview-after", Dir: "auto"}, html.Span(html.Props{Class: "muted"}, ui.Text(issue3971Text(model, "now")+": ")), ui.Text(value(change.After))))
}

func issue3971Source(model Model, source agentapproval.SourceReview) ui.Node {
	key := "source.unavailable"
	switch source.Status {
	case agentapproval.SourceMessageEdited:
		key = "source.edited"
	case agentapproval.SourceMessageDeleted:
		key = "source.deleted"
	}
	children := []ui.Node{
		html.Strong(html.Props{}, ui.Text(issue3971Text(model, "source.heading"))),
		html.P(html.Props{Class: "persona-rereview-source-status"}, ui.Text(issue3971Text(model, key))),
	}
	if source.Status == agentapproval.SourceMessageEdited {
		children = append(children,
			html.P(html.Props{Class: "persona-rereview-source-captured", Dir: "auto"}, html.Span(html.Props{Class: "muted"}, ui.Text(issue3971Text(model, "source.captured")+": ")), ui.Text(source.CapturedText)),
			html.P(html.Props{Class: "persona-rereview-source-current", Dir: "auto"}, html.Span(html.Props{Class: "muted"}, ui.Text(issue3971Text(model, "source.current")+": ")), ui.Text(source.CurrentText)))
	}
	return html.Div(html.Props{Class: "persona-rereview-source", Data: map[string]string{"agent-rereview-source": source.Status}}, children...)
}
