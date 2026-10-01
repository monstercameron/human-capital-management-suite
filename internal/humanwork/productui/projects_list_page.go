package productui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// projectListRow is one authorized project, already resolved to display
// text. Search is the lower-cased haystack the filter matches against;
// TaskCount/TaskKnown carry the raw count so sorting never parses the
// locale-formatted Tasks text.
type projectListRow struct {
	ID, Name, Description, Status, StatusKey string
	Owner, OwnerPhoto, Tasks, Href, Search   string
	TaskCount                                int
	TaskKnown                                bool
	// DoneCount is the finished share of TaskCount; Pending shows a quiet
	// placeholder while the counts load.
	DoneCount int
	Pending   bool
}

type projectListCopy struct {
	listLabel, searchLabel, searchPlaceholder, clearSearch string
	colProject, colStatus, colTasks, colOwner              string
	progressOpen                                           func(open int) string
	progressLabel                                          func(done, total int) string
	progressLoading, progressEmpty, progressAllDone        string
	shownAll                                               func(shown, total int, status string) string
	sortLabel, sortedAsc, sortedDesc                       string
	pagesLabel, previous, next, pageSize                   string
	noMatch                                                func(query string) string
	shown                                                  func(shown, total int) string
	openNamed                                              func(name string) string
	sortOption                                             func(column string, descending bool) string
	pageOf                                                 func(page, pages int) string
	pageRange                                              func(from, to, total int) string
}

type projectsListProps struct {
	Rows     []projectListRow
	Copy     projectListCopy
	Navigate func(string)
}

// projectListSort is the active sort: a column key and a direction.
type projectListSort struct {
	Key  string
	Desc bool
}

const (
	projectSearchInputID = "project-search"
	projectSortName      = "name"
	projectSortStatus    = "status"
	projectSortTasks     = "tasks"
	projectSortOwner     = "owner"
)

var projectPageSizes = []int{10, 25, 50, 100}

const projectPageSizeKey = "hcm.projects.pagesize"

// projectStoredPageSize is the viewer's remembered rows-per-page, when it is
// one of the offered sizes.
func projectStoredPageSize(fallback int) int {
	if n, err := strconv.Atoi(projectStorageRead(projectPageSizeKey)); err == nil {
		for _, size := range projectPageSizes {
			if size == n {
				return n
			}
		}
	}
	return fallback
}

// projectDefaultPageSize shows a typical workspace's projects on one page;
// paging and the size choice appear only past it.
const projectDefaultPageSize = 25

