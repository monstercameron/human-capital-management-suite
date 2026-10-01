package productui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// KioskScreen is the finite screen set for the shared tablet. There is no
// workspace or admin route in this state space.
type KioskScreen string

const (
	KioskScreenEnroll   KioskScreen = "enroll"
	KioskScreenIdle     KioskScreen = "idle"
	KioskScreenIdentify KioskScreen = "identify"
	KioskScreenConfirm  KioskScreen = "confirm"
	KioskScreenReceipt  KioskScreen = "receipt"
	KioskScreenRevoked  KioskScreen = "revoked"
	KioskScreenRecovery KioskScreen = "recovery"
)

// KioskPhase is the worker's position in the current session as the device
// last learned it. The zero value means unknown, as when the clock is offline.
type KioskPhase string

const (
	KioskPhaseOut   KioskPhase = "out"
	KioskPhaseIn    KioskPhase = "in"
	KioskPhaseBreak KioskPhase = "break"
)

// KioskPunchEvent names the public device punch variants exposed by the
// reference client. Attestation, tips, and transfer data travel with the same
// device punch contract rather than through private workspace endpoints.
type KioskPunchEvent string

const (
	KioskPunchClockIn     KioskPunchEvent = "clock-in"
	KioskPunchClockOut    KioskPunchEvent = "clock-out"
	KioskPunchAttestation KioskPunchEvent = "attestation"
	KioskPunchTips        KioskPunchEvent = "tips"
	KioskPunchJobTransfer KioskPunchEvent = "job-transfer"
)

// KioskPunchRequest is the safe, presentation-level input handed to the
// device client. It contains no tenant, worker, token, or admin identity.
type KioskPunchRequest struct {
	Event          KioskPunchEvent
	Attestation    string
	TipDeclaration string
	JobID          string
	CostCodeID     string
}

// KioskProjection is the client-owned, display-safe state for one kiosk
// render. Worker state is transient and is never a navigation destination.
type KioskProjection struct {
	Screen       KioskScreen
	Online       bool
	Busy         bool
	QueueDepth   int
	SiteLabel    string
	WorkerLabel  string
	ShiftLabel   string
	StatusLabel  string
	ReceiptLabel string
	ReceiptSeq   uint64
	ErrorLabel   string
	Attestation  string
	Tips         string
	JobID        string
	CostCodeID   string

	// Phase is the worker's session state, when the device knows it. It
	// decides which single punch the confirm screen offers first.
	Phase KioskPhase
	// AutoResetSeconds is how long the receipt stays before the device
	// returns to the start screen. Zero hides the countdown.
	AutoResetSeconds int

	// ReceiptEvent and ReceiptTime let the receipt say what was recorded and
	// when, in the worker's own words, instead of a device sequence number.
	ReceiptEvent KioskPunchEvent
	ReceiptTime  string

	OnStart       func()
	OnEnroll      func(string)
	OnIdentify    func(string)
	OnPunch       func(KioskPunchRequest)
	OnDone        func()
	OnCancel      func()
	OnDraftChange func(KioskPunchRequest)
}

// KioskPage renders the standalone tablet surface. It intentionally emits no
// anchors: the managed device can only identify, display, and submit public
// clock-device actions.
func KioskPage(view View, projection KioskProjection) ui.Node {
	locale := view.Locale.normalized()
	stage := []ui.Node{}
	switch projection.Screen {
	case KioskScreenEnroll:
		stage = append(stage, kioskEnroll(locale, projection))
	case KioskScreenIdentify:
		stage = append(stage, kioskIdentify(locale, projection))
	case KioskScreenConfirm:
		stage = append(stage, kioskConfirm(locale, projection))
	case KioskScreenReceipt:
		stage = append(stage, kioskReceipt(locale, projection))
	case KioskScreenRevoked:
		stage = append(stage, kioskAlert(locale, "revoked"))
	case KioskScreenRecovery:
		stage = append(stage, kioskAlert(locale, "recovery"))
	default:
		stage = append(stage, kioskIdle(locale, projection))
	}
	notices := []ui.Node{}
	if !projection.Online && (projection.Screen == KioskScreenIdle || projection.Screen == KioskScreenIdentify || projection.Screen == "") {
		notices = append(notices, html.P(html.Props{Class: "kiosk-offline", Role: "status"}, ui.Text(kioskCopy(locale, "offline_help"))))
	}
	if strings.TrimSpace(projection.ErrorLabel) != "" {
		notices = append(notices, html.P(html.Props{Class: "kiosk-error", Role: "alert"}, ui.Text(projection.ErrorLabel)))
	}
	stage = append(notices, stage...)
	children := []ui.Node{kioskHeader(locale, projection), html.Div(html.Props{Class: "kiosk-stage"}, stage...)}
	return html.Main(html.Props{
		Class: "timeclock-kiosk",
		Role:  "main",
		Dir:   string(locale.Direction),
		Data:  map[string]string{"kiosk": "true", "screen": string(projection.Screen)},
	}, children...)
}

