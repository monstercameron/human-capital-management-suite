package productui

import (
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ChatRetentionPolicy is the tenant-wide chat retention projection. Date is a
// YYYY-MM-DD UTC date from the administrator's date control.
type ChatRetentionPolicy struct {
	Mode        string
	BeforeDate  string
	BudgetBytes int64
	Revision    uint64
}

// ChatRetentionSettingsProps is the narrow contract for the admin retention
// editor. The owning route loads and persists policies through ChatExtensions.
type ChatRetentionSettingsProps struct {
	I18nProps
	Configured bool
	Policy     ChatRetentionPolicy
	Loading    bool
	Saving     bool
	Editable   bool
	Error      string
	Notice     string
	OnSave     func(ChatRetentionPolicy)
}

type chatRetentionDraft struct {
	Source     ChatRetentionPolicy
	Configured bool
	Mode       string
	BeforeDate string
	BudgetMiB  string
	Confirm    bool
	Invalid    bool
}

type chatRetentionCopy struct {
	title, intro, currentPolicy, rule, preserveAll, beforeDate, diskBudget string
	dateHelp, budgetHelp, budgetUnit, review, save, saving, loading        string
	errorTitle, noticeTitle, confirmTitle, confirmBody, confirmSave        string
	cancel, noDeletion, invalidDate, invalidBudget                         string
	settingsTitle, settingsDescription, settingsAction                     string
}

func retentionCopy(locale LocaleContext) chatRetentionCopy {
	switch locale.Resolved {
	case "de-DE":
		return chatRetentionCopy{
			settingsTitle: "Chat-Einstellungen", settingsDescription: "Verwalten Sie die tenantweiten Einstellungen zur Chat-Aufbewahrung.", settingsAction: "Chat-Einstellungen öffnen",
			title: "Chat-Aufbewahrung", intro: "Legen Sie die Aufbewahrungsrichtlinie für Chatnachrichten im gesamten Mandanten fest.",
			currentPolicy: "Aktuelle Richtlinie", rule: "Aufbewahren nach", preserveAll: "Keine Richtlinie konfiguriert. Alle Nachrichten werden aufbewahrt.", beforeDate: "Vor einem Datum", diskBudget: "Speicherlimit",
			dateHelp: "Nachrichten vor diesem UTC-Datum können für die Aufbewahrungsverarbeitung ausgewählt werden.", budgetHelp: "Das Speicherlimit gilt für alle Unterhaltungen dieses Mandanten.", budgetUnit: "MiB", review: "Änderung prüfen", save: "Richtlinie speichern", saving: "Wird gespeichert …", loading: "Aufbewahrungsrichtlinie wird geladen …",
			errorTitle: "Speichern fehlgeschlagen", noticeTitle: "Gespeichert", confirmTitle: "Aufbewahrung verkürzen?", confirmBody: "Diese Änderung kann ältere Nachrichten für die Aufbewahrungsverarbeitung freigeben.", confirmSave: "Änderung bestätigen", cancel: "Abbrechen",
			noDeletion: "Das Speichern hält die Richtlinie fest. Die Durchsetzung ist noch nicht aktiv und es werden keine Nachrichten gelöscht.", invalidDate: "Wählen Sie ein gültiges Datum.", invalidBudget: "Geben Sie ein positives Speicherlimit ein.",
		}
	case "ar":
		return chatRetentionCopy{
			settingsTitle: "إعدادات الدردشة", settingsDescription: "أدر إعدادات الاحتفاظ برسائل الدردشة على مستوى المستأجر.", settingsAction: "فتح إعدادات الدردشة",
			title: "الاحتفاظ برسائل الدردشة", intro: "حدد مدة الاحتفاظ برسائل الدردشة على مستوى المستأجر.",
			currentPolicy: "السياسة الحالية", rule: "أساس الاحتفاظ", preserveAll: "لا توجد سياسة مهيأة. يتم الاحتفاظ بكل الرسائل.", beforeDate: "قبل تاريخ", diskBudget: "حد التخزين",
			dateHelp: "قد تصبح الرسائل السابقة لهذا التاريخ بتوقيت UTC مؤهلة لمعالجة الاحتفاظ.", budgetHelp: "ينطبق حد التخزين على جميع محادثات هذا المستأجر.", budgetUnit: "ميبيبايت", review: "مراجعة التغيير", save: "حفظ السياسة", saving: "جارٍ الحفظ…", loading: "جارٍ تحميل سياسة الاحتفاظ…",
			errorTitle: "تعذر الحفظ", noticeTitle: "تم الحفظ", confirmTitle: "تقليل مدة الاحتفاظ؟", confirmBody: "قد يجعل هذا التغيير الرسائل الأقدم مؤهلة لمعالجة الاحتفاظ.", confirmSave: "تأكيد التغيير", cancel: "إلغاء",
			noDeletion: "يحفظ هذا الإعداد السياسة فقط. لم يبدأ تطبيقها بعد ولا يحذف أي رسائل.", invalidDate: "اختر تاريخًا صالحًا.", invalidBudget: "أدخل حد تخزين موجبًا.",
		}
	default:
		return chatRetentionCopy{
			settingsTitle: "Chat settings", settingsDescription: "Manage tenant-wide chat retention settings.", settingsAction: "Open chat settings",
			title: "Chat retention", intro: "Choose how long chat messages are kept across this tenant.",
			currentPolicy: "Current policy", rule: "Retention rule", preserveAll: "No policy is configured. All messages are retained.", beforeDate: "Before a date", diskBudget: "Storage limit",
			dateHelp: "Messages before this UTC date may become eligible for retention processing.", budgetHelp: "The storage limit applies to every conversation in this tenant.", budgetUnit: "MiB", review: "Review change", save: "Save policy", saving: "Saving…", loading: "Loading retention policy…",
			errorTitle: "Couldn't save policy", noticeTitle: "Saved", confirmTitle: "Shorten retention?", confirmBody: "This change can make older messages eligible for retention processing.", confirmSave: "Confirm change", cancel: "Cancel",
			noDeletion: "This saves the policy only. Enforcement is not active yet, and saving it does not delete messages.", invalidDate: "Choose a valid date.", invalidBudget: "Enter a positive storage limit.",
		}
	}
}

func retentionDraft(policy ChatRetentionPolicy, configured bool) chatRetentionDraft {
	draft := chatRetentionDraft{Source: policy, Configured: configured, Mode: policy.Mode, BeforeDate: policy.BeforeDate}
	if !configured || draft.Mode == "" {
		draft.Mode = "BEFORE_DATE"
	}
	if policy.BudgetBytes > 0 && policy.BudgetBytes%(1<<20) == 0 {
		draft.BudgetMiB = strconv.FormatInt(policy.BudgetBytes/(1<<20), 10)
	}
	return draft
}

func ChatRetentionSettings(props ChatRetentionSettingsProps) ui.Node {
	copy := retentionCopy(props.Locale)
	state := ui.UseState(retentionDraft(props.Policy, props.Configured))
	draft := state.Get()
	if !reflect.DeepEqual(draft.Source, props.Policy) || draft.Configured != props.Configured {
		draft = retentionDraft(props.Policy, props.Configured)
		state.Set(draft)
	}
	useDrawerFocusTrap("chat-retention-confirm-dialog", "chat-retention-save", draft.Confirm)
	edit := func(update func(*chatRetentionDraft)) {
		next := state.Get()
		update(&next)
		next.Confirm, next.Invalid = false, false
		state.Set(next)
	}
	changed := !retentionDraftMatchesPolicy(draft)
	needsConfirmation := retentionDraftReducesRetention(draft)
	canSave := props.Editable && !props.Loading && !props.Saving && changed && retentionDraftValid(draft)
	commit := func() {
		policy, ok := retentionPolicyFromDraft(draft)
		if !ok || props.OnSave == nil {
			return
		}
		props.OnSave(policy)
	}
	modeProps := html.Props{ID: "chat-retention-mode", Name: "mode", Value: draft.Mode, Disabled: !props.Editable || props.Loading || props.Saving, Aria: map[string]string{"label": copy.rule}}
	modeProps.OnChange = ui.UseEvent(func(event ui.InputEvent) { edit(func(next *chatRetentionDraft) { next.Mode = event.GetValue() }) })
	budget := html.Props{ID: "chat-retention-budget", Name: "budget_mib", Type: "number", Min: "1", Step: "1", Value: draft.BudgetMiB, Disabled: !props.Editable || props.Loading || props.Saving, Required: true, Aria: map[string]string{"label": copy.diskBudget, "describedby": "chat-retention-budget-help"}}
	budget.OnInput = ui.UseEvent(func(event ui.InputEvent) { edit(func(next *chatRetentionDraft) { next.BudgetMiB = event.GetValue() }) })
	date := html.Props{ID: "chat-retention-date", Name: "before_date", Type: "date", Value: draft.BeforeDate, Disabled: !props.Editable || props.Loading || props.Saving, Required: true, Aria: map[string]string{"label": copy.beforeDate, "describedby": "chat-retention-date-help"}}
	date.OnInput = ui.UseEvent(func(event ui.InputEvent) { edit(func(next *chatRetentionDraft) { next.BeforeDate = event.GetValue() }) })
	confirmSave := ui.UseEvent(func(ui.MouseEvent) { commit() })
	closeConfirm := func() {
		if props.Saving {
			return
		}
		next := state.Get()
		next.Confirm = false
		state.Set(next)
	}
	cancelConfirm := ui.UseEvent(func(ui.MouseEvent) { closeConfirm() })
	var policyInput ui.Node
	if draft.Mode == "SIZE_BUDGET" {
		policyInput = html.Div(html.Props{Class: "chat-retention-policy-field"}, html.Label(html.Props{For: budget.ID}, ui.Text(copy.diskBudget)), html.Div(html.Props{Class: "chat-retention-budget-input"}, html.Input(budget), html.Span(html.Props{}, ui.Text(copy.budgetUnit))), html.P(html.Props{ID: "chat-retention-budget-help", Class: "muted"}, ui.Text(copy.budgetHelp)))
	} else {
		policyInput = html.Div(html.Props{Class: "chat-retention-policy-field"}, html.Label(html.Props{For: date.ID}, ui.Text(copy.beforeDate)), html.Input(date), html.P(html.Props{ID: "chat-retention-date-help", Class: "muted"}, ui.Text(copy.dateHelp)))
	}
	onReview := func(event ui.FormEvent) {
		event.PreventDefault()
		if !canSave {
			next := state.Get()
			next.Invalid = true
			state.Set(next)
			return
		}
		if needsConfirmation {
			next := state.Get()
			next.Confirm = true
			state.Set(next)
			return
		}
		commit()
	}
	children := []ui.Node{
		html.Section(html.Props{Class: "surface chat-retention-settings", Raw: map[string]any{"aria-labelledby": "chat-retention-title", "data-chat-retention": "settings"}},
			html.Header(html.Props{}, html.H2(html.Props{ID: "chat-retention-title"}, ui.Text(copy.title)), html.P(html.Props{Class: "muted"}, ui.Text(copy.intro))),
			html.P(html.Props{Class: "chat-retention-current", Raw: map[string]any{"aria-live": "polite"}}, ui.Text(retentionCurrentSummary(copy, props))),
			html.Form(html.Props{ID: "chat-retention-form", Class: "chat-retention-form", OnSubmit: ui.UseEvent(onReview)},
				html.Div(html.Props{Class: "chat-retention-field"}, html.Label(html.Props{For: modeProps.ID}, ui.Text(copy.rule)), html.Select(modeProps,
					html.Option(html.Props{Value: "BEFORE_DATE"}, ui.Text(copy.beforeDate)),
					html.Option(html.Props{Value: "SIZE_BUDGET"}, ui.Text(copy.diskBudget)),
				)),
				policyInput,
				html.P(html.Props{Class: "muted"}, ui.Text(copy.noDeletion)),
				validationMessage(copy, draft),
				html.Button(html.Props{ID: "chat-retention-save", Class: "button primary", Type: "submit", Disabled: !canSave}, ui.Text(map[bool]string{true: copy.saving, false: map[bool]string{true: copy.review, false: copy.save}[needsConfirmation]}[props.Saving])),
			),
		),
	}
	if props.Loading {
		children = append(children, html.P(html.Props{Class: "muted", Role: "status", Raw: map[string]any{"aria-live": "polite"}}, ui.Text(copy.loading)))
	}
	if strings.TrimSpace(props.Error) != "" {
		children = append(children, html.Div(html.Props{Class: "notice error", Role: "alert"}, html.Strong(html.Props{}, ui.Text(copy.errorTitle)), html.P(html.Props{}, ui.Text(props.Error))))
	}
	if strings.TrimSpace(props.Notice) != "" {
		children = append(children, html.Div(html.Props{Class: "notice success", Role: "status"}, html.Strong(html.Props{}, ui.Text(copy.noticeTitle)), html.P(html.Props{}, ui.Text(props.Notice))))
	}
	if draft.Confirm {
		children = append(children, html.Div(html.Props{Class: "chat-retention-confirm-backdrop"},
			html.Section(html.Props{ID: "chat-retention-confirm-dialog", Class: "surface chat-retention-confirm", Raw: map[string]any{"role": "alertdialog", "aria-modal": "true", "aria-labelledby": "chat-retention-confirm-title", "aria-describedby": "chat-retention-confirm-body", "tabindex": "-1"}, OnKeyDown: ui.UseEvent(func(event ui.KeyboardEvent) {
				if drawerEscapeCloses(event.GetKey()) {
					event.PreventDefault()
					closeConfirm()
				}
			})},
				html.H3(html.Props{ID: "chat-retention-confirm-title"}, ui.Text(copy.confirmTitle)),
				html.P(html.Props{ID: "chat-retention-confirm-body"}, ui.Text(copy.confirmBody)),
				html.P(html.Props{Class: "muted"}, ui.Text(copy.noDeletion)),
				html.Button(html.Props{Class: "button primary", Type: "button", Disabled: props.Saving, OnClick: confirmSave}, ui.Text(copy.confirmSave)),
				html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: props.Saving, OnClick: cancelConfirm}, ui.Text(copy.cancel)),
			),
		))
	}
	return html.Div(html.Props{Class: "chat-retention-settings-page", Raw: map[string]any{"dir": map[bool]string{true: "rtl", false: "ltr"}[props.Locale.Direction == "rtl"]}}, children...)
}

