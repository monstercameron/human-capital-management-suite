package workspace

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"

func agentUXProblemText(locale productui.LocaleContext, key string) string {
	values := map[string][3]string{
		"go_to_agents": {"Go to Agents", "Zu Agenten", "الانتقال إلى الوكلاء"},
	}
	index := 0
	if locale.Resolved == "de-DE" {
		index = 1
	} else if locale.Resolved == "ar" {
		index = 2
	}
	return values[key][index]
}
