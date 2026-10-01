package timeclockapp

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
)

// KioskViewProps supplies the model and an optional action context to the
// reference tablet view. Callers own rerendering after model mutations.
type KioskViewProps struct {
	Model    *Kiosk
	Context  context.Context
	Now      time.Time
	OnEnroll func(string)
	OnPIN    func(string)
	OnBadge  func(string, bool)
	OnPunch  func(timev1.PunchEventType)
	OnCancel func()
	OnDone   func()
	OnStart  func()
	OnLocale func(Locale)
}

// Render is the stable page-mount entry point for the GWC client.
func Render(model *Kiosk, ctx context.Context, now time.Time) ui.Node {
	return KioskView(KioskViewProps{Model: model, Context: ctx, Now: now})
}

// KioskView renders the complete kiosk surface. It intentionally has no links
// or routes outside the enrollment and shared-device flow.
func KioskView(props KioskViewProps) ui.Node {
	model := props.Model
	if model == nil {
		return html.Main(html.Props{Class: "timeclock-kiosk", Role: "main"}, html.H1(html.Props{}, ui.Text("Time clock")))
	}
	if props.Now.IsZero() {
		props.Now = model.now()
	}
	loc := SiteLocation("")
	if model.device != nil {
		loc = SiteLocation(model.device.Timezone)
	}
	face := model.locale.Face(props.Now, loc)
	children := []ui.Node{localeSelect(model, props.OnLocale), kioskHeader(model, face)}
	switch model.screen {
	case ScreenEnroll:
		children = append(children, enrollView(model, props.Context, props.OnEnroll))
	case ScreenIdentify:
		children = append(children, identifyView(model, props.OnPIN, props.OnBadge))
	case ScreenConfirm:
		children = append(children, confirmView(model, props.OnPunch, props.OnCancel))
	case ScreenReceipt:
		children = append(children, receiptView(model, props.OnDone))
	case ScreenRevoked:
		children = append(children, html.Section(html.Props{Class: "timeclock-panel", Role: "alert"}, html.H1(html.Props{}, ui.Text(model.locale.Text("revoked.heading"))), html.P(html.Props{}, ui.Text(model.locale.Text("revoked.help")))))
	case ScreenRecovery:
		children = append(children, html.Section(html.Props{Class: "timeclock-panel", Role: "alert"}, html.H1(html.Props{}, ui.Text("Stored kiosk data needs administrator recovery")), html.P(html.Props{}, ui.Text("No worker state or queued punch was changed."))))
	default:
		children = append(children, idleView(model, props.OnStart))
	}
	return html.Main(html.Props{Class: "timeclock-kiosk", Dir: model.locale.Dir(), Role: "main", Data: map[string]string{"kiosk": "true", "screen": string(model.screen)}}, children...)
}

func kioskHeader(model *Kiosk, face ClockFace) ui.Node {
	onlineKey := "bar.offline"
	if model.online {
		onlineKey = "bar.online"
	}
	queueKey := "bar.queue"
	args := []string{"n", model.locale.Digits(strconv.Itoa(len(model.queue)))}
	if len(model.queue) == 0 {
		queueKey, args = "bar.queue_none", nil
	}
	site := ""
	if model.device != nil {
		site = model.device.SiteID
	}
	return html.Header(html.Props{Class: "timeclock-header"}, html.Div(html.Props{Class: "timeclock-site"}, ui.Text(model.locale.Text("bar.site", "site", site))), html.Div(html.Props{Class: "timeclock-status", Role: "status"}, html.Span(html.Props{Class: "timeclock-dot", Aria: map[string]string{"hidden": "true"}}, ui.Text("•")), ui.Text(model.locale.Text(onlineKey)), html.Span(html.Props{Class: "timeclock-queue"}, ui.Text(model.locale.Text(queueKey, args...)))), html.Time(html.Props{Class: "timeclock-face", Raw: map[string]any{"dateTime": face.ISO}}, ui.Text(face.HourMinute), html.Span(html.Props{Class: "timeclock-seconds"}, ui.Text(":"+face.Seconds)), html.Span(html.Props{Class: "timeclock-meridiem"}, ui.Text(face.Meridiem))))
}

func localeSelect(model *Kiosk, callback func(Locale)) ui.Node {
	props := html.Props{ID: "timeclock-locale", Name: "locale", Aria: map[string]string{"label": model.locale.Text("locale.label")}}
	if callback != nil {
		props.OnChange = ui.UseEvent(func(event ui.InputEvent) { callback(ResolveLocale(event.GetValue())) })
	}
	options := make([]ui.Node, 0, len(Locales()))
	for _, locale := range Locales() {
		options = append(options, html.Option(html.Props{Value: string(locale), Selected: locale == model.locale}, ui.Text(locale.Native())))
	}
	return html.Label(html.Props{Class: "timeclock-locale"}, ui.Text(model.locale.Text("locale.label")), html.Select(props, options...))
}

