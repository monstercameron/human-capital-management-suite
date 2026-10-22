package productui

import "strings"

// The agent access pages print words, never the codes behind them: whose
// account an agent uses, and where a connection revision stands. The raw values
// stay in data attributes for scripts.

var agentCredentialModeLabels = map[string][3]string{
	"USER_DELEGATED": {"Each person's own account", "Das eigene Konto jeder Person", "حساب كل شخص الخاص"},
	"BROKERED":       {"A shared administrator account", "Ein gemeinsames Administratorkonto", "حساب مسؤول مشترك"},
}

var agentRevisionStatusLabels = map[string][3]string{
	"DRAFT":             {"Draft", "Entwurf", "مسودة"},
	"AWAITING_APPROVAL": {"Waiting for a second administrator", "Wartet auf eine zweite Administration", "بانتظار مسؤول ثانٍ"},
	"APPROVED":          {"Approved, not yet published", "Genehmigt, noch nicht veröffentlicht", "تمت الموافقة، لم تُنشر بعد"},
	"PUBLISHED":         {"Published", "Veröffentlicht", "منشورة"},
	"SUPERSEDED":        {"Replaced", "Ersetzt", "تم استبدالها"},
}

func agentLabelFrom(table map[string][3]string, locale LocaleContext, code string) string {
	labels, ok := table[strings.ToUpper(strings.TrimSpace(code))]
	if !ok {
		return code
	}
	return labels[agentRPLocaleIndex(locale)]
}

func agentCredentialModeLabel(locale LocaleContext, mode string) string {
	return agentLabelFrom(agentCredentialModeLabels, locale, mode)
}

func agentRevisionStatusLabel(locale LocaleContext, status string) string {
	return agentLabelFrom(agentRevisionStatusLabels, locale, status)
}

// agentSpendLimitsTitle names the agent a limits card is for, so two cards one
// above the other on the Cost tab are not both just "Spend limits".
func agentSpendLimitsTitle(locale LocaleContext, agent string) string {
	title := agentCostText(locale, "limits_title")
	if strings.TrimSpace(agent) == "" {
		return title
	}
	return strings.NewReplacer("{title}", title, "{agent}", agent).Replace([3]string{"{title} for {agent}", "{title} für {agent}", "{title} لـ {agent}"}[agentRPLocaleIndex(locale)])
}
