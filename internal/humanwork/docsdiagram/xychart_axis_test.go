package docsdiagram

import (
	"strings"
	"testing"
)

// The attrition chart plotted a 12-month rate (13-14%) as a line on the
// Exits axis with no key (docs M8). A second y-axis statement now puts the
// line series on a right-hand axis, and unnamed series take their axis
// titles as legend names.
func TestXYChartSecondaryAxisForLineSeries(t *testing.T) {
	src := "xychart-beta\n x-axis [Jan, Feb, Mar]\n y-axis \"Exits\" 0 --> 20\n y-axis \"Rate (%)\" 0 --> 40\n bar [9, 7, 11]\n line [14.2, 13.9, 14.1]"
	out := renderHTML(t, src, Options{})
	if got := strings.Count(out, `class="dg-legend-item"`); got != 2 {
		t.Fatalf("legend items = %d, want 2", got)
	}
	for _, want := range []string{"Exits", "Rate (%)", ">40<", "line, right axis"} {
		if !strings.Contains(out, want) {
			t.Errorf("secondary-axis chart missing %q", want)
		}
	}
	// Three vertical axis lines would mean the plot drew a right axis: left,
	// right, plus the horizontal baseline.
	if got := strings.Count(out, `class="dg-axis"`); got != 3 {
		t.Errorf("axis lines = %d, want 3 (baseline, left, right)", got)
	}
	// 14.2 on a 0-40 axis sits well below the bar top of 11 on a 0-20 axis;
	// on the shared axis the point would sit higher than that bar.
	plain := renderHTML(t, strings.Replace(src, " y-axis \"Rate (%)\" 0 --> 40\n", "", 1), Options{})
	if strings.Count(plain, `class="dg-axis"`) != 2 {
		t.Error("a single y-axis must not draw a right axis")
	}
	if !strings.Contains(plain, "Exits (") {
		t.Error("the lone bar series should take the y-axis title as its name")
	}
	if strings.Contains(plain, "line, right axis") {
		t.Error("no right axis was declared")
	}
}

func TestXYChartSecondaryAxisIgnoredWithoutBars(t *testing.T) {
	out := renderHTML(t, "xychart-beta\n y-axis A 0 --> 10\n y-axis B 0 --> 50\n line [1, 2]\n line [3, 4]", Options{})
	if strings.Count(out, `class="dg-axis"`) != 2 {
		t.Error("an all-line chart has nothing to split across two axes")
	}
	if _, err := Render("xychart-beta\n y-axis A 0 --> 10\n y-axis B 9 --> 1\n bar [1]\n line [2]", Options{}); err == nil {
		t.Error("a decreasing secondary range must be rejected")
	}
}