func kioskHeader(locale LocaleContext, projection KioskProjection) ui.Node {
	connection := kioskCopy(locale, "offline")
	state := "offline"
	if projection.Online {
		connection = kioskCopy(locale, "online")
		state = "online"
	}
	queue := kioskCopy(locale, "queue_empty")
	if projection.QueueDepth > 0 {
		queue = strings.ReplaceAll(kioskCopy(locale, "queue"), "{n}", strconv.Itoa(projection.QueueDepth))
	}
	identity := html.Div(html.Props{Class: "kiosk-identity"},
		html.H1(html.Props{Class: "kiosk-title"}, ui.Text(kioskCopy(locale, "title"))),
		html.P(html.Props{Class: "kiosk-site"}, ui.Text(projection.SiteLabel)),
	)
	if projection.Screen == KioskScreenRevoked || projection.Screen == KioskScreenRecovery {
		// A retired or unreadable device has no connection story to tell.
		return html.Header(html.Props{Class: "kiosk-header"}, identity)
	}
	return html.Header(html.Props{Class: "kiosk-header"},
		identity,
		html.Div(html.Props{Class: "kiosk-connection", Role: "status", Data: map[string]string{"state": state}, Aria: map[string]string{"live": "polite"}},
			html.Span(html.Props{Class: "kiosk-dot", Raw: map[string]any{"aria-hidden": "true"}}),
			html.Span(html.Props{Class: "kiosk-connection-label"}, ui.Text(connection)),
			html.Span(html.Props{Class: "kiosk-queue"}, ui.Text(queue)),
		),
	)
}

func kioskEnroll(locale LocaleContext, projection KioskProjection) ui.Node {
	input := html.Input(html.Props{ID: "kiosk-enrollment-code", Name: "enrollment_code", Type: "text", Required: true, AutoComplete: "one-time-code", Aria: map[string]string{"describedby": "kiosk-enrollment-help"}})
	form := html.Props{ID: "kiosk-enroll", Class: "kiosk-panel kiosk-enroll"}
	if projection.OnEnroll != nil {
		form.OnSubmit = ui.UseEvent(func(event ui.FormEvent) { event.PreventDefault(); projection.OnEnroll(event.GetValue()) })
	}
	return html.Form(form,
		html.H2(html.Props{}, ui.Text(kioskCopy(locale, "enroll_title"))),
		html.P(html.Props{ID: "kiosk-enrollment-help", Class: "kiosk-help"}, ui.Text(kioskCopy(locale, "enroll_help"))),
		html.Label(html.Props{For: "kiosk-enrollment-code", Class: "kiosk-label"}, ui.Text(kioskCopy(locale, "enroll_code"))), input,
		kioskSubmitButton(kioskCopy(locale, "enroll"), "primary", projection.Busy),
	)
}

// kioskIdle is the resting screen. It is the identify panel itself, so a
// worker walking up sees the keypad at once instead of tapping through a
// start screen first.
func kioskIdle(locale LocaleContext, projection KioskProjection) ui.Node {
	return ui.CreateElement(kioskIdentifyPanel, kioskIdentifyProps{Locale: locale, Projection: projection})
}

func kioskIdentify(locale LocaleContext, projection KioskProjection) ui.Node {
	return ui.CreateElement(kioskIdentifyPanel, kioskIdentifyProps{Locale: locale, Projection: projection})
}

type kioskIdentifyProps struct {
	Locale     LocaleContext
	Projection KioskProjection
}