func validationMessage(copy chatRetentionCopy, draft chatRetentionDraft) ui.Node {
	if !draft.Invalid {
		return html.Span(html.Props{Class: "sr-only", ID: "chat-retention-validation"}, ui.Text(""))
	}
	message := copy.invalidDate
	if draft.Mode == "SIZE_BUDGET" {
		message = copy.invalidBudget
	}
	return html.P(html.Props{Class: "field-error", ID: "chat-retention-validation", Role: "alert"}, ui.Text(message))
}

func retentionCurrentSummary(copy chatRetentionCopy, props ChatRetentionSettingsProps) string {
	if !props.Configured {
		return copy.preserveAll
	}
	if props.Policy.Mode == "SIZE_BUDGET" && props.Policy.BudgetBytes > 0 {
		return copy.diskBudget + ": " + strconv.FormatInt(props.Policy.BudgetBytes/(1<<20), 10) + " " + copy.budgetUnit
	}
	if props.Policy.Mode == "BEFORE_DATE" && props.Policy.BeforeDate != "" {
		return copy.beforeDate + ": " + props.Policy.BeforeDate
	}
	return copy.preserveAll
}

func retentionDraftMatchesPolicy(draft chatRetentionDraft) bool {
	policy, ok := retentionPolicyFromDraft(draft)
	if !ok {
		return false
	}
	if !draft.Configured {
		return false
	}
	return policy.Mode == draft.Source.Mode && policy.BeforeDate == draft.Source.BeforeDate && policy.BudgetBytes == draft.Source.BudgetBytes
}

