package chatui

import "strings"

// agentCountLabel names how many agents are in a conversation. One agent reads
// "1 agent", never "1 agents".
func agentCountLabel(m Model, n int) string {
	if n != 1 {
		return chat5Format(m, "chat.agents.count", map[string]string{"n": m.nz(n)})
	}
	switch {
	case strings.HasPrefix(m.Locale, "de"):
		return m.nz(1) + " Agent"
	case direction(m.Locale) == "rtl":
		return "وكيل واحد"
	}
	return m.nz(1) + " agent"
}