// projectsList is the Projects home: a dense, searchable, sortable, paged
// list. Filter, sort and paging all run over the authorized rows already on
// the page, so they are instant and never widen what the service returned.
// The search box is uncontrolled (no Value prop): a render that lands
// mid-word must not write an older string back over what the user typed.
func projectsList(props projectsListProps) ui.Node {
	query := ui.UseState("")
	order := ui.UseState(projectListSort{Key: projectSortName})
	page := ui.UseState(1)
	// The rows-per-page choice is a per-viewer convenience, remembered in
	// the browser; the page still works when storage is unavailable.
	size := ui.UseState(projectStoredPageSize(projectDefaultPageSize))

	onInput := ui.UseEvent(func(event ui.InputEvent) {
		query.Set(event.GetValue())
		page.Set(1)
	})
	// The box is the source of truth. Text typed before hydration attached
	// OnInput, and keystrokes that arrive in a burst while a render is in
	// flight, land in the box without reaching state. On mount and after
	// each query change (GWC runs a no-deps effect only on mount), adopt the
	// box's live value if it differs; it converges in one render.
	ui.UseEffect(func() func() {
		if value := projectFieldValue(projectSearchInputID); value != query.Get() && (value != "" || query.Get() != "") {
			query.Set(value)
			page.Set(1)
		}
		return nil
	}, query.Get())
	clear := ui.UseEvent(func() {
		setDocsFieldValue(projectSearchInputID, "")
		query.Set("")
		page.Set(1)
		docsFocusElement(projectSearchInputID)
	})
	onKey := ui.UseEvent(func(event ui.KeyboardEvent) {
		if event.GetKey() == "Escape" && query.Get() != "" {
			event.PreventDefault()
			setDocsFieldValue(projectSearchInputID, "")
			query.Set("")
			page.Set(1)
		}
	})
	sortBy := func(key string) ui.Handler {
		return ui.UseEvent(func() {
			current := order.Get()
			next := projectListSort{Key: key, Desc: key == projectSortTasks}
			if current.Key == key {
				next.Desc = !current.Desc
			}
			order.Set(next)
			page.Set(1)
		})
	}
	sortName, sortStatus, sortTasks, sortOwner := sortBy(projectSortName), sortBy(projectSortStatus), sortBy(projectSortTasks), sortBy(projectSortOwner)
	onSortSelect := ui.UseEvent(func(event ui.ChangeEvent) {
		key, dir, _ := strings.Cut(event.GetValue(), ":")
		order.Set(projectListSort{Key: key, Desc: dir == "desc"})
		page.Set(1)
	})
	onSize := ui.UseEvent(func(event ui.ChangeEvent) {
		if n, err := strconv.Atoi(event.GetValue()); err == nil && n > 0 {
			size.Set(n)
			projectStorageWrite(projectPageSizeKey, strconv.Itoa(n))
			page.Set(1)
		}
	})
	previous := ui.UseEvent(func() { page.Set(max(1, page.Get()-1)) })
	next := ui.UseEvent(func() { page.Set(page.Get() + 1) })

	// A Tasks column of dashes says nothing; it appears only when the
	// service reported a count for at least one project.
	showTasks := false
	for _, row := range props.Rows {
		if row.TaskKnown || row.Pending {
			showTasks = true
			break
		}
	}
	current := order.Get()
	if current.Key == projectSortTasks && !showTasks {
		current = projectListSort{Key: projectSortName}
	}

	terms := strings.Fields(strings.ToLower(query.Get()))
	matched := make([]projectListRow, 0, len(props.Rows))
	for _, row := range props.Rows {
		if projectRowMatches(row.Search, terms) {
			matched = append(matched, row)
		}
	}
	sortProjectRows(matched, current)

	perPage := size.Get()
	pages := max(1, (len(matched)+perPage-1)/perPage)
	pageNumber := min(max(1, page.Get()), pages)
	from := (pageNumber - 1) * perPage
	to := min(from+perPage, len(matched))
	rows := make([]ui.Node, 0, to-from)
	// One status on every matching row is a fact about the list, not a
	// column: it moves into the count line and the column goes.
	showStatus := false
	for _, row := range matched {
		if row.StatusKey != matched[0].StatusKey {
			showStatus = true
			break
		}
	}
	countText := props.Copy.shown(len(matched), len(props.Rows))
	if !showStatus && len(matched) > 0 && matched[0].Status != "" && props.Copy.shownAll != nil {
		countText = props.Copy.shownAll(len(matched), len(props.Rows), matched[0].Status)
	}
	for _, row := range matched[from:to] {
		rows = append(rows, projectListItem(props, row, showTasks, showStatus, terms...))
	}

	// Phones hide the column headers, so the sort lives in a select there.
	sortOptions := []ui.Node{}
	addOption := func(key, column string, desc bool) {
		value := key + ":asc"
		if desc {
			value = key + ":desc"
		}
		sortOptions = append(sortOptions, html.Option(html.Props{Value: value, Selected: current.Key == key && current.Desc == desc, Text: props.Copy.sortOption(column, desc)}))
	}
	addOption(projectSortName, props.Copy.colProject, false)
	addOption(projectSortName, props.Copy.colProject, true)
	addOption(projectSortStatus, props.Copy.colStatus, false)
	addOption(projectSortOwner, props.Copy.colOwner, false)
	if showTasks {
		addOption(projectSortTasks, props.Copy.colTasks, true)
	}

	toolbar := html.Div(html.Props{Class: "project-list-toolbar"},
		html.Div(html.Props{Class: "project-search", Role: "search"},
			html.Label(html.Props{For: projectSearchInputID, Class: "sr-only", Text: props.Copy.searchLabel}),
			productIcon("search", "project-search-icon"),
			html.Input(html.Props{ID: projectSearchInputID, Type: "search", Placeholder: props.Copy.searchPlaceholder, AutoComplete: "off", OnInput: onInput, OnKeyDown: onKey, Aria: map[string]string{"controls": "project-list"}}),
		),
		html.Label(html.Props{Class: "project-sort-select"},
			html.Span(html.Props{Class: "sr-only", Text: props.Copy.sortLabel}),
			html.Select(html.Props{OnChange: onSortSelect, Aria: map[string]string{"label": props.Copy.sortLabel}}, sortOptions...),
		),
		html.P(html.Props{Class: "project-list-count", Role: "status", Aria: map[string]string{"live": "polite"}, Text: countText}),
	)

	if len(matched) == 0 {
		return html.Div(html.Props{Class: "project-list-shell"}, toolbar, html.Div(html.Props{Class: "project-list-empty"},
			html.P(html.Props{Text: props.Copy.noMatch(strings.TrimSpace(query.Get()))}),
			html.Button(html.Props{Class: "button secondary", Type: "button", OnClick: clear, Text: props.Copy.clearSearch}),
		))
	}

	header := func(key, label, class string, handler ui.Handler) ui.Node {
		sortState := "none"
		indicator := ""
		hint := ""
		if current.Key == key {
			sortState, indicator, hint = "ascending", "↑", props.Copy.sortedAsc
			if current.Desc {
				sortState, indicator, hint = "descending", "↓", props.Copy.sortedDesc
			}
		}
		children := []ui.Node{html.Span(html.Props{Text: label})}
		if hint != "" {
			children = append(children, html.Span(html.Props{Class: "project-sort-indicator", Aria: map[string]string{"hidden": "true"}, Text: indicator}), html.Span(html.Props{Class: "sr-only", Text: " " + hint}))
		} else {
			children = append(children, html.Span(html.Props{Class: "project-sort-indicator project-sort-idle", Aria: map[string]string{"hidden": "true"}, Text: "\u2195"}))
		}
		return html.Span(html.Props{Class: "project-list-headcell " + class, Role: "columnheader", Aria: map[string]string{"sort": sortState}},
			html.Button(html.Props{Class: "project-sort", Type: "button", OnClick: handler, Data: map[string]string{"sort-key": key, "active": strconv.FormatBool(current.Key == key)}}, children...))
	}
	headCells := []ui.Node{header(projectSortName, props.Copy.colProject, "", sortName)}
	if showStatus {
		headCells = append(headCells, header(projectSortStatus, props.Copy.colStatus, "", sortStatus))
	}
	if showTasks {
		headCells = append(headCells, header(projectSortTasks, props.Copy.colTasks, "", sortTasks))
	}
	headCells = append(headCells, header(projectSortOwner, props.Copy.colOwner, "", sortOwner))
	tableClass := "project-list-table"
	if !showTasks {
		tableClass += " project-list-no-tasks"
	}
	if !showStatus {
		tableClass += " project-list-no-status"
	}
	table := html.Div(html.Props{Class: tableClass},
		html.Div(html.Props{Class: "project-list-head", Role: "row"}, headCells...),
		html.Ul(html.Props{ID: "project-list", Class: "project-list", Role: "list", Aria: map[string]string{"label": props.Copy.listLabel}}, rows...),
	)

	sizeOptions := make([]ui.Node, 0, len(projectPageSizes))
	for _, n := range projectPageSizes {
		sizeOptions = append(sizeOptions, html.Option(html.Props{Value: strconv.Itoa(n), Selected: n == perPage, Text: strconv.Itoa(n)}))
	}
	// Every table in Projects has the same footer: the range, an always
	// visible rows-per-page choice, and the pager once there is more than
	// one page.
	footer := []ui.Node{
		html.P(html.Props{Class: "project-page-range", Text: props.Copy.pageRange(from+1, to, len(matched))}),
		html.Label(html.Props{Class: "project-page-size"},
			html.Span(html.Props{Text: props.Copy.pageSize}),
			html.Select(html.Props{ID: "project-page-size", OnChange: onSize}, sizeOptions...),
		),
	}
	if pages > 1 {
		footer = append(footer, html.Div(html.Props{Class: "project-pager"},
			html.Button(html.Props{Class: "button secondary small", Type: "button", Disabled: pageNumber <= 1, OnClick: previous, Text: props.Copy.previous}),
			html.Span(html.Props{Class: "project-pager-status", Aria: map[string]string{"live": "polite"}, Text: props.Copy.pageOf(pageNumber, pages)}),
			html.Button(html.Props{Class: "button secondary small", Type: "button", Disabled: pageNumber >= pages, OnClick: next, Text: props.Copy.next}),
		))
	}
	return html.Div(html.Props{Class: "project-list-shell"}, toolbar, table,
		html.Nav(html.Props{Class: "project-list-footer", Aria: map[string]string{"label": props.Copy.pagesLabel}}, footer...))
}