// kioskIdentifyPanel collects a PIN from the on-screen keypad, or a badge or
// QR code typed by a scanner into the same field. The field is masked so a
// PIN is never readable over a shoulder; the keypad keeps a tablet's own
// keyboard from covering the screen.
func kioskIdentifyPanel(props kioskIdentifyProps) ui.Node {
	locale, projection := props.Locale, props.Projection
	value := ui.UseState("")
	current := value.Get()
	press := func(digit string) ui.Handler {
		return ui.UseEvent(func(ui.MouseEvent) { value.Set(value.Get() + digit) })
	}
	inputProps := html.Props{
		ID: "kiosk-credential", Name: "credential", Type: "text", Value: current, Required: true, AutoComplete: "off",
		Class: "kiosk-credential", Placeholder: kioskCopy(locale, "credential_placeholder"),
		Aria: map[string]string{"describedby": "kiosk-credential-help"},
		Raw:  map[string]any{"inputmode": "none", "autocapitalize": "off", "spellcheck": "false"},
		OnInput: ui.UseEvent(func(event ui.InputEvent) {
			value.Set(event.GetValue())
		}),
	}
	keys := make([]ui.Node, 0, 12)
	for digit := 1; digit <= 9; digit++ {
		label := strconv.Itoa(digit)
		keys = append(keys, html.Button(html.Props{Class: "kiosk-key", Type: "button", Disabled: !projection.Online, OnClick: press(label)}, ui.Text(label)))
	}
	keys = append(keys,
		html.Button(html.Props{Class: "kiosk-key kiosk-key-quiet", Type: "button", OnClick: ui.UseEvent(func(ui.MouseEvent) { value.Set("") })}, ui.Text(kioskCopy(locale, "key_clear"))),
		html.Button(html.Props{Class: "kiosk-key", Type: "button", Disabled: !projection.Online, OnClick: press("0")}, ui.Text("0")),
		html.Button(html.Props{Class: "kiosk-key kiosk-key-quiet", Type: "button", Aria: map[string]string{"label": kioskCopy(locale, "key_backspace")}, OnClick: ui.UseEvent(func(ui.MouseEvent) {
			text := value.Get()
			if text != "" {
				runes := []rune(text)
				value.Set(string(runes[:len(runes)-1]))
			}
		})}, ui.Text("⌫")),
	)
	form := html.Props{ID: "kiosk-identify", Class: "kiosk-panel kiosk-identify", OnSubmit: ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		credential := strings.TrimSpace(value.Get())
		if credential == "" || projection.Busy || projection.OnIdentify == nil {
			return
		}
		projection.OnIdentify(credential)
		value.Set("")
	})}
	submit := kioskCopy(locale, "identify")
	if projection.Busy {
		submit = kioskCopy(locale, "checking")
	}
	return html.Form(form,
		html.H2(html.Props{ID: "kiosk-identify-title"}, ui.Text(kioskCopy(locale, "start"))),
		html.P(html.Props{ID: "kiosk-credential-help", Class: "kiosk-help"}, ui.Text(kioskCopy(locale, "identify_help"))),
		html.Label(html.Props{For: "kiosk-credential", Class: "kiosk-label sr-only"}, ui.Text(kioskCopy(locale, "credential"))),
		html.Input(inputProps),
		html.Div(html.Props{Class: "kiosk-keypad", Role: "group", Dir: "ltr", Aria: map[string]string{"label": kioskCopy(locale, "keypad")}}, keys...),
		html.Button(html.Props{Class: "button primary kiosk-submit", Type: "submit", Disabled: projection.Busy || strings.TrimSpace(current) == ""}, ui.Text(submit)),
	)
}

