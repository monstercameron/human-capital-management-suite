package docsdiagram

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderHTML(t *testing.T, src string, opts Options) string {
	t.Helper()
	node, err := Render(src, opts)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	out, err := ui.RenderToString(node)
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	return out
}

var (
	hiringFlow = `flowchart TD
    req([Requisition]) --> budget{Budget available?}
    budget -->|Yes| hrbp[HR partner review]
    budget -->|No| finance[Finance exception]
    finance -.->|approved| hrbp
    finance -->|rejected| closed((Closed))
    subgraph approvals [Approval chain]
        hrbp --> dir[Director]
    end
    dir --> post[Post job]`
	leaveSeq = `sequenceDiagram
    autonumber
    actor E as Employee
    participant M as Manager
    E->>+M: Request leave
    Note right of M: Two day SLA
    alt approved
        M-->>E: Approve
    else rejected
        M-->>-E: Reject
    end
    loop Weekly
        M->>M: Review
    end`
	headcountPie = `pie showData
    title Headcount
    "Engineering" : 412
    "Sales" : 238
    "Finance" : 58`
	attritionChart = `xychart-beta
    title "Attrition"
    x-axis [Jan, Feb, Mar, Apr]
    y-axis "Leavers" 0 --> 30
    bar [12, 9, 14, 18]
    line [10, 11, 12, 13]`
	onboardingGantt = `gantt
    title Onboarding
    dateFormat YYYY-MM-DD
    section Before
    Offer accepted :milestone, m1, 2026-01-05, 0d
    Background check :done, bg, 2026-01-05, 5d
    Accounts :active, it, after bg, 4d
    section Week one
    Orientation :crit, o, 2026-01-19, 2d
    Buddy intro :3d`
	policyTimeline = `timeline
    title Remote policy
    section Pilot
    2019 : Two days
    2020 : Remote : Stipend
         : Laptop refresh`
	firstWeekJourney = `journey
    title First week
    section Day one
      Collect laptop: 3: New hire, IT
      Meet team: 5: New hire`
)

func TestRenderAllKindsAreAccessibleAndCSPSafe(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		kind   string
		desc   string
		counts map[string]int
	}{
		{"flowchart", hiringFlow, "flowchart", "Flowchart with 7 steps and 7 connections: Requisition → Budget available?",
			map[string]int{`class="dg-node-group"`: 7, `<path class="dg-edge`: 7, `class="dg-cluster"`: 1, `class="dg-edge-label"`: 4}},
		{"sequence", leaveSeq, "sequence", "Sequence diagram between Employee, Manager with 4 messages.",
			map[string]int{`class="dg-msg`: 4, `class="dg-frame"`: 2, `class="dg-note"`: 1, `class="dg-seqnum"`: 4, `<rect class="dg-participant-box"`: 2}},
		{"pie", headcountPie, "pie", "Pie chart of 3 parts totalling 708: Engineering 58.2%, Sales 33.6%, Finance 8.2%",
			map[string]int{`<path class="dg-slice`: 3, `class="dg-legend-item"`: 3}},
		{"xychart", attritionChart, "xychart", "Chart with 4 categories and 2 series",
			map[string]int{`class="dg-bar`: 4, `<polyline class="dg-line`: 1, `<circle class="dg-point`: 5}}, // 4 points and the legend swatch
		{"gantt", onboardingGantt, "gantt", "Gantt chart with 5 tasks in 2 sections from 2026-01-05 to 2026-01-24.",
			map[string]int{`class="dg-task `: 4, `class="dg-milestone`: 1}},
		{"timeline", policyTimeline, "timeline", "Timeline with 2 periods: 2019, 2020",
			map[string]int{`<rect class="dg-period`: 2, `class="dg-event `: 4}},
		{"journey", firstWeekJourney, "journey", "User journey with 2 tasks in 1 sections. Average score 4 of 5.",
			map[string]int{`class="dg-face `: 2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := renderHTML(t, c.src, Options{})
			if !strings.HasPrefix(out, `<figure class="docs-diagram docs-diagram-`+c.kind+`">`) {
				t.Fatalf("figure prefix wrong: %.120s", out)
			}
			for _, bad := range []string{"style=", "<script", "javascript:", "<text", "onload"} {
				if strings.Contains(strings.ToLower(out), bad) {
					t.Errorf("output contains %q", bad)
				}
			}
			m := regexp.MustCompile(`<svg[^>]*aria-labelledby="([^"]+)"[^>]*>`).FindStringSubmatch(out)
			if m == nil || !strings.Contains(m[0], `role="img"`) || !strings.Contains(m[0], `viewBox="0 0 `) || !strings.Contains(m[0], `width="100%"`) {
				t.Fatalf("svg root lacks role/labelledby/viewBox: %v", m)
			}
			if !strings.Contains(out, `<title id="`+m[1]+`">`) {
				t.Errorf("title with id %s missing", m[1])
			}
			desc := regexp.MustCompile(`<desc id="[^"]+">([^<]*)</desc>`).FindStringSubmatch(out)
			if desc == nil || !strings.Contains(unescape(desc[1]), c.desc) {
				t.Errorf("desc = %v, want it to contain %q", desc, c.desc)
			}
			if !strings.Contains(out, `class="docs-diagram-sr"`) {
				t.Error("text fallback missing")
			}
			for needle, want := range c.counts {
				if got := strings.Count(out, needle); got != want {
					t.Errorf("count %s = %d, want %d", needle, got, want)
				}
			}
		})
	}
}