// sortProjectRows orders rows in place. Ties fall back to name then ID, so
// the order is stable across renders and pages never reshuffle.
func sortProjectRows(rows []projectListRow, order projectListSort) {
	fold := strings.ToLower
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		cmp := 0
		switch order.Key {
		case projectSortStatus:
			cmp = strings.Compare(fold(a.Status), fold(b.Status))
		case projectSortOwner:
			cmp = strings.Compare(fold(a.Owner), fold(b.Owner))
		case projectSortTasks:
			// Progress (the finished share) first, then the task count.
			ratio := func(row projectListRow) float64 {
				if row.TaskCount == 0 {
					return 0
				}
				return float64(row.DoneCount) / float64(row.TaskCount)
			}
			switch {
			case a.TaskKnown != b.TaskKnown:
				// Unknown counts sort last in either direction.
				return a.TaskKnown
			case ratio(a) < ratio(b):
				cmp = -1
			case ratio(a) > ratio(b):
				cmp = 1
			case a.TaskCount < b.TaskCount:
				cmp = -1
			case a.TaskCount > b.TaskCount:
				cmp = 1
			}
		}
		if cmp != 0 {
			if order.Desc {
				return cmp > 0
			}
			return cmp < 0
		}
		name := strings.Compare(fold(a.Name), fold(b.Name))
		if name != 0 {
			if order.Key == projectSortName && order.Desc {
				return name > 0
			}
			return name < 0
		}
		return a.ID < b.ID
	})
}