func kioskConfirm(locale LocaleContext, projection KioskProjection) ui.Node {
	name := strings.TrimSpace(projection.WorkerLabel)
	greeting := kioskCopy(locale, "hello")
	if name != "" {
		greeting = strings.ReplaceAll(kioskCopy(locale, "hello_name"), "{name}", name)
	}
	status := []ui.Node{}
	if label := strings.TrimSpace(projection.StatusLabel); label != "" {
		status = append(status, html.P(html.Props{Class: "kiosk-state", Data: map[string]string{"phase": string(projection.Phase)}},
			html.Span(html.Props{Class: "kiosk-dot", Raw: map[string]any{"aria-hidden": "true"}}), ui.Text(label)))
	}
	if shift := strings.TrimSpace(projection.ShiftLabel); shift != "" {
		status = append(status, html.P(html.Props{Class: "kiosk-shift"}, ui.Text(shift)))
	}
	primary := kioskPrimaryActions(locale, projection)
	// Each question sits with the one button that sends it, so it is clear
	// what a button applies to.
	details := html.Details(html.Props{Class: "kiosk-details"},
		html.Summary(html.Props{}, ui.Text(kioskCopy(locale, "details"))),
		html.Div(html.Props{Class: "kiosk-details-body"},
			html.Div(html.Props{Class: "kiosk-detail-group"},
				kioskDraftInput(locale, "kiosk-attestation", "attestation", projection.Attestation, projection),
				kioskActionButton(locale, "attest", KioskPunchAttestation, projection, "secondary"),
			),
			html.Div(html.Props{Class: "kiosk-detail-group"},
				kioskDraftInput(locale, "kiosk-tips", "tips", projection.Tips, projection),
				kioskActionButton(locale, "declare_tips", KioskPunchTips, projection, "secondary"),
			),
			html.Div(html.Props{Class: "kiosk-detail-group"},
				kioskDraftInput(locale, "kiosk-job", "job", projection.JobID, projection),
				kioskDraftInput(locale, "kiosk-cost-code", "cost_code", projection.CostCodeID, projection),
				kioskActionButton(locale, "transfer", KioskPunchJobTransfer, projection, "secondary"),
			),
		),
	)
	head := append([]ui.Node{html.H2(html.Props{ID: "kiosk-status-title", Class: "kiosk-greeting"}, ui.Text(greeting))}, status...)
	body := append(head, primary, details, kioskButton(kioskCopy(locale, "not_you"), "secondary kiosk-cancel", projection.OnCancel))
	return html.Div(html.Props{Class: "kiosk-confirm"},
		html.Section(html.Props{Class: "kiosk-panel kiosk-hello", Data: map[string]string{"phase": string(projection.Phase)}, Aria: map[string]string{"labelledby": "kiosk-status-title"}}, body...),
	)
}

// kioskPrimaryActions offers the one punch that follows from the worker's
// state. Only when the state is unknown (an offline clock cannot ask the
// server) do both appear, with equal weight and an explicit question.
func kioskPrimaryActions(locale LocaleContext, projection KioskProjection) ui.Node {
	switch projection.Phase {
	case KioskPhaseIn, KioskPhaseBreak:
		return html.Div(html.Props{Class: "kiosk-actions"}, kioskActionButton(locale, "clock_out", KioskPunchClockOut, projection, "primary kiosk-action kiosk-action-out"))
	case KioskPhaseOut:
		return html.Div(html.Props{Class: "kiosk-actions"}, kioskActionButton(locale, "clock_in", KioskPunchClockIn, projection, "primary kiosk-action"))
	}
	return html.Div(html.Props{Class: "kiosk-actions kiosk-actions-pair"},
		html.P(html.Props{Class: "kiosk-help kiosk-actions-question"}, ui.Text(kioskCopy(locale, "which"))),
		kioskActionButton(locale, "clock_in", KioskPunchClockIn, projection, "primary kiosk-action"),
		kioskActionButton(locale, "clock_out", KioskPunchClockOut, projection, "primary kiosk-action kiosk-action-out"),
	)
}

func kioskDraftInput(locale LocaleContext, id, key, value string, projection KioskProjection) ui.Node {
	props := html.Props{ID: id, Name: key, Type: "text", Value: value}
	if projection.OnDraftChange != nil {
		props.OnInput = ui.UseEvent(func(event ui.InputEvent) {
			draft := KioskPunchRequest{Attestation: projection.Attestation, TipDeclaration: projection.Tips, JobID: projection.JobID, CostCodeID: projection.CostCodeID}
			switch key {
			case "attestation":
				draft.Attestation = event.GetValue()
			case "tips":
				draft.TipDeclaration = event.GetValue()
			case "job":
				draft.JobID = event.GetValue()
			case "cost_code":
				draft.CostCodeID = event.GetValue()
			}
			projection.OnDraftChange(draft)
		})
	}
	return html.Label(html.Props{For: id, Class: "kiosk-field"}, html.Span(html.Props{Class: "kiosk-label"}, ui.Text(kioskCopy(locale, key))), html.Input(props))
}

