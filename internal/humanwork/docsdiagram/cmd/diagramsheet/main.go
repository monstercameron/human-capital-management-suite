// Command diagramsheet renders every supported Mermaid kind with realistic HR
// examples into one HTML gallery for visual review:
//
//	go run ./internal/humanwork/docsdiagram/cmd/diagramsheet [-out path]
//
// The page carries the package stylesheet, the product light and dark token
// values, and CSS-only toggles for dark mode, right-to-left page direction and
// a 360 px phone column.
package main

import (
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/docsdiagram"
)

type example struct {
	heading string
	source  string
	locale  string
}

var examples = []example{
	{"Flowchart: hiring approval", `flowchart TD
    accTitle: Hiring approval
    req([Hiring manager opens requisition]) --> budget{Budget available?}
    budget -->|Yes| hrbp[HR business partner review]
    budget -->|No| finance[Finance exception request]
    finance -.->|approved| hrbp
    finance -->|rejected| closed((Closed))
    subgraph approvals [Approval chain]
        hrbp --> dir[Department director]
        dir --> vp{Level 7 or above?}
        vp -->|Yes| exec[VP sign-off]
    end
    vp -->|No| post[Post job to careers site]
    exec --> post
    post --> screen[Recruiter screening]
    screen --> panel[Interview panel]
    panel --> offer[[Offer letter]]
    offer ==> hired([Candidate hired])`, ""},
	{"Flowchart: left to right with a loop", `graph LR
    A[Timesheet submitted] --> B{Hours over 40?}
    B -- yes --> C[Overtime approval]
    C --> D[(Payroll ledger)]
    B -- no --> D
    D --> E[Pay run]
    E -.-> A`, ""},
	{"Sequence: leave request", `sequenceDiagram
    title Annual leave request
    autonumber
    actor E as Employee
    participant P as HCM portal
    participant M as Manager
    participant H as HR system
    E->>+P: Request 5 days of annual leave
    P->>H: Check remaining balance
    H-->>P: 18 days available
    P->>+M: Approval task
    Note right of M: SLA is two working days
    alt approved
        M-->>P: Approve
        P->>H: Book absence
    else rejected
        M-->>-P: Reject with reason
    end
    P-->>-E: Decision notification
    loop Every Friday
        H->>H: Accrue leave
    end`, ""},
	{"Pie: headcount by department", `pie showData
    title Headcount by department
    "Engineering" : 412
    "Sales" : 238
    "Customer success" : 164
    "Operations" : 121
    "Finance" : 58
    "People team" : 47`, ""},
	{"Bar and line: monthly attrition", `xychart-beta
    title "Monthly attrition, 2026"
    x-axis [Jan, Feb, Mar, Apr, May, Jun, Jul, Aug, Sep, Oct, Nov, Dec]
    y-axis "Leavers" 0 --> 40
    bar "Voluntary" [12, 9, 14, 18, 11, 16, 21, 25, 17, 13, 10, 8]
    bar "Involuntary" [3, 4, 2, 6, 3, 5, 4, 7, 3, 2, 4, 3]
    line "3-month average" [12, 11.7, 14, 16.3, 17.3, 19.7, 20, 26, 25.7, 22.7, 16.3, 13.3]`, ""},
	{"Gantt: onboarding plan", `gantt
    title New hire onboarding
    dateFormat YYYY-MM-DD
    axisFormat %b %e
    section Before day one
    Offer accepted          :milestone, m1, 2026-01-05, 0d
    Background check        :done, bg, 2026-01-05, 5d
    Laptop and accounts     :active, it, after bg, 4d
    section First week
    Orientation             :crit, orient, 2026-01-19, 2d
    Benefits enrolment      :ben, after orient, 3d
    Buddy introductions     :3d
    section First month
    Role training           :train, 2026-01-26, 12d
    30-day check-in         :milestone, after train, 0d`, ""},
	{"Timeline: policy history", `timeline
    title Remote work policy
    section Pilot
    2019 : Two remote days per team
    2020 : Fully remote : Home office stipend
    section Permanent
    2022 : Hybrid by default
    2024 : Work from anywhere, four weeks per year
    2026 : Regional hubs`, ""},
	{"Journey: first week experience", `journey
    title New hire first week
    section Day one
      Collect laptop: 3: New hire, IT
      Meet the team: 5: New hire, Manager
    section Rest of week
      Set up payroll: 2: New hire
      First project task: 4: New hire, Buddy`, ""},
	{"Pie in German", `pie title Belegschaft nach Standort
    "Berlin" : 1234.5
    "München" : 870.25
    "Hamburg" : 410`, "de-DE"},
	{"Unsupported kind (shows as code)", `classDiagram
    Employee <|-- Manager`, ""},
}