func projectListItem(props projectsListProps, row projectListRow, showTasks, showStatus bool, terms ...string) ui.Node {
	name := []ui.Node{html.Span(html.Props{Class: "project-list-name"}, projectHighlight(row.Name, terms)...)}
	if row.Description != "" {
		name = append(name, html.Span(html.Props{Class: "project-list-description", Text: row.Description}))
	}
	status := ui.Node(html.Span(html.Props{Class: "project-list-muted", Text: "—"}))
	if row.Status != "" {
		status = html.Span(html.Props{Class: "project-page-status", Data: map[string]string{"state": row.StatusKey}},
			html.Span(html.Props{Class: "project-page-status-dot", Aria: map[string]string{"hidden": "true"}}), html.Span(html.Props{Text: row.Status}))
	}
	owner := ui.Node(html.Span(html.Props{Class: "project-list-muted", Text: "—"}))
	if row.Owner != "" {
		owner = html.Span(html.Props{Class: "project-list-owner"}, personAvatar(row.Owner, "", row.OwnerPhoto, "tiny"), html.Span(html.Props{Text: row.Owner}))
	}
	cells := []ui.Node{html.Span(html.Props{Class: "project-list-cell project-list-title"}, name...)}
	if showStatus {
		cells = append(cells, html.Span(html.Props{Class: "project-list-cell project-list-status"}, status))
	}
	if showTasks {
		cells = append(cells, html.Span(html.Props{Class: "project-list-cell project-list-progress-cell"}, projectProgress(props.Copy, row)))
	}
	cells = append(cells, html.Span(html.Props{Class: "project-list-cell"}, owner))
	link := softwareLink(props.Navigate, html.Props{Class: "project-list-row", Aria: map[string]string{"label": props.Copy.openNamed(row.Name)}}, row.Href, cells...)
	return html.WithKey(html.Li(html.Props{Data: map[string]string{"project-id": row.ID}}, link), row.ID)
}

