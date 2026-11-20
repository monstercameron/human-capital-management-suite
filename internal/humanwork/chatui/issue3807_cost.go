package chatui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PersonaCostFooter is what an agent's run cost its invoker (#3807): how many
// model calls it made and how much of the agent's daily allowance is left in
// this conversation. Both come from the server, for the invoker's own run. A
// nil AllowanceRemainingPct means the agent has no limit or it could not be
// read, and then nothing is said about it.
type PersonaCostFooter struct {
	UsedUnits             int64
	AllowanceRemainingPct *int
}

// issue3807CostFooter is the quiet line under a task card or an answer. It is
// nil when there is nothing true to say, so a page never shows a made-up zero.
func issue3807CostFooter(model Model, scope string, cost *PersonaCostFooter) ui.Node {
	if cost == nil {
		return nil
	}
	var parts []string
	if cost.UsedUnits > 0 {
		parts = append(parts, issue3807Text(model, "used", map[string]string{"n": chatNumeral(model.Locale, strconv.FormatInt(cost.UsedUnits, 10))}))
	}
	if pct := cost.AllowanceRemainingPct; pct != nil {
		left := max(0, min(100, *pct))
		if left == 0 {
			parts = append(parts, issue3807Text(model, "none_left", nil))
		} else {
			parts = append(parts, issue3807Text(model, "left", map[string]string{"pct": chatNumeral(model.Locale, strconv.Itoa(left)) + percentSign(model.Locale)}))
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return html.P(html.Props{Class: "agent-cost-footer", Dir: "auto", Data: map[string]string{"agent-cost": scope}, Text: strings.Join(parts, " · ")})
}

func percentSign(locale string) string {
	if dateLocale(locale) == "ar" {
		return "٪"
	}
	return "%"
}

func issue3807Text(model Model, key string, values map[string]string) string {
	table := map[string][3]string{
		"used":      {"Model calls used: {n}", "Verwendete Modellaufrufe: {n}", "استدعاءات النموذج المستخدمة: {n}"},
		"left":      {"Allowance left today: {pct}", "Heute noch verfügbares Kontingent: {pct}", "الحصة المتبقية اليوم: {pct}"},
		"none_left": {"No allowance left today", "Heute kein Kontingent mehr übrig", "لا توجد حصة متبقية اليوم"},
	}
	index := 0
	switch dateLocale(model.Locale) {
	case "de":
		index = 1
	case "ar":
		index = 2
	}
	text := chatbug039Text("chat.agent.cost."+key, table[key][index], table[key][0])
	for name, value := range values {
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

// issue3807OwnCost is the footer for a viewer who is the run's invoker, and
// nothing for anyone else: a shared thread never carries another person's cost.
func issue3807OwnCost(model Model, projection PersonaProgressProjection, scope string) ui.Node {
	if projection.Cost == nil || strings.TrimSpace(projection.ViewerID) == "" || projection.ViewerID != projection.InvokerID {
		return nil
	}
	return issue3807CostFooter(model, scope, projection.Cost)
}

const issue3807Styles = `.agent-cost-footer{margin:4px 0 0;color:var(--muted);font-size:.75rem;line-height:1.4;overflow-wrap:anywhere}`