func enrollView(model *Kiosk, ctx context.Context, callback func(string)) ui.Node {
	code := html.Input(html.Props{ID: "timeclock-enrollment-code", Name: "enrollment_code", Type: "text", AutoComplete: "one-time-code", Required: true, Aria: map[string]string{"describedby": "timeclock-enrollment-help"}})
	button := html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: model.busy}, ui.Text(model.locale.Text("enroll.submit")))
	_ = ctx
	form := html.Props{Class: "timeclock-panel", ID: "timeclock-enroll"}
	if callback != nil {
		form.OnSubmit = ui.UseEvent(func(event ui.FormEvent) { event.PreventDefault(); callback(event.GetValue()) })
	}
	_ = ctx
	return html.Form(form, html.H1(html.Props{}, ui.Text(model.locale.Text("enroll.heading"))), html.P(html.Props{ID: "timeclock-enrollment-help"}, ui.Text(model.locale.Text("enroll.help"))), html.Label(html.Props{For: "timeclock-enrollment-code"}, ui.Text(model.locale.Text("enroll.code_label"))), code, button, errorNode(model))
}

func identifyView(model *Kiosk, onPIN func(string), onBadge func(string, bool)) ui.Node {
	submit := html.Button(html.Props{Class: "button primary", Type: "button", Disabled: model.PINDraft() == "", OnClick: ui.UseEvent(func(ui.MouseEvent) {
		pin := model.PINDraft()
		if pin != "" && onPIN != nil {
			onPIN(pin)
		}
	})}, ui.Text(model.locale.Text("identify.submit")))
	draft := strings.Repeat("•", len(model.PINDraft()))
	return html.Section(html.Props{Class: "timeclock-panel", Aria: map[string]string{"labelledby": "timeclock-identify-title"}}, html.H1(html.Props{ID: "timeclock-identify-title"}, ui.Text(model.locale.Text("identify.heading"))), html.P(html.Props{Class: "timeclock-pin-draft", Role: "status", Aria: map[string]string{"label": model.locale.Text("identify.keypad")}}, ui.Text(draft)), html.Div(html.Props{Class: "timeclock-keypad", Role: "group", Aria: map[string]string{"label": model.locale.Text("identify.keypad")}}, keypadButtons(model, onPIN)...), submit, html.Button(html.Props{Class: "button secondary", Type: "button", OnClick: ui.UseEvent(func(ui.MouseEvent) {
		if onBadge != nil {
			onBadge("", false)
		}
	})}, ui.Text(model.locale.Text("badge.submit"))), errorNode(model))
}

func keypadButtons(model *Kiosk, onPIN func(string)) []ui.Node {
	locale := model.locale
	buttons := make([]ui.Node, 0, 12)
	for i := 1; i <= 9; i++ {
		digit := strconv.Itoa(i)
		buttons = append(buttons, html.Button(html.Props{Type: "button", Class: "timeclock-key", Aria: map[string]string{"label": locale.Digits(digit)}, OnClick: ui.UseEvent(func(ui.MouseEvent) {
			model.AppendPIN(digit)
		})}, ui.Text(locale.Digits(digit))))
	}
	buttons = append(buttons, html.Button(html.Props{Type: "button", Class: "timeclock-key secondary", OnClick: ui.UseEvent(func(ui.MouseEvent) { model.ClearPIN() })}, ui.Text(locale.Text("identify.clear"))), html.Button(html.Props{Type: "button", Class: "timeclock-key", OnClick: ui.UseEvent(func(ui.MouseEvent) { model.AppendPIN("0") })}, ui.Text(locale.Digits("0"))), html.Button(html.Props{Type: "button", Class: "timeclock-key secondary", Aria: map[string]string{"label": locale.Text("identify.backspace")}, OnClick: ui.UseEvent(func(ui.MouseEvent) { model.BackspacePIN() })}, ui.Text("⌫")))
	return buttons
}

func idleView(model *Kiosk, onStart func()) ui.Node {
	return html.Section(html.Props{Class: "timeclock-panel timeclock-idle", Aria: map[string]string{"labelledby": "timeclock-idle-title"}}, html.H1(html.Props{ID: "timeclock-idle-title"}, ui.Text(model.locale.Text("idle.start"))), html.P(html.Props{}, ui.Text(model.locale.Text("idle.hint"))), html.Button(html.Props{Class: "button primary", Type: "button", OnClick: ui.UseEvent(func(ui.MouseEvent) {
		model.BeginIdentify()
		if onStart != nil {
			onStart()
		}
	})}, ui.Text(model.locale.Text("identify.submit"))))
}