// projectProgress is a small done/total bar with the open count. While the
// counts load it is a quiet placeholder; without counts it is a dash.
func projectProgress(copy projectListCopy, row projectListRow) ui.Node {
	if row.Pending {
		return html.Span(html.Props{Class: "project-progress project-progress-shimmer", Title: copy.progressLoading},
			html.Span(html.Props{Class: "project-progress-track", Aria: map[string]string{"hidden": "true"}}),
			html.Span(html.Props{Class: "sr-only", Text: copy.progressLoading}))
	}
	if !row.TaskKnown {
		return html.Span(html.Props{Class: "project-list-muted", Text: "—"})
	}
	if row.TaskCount == 0 {
		return html.Span(html.Props{Class: "project-progress project-progress-empty"}, html.Span(html.Props{Class: "project-progress-text", Text: copy.progressEmpty}))
	}
	percent := 0
	if row.TaskCount > 0 {
		percent = row.DoneCount * 100 / row.TaskCount
	}
	label := copy.progressLabel(row.DoneCount, row.TaskCount)
	openText := copy.progressOpen(row.TaskCount - row.DoneCount)
	if row.DoneCount == row.TaskCount {
		openText = copy.progressAllDone
	}
	return html.Span(html.Props{Class: "project-progress", Title: openText, Data: map[string]string{"empty": strconv.FormatBool(row.DoneCount == 0)}},
		html.Span(html.Props{Class: "project-progress-track", Aria: map[string]string{"hidden": "true"}},
			html.Span(html.Props{Class: "project-progress-fill", Style: map[string]string{"inline-size": strconv.Itoa(percent) + "%"}})),
		html.Span(html.Props{Class: "project-progress-text", Text: label}),
	)
}

// projectHighlight splits text so every case-insensitive occurrence of a
// search term is wrapped in <mark>; with no terms it is the plain text.
func projectHighlight(text string, terms []string) []ui.Node {
	lower := strings.ToLower(text)
	marks := make([]bool, len(text))
	any := false
	for _, term := range terms {
		if term == "" {
			continue
		}
		for start := 0; ; {
			at := strings.Index(lower[start:], term)
			if at < 0 {
				break
			}
			for i := start + at; i < start+at+len(term) && i < len(marks); i++ {
				marks[i], any = true, true
			}
			start += at + len(term)
		}
	}
	if !any || len(lower) != len(text) {
		return []ui.Node{ui.Text(text)}
	}
	out := []ui.Node{}
	for i := 0; i < len(text); {
		j := i
		for j < len(text) && marks[j] == marks[i] {
			j++
		}
		if marks[i] {
			out = append(out, html.Tag("mark", html.Props{Class: "project-match", Text: text[i:j]}))
		} else {
			out = append(out, ui.Text(text[i:j]))
		}
		i = j
	}
	return out
}

func projectRowMatches(haystack string, terms []string) bool {
	for _, term := range terms {
		if !strings.Contains(haystack, term) {
			return false
		}
	}
	return true
}

