package chatui

import (
	"net/url"
	"strings"
	"testing"
)

const issue3959Ask = "Can you pull time-to-fill, offer acceptance and first-90-day attrition for engineering this year?"

func issue3959Model() (Model, *[]string) {
	calls := &[]string{}
	m := chatux021Model()
	m.Messages = []Message{
		{ID: "ask", Author: "Loretta Haynes", AuthorID: "loretta", Body: issue3959Ask, Sequence: 1},
		{ID: "lunch", Author: "Walt Brennan", AuthorID: "walt", Body: "Lunch is at noon", Sequence: 2},
	}
	m.Analysis = AnalysisState{
		Enabled: true,
		Open:    func(id string) { *calls = append(*calls, "open:"+id) },
		Close:   func() { *calls = append(*calls, "close") },
		Metric:  func(id string) { *calls = append(*calls, "metric:"+id) },
		Choose:  func(field, value string) { *calls = append(*calls, "choose:"+field+"="+value) },
		HandOff: func(conversation, post string, r AnalysisRequest) {
			*calls = append(*calls, "handoff:"+conversation+":"+post+":"+strings.Join(r.Metrics, ","))
		},
	}
	return m, calls
}

func TestIssue3959_MetricsAreFoundByWordsOnly(t *testing.T) {
	got := AnalysisMetricsIn(issue3959Ask)
	want := []string{"time_to_fill", "offer_acceptance", "attrition"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("metrics = %v, want %v", got, want)
	}
	if len(AnalysisMetricsIn("Lunch is at noon")) != 0 || len(AnalysisMetricsIn("")) != 0 {
		t.Error("a message that asks for no figures offers an analysis")
	}
	if got := AnalysisMetricsIn("Wie ist die Fluktuation und der Personalbestand?"); len(got) != 2 {
		t.Errorf("german request metrics = %v", got)
	}
}

func TestIssue3959_MenuOffersPrepareOnlyForARequestForFigures(t *testing.T) {
	m, _ := issue3959Model()
	got := renderNodes3737(t, analysisMenuItem(m, m.Messages[0]))
	for _, want := range []string{`data-action="analysis-open" data-id="ask"`, "Prepare an analysis", `role="menuitem"`} {
		if !strings.Contains(got, want) {
			t.Errorf("menu misses %q: %s", want, got)
		}
	}
	if len(analysisMenuItem(m, m.Messages[1])) != 0 {
		t.Error("a message with no figures request is offered an analysis")
	}
	off := m
	off.Analysis.Enabled = false
	if len(analysisMenuItem(off, off.Messages[0])) != 0 {
		t.Error("the action is offered to a person who cannot reach the analytics surface")
	}
	removed := m
	removed.Messages = []Message{{ID: "ask", Body: ""}}
	if len(analysisMenuItem(removed, removed.Messages[0])) != 0 {
		t.Error("an empty message is offered an analysis")
	}
}