func unescape(s string) string {
	return strings.NewReplacer("&gt;", ">", "&lt;", "<", "&amp;", "&", "&#34;", `"`, "&#39;", "'").Replace(s)
}

func TestRenderEscapesText(t *testing.T) {
	out := renderHTML(t, "flowchart LR\n  A[\"<img src=x onerror=alert(1)>\"] --> B[a & b]", Options{Title: "<b>t</b>"})
	if strings.Contains(out, "<img") || strings.Contains(out, "<b>") {
		t.Fatalf("markup was not escaped: %s", out)
	}
	if !strings.Contains(out, "&lt;img") {
		t.Fatalf("escaped label missing")
	}
}

func TestRenderTitleOptions(t *testing.T) {
	out := renderHTML(t, headcountPie, Options{Title: "Custom name"})
	if !strings.Contains(out, ">Custom name</title>") {
		t.Error("Options.Title should name the svg")
	}
	if !strings.Contains(out, `<figcaption class="docs-diagram-caption">Headcount</figcaption>`) {
		t.Error("diagram title should be the caption")
	}
	fm := renderHTML(t, "---\ntitle: Front matter title\n---\nflowchart LR\n A-->B", Options{})
	if !strings.Contains(fm, ">Front matter title</figcaption>") {
		t.Error("front matter title not used")
	}
	acc := renderHTML(t, "flowchart LR\n accTitle: Named flow\n accDescr: Custom description\n A-->B", Options{})
	if !strings.Contains(acc, ">Named flow</title>") || !strings.Contains(acc, ">Custom description</desc>") {
		t.Error("accTitle/accDescr not used")
	}
}

func TestMarkerIDsAreUniquePerDiagram(t *testing.T) {
	a := renderHTML(t, "flowchart LR\n A-->B", Options{})
	b := renderHTML(t, "flowchart LR\n A-->C", Options{})
	ida := regexp.MustCompile(`<marker id="([^"]+)-arrow"`).FindStringSubmatch(a)
	idb := regexp.MustCompile(`<marker id="([^"]+)-arrow"`).FindStringSubmatch(b)
	if ida == nil || idb == nil || ida[1] == idb[1] {
		t.Fatalf("marker ids not unique: %v %v", ida, idb)
	}
	if !strings.Contains(a, `marker-end="url(#`+ida[1]+`-arrow)"`) {
		t.Error("edge does not reference its own marker")
	}
	again := renderHTML(t, "flowchart LR\n A-->B", Options{})
	if again != a {
		t.Error("rendering is not deterministic")
	}
}