func kioskActionButton(locale LocaleContext, key string, event KioskPunchEvent, projection KioskProjection, class string) ui.Node {
	return kioskButton(kioskCopy(locale, key), class, func() {
		if projection.Busy {
			return
		}
		if projection.OnPunch != nil {
			projection.OnPunch(KioskPunchRequest{Event: event, Attestation: projection.Attestation, TipDeclaration: projection.Tips, JobID: projection.JobID, CostCodeID: projection.CostCodeID})
		}
	})
}

func kioskReceipt(locale LocaleContext, projection KioskProjection) ui.Node {
	sequence := strings.ReplaceAll(kioskCopy(locale, "receipt_sequence"), "{n}", strconv.FormatUint(projection.ReceiptSeq, 10))
	title := strings.TrimSpace(projection.ReceiptLabel)
	if title == "" {
		title = kioskCopy(locale, "receipt")
	}
	children := []ui.Node{
		html.Span(html.Props{Class: "kiosk-receipt-mark", Raw: map[string]any{"aria-hidden": "true"}},
			html.Tag("svg", html.Props{Class: "kiosk-receipt-icon", Raw: map[string]any{"viewBox": "0 0 24 24", "fill": "none", "stroke": "currentColor", "stroke-width": "2.5", "stroke-linecap": "round", "stroke-linejoin": "round"}},
				html.Tag("path", html.Props{Raw: map[string]any{"d": "M5 12.5l4.5 4.5L19 7.5"}}))),
		html.H2(html.Props{}, ui.Text(title)),
		kioskReceiptSentence(locale, projection),
		html.P(html.Props{Class: "kiosk-help"}, ui.Text(sequence)),
		kioskButton(kioskCopy(locale, "done"), "primary kiosk-done", projection.OnDone),
	}
	if projection.AutoResetSeconds > 0 {
		reset := strings.ReplaceAll(kioskCopy(locale, "return_soon"), "{s}", strconv.Itoa(projection.AutoResetSeconds))
		children = append(children, html.P(html.Props{Class: "kiosk-return"}, ui.Text(reset)),
			html.Div(html.Props{Class: "kiosk-return-bar", Style: map[string]string{"--kiosk-return": strconv.Itoa(projection.AutoResetSeconds) + "s"}, Raw: map[string]any{"aria-hidden": "true"}}))
	}
	return html.Section(html.Props{Class: "kiosk-panel kiosk-receipt", Role: "status", Aria: map[string]string{"live": "polite"}}, children...)
}

// kioskReceiptSentence says who did what and when. It is omitted when the
// device cannot name all three, rather than guessing.
func kioskReceiptSentence(locale LocaleContext, projection KioskProjection) ui.Node {
	key := ""
	switch projection.ReceiptEvent {
	case KioskPunchClockIn:
		key = "receipt_in"
	case KioskPunchClockOut:
		key = "receipt_out"
	}
	name, when := strings.TrimSpace(projection.WorkerLabel), strings.TrimSpace(projection.ReceiptTime)
	if key == "" || name == "" || when == "" {
		return html.Fragment()
	}
	return html.P(html.Props{Class: "kiosk-receipt-detail"}, fillBidi(kioskCopy(locale, key), map[string]string{"name": name, "time": when})...)
}

func kioskAlert(locale LocaleContext, key string) ui.Node {
	return html.Section(html.Props{Class: "kiosk-panel kiosk-alert", Role: "alert"}, html.H2(html.Props{}, ui.Text(kioskCopy(locale, key))), html.P(html.Props{Class: "kiosk-help"}, ui.Text(kioskCopy(locale, key+"_help"))))
}

func kioskButton(label, class string, callback func()) ui.Node {
	props := html.Props{Class: "button " + class, Type: "button"}
	if callback != nil {
		props.OnClick = ui.UseEvent(func(ui.MouseEvent) { callback() })
	}
	return html.Button(props, ui.Text(label))
}

func kioskSubmitButton(label, class string, busy bool) ui.Node {
	return html.Button(html.Props{Class: "button " + class + " kiosk-submit", Type: "submit", Disabled: busy}, ui.Text(label))
}
