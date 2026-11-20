package chatui

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// Issue 3758: a search result for an agent task says where the task stands, in
// plain words in the reader's language, beside the question it asked. The state
// code is the task's own; a code this page does not know is not shown rather
// than printed raw.
var issue3758TaskStates = map[string][3]string{
	"drafting":                   {"Drafting a plan", "Plan wird entworfen", "إعداد خطة"},
	"awaiting_plan_confirmation": {"Waiting for you to confirm the plan", "Wartet auf Ihre Bestätigung des Plans", "بانتظار تأكيدك للخطة"},
	"running":                    {"Working on it", "In Arbeit", "قيد التنفيذ"},
	"waiting":                    {"Waiting", "Wartet", "في الانتظار"},
	"awaiting_approval":          {"Waiting for approval", "Wartet auf Genehmigung", "بانتظار الموافقة"},
	"paused":                     {"Paused", "Pausiert", "متوقفة مؤقتًا"},
	"completed":                  {"Completed", "Abgeschlossen", "مكتملة"},
	"failed":                     {"Did not finish", "Nicht abgeschlossen", "لم تكتمل"},
	"cancelled":                  {"Cancelled", "Abgebrochen", "ملغاة"},
	"expired":                    {"Expired", "Abgelaufen", "انتهت صلاحيتها"},
}

// issue3758StateLabel is the state of an agent task result in words, or "".
func issue3758StateLabel(locale string, row chatsearch.Row) string {
	if row.Kind != chatsearch.AgentTask {
		return ""
	}
	copy, ok := issue3758TaskStates[strings.ToLower(strings.TrimSpace(row.State))]
	if !ok {
		return ""
	}
	return chatbug039Text("issue3758.state", copy[chatbug039LocaleIndex(locale)], copy[0])
}
