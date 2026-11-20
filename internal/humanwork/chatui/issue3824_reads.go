package chatui

import (
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3824: "Who has seen my answers" lists other people's reads once each,
// with their full names, and the answer count has a singular form.
func init() {
	for locale, one := range map[string]string{"en-US": "1 answer", "de-DE": "1 Antwort", "ar": "إجابة واحدة"} {
		chatgateExtraCopy[locale]["countOne"] = one
	}
}

// gateAnswerCount is "1 answer" or "N answers" in the locale.
func gateAnswerCount(locale string, n int) string {
	if n == 1 {
		return GateText(locale, "countOne")
	}
	return fmt.Sprintf(GateText(locale, "count"), n)
}

// gateReaderRows is one line per distinct reader, purpose and day.
func gateReaderRows(v GateView) []string {
	rows := []string{}
	seen := map[string]bool{}
	for _, a := range v.Reads {
		line := gateReaderName(v, a) + " · " + a.Purpose + " · " + a.At.Format("2006-01-02")
		if !seen[line] {
			seen[line] = true
			rows = append(rows, line)
		}
	}
	return rows
}

func gateReaderNodes(v GateView) []ui.Node {
	nodes := []ui.Node{}
	for _, line := range gateReaderRows(v) {
		nodes = append(nodes, html.P(html.Props{Text: line}))
	}
	return nodes
}