func TestRenderErrors(t *testing.T) {
	bigFlow := "flowchart TD\n"
	for i := 0; i < MaxElements+1; i++ {
		bigFlow += "  n" + itoa(i) + "\n"
	}
	bigEdges := "flowchart TD\n"
	for i := 0; i < MaxElements+1; i++ {
		bigEdges += "  a --> b\n"
	}
	vals := strings.Repeat("1,", MaxDataPoints) + "1"
	cases := []struct {
		name string
		src  string
		want error
	}{
		{"empty", "", ErrEmpty},
		{"only comments", "%% nothing\n\n", ErrEmpty},
		{"classDiagram", "classDiagram\n A <|-- B", ErrUnsupported},
		{"erDiagram", "erDiagram\n A ||--o{ B : has", ErrUnsupported},
		{"stateDiagram", "stateDiagram-v2\n [*] --> A", ErrUnsupported},
		{"mindmap", "mindmap\n root", ErrUnsupported},
		{"horizontal xychart", "xychart-beta horizontal\n bar [1]", ErrUnsupported},
		{"source too long", "flowchart TD\n%%" + strings.Repeat("x", MaxSourceBytes), ErrTooLarge},
		{"too many nodes", bigFlow, ErrTooLarge},
		{"too many edges", bigEdges, ErrTooLarge},
		{"too many points", "xychart-beta\n bar [" + vals + "]", ErrTooLarge},
		{"bad direction", "flowchart XY\n A-->B", ErrSyntax},
		{"unclosed node", "flowchart TD\n A[open --> B", ErrSyntax},
		{"stray end", "flowchart TD\n end", ErrSyntax},
		{"unclosed pipe label", "flowchart TD\n A -->|oops B", ErrSyntax},
		{"bad link", "flowchart TD\n A -- B", ErrSyntax},
		{"unclosed front matter", "---\ntitle: x\nflowchart TD\n A-->B", ErrSyntax},
		{"sequence garbage", "sequenceDiagram\n hello world", ErrSyntax},
		{"sequence else outside alt", "sequenceDiagram\n else nope", ErrSyntax},
		{"sequence stray end", "sequenceDiagram\n A->>B: x\n end", ErrSyntax},
		{"sequence bad note", "sequenceDiagram\n Note beside A: x", ErrSyntax},
		{"sequence note without colon", "sequenceDiagram\n Note over A", ErrSyntax},
		{"pie unquoted", "pie\n Eng : 4", ErrSyntax},
		{"pie negative", "pie\n \"Eng\" : -4", ErrSyntax},
		{"pie not number", "pie\n \"Eng\" : lots", ErrSyntax},
		{"pie zero total", "pie\n \"Eng\" : 0", ErrSyntax},
		{"pie empty", "pie title Nothing", ErrSyntax},
		{"xy no series", "xychart-beta\n title x", ErrSyntax},
		{"xy bad value", "xychart-beta\n bar [1, x]", ErrSyntax},
		{"xy too many values", "xychart-beta\n x-axis [a]\n bar [1, 2]", ErrSyntax},
		{"xy bad range", "xychart-beta\n y-axis 10 --> 1\n bar [1]", ErrSyntax},
		{"xy range not numbers", "xychart-beta\n y-axis a --> b\n bar [1]", ErrSyntax},
		{"xy unknown", "xychart-beta\n pie [1]", ErrSyntax},
		{"gantt no tasks", "gantt\n title x", ErrSyntax},
		{"gantt first task needs start", "gantt\n A : 3d", ErrSyntax},
		{"gantt bad date", "gantt\n A : 2026-13-45, 3d", ErrSyntax},
		{"gantt unknown after", "gantt\n A : a, 2026-01-01, 2d\n B : after zz, 2d", ErrSyntax},
		{"gantt ends before start", "gantt\n A : 2026-01-10, 2026-01-01", ErrSyntax},
		{"gantt bad dateFormat", "gantt\n dateFormat QQ\n A : 2026-01-01, 1d", ErrSyntax},
		{"gantt too long span", "gantt\n A : 2000-01-01, 2026-01-01", ErrTooLarge},
		{"gantt no colon", "gantt\n A task", ErrSyntax},
		{"timeline empty", "timeline\n title x", ErrSyntax},
		{"timeline orphan event", "timeline\n : event", ErrSyntax},
		{"journey bad score", "journey\n Task: 9: Me", ErrSyntax},
		{"journey no score", "journey\n Task", ErrSyntax},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			node, err := Render(c.src, Options{})
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if node != nil {
				t.Error("node returned alongside an error")
			}
		})
	}
}

func TestSyntaxErrorCarriesLine(t *testing.T) {
	_, err := Render("flowchart TD\n A-->B\n C[open", Options{})
	var se *SyntaxError
	if !errors.As(err, &se) || se.Line != 3 {
		t.Fatalf("err = %v, want SyntaxError on line 3", err)
	}
	if !strings.Contains(se.Error(), "line 3") {
		t.Errorf("message %q lacks line", se.Error())
	}
}

func TestDetectKind(t *testing.T) {
	cases := map[string]Kind{
		"graph LR\n A-->B":            KindFlowchart,
		"flowchart TD":                KindFlowchart,
		"sequenceDiagram\n A->>B: x":  KindSequence,
		"pie\n \"a\": 1":              KindPie,
		"xychart-beta\n bar [1]":      KindXYChart,
		"gantt":                       KindGantt,
		"timeline":                    KindTimeline,
		"journey":                     KindJourney,
		"%%{init: {}}%%\ngraph TD":    KindFlowchart,
		"---\nconfig: {}\n---\npie\n": KindPie,
	}
	for src, want := range cases {
		got, err := DetectKind(src)
		if err != nil || got != want {
			t.Errorf("DetectKind(%q) = %q, %v; want %q", src, got, err, want)
		}
	}
	if _, err := DetectKind("gitGraph"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("gitGraph err = %v", err)
	}
	if _, err := DetectKind("  "); !errors.Is(err, ErrEmpty) {
		t.Errorf("blank err = %v", err)
	}
}

