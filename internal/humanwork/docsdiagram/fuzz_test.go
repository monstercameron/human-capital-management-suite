package docsdiagram

import (
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// FuzzRender drives every parser and layout without the panic guard, so a
// crash on hostile input fails the fuzz run instead of hiding as an error.
func FuzzRender(f *testing.F) {
	for _, seed := range []string{
		hiringFlow, leaveSeq, headcountPie, attritionChart, onboardingGantt, policyTimeline, firstWeekJourney, twelveNodes,
		"graph LR\n A-- x -->B\n A-.->|y|C\n C==>A\n subgraph s\n B\n end\n s --> A",
		"flowchart TD\n A@{ label: \"x\" }\n A[\"q\"] & B --> C((c))",
		"sequenceDiagram\n A->>+B: x\n alt a\n B-->>-A: y\n else\n Note over A,B: n\n end",
		"gantt\n dateFormat DD/MM/YYYY\n A :a, 01/02/2026, 2w\n B :until a",
		"xychart-beta\n x-axis 0 --> 1\n line [1e14, -1e14]",
		"pie\n \"a\" : 1e300",
		"timeline\n : x",
		"journey\n section s\n t: 5: a,,b",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		d, err := buildUnguarded(src, Options{Locale: "de"}, "fz")
		if err != nil {
			return
		}
		if d == nil || d.width <= 0 || d.height <= 0 {
			t.Fatalf("drawing without size: %+v", d)
		}
		if _, err := ui.RenderToString(figure(d, Options{}, "fz")); err != nil {
			t.Fatalf("render: %v", err)
		}
	})
}
