package journey

import (
	"strconv"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// This file is the workforce half of the list view: the People table, the
// New employee form, and the small vocabulary they share.
//
// The page is workforce-first because a promotion is proposed *for someone*.
// A reader arriving with an empty cell has to be able to create an employee,
// see them, pick them, and only then propose the change -- so People comes
// before the proposal form, and the proposal form's heading names whoever
// the People table currently has selected.
//
// Two rules from components.go carry over unchanged and are load-bearing
// here:
//
//   - Nothing means anything by color alone. The Source chip carries its own
//     word and its own glyph; the selected row carries aria-selected and a
//     spelled-out "Selected" marker beside the name, not just an accent
//     rule; WorkerCard.Tone tints a row's edge but never says anything the
//     row does not also say in text.
//   - The live path and the SSR path are one tree. A row's action is a
//     button when OnPropose is set and a plain link to ProposeHref when it
//     is not; the name is a toggle button when OnSelect is set and plain
//     text when it is not. Nothing here simulates a DOM.

// Worker provenance vocabulary. Anything outside this set reads as CORPUS,
// so a projection that invents a source degrades to "loaded with the cell"
// rather than to an unstyled chip.
const (
	sourceCorpus  = "CORPUS"
	sourceCreated = "CREATED"
	// The full workforce already has a dedicated, filterable product route.
	// Keeping this operational journey page to a representative window avoids
	// mounting hundreds of row controls before the user has chosen a worker.
	peoplePreviewLimit = 20
)

func sourceOf(source string) string {
	if source == sourceCreated {
		return sourceCreated
	}
	return sourceCorpus
}

// sourceWord is the chip's fallback label. A chip whose label the projection
// forgot would otherwise be a bare colored pill, which is exactly the thing
// this page does not do.
func sourceWord(source string) string {
	if sourceOf(source) == sourceCreated {
		return "Added here"
	}
	return "From corpus"
}

// ----------------------------------------------------------------------
// People panel
// ----------------------------------------------------------------------

// peopleSection is the panel the list view opens with. It is absent, not
// empty, when the projection carried no PeopleView: a cell that cannot read
// its workforce should show no workforce panel rather than an empty one.
func peopleSection(v *PeopleView) ui.Node {
	if v == nil {
		return nil
	}
	body := ui.Node(nil)
	if len(v.Workers) == 0 {
		body = peopleEmptyState(v.Empty)
	} else {
		body = peopleTable(*v)
	}
	return html.Section(html.Props{Class: "jn-panel", Aria: map[string]string{"labelledby": "people-heading"}},
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "people-heading"}, html.Text("People")),
			chip(toneNeutral, peopleCountLabel(len(v.Workers))),
		),
		htmlIf(v.Note != "", func() ui.Node {
			return html.P(html.Props{Class: "jn-people-note"},
				RenderIcon(IconInfo, "jn-people-note-icon", nil), html.Text(v.Note))
		}),
		body,
	)
}

func peopleCountLabel(n int) string {
	if n == 1 {
		return "1 employee"
	}
	return strconv.Itoa(n) + " employees"
}

func peopleEmptyState(message string) ui.Node {
	if message == "" {
		message = "No employees are available in this view yet."
	}
	return html.Div(html.Props{Class: "jn-empty jn-empty-inset"},
		RenderIcon(IconEmpty, "jn-empty-mark", nil),
		html.P(html.Props{Class: "jn-empty-title"}, html.Text("No employees yet")),
		html.P(html.Props{}, html.Text(message)),
	)
}