func retentionDraftReducesRetention(draft chatRetentionDraft) bool {
	if !draft.Configured || !retentionDraftValid(draft) {
		return false
	}
	policy, ok := retentionPolicyFromDraft(draft)
	if !ok {
		return false
	}
	if policy.Mode != draft.Source.Mode {
		return true
	}
	switch policy.Mode {
	case "BEFORE_DATE":
		return policy.BeforeDate > draft.Source.BeforeDate
	case "SIZE_BUDGET":
		return policy.BudgetBytes < draft.Source.BudgetBytes
	default:
		return false
	}
}

func retentionDraftValid(draft chatRetentionDraft) bool {
	_, ok := retentionPolicyFromDraft(draft)
	return ok
}

func retentionPolicyFromDraft(draft chatRetentionDraft) (ChatRetentionPolicy, bool) {
	switch draft.Mode {
	case "BEFORE_DATE":
		if len(draft.BeforeDate) != 10 {
			return ChatRetentionPolicy{}, false
		}
		if _, err := time.Parse("2006-01-02", draft.BeforeDate); err != nil {
			return ChatRetentionPolicy{}, false
		}
		return ChatRetentionPolicy{Mode: draft.Mode, BeforeDate: draft.BeforeDate, Revision: draft.Source.Revision}, true
	case "SIZE_BUDGET":
		bytes, ok := RetentionBudgetBytes(draft.BudgetMiB)
		if !ok {
			return ChatRetentionPolicy{}, false
		}
		return ChatRetentionPolicy{Mode: draft.Mode, BudgetBytes: bytes, Revision: draft.Source.Revision}, true
	default:
		return ChatRetentionPolicy{}, false
	}
}

// RetentionBudgetBytes converts an integer MiB form value into bytes while
// rejecting zero, fractions, and values that overflow the signed wire field.
func RetentionBudgetBytes(mib string) (int64, bool) {
	value, err := strconv.ParseUint(strings.TrimSpace(mib), 10, 64)
	if err != nil || value == 0 || value > uint64(math.MaxInt64)/(1<<20) {
		return 0, false
	}
	return int64(value * (1 << 20)), true
}
