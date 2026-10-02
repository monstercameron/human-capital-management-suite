package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/extcost"
	"strings"
	"testing"
	"time"
)

func extcostPageFixture(locale string) ExtcostPageProps {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	view := ApplyLocale(NewView(ExtcostPageID, "tenant-a", "admin", ""), ResolveProductLocale(locale))
	line := extcost.Line{Call: extcost.Call{Tenant: "tenant-a", Feature: "answers", Provider: "OpenAI", Operation: "model.invoke", Key: "attempt", Cause: "cause", Attribution: extcost.Attribution{Actor: "person-id", Agent: "agent-id", WorkflowDefinition: "workflow-id"}}, Measurement: extcost.Measurement{Units: extcost.Units{extcost.InputTokens: 10, extcost.OutputTokens: 20}, Outcome: "succeeded", Billed: true}, ScheduleVersion: "approved", Currency: "USD", CostMicros: 123456, At: at, BudgetAt: at}
	groups := []extcost.Group{{Kind: "total", Currency: "USD", CostMicros: 123456, Calls: 1}}
	for _, v := range [][2]string{{"day", "2026-10-01"}, {"provider", "OpenAI"}, {"feature", "answers"}, {"purpose", "answers"}, {"agent", "agent-id"}, {"workflow", "workflow-id"}, {"person", "person-id"}} {
		groups = append(groups, extcost.Group{Kind: v[0], Name: v[1], Currency: "USD", CostMicros: 123456, Calls: 1})
	}
	return ExtcostPageProps{View: view, Allowed: true, Report: extcost.Report{Lines: []extcost.Line{line}, Groups: groups, Budgets: []extcost.BudgetStatus{{Budget: extcost.Budget{Tenant: "tenant-a", Scope: extcost.Scope{Kind: "tenant", ID: "tenant-a", Period: "month"}, Currency: "USD", LimitMicros: 150000, WarningBasisPoints: 8000}, SpentMicros: 123456, Warning: true}}, Statistics: []extcost.Statistic{{Workflow: "workflow-id", Currency: "USD", Runs: 1, MedianMicros: 123456, P95Micros: 123456}}}, Names: map[string]string{"agent:agent-id": "Support helper", "workflow:workflow-id": "Leave approval", "person:person-id": "Curtis Bell"}}
}
func extcostMarkup(t *testing.T, p ExtcostPageProps) string {
	t.Helper()
	markup, err := ui.RenderToString(BuildExternalUsagePage(p))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}