// peopleTable draws the workforce as a real table rather than as cards,
// because the reader's question here is comparative -- who is on what grade,
// in which org unit, at what pay -- and a card grid answers that badly.
//
// The scroll container is what keeps the page off a horizontal scrollbar at
// 320px: the table keeps its natural width and scrolls inside its own box.
// A scroll container that cannot be focused cannot be scrolled by keyboard
// at all, so it carries tabindex="0" and a name, which is also why it is a
// labelled region rather than a bare div.
func peopleTable(v PeopleView) ui.Node {
	workers := peoplePreviewWorkers(v)
	showDirectoryLink := len(workers) < len(v.Workers)
	directoryLink := v.DirectoryLink
	if directoryLink.Label == "" {
		directoryLink.Label = "Open the full People directory"
	}
	if directoryLink.Href == "" {
		directoryLink.Href = "/workspace/app/people"
	}
	return html.Div(html.Props{Class: "jn-tablewrap jn-peoplewrap", Role: "region",
		TabIndex: html.TabIndexZero, Aria: map[string]string{"label": "People"}},
		html.Table(html.Props{Class: "jn-table jn-zebra jn-people"},
			html.Caption(html.Props{Class: "jn-visually-hidden"},
				html.Text("Employees available to you, with their open promotion journeys")),
			html.Thead(html.Props{},
				html.Tr(html.Props{},
					peopleHead("Employee", false),
					peopleHead("Job code and grade", false),
					peopleHead("Org unit", false),
					peopleHead("Location", false),
					peopleHead("Base pay", true),
					peopleHead("Hired", false),
					peopleHead("Source", false),
					peopleHead("Journeys", true),
					html.Th(html.Props{Raw: map[string]any{"scope": "col"}},
						visuallyHidden("Row actions")),
				),
			),
			html.Tbody(html.Props{}, html.Map(workers, func(c WorkerCard) ui.Node {
				return workerRow(v, c)
			})...),
		),
		htmlIf(showDirectoryLink, func() ui.Node {
			return html.Div(html.Props{Class: "jn-people-preview-foot"},
				html.P(html.Props{}, html.Text("Showing "+strconv.Itoa(len(workers))+" of "+strconv.Itoa(len(v.Workers))+" employees.")),
				html.A(html.Props{Class: "jn-btn", Href: directoryLink.Href, OnClick: activate(directoryLink.OnNavigate), Data: map[string]string{"variant": "secondary", "size": "sm"}}, html.Text(directoryLink.Label)),
			)
		}),
	)
}

func peoplePreviewWorkers(v PeopleView) []WorkerCard {
	if len(v.Workers) <= peoplePreviewLimit {
		return v.Workers
	}
	workers := append([]WorkerCard(nil), v.Workers[:peoplePreviewLimit]...)
	if v.SelectedRef == "" {
		return workers
	}
	for _, worker := range workers {
		if worker.Ref == v.SelectedRef {
			return workers
		}
	}
	for _, worker := range v.Workers[peoplePreviewLimit:] {
		if worker.Ref == v.SelectedRef {
			return append(workers, worker)
		}
	}
	return workers
}

func peopleHead(label string, numeric bool) ui.Node {
	props := html.Props{Raw: map[string]any{"scope": "col"}}
	if numeric {
		props.Class = "jn-num"
	}
	return html.Th(props, html.Text(label))
}

// workerSelected reports whether this row is the one the proposal panel is
// about. Either half of the contract may say so -- the row's own flag or the
// view's SelectedRef -- so a projection that sets only one still marks
// exactly the row it meant.
func workerSelected(v PeopleView, c WorkerCard) bool {
	if c.Selected {
		return true
	}
	return c.Ref != "" && c.Ref == v.SelectedRef
}