const projectListStylesheet = `
.project-list-shell{display:grid;gap:var(--hcm-space-2)}
.project-list-toolbar{display:flex;align-items:center;justify-content:space-between;gap:var(--hcm-space-2);flex-wrap:wrap}
.project-search{position:relative;flex:1 1 20rem;max-inline-size:32rem}
.project-search-icon{position:absolute;inset-inline-start:.75rem;inset-block-start:50%;translate:0 -50%;width:1rem;height:1rem;color:var(--muted);pointer-events:none}
.project-search input{inline-size:100%;min-block-size:2.5rem;padding-inline:2.25rem .75rem;border:1px solid var(--control-border,var(--line));border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit}
.project-search input:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:1px}
.project-sort-select{display:none}
.project-sort-select select,.project-page-size select{min-block-size:2.5rem;padding-inline:.6rem;border:1px solid var(--control-border,var(--line));border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit}
.project-list-count{margin:0;color:var(--muted);font-size:var(--hcm-font-size-small)}
.project-list-table{border:1px solid var(--line);border-radius:var(--hcm-radius-surface);background:var(--surface);overflow:hidden}
.project-list-head,.project-list-row{display:grid;grid-template-columns:minmax(0,1fr) 9rem 5rem 12rem;align-items:center;column-gap:var(--hcm-space-3);padding-inline:var(--hcm-space-3)}
.project-list-head{min-block-size:2.5rem;border-block-end:1px solid var(--line);background:color-mix(in srgb,var(--ink) 2.5%,var(--surface))}
.project-list-headcell{display:flex;min-inline-size:0}
.project-list-headcell.project-list-num{justify-content:flex-end}
.project-sort{display:inline-flex;align-items:center;gap:.3rem;min-block-size:2rem;margin-inline:-.4rem;padding-inline:.4rem;border:0;border-radius:var(--hcm-radius-control);background:transparent;color:var(--muted);font:inherit;font-size:var(--hcm-font-size-small);font-weight:600;cursor:pointer}
.project-sort:hover{background:color-mix(in srgb,var(--ink) 6%,transparent);color:var(--ink)}
.project-sort[data-active="true"]{color:var(--ink)}
.project-sort:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:1px}
.project-sort-indicator{color:var(--accent);font-weight:700}
.project-list{list-style:none;margin:0;padding:0}
.project-list>li{max-inline-size:none;margin:0}
.project-list-no-tasks .project-list-head,.project-list-no-tasks .project-list-row{grid-template-columns:minmax(0,1fr) 9rem 12rem}
.project-list .project-page-status{font-size:var(--hcm-font-size-small)}
.project-list>li+li{border-block-start:1px solid var(--line)}
.project-list-row{min-block-size:3.5rem;padding-block:.6rem;color:var(--ink);text-decoration:none}
.project-list-row:hover{background:color-mix(in srgb,var(--ink) 4%,transparent)}
.project-list-row:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:-3px}
.project-list-cell{min-inline-size:0}
.project-list-title{display:grid;gap:.1rem}
.project-list-name{font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.project-list-description{color:var(--muted);font-size:var(--hcm-font-size-small);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.project-list-num{text-align:end;font-variant-numeric:tabular-nums}
.project-list-owner{display:inline-flex;align-items:center;gap:.45rem;min-inline-size:0}
.project-list-owner>span:last-child{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.project-list-muted{color:var(--muted)}
.project-list-footer{display:flex;align-items:center;flex-wrap:wrap;gap:var(--hcm-space-2) var(--hcm-space-3);color:var(--muted);font-size:var(--hcm-font-size-small)}
.project-page-range{margin:0;margin-inline-end:auto;font-variant-numeric:tabular-nums}
.project-page-size{display:inline-flex;align-items:center;gap:.5rem}
.project-pager{display:inline-flex;align-items:center;gap:.5rem}
.project-pager-status{min-inline-size:6rem;text-align:center;color:var(--ink);font-variant-numeric:tabular-nums}
.project-list-empty{display:grid;justify-items:start;gap:var(--hcm-space-2);padding:var(--hcm-space-4);border:1px dashed var(--line);border-radius:var(--hcm-radius-surface);color:var(--muted)}
.project-list-empty p{margin:0}
.project-page-range,.project-list-footer p{font-size:var(--hcm-font-size-small);color:var(--muted)}
.project-list-footer:empty{display:none}
.project-search input::-webkit-search-cancel-button{-webkit-appearance:none;appearance:none;inline-size:.75rem;block-size:.75rem;background:linear-gradient(45deg,transparent 44%,var(--muted) 44% 56%,transparent 56%),linear-gradient(-45deg,transparent 44%,var(--muted) 44% 56%,transparent 56%);cursor:pointer}
.project-search input::-webkit-search-cancel-button:hover{background:linear-gradient(45deg,transparent 44%,var(--ink) 44% 56%,transparent 56%),linear-gradient(-45deg,transparent 44%,var(--ink) 44% 56%,transparent 56%)}
.project-list-owner>span:last-child{font-size:.875rem;line-height:1.3;color:var(--ink)}
.project-list-name{line-height:1.3}
.project-match{padding:0 .05em;border-radius:2px;background:color-mix(in srgb,var(--accent) 22%,transparent);color:inherit}
.project-list-owner>:first-child{flex:none;inline-size:1.75rem;block-size:1.75rem}
.project-list-row{align-items:center}
.project-list-head,.project-list-row{grid-template-columns:minmax(0,1fr) 9rem 11rem 12rem}
.project-list-no-tasks .project-list-head,.project-list-no-tasks .project-list-row{grid-template-columns:minmax(0,1fr) 9rem 12rem}
.project-progress{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:.625rem;min-inline-size:0}
.project-progress-empty{grid-template-columns:minmax(0,1fr)}
.project-progress[data-empty="true"] .project-progress-track::before{content:"";position:absolute;inset-block:0;inset-inline-start:0;inline-size:3px;border-radius:999px;background:color-mix(in srgb,var(--ink) 35%,transparent)}
.project-progress-text{white-space:nowrap}
.project-sort-idle{color:currentColor;font-weight:500;opacity:.35}
.project-sort:hover .project-sort-idle{opacity:1}
.project-list-no-status .project-list-head,.project-list-no-status .project-list-row{grid-template-columns:minmax(0,1fr) 11rem 12rem}
.project-list-no-status.project-list-no-tasks .project-list-head,.project-list-no-status.project-list-no-tasks .project-list-row{grid-template-columns:minmax(0,1fr) 12rem}
.project-list-toolbar .project-list-count{margin-inline-start:.25rem;margin-inline-end:auto}
.project-progress-track{position:relative;display:block;block-size:.375rem;border-radius:999px;background:color-mix(in srgb,var(--ink) 9%,var(--surface));overflow:hidden}
.project-progress-fill{position:absolute;inset-block:0;inset-inline-start:0;display:block;border-radius:inherit;background:var(--hcm-color-success)}
.project-progress-text{color:var(--muted);font-size:.8125rem;font-variant-numeric:tabular-nums;line-height:1.2}
.project-progress-shimmer .project-progress-track{inline-size:70%;background:color-mix(in srgb,var(--ink) 6%,var(--surface))}
.project-list-row>.project-list-cell{align-self:center}
.project-list-status .project-page-status{vertical-align:middle}
.project-sort-select{position:relative}
.project-sort-select select:not(#project-none){appearance:none;-webkit-appearance:none;min-block-size:2.75rem;padding-inline:.75rem 2.25rem;background:var(--surface);color:var(--ink)}
.project-sort-select::after{content:"";position:absolute;inset-inline-end:.9375rem;inset-block-start:50%;inline-size:.4rem;block-size:.4rem;border-right:1.5px solid currentColor;border-bottom:1.5px solid currentColor;transform:translateY(-70%) rotate(45deg);color:var(--ink);pointer-events:none}
.project-sort-select select:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:1px}
@media(max-width:48rem){
.project-list-table .project-list-row{grid-template-columns:minmax(0,1fr) auto}
.project-list-row>.project-list-cell.project-list-status{grid-column:1}
.project-list-row>.project-list-cell:last-child:not(.project-list-title){display:block;grid-column:2;grid-row:1 / span 2;align-self:center}
.project-list-row>.project-list-cell:last-child .project-list-owner>span:last-child{display:none}
.project-sort-select select{min-block-size:2.75rem}
.project-list-name{display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;white-space:normal;overflow:hidden}
.project-search input{min-block-size:2.75rem}
.project-list-head{display:none}
.project-sort-select{display:inline-block}
.project-search{flex-basis:100%;max-inline-size:none}
.project-list-table .project-list-row{grid-template-columns:minmax(0,1fr);row-gap:.35rem;min-block-size:4rem}
.project-list-title{grid-column:1}
.project-list-row>.project-list-cell:not(.project-list-title){grid-column:1;display:none}
.project-list-row>.project-list-cell.project-list-status{display:block}
.project-list-row>.project-list-cell.project-list-progress-cell{display:block;max-inline-size:16rem}
.project-list-footer{justify-content:space-between}
.project-page-range{margin-inline-end:0}
.project-pager{inline-size:100%;justify-content:space-between}
.project-pager .button{min-block-size:44px}
}
`