func TestTodo_EXTCOST_006(t *testing.T) {
	p := extcostPageFixture("en-US")
	p.Report.Statistics = append(p.Report.Statistics, extcost.Statistic{Workflow: "workflow-id", Node: "node-id", Currency: "USD", Runs: 1, MedianMicros: 123456, P95Micros: 123456})
	p.Names["node:node-id"] = "Review request"

	markup := extcostMarkup(t, p)
	for _, want := range []string{"External usage", "Spend by day", "Spend by feature", "Spend by agent", "Top callers", "Support helper", "Leave approval", "Curtis Bell", "Measured", "Not yet reconciled", "Apply filters", "Export usage", "95th percentile", "Leave approval / Review request"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Count(markup, "<h1") != 1 || strings.Contains(markup, ">tenant-a<") || strings.Contains(markup, ">agent-id<") {
		t.Fatal("heading or identifier disclosure")
	}
	m := ExtcostPageModule(ExtcostPageRenderer{})
	if m.Definition.Route != ExtcostPagePath || m.Definition.ParentNav != PageAdmin || m.Access.Audience != PageAudienceDenied {
		t.Fatal("admin module access/route")
	}
	if _, err := ui.RenderToString(m.Render.Render(p.View)); err != nil {
		t.Fatal(err)
	}
}
func TestTodo_EXTCOST_006_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			p := extcostPageFixture(locale)
			for _, kind := range []string{"tenant", "feature", "agent", "workflow", "run", "node", "step"} {
				if extcostText(p.View.Locale, kind) == "" {
					t.Fatal("budget scope has no localized label", kind)
				}
			}

			for _, state := range []string{"ready", "denied", "loading", "error", "empty"} {
				x := p
				switch state {
				case "denied":
					x.Allowed = false
				case "loading":
					x.Loading = true
				case "error":
					x.Error = true
				case "empty":
					x.Report = extcost.Report{}
				}
				markup := extcostMarkup(t, x)
				if !strings.Contains(markup, `data-extcost-state="`+state+`"`) {
					t.Fatal("state", state)
				}
				for _, bad := range []string{"⟦", "⟧", "[external.", "EXTCOST-", "localStorage", "sessionStorage", "indexedDB"} {
					if strings.Contains(markup, bad) {
						t.Fatal("raw key/internal implementation", bad)
					}
				}
				if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
					t.Fatal("RTL missing")
				}
			}
			p.Report.ReconciledAt = map[string]time.Time{"attempt": p.Report.Lines[0].At}
			scoped := extcostMarkup(t, p)
			if !strings.Contains(scoped, extcostText(p.View.Locale, "reconciled")) || strings.Contains(scoped, extcostText(p.View.Locale, "unreconciled")) {
				t.Fatal("scoped last-reconciled status missing")
			}
			p.Report.Truncated = true
			p.Report.Lines[0].Measurement.Estimated = true
			p.Report.Lines[0].Finding = "provider_price_difference"
			p.Report.Reconciliations = []extcost.Reconciliation{{Provider: "OpenAI", Operation: "model.invoke", Currency: "USD", Day: extcost.PeriodStart(p.Report.Lines[0].At, "day"), At: p.Report.Lines[0].At}}
			markup := extcostMarkup(t, p)
			for _, key := range []string{"estimated", "reconciled", "finding", "truncated"} {
				if !strings.Contains(markup, extcostText(p.View.Locale, key)) {
					t.Fatal("copy missing", key)
				}
			}
			amount := extcostAmount(p.View.Locale, 123456, "USD")
			if !strings.Contains(markup, amount) {
				t.Fatal("localized currency not rendered")
			}
		})
	}
	t.Log("SSR with the real product catalog: en-US/de-DE/ar; denied/loading/error/empty/ready; RTL; no bracketed keys PASS")
}
func TestTodo_EXTCOST_006_Accessibility(t *testing.T) {
	markup := extcostMarkup(t, extcostPageFixture("en-US"))
	for _, key := range []string{"provider", "operation", "feature", "purpose", "agent", "workflow", "person", "search", "from", "until"} {
		if !strings.Contains(markup, `for="extcost-filter-`+key+`"`) || !strings.Contains(markup, `id="extcost-filter-`+key+`"`) {
			t.Fatal("field not labeled", key)
		}
	}
	for _, want := range []string{"<form", `method="get"`, `type="submit"`, "<details", "<summary", `aria-label="View calls:`} {
		if !strings.Contains(markup, want) {
			t.Fatal("keyboard/semantic behavior missing", want)
		}
	}
	css := ExtcostStylesheet()
	for _, want := range []string{"minmax(min(100%,12rem)", "max-width:800px", "max-width:390px", "overflow-wrap:anywhere", ":focus-visible", "--hcm-color-text", "--hcm-radius-surface", "--hcm-shadow-resting"} {
		if !strings.Contains(css, want) {
			t.Fatal("responsive/token contract", want)
		}
	}
	if strings.Contains(css, "#") || strings.Contains(css, "font-family") || strings.Contains(css, "animation") {
		t.Fatal("hard-coded color/font or motion")
	}
	t.Log("semantic labels, keyboard form/details, token styling and responsive 1440/800/390/320 CSS contract PASS (not browser layout verification)")
}
func TestTodo_EXTCOST_006_Security(t *testing.T) {
	p := extcostPageFixture("en-US")
	p.Allowed = false
	markup := extcostMarkup(t, p)
	for _, secret := range []string{"OpenAI", "Curtis Bell", "Support helper", "Export usage", "extcost-filter-provider"} {
		if strings.Contains(markup, secret) {
			t.Fatal("denied page exposed data", secret)
		}
	}
	p.Allowed = true
	p.Loading = true
	markup = extcostMarkup(t, p)
	if !strings.Contains(markup, "Refresh usage") {
		t.Fatal("loading has no next action")
	}
	renderer := ExtcostPageRenderer{Project: func(v View) ExtcostPageProps { out := extcostPageFixture("en-US"); out.View = v; return out }}
	if _, err := ui.RenderToString(renderer.Render(p.View)); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_EXTCOST_006_Pending(t *testing.T) {
	for _, tag := range []string{"en-US", "de-DE", "ar"} {
		p := extcostPageFixture(tag)
		p.Report = extcost.Report{Pending: []extcost.Reservation{{Call: extcost.Call{Provider: "OpenAI", Key: "pending-attempt"}, MaximumMicros: 123456, Currency: "USD", State: "sent"}}}
		markup := extcostMarkup(t, p)
		if !strings.Contains(markup, extcostText(p.View.Locale, "pending")) || !strings.Contains(markup, extcostText(p.View.Locale, "pending_help")) || strings.Contains(markup, extcostText(p.View.Locale, "empty")) {
			t.Fatal("pending call presented as zero/empty", tag)
		}
		if strings.Contains(markup, "??") {
			t.Fatal("copy was corrupted", tag)
		}
		p = extcostPageFixture(tag)
		p.Report.Lines[0].ScheduleVersion = ""
		markup = extcostMarkup(t, p)
		if !strings.Contains(markup, extcostText(p.View.Locale, "unpriced")) {
			t.Fatal("unpriced shown as zero", tag)
		}
	}
	if !strings.Contains(extcostText(ResolveProductLocale("de-DE"), "pending_help"), "Prüfen") {
		t.Fatal("German pending copy corrupted")
	}
	if !strings.Contains(extcostText(ResolveProductLocale("ar"), "pending"), "اتصالات") {
		t.Fatal("Arabic pending copy corrupted")
	}
}