// workerRow is one employee. The row carries aria-selected and data-selected
// (the styling hook) and, when the projection supplied one, data-tone; none
// of those is the only carrier of anything, because the selected row also
// says "Selected" in words beside the name and the source says its own.
func workerRow(v PeopleView, c WorkerCard) ui.Node {
	selected := workerSelected(v, c)
	data := map[string]string{"tone": toneOf(c.Tone)}
	if selected {
		data["selected"] = "true"
	}
	props := html.Props{
		Class: "jn-people-row",
		Role:  "row",
		Data:  data,
		Aria:  map[string]string{"selected": strconv.FormatBool(selected)},
	}
	return html.Tr(props,
		workerNameCell(c, selected),
		html.Td(html.Props{Class: "jn-people-job"}, html.Text(jobLine(c))),
		html.Td(html.Props{}, html.Text(c.OrgUnit)),
		html.Td(html.Props{}, html.Text(c.Location)),
		html.Td(html.Props{Class: "jn-num jn-people-pay"}, html.Text(c.PayLine)),
		html.Td(html.Props{Class: "jn-num"}, html.Text(c.HireDate)),
		html.Td(html.Props{}, sourceChip(c)),
		html.Td(html.Props{Class: "jn-num jn-people-journeys"},
			html.Span(html.Props{Class: "jn-people-count"}, html.Text(strconv.Itoa(c.OpenJourneys))),
			visuallyHidden(" "+journeyWord(c.OpenJourneys)+" open"),
		),
		workerActionCell(c),
	)
}

// jobLine joins the two halves of a placement the way the rest of the page
// writes it, and degrades to whichever half exists rather than rendering a
// dangling separator.
func jobLine(c WorkerCard) string {
	switch {
	case c.JobCode != "" && c.Grade != "":
		return c.JobCode + " · " + c.Grade
	case c.JobCode != "":
		return c.JobCode
	default:
		return c.Grade
	}
}

func journeyWord(n int) string {
	if n == 1 {
		return "journey"
	}
	return "journeys"
}

// workerNameCell is the row header. It is a toggle button on the live path
// and plain text on the SSR path, which is the same rule every other
// interaction on this page follows: no client, no simulated control.
//
// The row is reachable from the keyboard through the controls inside its
// cells rather than through a tabindex on the <tr>. That is the accessible
// half of the choice the brief offers: a focusable row containing a
// focusable button is two tab stops for one thing, and a row that is
// focusable but does nothing when activated is worse than one that is not.
func workerNameCell(c WorkerCard, selected bool) ui.Node {
	body := []ui.Node{
		html.Span(html.Props{Class: "jn-people-name"}, html.Text(c.Name)),
		htmlIf(c.Title != "", func() ui.Node {
			return html.Span(html.Props{Class: "jn-people-title"}, html.Text(c.Title))
		}),
		htmlIf(c.Number != "", func() ui.Node {
			return html.Span(html.Props{Class: "jn-people-number jn-mono"}, html.Text(c.Number))
		}),
	}
	if selected {
		body = append(body, html.Span(html.Props{Class: "jn-people-selected"},
			RenderIcon(IconCheck, "jn-people-selected-icon", nil), html.Text("Selected")))
	}

	cell := html.Props{Class: "jn-people-idcell", Raw: map[string]any{"scope": "row"}}
	if c.OnSelect == nil {
		return html.Th(cell, html.Div(html.Props{Class: "jn-people-idbox"}, body...))
	}
	return html.Th(cell,
		html.Button(html.Props{Class: "jn-people-idbox jn-people-pick", Type: "button",
			Aria:    map[string]string{"pressed": strconv.FormatBool(selected)},
			OnClick: activate(c.OnSelect)},
			append(body, visuallyHidden(" — select this employee"))...),
	)
}

// workerActionCell is the row's one governed affordance. Live clients get a
// button that calls back; everyone else gets a real link to ProposeHref, so
// the whole flow -- see the workforce, pick a person, propose -- works with
// scripting off. A row with neither is simply not actionable, which is what
// a zero-valued card is.
func workerActionCell(c WorkerCard) ui.Node {
	label := func() []ui.Node {
		return []ui.Node{html.Text("Propose promotion"), visuallyHidden(" for " + c.Name)}
	}
	style := map[string]string{"variant": "primary", "size": "sm"}
	switch {
	case c.OnPropose != nil:
		return html.Td(html.Props{Class: "jn-people-action"},
			html.Button(html.Props{Class: "jn-btn", Type: "button", Data: style,
				OnClick: activate(c.OnPropose)}, label()...))
	case c.ProposeHref != "":
		return html.Td(html.Props{Class: "jn-people-action"},
			html.A(html.Props{Class: "jn-btn", Href: c.ProposeHref, Data: style}, label()...))
	default:
		return html.Td(html.Props{Class: "jn-people-action"},
			html.Span(html.Props{Class: "jn-muted"}, html.Text("—"),
				visuallyHidden(" no action available")))
	}
}

