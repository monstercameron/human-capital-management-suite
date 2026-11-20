package chatui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3819: a reviewer who decides without a reason is told to give one, under
// the reason field, in words written for reviewers (not the applicant sentence).
func init() {
	for locale, words := range map[string][2]string{
		"en-US": {"Give a reason for this decision.", "Give a reason, then choose again."},
		"de-DE": {"Geben Sie einen Grund für diese Entscheidung an.", "Geben Sie einen Grund an und wählen Sie dann erneut."},
		"ar":    {"اكتب سببًا لهذا القرار.", "اكتب سببًا ثم اختر مرة أخرى."},
	} {
		chatgateExtraCopy[locale]["reasonRequired"] = words[0]
		chatgateExtraCopy[locale]["reasonNotice"] = words[1]
	}
}

// GateReasonProblem checks a review decision before it is sent. It returns the
// sentence for the reason input, keyed by that input's id, or nil when the
// decision needs nothing more.
func GateReasonProblem(locale, action, submissionID, reason string) map[string]string {
	if strings.TrimSpace(reason) != "" {
		return nil
	}
	switch action {
	case "admit", "decline":
		return map[string]string{"gate-reason-" + submissionID: GateText(locale, "reasonRequired")}
	case "bulk-admit", "bulk-decline":
		return map[string]string{"gate-bulk-reason": GateText(locale, "reasonRequired")}
	}
	return nil
}

// gateReasonInput is the decision reason input with its error line, tied
// together for assistive technology.
func gateReasonInput(v GateView, id string, required bool) []ui.Node {
	message := v.FieldErrors[id]
	props := html.Props{ID: id, Type: "text", Required: required, Aria: map[string]string{"describedby": id + "-error", "invalid": strconv.FormatBool(message != "")}}
	return []ui.Node{
		html.Label(html.Props{For: id, Text: GateText(v.Locale, "reason")}),
		html.Input(props),
		html.P(html.Props{ID: id + "-error", Class: "chatgate-error", Role: "alert", Text: message}),
	}
}
