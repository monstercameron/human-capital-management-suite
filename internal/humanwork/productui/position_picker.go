package productui

// PROMOUX-004 GREEN clause one: "an accessible server-filtered picker shows
// only authorized compatible positions with title, organization, manager,
// location, vacancy window and reservation state." This file is the one
// seam that turns internal/domains/promotion/positionpicker.Candidate --
// the domain's own already-filtered, already-authorized candidate list --
// into presentation. It performs no filtering, no authorization and no
// capacity computation of its own; every candidate rendered here is
// whatever positionpicker.ResolveCandidates already decided belongs in the
// list. This mirrors the pattern promotion_availability.go and
// approval_disposition.go already established: a domain function decides,
// and the render path only localizes.
//
// REFACTOR: "the browser carries only the selected position revision
// reference." PositionPickerOptionProps.Reference is exactly that
// reference's wire text (position.RevisionRef.String()) -- never the raw
// position id -- and it is the radio input's own value attribute, so a
// selection round-trips as that one opaque token.

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/positionpicker"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PositionPickerOptionProps is one presentation-ready candidate: every field
// is already localized text, so PositionPicker never resolves locale copy
// or recomputes availability itself.
type PositionPickerOptionProps struct {
	// Reference is the opaque position+revision token this option's radio
	// input carries as its value -- the one thing REFACTOR lets the browser
	// carry back on submission.
	Reference string
	// Code is the authorized role/position discriminator supplied with the
	// vacancy projection. It is rendered with the title so two otherwise
	// similar choices do not collapse into the same accessible label.
	Code             string
	Title            string
	Organization     string
	Manager          string
	Location         string
	VacancyWindow    string
	ReservationState string
}

// PositionPickerProps is the whole picker: a labeled group of options, the
// current selection (if any), and what to show when no candidate is
// authorized and compatible.
type PositionPickerProps struct {
	I18nProps
	Name        string
	Legend      string
	Options     []PositionPickerOptionProps
	Selected    string
	EmptyTitle  string
	EmptyDetail string
	OnSelect    func(string)
}

// PositionPickerOptionPropsFrom adapts one already-resolved
// positionpicker.Candidate into presentation. It is a pure field copy with
// locale formatting only: there is no branch here that could disagree with
// the domain's own filtered candidate.
func PositionPickerOptionPropsFrom(locale LocaleContext, c positionpicker.Candidate) PositionPickerOptionProps {
	vacancy := locale.Text("position_picker.open_now")
	if c.HasVacancyEnd {
		vacancy = locale.Text("position_picker.open_until", map[string]string{"date": locale.FormatDate(localDateToTime(c.VacancyEnd))})
	}
	return PositionPickerOptionProps{
		Reference: c.Reference.String(), Title: c.Title, Organization: c.Organization,
		Manager: c.Manager, Location: c.Location,
		VacancyWindow:    vacancy,
		ReservationState: positionPickerReservationStateLabel(locale, c.ReservationState),
	}
}

// positionPickerReservationStateLabel maps the candidate's reservation
// state to localized copy. The mapping is exhaustive with no permissive
// default: positionpicker.ReservationAvailable is the only state the domain
// declares today, and any other value -- including one a future domain
// change introduces before this file's own review -- falls back to the
// same non-revealing "unavailable" copy rather than rendering the raw
// internal token or claiming availability it cannot back up.
func positionPickerReservationStateLabel(locale LocaleContext, state string) string {
	switch state {
	case positionpicker.ReservationAvailable:
		return locale.Text("position_picker.reservation_available")
	default:
		return locale.Text("position_picker.reservation_unavailable")
	}
}

func localDateToTime(d values.LocalDate) time.Time {
	if !d.IsSet() {
		return time.Time{}
	}
	return time.Date(int(d.Year()), d.Month(), int(d.Day()), 0, 0, 0, 0, time.UTC)
}

// PositionPicker renders PROMOUX-004's accessible position picker: a native
// fieldset/legend/radio group, which carries the required grouping and
// labeling semantics without a custom ARIA widget role. Each option's
// vacancy window and reservation state are associated with its radio input
// through aria-describedby, so assistive technology announces them as part
// of choosing that option, not as disconnected trailing text. An empty
// candidate list renders the same fieldset with an explanatory empty state
// instead of silently disappearing, so a viewer who is authorized to
// promote but has no compatible vacant position still finds a labeled
// region explaining why.
func PositionPicker(props PositionPickerProps) ui.Node {
	if len(props.Options) == 0 {
		return html.Fieldset(html.Props{Class: "position-picker position-picker-empty"},
			html.Legend(html.Props{}, ui.Text(props.Legend)),
			html.P(html.Props{Class: "position-picker-empty-title"}, ui.Text(props.EmptyTitle)),
			html.Small(html.Props{Class: "position-picker-empty-detail"}, ui.Text(props.EmptyDetail)),
		)
	}
	choices := make([]ui.Node, 0, len(props.Options))
	for _, opt := range props.Options {
		opt := opt
		descID := "position-picker-desc-" + opt.Reference
		input := html.Props{
			Type: "radio", Name: props.Name, Value: opt.Reference,
			Checked: opt.Reference != "" && opt.Reference == props.Selected,
			Raw:     map[string]any{"aria-describedby": descID},
		}
		if props.OnSelect != nil {
			selected := opt.Reference
			input.OnChange = ui.UseEvent(func(ui.InputEvent) { props.OnSelect(selected) })
		}
		choices = append(choices, html.Li(html.Props{Class: "position-picker-option"},
			html.Label(html.Props{},
				html.Input(input),
				html.Span(html.Props{Class: "position-picker-option-main"},
					html.Strong(html.Props{}, ui.Text(opt.Title)),
					html.Small(html.Props{Class: "position-picker-option-code"}, ui.Text(positionPickerCode(opt))),
					html.Small(html.Props{}, ui.Text(opt.Organization)),
					html.Small(html.Props{}, ui.Text(opt.Manager)),
					html.Small(html.Props{}, ui.Text(opt.Location)),
				),
			),
			html.Span(html.Props{ID: descID, Class: "position-picker-option-meta"},
				ui.Text(opt.VacancyWindow), ui.Text(" — "), ui.Text(opt.ReservationState),
			),
		))
	}
	return html.Fieldset(html.Props{Class: "position-picker"},
		html.Legend(html.Props{}, ui.Text(props.Legend)),
		html.Ul(html.Props{Class: "position-picker-options", Raw: map[string]any{"role": "list"}}, choices...),
	)
}

func positionPickerCode(opt PositionPickerOptionProps) string {
	if code := strings.TrimSpace(opt.Code); code != "" {
		return code
	}
	if reference := strings.TrimSpace(opt.Reference); reference != "" {
		return reference
	}
	return ""
}