// sourceChip says where an employee's record came from. CREATED carries a
// spark so the two sources differ by glyph as well as by tint, and both
// carry their own word, so the column reads the same in monochrome.
func sourceChip(c WorkerCard) ui.Node {
	source := sourceOf(c.Source)
	label := c.SourceLabel
	if label == "" {
		label = sourceWord(source)
	}
	if source == sourceCreated {
		return html.Span(html.Props{Class: "jn-chip jn-source",
			Data: map[string]string{"tone": toneInfo, "source": sourceCreated}},
			RenderIcon(IconSpark, "jn-chip-icon", nil), html.Text(label))
	}
	return html.Span(html.Props{Class: "jn-chip jn-source",
		Data: map[string]string{"tone": toneNeutral, "source": sourceCorpus}},
		html.Span(html.Props{Class: "jn-chip-dot", Aria: map[string]string{"hidden": "true"}}),
		html.Text(label),
	)
}

// ----------------------------------------------------------------------
// New employee form
// ----------------------------------------------------------------------

// newEmployeeSection is the panel that turns "there is nobody here" into
// "there is somebody here". It is the same two-column field grid the
// proposal form uses, because they are the same kind of thing: a governed
// write expressed as a plain POST that a live client may intercept.
func newEmployeeSection(l live, f WorkerForm) ui.Node {
	fields := make([]ui.Node, 0, len(f.Fields))
	for _, field := range f.Fields {
		fields = append(fields, fieldNode(l, field, f.Disabled))
	}
	submit := f.Submit
	if submit == "" {
		submit = "Add employee"
	}

	btn := html.Props{Class: "jn-btn", Type: "submit",
		DataAttr: html.DataAttribute{Name: "variant", Value: "primary"}}
	foot := []ui.Node{}
	if f.Disabled {
		btn.Disabled = true
		btn.Aria = map[string]string{"describedby": "worker-form-disabled"}
		foot = append(foot, html.Button(btn, html.Text(submit)),
			html.P(html.Props{ID: "worker-form-disabled", Class: "jn-blocked"},
				RenderIcon(IconWarning, "jn-blocked-icon", nil), html.Text(f.DisabledReason)))
	} else {
		foot = append(foot, html.Button(btn, html.Text(submit)),
			html.P(html.Props{Class: "jn-help"},
				html.Text("The employee is added to the directory and can be promoted from the table above.")))
	}

	return html.Section(html.Props{Class: "jn-panel", Aria: map[string]string{"labelledby": "new-employee-heading"}},
		html.Div(html.Props{Class: "jn-sectionhead"},
			html.H2(html.Props{ID: "new-employee-heading"}, html.Text("New employee")),
		),
		html.Form(formProps(l, f.Action, f.OnSubmit, f.Hidden, f.Fields),
			html.Fragment(hiddenInputs(f.Hidden)...),
			html.Div(html.Props{Class: "jn-fieldgrid"}, fields...),
			html.Div(html.Props{Class: "jn-formfoot"}, foot...),
		),
	)
}

// selectedWorkerName is the name the proposal panel puts in its heading. An
// empty answer -- no People view, no selection, or a ref no row carries --
// leaves the heading as the plain "Propose a promotion", so the page never
// claims a selection it does not have.
func selectedWorkerName(v *PeopleView) string {
	if v == nil || v.SelectedRef == "" {
		return ""
	}
	for _, c := range v.Workers {
		if c.Ref == v.SelectedRef {
			return c.Name
		}
	}
	return ""
}