func confirmView(model *Kiosk, onPunch func(timev1.PunchEventType), onCancel func()) ui.Node {
	name := ""
	if model.worker != nil {
		name = model.worker.DisplayName
	}
	inClick := func(event timev1.PunchEventType) ui.Handler {
		return ui.UseEvent(func(ui.MouseEvent) {
			if onPunch != nil {
				onPunch(event)
			}
		})
	}
	return html.Section(html.Props{Class: "timeclock-panel", Aria: map[string]string{"labelledby": "timeclock-confirm-title"}}, html.H1(html.Props{ID: "timeclock-confirm-title"}, ui.Text(model.locale.Text("confirm.greeting", "name", name))), html.P(html.Props{Class: "timeclock-status-copy", Role: "status"}, ui.Text(confirmStatus(model))), html.Div(html.Props{Class: "timeclock-actions"}, html.Button(html.Props{Class: "button primary", Type: "button", OnClick: inClick(timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_IN)}, ui.Text(model.locale.Text("action.in"))), html.Button(html.Props{Class: "button secondary", Type: "button", OnClick: inClick(timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_OUT)}, ui.Text(model.locale.Text("action.out")))), html.Button(html.Props{Class: "button text", Type: "button", OnClick: ui.UseEvent(func(ui.MouseEvent) {
		if onCancel != nil {
			onCancel()
		}
	})}, ui.Text(model.locale.Text("common.cancel"))))
}

func confirmStatus(model *Kiosk) string {
	if model.worker == nil {
		return model.locale.Text("confirm.status_unknown")
	}
	switch model.worker.SessionStatus {
	case timev1.SessionStatus_SESSION_STATUS_OPEN, timev1.SessionStatus_SESSION_STATUS_ON_BREAK:
		return model.locale.Text("confirm.status_in")
	case timev1.SessionStatus_SESSION_STATUS_CLOSED:
		return model.locale.Text("confirm.status_out")
	default:
		return model.locale.Text("confirm.status_unknown")
	}
}

func receiptView(model *Kiosk, onDone func()) ui.Node {
	receipt := model.receipt
	title := model.locale.Text("receipt.queued")
	detail := model.locale.Text("receipt.queued_detail")
	if !receipt.Queued {
		switch receipt.Status {
		case timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_ACCEPTED:
			title = model.locale.Text("receipt.accepted_in")
			detail = model.locale.Text("receipt.accepted_detail")
		case timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_DUPLICATE:
			title, detail = model.locale.Text("receipt.duplicate"), model.locale.Text("receipt.duplicate_detail")
		case timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_HELD_FOR_REVIEW:
			title, detail = model.locale.Text("receipt.held"), model.locale.Text("receipt.held_detail")
		case timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_REJECTED:
			title, detail = model.locale.Text("receipt.rejected"), model.locale.Text("receipt.rejected_detail", "reason", rejectionText(model.locale, receipt.Reason, receipt.Detail))
		}
	}
	return html.Section(html.Props{Class: "timeclock-panel timeclock-receipt", Role: "status", Aria: map[string]string{"live": "polite"}}, html.H1(html.Props{}, ui.Text(title)), html.P(html.Props{}, ui.Text(detail)), html.P(html.Props{Class: "muted"}, ui.Text(model.locale.Text("receipt.sequence", "n", model.locale.Digits(strconv.FormatUint(receipt.Sequence, 10))))), html.Button(html.Props{Class: "button primary", Type: "button", OnClick: ui.UseEvent(func(ui.MouseEvent) {
		if onDone != nil {
			onDone()
		}
	})}, ui.Text(model.locale.Text("receipt.done"))))
}

func rejectionText(locale Locale, reason timev1.PunchRejectionReason, detail string) string {
	if strings.TrimSpace(detail) != "" {
		return detail
	}
	keys := map[timev1.PunchRejectionReason]string{1: "reason.unknown_worker", 2: "reason.not_on_roster", 3: "reason.device_not_eligible", 4: "reason.policy_violation", 5: "reason.sequence_gap", 6: "reason.malformed", 7: "reason.stale_roster", 8: "reason.duplicate_rejected"}
	if key, ok := keys[reason]; ok {
		return locale.Text(key)
	}
	return locale.Text("reason.unspecified")
}

func errorNode(model *Kiosk) ui.Node {
	if model.err == nil {
		return html.Span(html.Props{Class: "sr-only", ID: "timeclock-error"}, ui.Text(""))
	}
	return html.P(html.Props{Class: "field-error", ID: "timeclock-error", Role: "alert"}, ui.Text(model.err.Error()))
}