func projectListCopyFor(locale LocaleContext) projectListCopy {
	number := func(n int) string { return locale.FormatNumber(strconv.Itoa(n), 0) }
	return projectListCopy{
		listLabel: locale.Text("projects.list_label"), searchLabel: locale.Text("projects.search_label"), searchPlaceholder: locale.Text("projects.search_placeholder"),
		clearSearch: locale.Text("projects.clear_search"), colProject: locale.Text("projects.col_project"), colStatus: locale.Text("projects.col_status"),
		colTasks: locale.Text("projects.col_progress"), colOwner: locale.Text("projects.owner"),
		shownAll: func(shown, total int, status string) string {
			key := "projects.count_all_status"
			if shown != total {
				key = "projects.count_all_status_of"
			}
			return locale.Text(key, map[string]string{"shown": locale.FormatNumber(strconv.Itoa(shown), 0), "total": locale.FormatNumber(strconv.Itoa(total), 0), "status": status})
		},
		progressLoading: locale.Text("projects.progress_loading"), progressEmpty: locale.Text("projects.progress_empty"), progressAllDone: locale.Text("projects.progress_all_done"),
		progressOpen: func(open int) string {
			return locale.Text("projects.progress_open", map[string]string{"count": locale.FormatNumber(strconv.Itoa(open), 0)})
		},
		progressLabel: func(done, total int) string {
			return locale.Text("projects.progress_label", map[string]string{"done": locale.FormatNumber(strconv.Itoa(done), 0), "total": locale.FormatNumber(strconv.Itoa(total), 0)})
		},
		sortLabel: locale.Text("projects.sort_label"), sortedAsc: locale.Text("projects.sorted_asc"), sortedDesc: locale.Text("projects.sorted_desc"),
		pagesLabel: locale.Text("projects.pages_label"), previous: locale.Text("projects.page_previous"), next: locale.Text("projects.page_next"), pageSize: locale.Text("projects.page_size"),
		noMatch: func(query string) string {
			return locale.Text("projects.no_match", map[string]string{"query": query})
		},
		shown: func(shown, total int) string {
			if shown == total && total == 1 {
				return locale.Text("projects.count_one")
			}
			if shown == total {
				return locale.Text("projects.count_all", map[string]string{"count": number(total)})
			}
			return locale.Text("projects.count_filtered", map[string]string{"shown": number(shown), "count": number(total)})
		},
		openNamed: func(name string) string {
			return locale.Text("projects.open_project_named", map[string]string{"name": name})
		},
		sortOption: func(column string, descending bool) string {
			key := "projects.sort_option_asc"
			if descending {
				key = "projects.sort_option_desc"
			}
			return locale.Text(key, map[string]string{"column": column})
		},
		pageOf: func(page, pages int) string {
			return locale.Text("projects.page_of", map[string]string{"page": number(page), "pages": number(pages)})
		},
		pageRange: func(from, to, total int) string {
			return locale.Text("projects.page_range", map[string]string{"from": number(from), "to": number(to), "count": number(total)})
		},
	}
}
