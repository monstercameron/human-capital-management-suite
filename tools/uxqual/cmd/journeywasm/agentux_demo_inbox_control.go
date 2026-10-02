package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentdemo"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func agentuxDemoInboxText(locale, key string) string {
	copy := map[string][3]string{
		"title": {"Simulate a customer email", "Kunden-E-Mail simulieren", "محاكاة بريد من عميل"},
		"help":  {"Support Desk creates a ticket and alerts the incident channel. Sender details remain unverified.", "Support Desk erstellt ein Ticket und benachrichtigt den Vorfallkanal. Absenderangaben bleiben unbestätigt.", "ينشئ مكتب الدعم تذكرة وينبّه قناة الحوادث. تبقى بيانات المرسل غير موثّقة."},
		"from":  {"From", "Von", "من"}, "subject": {"Subject", "Betreff", "الموضوع"}, "body": {"Email body", "E-Mail-Text", "نص البريد"},
		"submit":      {"Simulate email", "E-Mail simulieren", "حاكِ البريد"},
		"idle":        {"Enter an email, then simulate it to see a support ticket and channel alert.", "Geben Sie eine E-Mail ein und simulieren Sie sie, um das Ticket und die Kanalbenachrichtigung zu sehen.", "أدخل بريدًا ثم حاكِه لعرض تذكرة الدعم وتنبيه القناة."},
		"sending":     {"Storing the email. Please wait.", "E-Mail wird gespeichert. Bitte warten.", "جارٍ حفظ البريد. يرجى الانتظار."},
		"queued":      {"Email received. Check Customer support and the incident channel as Support Desk processes it.", "E-Mail empfangen. Sehen Sie im Kundensupport und Vorfallkanal nach, während Support Desk sie bearbeitet.", "تم استلام البريد. راجع دعم العملاء وقناة الحوادث أثناء معالجة مكتب الدعم له."},
		"invalid":     {"Check the sender address, subject and email body, then try again.", "Prüfen Sie Absenderadresse, Betreff und E-Mail-Text und versuchen Sie es erneut.", "تحقّق من عنوان المرسل والموضوع ونص البريد ثم حاول مجددًا."},
		"denied":      {"Only the Support Desk owner may simulate email. Ask the owner for access.", "Nur der Support-Desk-Verantwortliche darf E-Mails simulieren. Bitten Sie ihn um Zugriff.", "يمكن لمالك مكتب الدعم وحده محاكاة البريد. اطلب الوصول من المالك."},
		"conflict":    {"This request changed after it was received. Edit the email to submit a new request.", "Diese Anfrage wurde nach dem Empfang geändert. Bearbeiten Sie die E-Mail für eine neue Anfrage.", "تغيّر هذا الطلب بعد استلامه. عدّل البريد لإرسال طلب جديد."},
		"unavailable": {"The email could not be queued. Your text is still here. Press Simulate email to retry.", "Die E-Mail konnte nicht eingereiht werden. Ihr Text bleibt erhalten. Drücken Sie E-Mail simulieren, um es erneut zu versuchen.", "تعذّر وضع البريد في قائمة المعالجة. بقي نصك هنا. اضغط حاكِ البريد للمحاولة مجددًا."},
	}
	i := 0
	if locale == "de-DE" {
		i = 1
	}
	if locale == "ar" {
		i = 2
	}
	if value, ok := copy[key]; ok {
		return value[i]
	}
	return copy["unavailable"][i]
}