func TestIssue3959_BuilderShowsContextGapsAndNoFigures(t *testing.T) {
	m, _ := issue3959Model()
	m.Analysis.PostID = "ask"
	got := renderNodes3737(t, analysisBlock(m, m.Messages[0]))
	for _, want := range []string{
		"Only you see this request", "does not run the analysis", "Time to fill", "Offer acceptance", "Attrition",
		issue3959Ask, `data-action="analysis-metric" data-id="attrition"`, `aria-pressed="false"`,
		"Choose at least one metric", "Choose a population", "Choose a reporting period", "Choose a grouping",
		`data-action="analysis-choose" data-id="period:year_to_date"`, `role="status"`,
		`data-action="analysis-handoff" data-id="ask"`, `data-action="analysis-close"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("builder misses %q: %s", want, got)
		}
	}
	// A gap is shown before the metric is chosen; the decision notes only after.
	if strings.Contains(got, "Say where the clock starts") {
		t.Errorf("decision note for an unchosen metric: %s", got)
	}
	other := m
	other.Analysis.PostID = "ask"
	if len(analysisBlock(other, other.Messages[1])) != 0 {
		t.Error("the builder opened under a different message")
	}
}

func TestIssue3959_ChosenFieldsClearGapsAndRaiseDecisions(t *testing.T) {
	m, _ := issue3959Model()
	m.Analysis.PostID = "ask"
	m.Analysis.Metrics = map[string]bool{"time_to_fill": true, "attrition": true}
	m.Analysis.Population, m.Analysis.Period, m.Analysis.Grouping = "all", "year_to_date", "department"
	got := renderNodes3737(t, analysisBlock(m, m.Messages[0]))
	for _, want := range []string{"Say where the clock starts", "Say whether voluntary", "Every field is chosen"} {
		if !strings.Contains(got, want) {
			t.Errorf("builder misses %q: %s", want, got)
		}
	}
	for _, gone := range []string{"Choose at least one metric", "Choose a population", "Choose a reporting period", "Choose a grouping"} {
		if strings.Contains(got, gone) {
			t.Errorf("builder still lists %q: %s", gone, got)
		}
	}
	if strings.Count(got, `aria-pressed="true"`) != 5 {
		t.Errorf("expected 2 metrics and 3 choices pressed: %s", got)
	}
}

func TestIssue3959_ActionsRunTheCallbacksAndHandOffThePartialRequest(t *testing.T) {
	m, calls := issue3959Model()
	m.Analysis.PostID = "ask"
	m.Analysis.Metrics = map[string]bool{"attrition": true}
	m.Analysis.Period = "last_quarter"
	opener := m
	opener.Analysis.PostID = ""
	opener.analysisAction("analysis-open", "ask")
	for _, step := range [][2]string{{"analysis-metric", "headcount"}, {"analysis-choose", "population:all"}, {"analysis-handoff", "ask"}, {"analysis-close", ""}} {
		m.analysisAction(step[0], step[1])
	}
	want := "open:ask|metric:headcount|choose:population=all|handoff:general:ask:attrition|close"
	if got := strings.Join(*calls, "|"); got != want {
		t.Errorf("calls = %s, want %s", got, want)
	}
	// Pressing the open control of the open builder closes it.
	*calls = nil
	m.analysisAction("analysis-open", "ask")
	if strings.Join(*calls, "|") != "close" {
		t.Errorf("open on the open builder = %v", *calls)
	}
	// A malformed choice runs nothing.
	*calls = nil
	m.analysisAction("analysis-choose", "nonsense")
	m.analysisAction("analysis-choose", "period:forever")
	m.analysisAction("analysis-metric", "salary_of_alice")
	if len(*calls) != 0 {
		t.Errorf("invalid choices ran callbacks: %v", *calls)
	}
}

func TestIssue3959_HrefCarriesOnlyIdsAndChoiceTokens(t *testing.T) {
	href := AnalysisHandoffHref("de-DE", "general", "ask", AnalysisRequest{Metrics: []string{"attrition", "bogus"}, Population: "all", Period: "last_quarter", Grouping: "evil&x=1"})
	u, err := url.Parse(href)
	if err != nil || u.Path != "/workspace/app/reports/catalog" {
		t.Fatalf("href = %q (%v)", href, err)
	}
	q := u.Query()
	if q.Get("from_conversation") != "general" || q.Get("from_message") != "ask" || q.Get("population") != "all" || q.Get("period") != "last_quarter" || q.Get("locale") != "de-DE" {
		t.Errorf("query = %v", q)
	}
	if strings.Join(q["metric"], ",") != "attrition" || q.Get("grouping") != "" || q.Get("x") != "" {
		t.Errorf("unknown tokens travelled: %v", q)
	}
	if strings.Contains(href, "Can") || strings.Contains(href, "engineering") {
		t.Errorf("message text travelled: %s", href)
	}
	if AnalysisHandoffHref("en-US", "", "ask", AnalysisRequest{}) != "" {
		t.Error("an address without a conversation")
	}
}

func TestIssue3959_CopyInEveryLocaleAndRTL(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for key := range issue3959Copy["en-US"] {
			if strings.TrimSpace(issue3959Copy[locale][key]) == "" {
				t.Errorf("%s misses %s", locale, key)
			}
		}
	}
	m, _ := issue3959Model()
	m.Locale = "ar"
	m.Analysis.PostID = "ask"
	got := renderNodes3737(t, analysisBlock(m, m.Messages[0]))
	if !strings.Contains(got, "تحليلاً") && !strings.Contains(got, "تحليل") {
		t.Errorf("arabic builder is not translated: %s", got)
	}
	if strings.Contains(Issue3959Styles, "margin-left") || strings.Contains(Issue3959Styles, "padding-right") {
		t.Error("physical properties break RTL")
	}
}

// The builder is wired end to end: the full message menu offers the row, the
// dispatcher runs the controls, and the message row carries the open builder.
func TestIssue3959_WiredThroughMenuDispatcherAndMessageRow_Integration(t *testing.T) {
	m, calls := issue3959Model()
	menu := renderNode(t, spanOf(chatux022MenuItems(m, m.Messages[0])))
	if !strings.Contains(menu, `data-action="analysis-open" data-id="ask"`) {
		t.Fatalf("the message menu has no Prepare an analysis row: %s", menu)
	}
	if strings.Contains(renderNode(t, spanOf(chatux022MenuItems(m, m.Messages[1]))), "analysis-open") {
		t.Error("a message with no figures request is offered the row in the full menu")
	}
	if strings.Contains(renderNode(t, message(m, handlers{}, m.Messages[0], false)), "analysis-builder") {
		t.Error("the builder shows before it is opened")
	}
	m.actWith("analysis-open", "ask", "")
	m.Analysis.PostID = "ask" // the page shows the builder after Open
	m.actWith("analysis-metric", "attrition", "")
	m.actWith("analysis-choose", "grouping:department", "")
	m.actWith("analysis-handoff", "ask", "")
	if got, want := strings.Join(*calls, "|"), "open:ask|metric:attrition|choose:grouping=department|handoff:general:ask:"; got != want {
		t.Errorf("dispatcher calls = %s, want %s", got, want)
	}
	m.Analysis.PostID = "ask"
	row := renderNode(t, message(m, handlers{}, m.Messages[0], false))
	if !strings.Contains(row, `id="analysis-ask"`) || !strings.Contains(row, "Only you see this request") {
		t.Errorf("the message row does not carry the open builder: %s", row)
	}
	if strings.Contains(renderNode(t, message(m, handlers{}, m.Messages[1], false)), "analysis-builder") {
		t.Error("the builder shows under another message")
	}
}

// A room another company hosts, or a signed-out page, never offers the builder
// and never hands anything off.
func TestIssue3959_NotOfferedInAnotherCompanysRoomOrSignedOut_Security(t *testing.T) {
	for name, mutate := range map[string]func(*Model){
		"signed out": func(m *Model) { m.CurrentUser = "" },
		"another company hosts the room": func(m *Model) {
			for i := range m.Conversations {
				m.Conversations[i].HostTenantID = "other-tenant"
			}
		},
	} {
		m, calls := issue3959Model()
		m.Analysis.PostID = "ask"
		mutate(&m)
		if len(analysisMenuItem(m, m.Messages[0])) != 0 || len(analysisBlock(m, m.Messages[0])) != 0 {
			t.Errorf("%s: the builder is offered", name)
		}
		m.actWith("analysis-open", "ask", "")
		m.actWith("analysis-handoff", "ask", "")
		if len(*calls) != 0 {
			t.Errorf("%s: controls ran: %v", name, *calls)
		}
	}
}