func TestLocaleFormatting(t *testing.T) {
	cases := []struct {
		tag  string
		v    float64
		dec  int
		want string
	}{
		{"en-US", 1234567.891, 2, "1,234,567.89"},
		{"de-DE", 1234567.891, 2, "1.234.567,89"},
		{"ar", 1234.5, 1, "1٬234٫5"},
		{"fr", 12.5, 1, "12.5"},
		{"", -1000, 0, "-1,000"},
		{"en", 0.0001, 2, "0"},
	}
	for _, c := range cases {
		if got := localeFor(c.tag).number(c.v, c.dec); got != c.want {
			t.Errorf("%s number(%v) = %q, want %q", c.tag, c.v, got, c.want)
		}
	}
	de := renderHTML(t, "pie\n \"Berlin\" : 1234.5\n \"Hamburg\" : 410", Options{Locale: "de-DE"})
	if !strings.Contains(de, "Kreisdiagramm") || !strings.Contains(de, "75,1%") {
		t.Errorf("German pie lacks localized summary or percent")
	}
	if got := localeFor("en").t("no-such-key"); got != "" {
		t.Errorf("unknown key = %q", got)
	}
}

func TestStylesheet(t *testing.T) {
	css := Stylesheet()
	for i := 0; i < 8; i++ {
		if !strings.Contains(css, ".docs-diagram .series-"+itoa(i)+"{") {
			t.Errorf("series-%d missing", i)
		}
	}
	for _, class := range []string{".docs-diagram{", ".docs-diagram-sr{", ".docs-diagram-svg{", ".docs-diagram-canvas{",
		".dg-node{", ".dg-edge{", ".dg-msg{", ".dg-bar{", ".dg-line{", ".dg-task-crit{", ".dg-label{", ".docs-diagram-w-640{"} {
		if !strings.Contains(css, class) {
			t.Errorf("stylesheet lacks %s", class)
		}
	}
	allowed := map[string]bool{"ink": true, "muted": true, "surface": true, "line": true, "accent": true, "surface-subtle": true,
		"hcm-color-info": true, "hcm-color-warning": true, "hcm-color-success": true, "hcm-color-danger": true, "dg-series": true,
		"hcm-focus-ring-width": true, "hcm-color-focus": true}
	for _, m := range regexp.MustCompile(`var\(--([a-zA-Z0-9_-]+)`).FindAllStringSubmatch(css, -1) {
		if !allowed[m[1]] {
			t.Errorf("stylesheet reads unknown token --%s", m[1])
		}
	}
	if !regexp.MustCompile(`[;{]\s*--dg-series\s*:`).MatchString(css) {
		t.Error("--dg-series is used but never defined")
	}
	if strings.Contains(css, "url(") || strings.Contains(css, "@import") {
		t.Error("stylesheet must not load resources")
	}
	if !strings.Contains(css, "direction:ltr") {
		t.Error("figure must stay LTR inside RTL pages")
	}
	// Every class the renderers emit is styled or structural.
	out := renderHTML(t, hiringFlow, Options{}) + renderHTML(t, leaveSeq, Options{}) + renderHTML(t, onboardingGantt, Options{}) +
		renderHTML(t, attritionChart, Options{}) + renderHTML(t, policyTimeline, Options{}) + renderHTML(t, firstWeekJourney, Options{})
	structural := map[string]bool{"dg-node-group": true, "dg-edges": true, "dg-nodes": true, "dg-edge-labels": true, "dg-edge-label": true,
		"dg-cluster": true, "dg-frames": true, "dg-lifelines": true, "dg-participants": true, "dg-activations": true, "dg-messages": true,
		"dg-notes": true, "dg-message-labels": true, "dg-participant": true, "dg-participant-actor": true, "dg-frame-group": true,
		"dg-frame-text": true, "dg-frame-labels": true, "dg-seqnum": true, "dg-bands": true, "dg-grid-lines": true, "dg-tasks": true, "dg-labels": true,
		"dg-marks": true, "dg-axes": true, "dg-points": true, "dg-ticks": true, "dg-titles": true, "dg-legend": true, "dg-legend-item": true,
		"dg-sections": true, "dg-links": true, "dg-periods": true, "dg-events": true, "dg-faces": true, "dg-names": true, "dg-face": true,
		"docs-diagram-flowchart": true, "docs-diagram-gantt": true, "docs-diagram-xychart": true, "docs-diagram-timeline": true,
		"docs-diagram-journey": true, "dg-task-planned": false}
	for _, m := range regexp.MustCompile(`class="([^"]+)"`).FindAllStringSubmatch(out, -1) {
		for _, c := range strings.Fields(m[1]) {
			if structural[c] {
				continue
			}
			if !strings.Contains(css, "."+c+"{") && !strings.Contains(css, "."+c+" ") && !strings.Contains(css, "."+c+",") &&
				!strings.Contains(css, "."+c+":") && !strings.Contains(css, "."+c+".") {
				t.Errorf("class %q is emitted but never styled", c)
			}
		}
	}
}