func agentuxDemoInboxControl(locale productui.LocaleContext) ui.Node {
	dir := "ltr"
	if locale.Resolved == "ar" {
		dir = "rtl"
	}
	field := func(name, key string, textarea bool) ui.Node {
		id := "support-demo-" + name
		props := html.Props{ID: id, Name: name, Required: true, Raw: map[string]any{"aria-describedby": "support-demo-status", "data-support-demo-field": name}}
		var control ui.Node
		if textarea {
			props.Rows = 5
			props.Raw["maxlength"] = "65536"
			control = html.Textarea(props)
		} else {
			props.Type = "text"
			props.Raw["maxlength"] = "200"
			if name == "from" {
				props.Raw["maxlength"] = "520"
			}
			control = html.Input(props)
		}
		return html.Div(html.Props{Class: "support-demo-field"}, html.Label(html.Props{For: id}, ui.Text(agentuxDemoInboxText(locale.Resolved, key))), control)
	}
	return html.Section(html.Props{Class: "support-demo-inbox", Raw: map[string]any{"dir": dir, "data-locale": locale.Resolved}},
		html.Tag("style", html.Props{}, ui.Text(agentuxDemoInboxStyles)),
		html.H3(html.Props{}, ui.Text(agentuxDemoInboxText(locale.Resolved, "title"))),
		html.P(html.Props{}, ui.Text(agentuxDemoInboxText(locale.Resolved, "help"))),
		html.Form(html.Props{Raw: map[string]any{"data-support-demo-form": "true"}}, field("from", "from", false), field("subject", "subject", false), field("body", "body", true), html.Button(html.Props{Type: "submit", Class: "button primary"}, ui.Text(agentuxDemoInboxText(locale.Resolved, "submit")))),
		html.P(html.Props{ID: "support-demo-status", Role: "status", Raw: map[string]any{"aria-live": "polite", "aria-atomic": "true", "data-support-demo-status": "true", "tabindex": "-1"}}, ui.Text(agentuxDemoInboxText(locale.Resolved, "idle"))),
	)
}

const agentuxDemoInboxStyles = `.support-demo-inbox{min-width:0;padding:var(--hcm-space-3);border:1px solid var(--hcm-color-border);border-radius:var(--hcm-radius-surface);color:var(--hcm-color-text);background:var(--hcm-color-canvas);overflow-wrap:anywhere}.support-demo-inbox form,.support-demo-field{display:grid;grid-template-columns:minmax(0,1fr);gap:var(--hcm-space-2);min-width:0}.support-demo-inbox form{gap:var(--hcm-space-3)}.support-demo-inbox input,.support-demo-inbox textarea{width:100%;min-width:0;max-width:100%;box-sizing:border-box;font:inherit;color:var(--hcm-color-text);background:var(--hcm-color-canvas);border:1px solid var(--hcm-color-control-border);border-radius:var(--hcm-radius-control);padding:var(--hcm-space-2)}.support-demo-inbox button{white-space:normal;overflow-wrap:anywhere;max-width:100%}@media(max-width:800px){.support-demo-inbox{padding:var(--hcm-space-2)}}`

func agentuxDemoInboxRequest(ctx context.Context, client *http.Client, cfg journeyclient.Config, email agentdemo.Email) (agentdemo.Receipt, error) {
	var result agentdemo.Receipt
	if ctx == nil || client == nil || strings.TrimSpace(cfg.Bearer) == "" {
		return result, agentdemo.ErrUnavailable
	}
	endpoint, err := url.Parse(cfg.TunnelURL)
	if err != nil || endpoint.Host == "" {
		return result, agentdemo.ErrUnavailable
	}
	switch endpoint.Scheme {
	case "ws":
		endpoint.Scheme = "http"
	case "wss":
		endpoint.Scheme = "https"
	case "http", "https":
	default:
		return result, agentdemo.ErrUnavailable
	}
	endpoint.Path, endpoint.RawQuery, endpoint.Fragment = agentdemo.Path, "", ""
	raw, err := json.Marshal(email)
	if err != nil {
		return result, agentdemo.ErrInvalid
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return result, agentdemo.ErrUnavailable
	}
	r.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	r.Header.Set("Content-Type", "application/json")
	response, err := client.Do(r)
	if err != nil {
		return result, agentdemo.ErrUnavailable
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusAccepted:
	case http.StatusForbidden:
		return result, agentdemo.ErrDenied
	case http.StatusBadRequest:
		return result, agentdemo.ErrInvalid
	case http.StatusConflict:
		return result, agentdemo.ErrConflict
	default:
		return result, agentdemo.ErrUnavailable
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8192)).Decode(&result); err != nil || result.MessageID == "" || result.State != "QUEUED" {
		return agentdemo.Receipt{}, agentdemo.ErrUnavailable
	}
	return result, nil
}

func agentuxDemoInboxErrorKey(err error) string {
	switch {
	case errors.Is(err, agentdemo.ErrInvalid):
		return "invalid"
	case errors.Is(err, agentdemo.ErrDenied):
		return "denied"
	case errors.Is(err, agentdemo.ErrConflict):
		return "conflict"
	default:
		return "unavailable"
	}
}