const pageCSS = `:root{color-scheme:light;--accent:#006b57;--ink:#102238;--muted:#526171;--canvas:#fafaf7;--surface:#ffffff;--line:#d5ddd8;` +
	`--hcm-color-success:#0f6136;--hcm-color-warning:#925400;--hcm-color-danger:#b42318;--hcm-color-info:#1555a3;` +
	`--surface-subtle:color-mix(in srgb,var(--surface) 72%,var(--canvas))}` +
	`:root:has(#dark:checked){color-scheme:dark;--accent:color-mix(in srgb,#006b57 40%,#fff);--ink:#f3f7fb;--muted:#aebdcb;--canvas:#0b1118;--surface:#131c26;--line:#354454;` +
	`--hcm-color-success:#69dda2;--hcm-color-warning:#f3c56f;--hcm-color-danger:#ff9d95;--hcm-color-info:#8abfff}` +
	`body{margin:0;background:var(--canvas);color:var(--ink);font-family:"Segoe UI Variable","Segoe UI",system-ui,sans-serif}` +
	`header{position:sticky;top:0;z-index:1;display:flex;flex-wrap:wrap;gap:16px;align-items:center;padding:12px 16px;background:var(--surface);border-bottom:1px solid var(--line)}` +
	`header h1{font-size:1.1rem;margin:0 16px 0 0}` +
	`main{max-width:1040px;margin:0 auto;padding:16px}` +
	`:root:has(#phone:checked) main{max-width:360px;padding:16px 8px}` +
	`:root:has(#rtl:checked) main{direction:rtl}` +
	`section.example{margin:0 0 32px}section.example h2{font-size:1rem;margin:0 0 8px}` +
	`details{margin-top:8px;color:var(--muted);font-size:.85rem}pre{white-space:pre-wrap;background:var(--surface);border:1px solid var(--line);padding:8px;border-radius:6px;color:var(--ink)}` +
	`.failed{border:1px dashed var(--line);padding:8px;border-radius:6px}.failed p{margin:0 0 6px;color:var(--muted)}`

func main() {
	out := flag.String("out", filepath.Join(".artifacts", "tmp", "diagram-gallery.html"), "output HTML path")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "diagramsheet:", err)
		os.Exit(1)
	}
	fmt.Println(*out)
}

func run(out string) error {
	page, err := gallery()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	return os.WriteFile(out, []byte(page), 0o644)
}

func gallery() (string, error) {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">`)
	b.WriteString(`<title>Docs diagram gallery</title><style>` + pageCSS + docsdiagram.Stylesheet() + `</style></head><body>`)
	b.WriteString(`<header><h1>Docs diagram gallery</h1>` +
		`<label><input type="checkbox" id="dark"> Dark</label>` +
		`<label><input type="checkbox" id="rtl"> Page dir=rtl</label>` +
		`<label><input type="checkbox" id="phone"> 360 px column</label></header><main>`)
	for _, ex := range examples {
		b.WriteString(`<section class="example"><h2>` + html.EscapeString(ex.heading) + `</h2>`)
		node, err := docsdiagram.Render(ex.source, docsdiagram.Options{Locale: ex.locale})
		if err != nil {
			b.WriteString(`<div class="failed"><p>This diagram could not be drawn (` + html.EscapeString(err.Error()) + `).</p><pre>` + html.EscapeString(ex.source) + `</pre></div></section>`)
			continue
		}
		markup, err := ui.RenderToString(node)
		if err != nil {
			return "", fmt.Errorf("%s: %w", ex.heading, err)
		}
		b.WriteString(markup)
		b.WriteString(`<details><summary>Mermaid source</summary><pre>` + html.EscapeString(ex.source) + `</pre></details></section>`)
	}
	b.WriteString(`</main></body></html>`)
	return b.String(), nil
}
