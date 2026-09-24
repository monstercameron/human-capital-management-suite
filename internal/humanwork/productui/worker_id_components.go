package productui

import (
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/workerids"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var workerIDValidationFieldIDs = []string{
	"worker-prefix", "worker-suffix", "worker-digits", "worker-start",
	"worker-increment", "worker-excluded",
}

type WorkerIDPageProps struct {
	I18nProps
	Policy     WorkerIDPolicy
	Validation ValidationState
	Back       ActionLinkProps
	Editable   bool
	OnSave     func(WorkerIDPolicy)
}

// AdminFormSection is the shared semantic boundary for long configuration
// forms. A fieldset/legend pair keeps related decisions together for screen
// readers while allowing each page to supply its own controls.
type AdminFormSectionProps struct {
	ID     string
	Title  string
	Fields []ui.Node
}

func AdminFormSection(props AdminFormSectionProps) ui.Node {
	return html.Fieldset(html.Props{Class: "admin-form-section", DataAttr: html.DataAttribute{Name: "field-group", Value: props.ID}},
		html.Legend(html.Props{}, ui.Text(props.Title)),
		html.Div(html.Props{Class: "admin-form-section-fields"}, props.Fields...),
	)
}

type workerIDDraft struct {
	Source  WorkerIDPolicy
	Policy  WorkerIDPolicy
	At      time.Time
	Numbers [3]string
}

func workerIDPolicyChanged(source, draft WorkerIDPolicy) bool {
	return source.Prefix != draft.Prefix || source.Suffix != draft.Suffix || source.Separator != draft.Separator ||
		source.SequenceDigits != draft.SequenceDigits || source.StartAt != draft.StartAt || source.IncrementBy != draft.IncrementBy ||
		source.ZeroPad != draft.ZeroPad || source.YearFormat != draft.YearFormat || source.IncludeUnitCode != draft.IncludeUnitCode ||
		source.CheckDigit != draft.CheckDigit || source.ExcludedRanges != draft.ExcludedRanges
}

func workerIDChangedFields(i18n I18nProps, source, draft WorkerIDPolicy) []string {
	fields := make([]string, 0, 4)
	if source.Prefix != draft.Prefix || source.Suffix != draft.Suffix || source.Separator != draft.Separator {
		fields = append(fields, i18n.Text("worker_ids.change_identity"))
	}
	if source.SequenceDigits != draft.SequenceDigits || source.StartAt != draft.StartAt || source.IncrementBy != draft.IncrementBy {
		fields = append(fields, i18n.Text("worker_ids.change_sequence"))
	}
	if source.ZeroPad != draft.ZeroPad || source.YearFormat != draft.YearFormat || source.IncludeUnitCode != draft.IncludeUnitCode || source.CheckDigit != draft.CheckDigit {
		fields = append(fields, i18n.Text("worker_ids.change_format"))
	}
	if source.ExcludedRanges != draft.ExcludedRanges {
		fields = append(fields, i18n.Text("worker_ids.change_reserved"))
	}
	return fields
}

func newWorkerIDDraft(p WorkerIDPolicy, at time.Time) workerIDDraft {
	return workerIDDraft{Source: p, Policy: p, At: at, Numbers: [3]string{
		strconv.Itoa(p.SequenceDigits), strconv.FormatInt(p.StartAt, 10), strconv.FormatInt(p.IncrementBy, 10),
	}}
}

// Keep incomplete input as text, never substitute the last valid number.
func workerIDNumericDraft(raw string) int64 {
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return -1
	}
	return v
}

func workerIDNumbersValid(p WorkerIDPolicy) bool {
	return p.SequenceDigits >= 1 && p.SequenceDigits <= 12 && p.IncrementBy >= 1 && p.IncrementBy <= 1000000 && p.StartAt >= 0 && p.StartAt <= 999999999999
}

func workerIDDraftExamples(p WorkerIDPolicy, at time.Time) ([]string, error) {
	if !workerIDNumbersValid(p) {
		return nil, workerids.ErrInvalid
	}
	next := p.NextSequence
	if p.Version == 0 {
		next = p.StartAt
	}
	return workerids.Preview(workerids.Policy{Prefix: p.Prefix, Suffix: p.Suffix, Separator: p.Separator,
		SequenceDigits: p.SequenceDigits, StartAt: p.StartAt, NextSequence: next, IncrementBy: p.IncrementBy,
		ZeroPad: p.ZeroPad, YearFormat: p.YearFormat, IncludeUnitCode: p.IncludeUnitCode,
		CheckDigit: p.CheckDigit, ExcludedRanges: p.ExcludedRanges}, workerids.FormatContext{At: at, UnitCode: "CARE"})
}

func WorkerIDPage(props WorkerIDPageProps) ui.Node {
	state := ui.UseState(newWorkerIDDraft(props.Policy, time.Now().UTC()))
	current := state.Get()
	if !reflect.DeepEqual(current.Source, props.Policy) {
		current = newWorkerIDDraft(props.Policy, current.At)
		state.Set(current)
	}
	draft := current.Policy
	unsaved := workerIDPolicyChanged(current.Source, draft)
	refresh := func() { current.Policy = draft; state.Set(current) }
	previewPolicy := draft
	previewPolicy.Previews, _ = workerIDDraftExamples(draft, current.At)
	statusText := props.Text("worker_ids.status")
	if !workerIDNumbersValid(draft) {
		statusText = props.Text("worker_ids.numeric_required")
	}
	errors := props.Validation.Errors()
	summaryRef := ui.UseDOMRef()
	ui.UseAutoFocus(summaryRef, props.Validation.SubmissionAttempted && len(errors) > 0)
	changedFields := workerIDChangedFields(props.I18nProps, current.Source, draft)
	return html.Div(html.Props{Class: "worker-id-page", DataAttr: html.DataAttribute{Name: "unsaved", Value: map[bool]string{true: "true", false: "false"}[unsaved]}, Raw: map[string]any{"data-unsaved-protection": "true"}},
		html.Section(html.Props{Class: "surface worker-id-intro"},
			html.Div(html.Props{}, html.Span(html.Props{Class: "eyebrow"}, ui.Text(props.Text("worker_ids.eyebrow"))), html.H2(html.Props{}, ui.Text(props.Text("worker_ids.heading"))), html.P(html.Props{Class: "muted"}, ui.Text(props.Text("worker_ids.description")))),
			ui.CreateElement(ActionLink, props.Back),
		),
		html.Form(html.Props{Class: "worker-id-layout", OnSubmit: saveWorkerIDPolicy(func(p WorkerIDPolicy) {
			if workerIDNumbersValid(p) && props.OnSave != nil {
				props.OnSave(p)
			}
		}, &draft), Raw: map[string]any{"data-unsaved-form": "worker-id", "data-unsaved-message": props.Text("worker_ids.unsaved")}},
			ui.CreateElement(ValidationSummary, ValidationSummaryProps{I18nProps: props.I18nProps, ID: "worker-id-validation-summary", Issues: errors, FieldIDs: workerIDValidationFieldIDs, Ref: summaryRef}),
			html.Fieldset(html.Props{Class: "worker-id-edit-boundary", Disabled: !props.Editable},
				html.Section(html.Props{Class: "surface worker-id-rules"},
					ui.CreateElement(SectionHeading, SectionHeadingProps{Title: props.Text("worker_ids.format_title"), Description: props.Text("worker_ids.format_help"), ShowDescription: true}),
					html.Div(html.Props{Class: "worker-id-fields"},
						AdminFormSection(AdminFormSectionProps{ID: "worker-id-identity", Title: props.Text("worker_ids.prefix"), Fields: []ui.Node{
							workerIDTextField(props.I18nProps, "worker-prefix", props.Text("worker_ids.prefix"), props.Text("worker_ids.prefix_help"), draft.Prefix, 12, props.Validation.ForField("worker-prefix"), func(v string) { draft.Prefix = v; refresh() }),
							workerIDTextField(props.I18nProps, "worker-suffix", props.Text("worker_ids.suffix"), props.Text("worker_ids.suffix_help"), draft.Suffix, 12, props.Validation.ForField("worker-suffix"), func(v string) { draft.Suffix = v; refresh() }),
							workerIDSelect("worker-separator", props.Text("worker_ids.separator"), draft.Separator, workerIDSeparatorOptions(props.Locale), func(v string) { draft.Separator = v; refresh() }),
						}}),
						AdminFormSection(AdminFormSectionProps{ID: "worker-id-sequence", Title: props.Text("worker_ids.digits"), Fields: []ui.Node{
							workerIDNumberField(props.I18nProps, "worker-digits", props.Text("worker_ids.digits"), props.Text("worker_ids.digits_help"), current.Numbers[0], 1, 12, props.Validation.ForField("worker-digits"), func(v string) { current.Numbers[0] = v; draft.SequenceDigits = int(workerIDNumericDraft(v)); refresh() }),
							workerIDNumberField(props.I18nProps, "worker-start", props.Text("worker_ids.start"), props.Text("worker_ids.start_help"), current.Numbers[1], 0, 999999999999, props.Validation.ForField("worker-start"), func(v string) { current.Numbers[1] = v; draft.StartAt = workerIDNumericDraft(v); refresh() }),
							workerIDNumberField(props.I18nProps, "worker-increment", props.Text("worker_ids.increment"), props.Text("worker_ids.increment_help"), current.Numbers[2], 1, 1000000, props.Validation.ForField("worker-increment"), func(v string) { current.Numbers[2] = v; draft.IncrementBy = workerIDNumericDraft(v); refresh() }),
						}}),
						AdminFormSection(AdminFormSectionProps{ID: "worker-id-format", Title: props.Text("worker_ids.preview"), Fields: []ui.Node{
							workerIDSelect("worker-padding", props.Text("worker_ids.padding"), strconv.FormatBool(draft.ZeroPad), workerIDOptions(props.Locale, "padding"), func(v string) { draft.ZeroPad = v == "true"; refresh() }),
							workerIDSelect("worker-year", props.Text("worker_ids.year"), draft.YearFormat, workerIDOptions(props.Locale, "year"), func(v string) { draft.YearFormat = v; refresh() }),
							workerIDSelect("worker-unit", props.Text("worker_ids.unit"), strconv.FormatBool(draft.IncludeUnitCode), workerIDOptions(props.Locale, "unit"), func(v string) { draft.IncludeUnitCode = v == "true"; refresh() }),
							workerIDSelect("worker-check", props.Text("worker_ids.check"), draft.CheckDigit, workerIDOptions(props.Locale, "check"), func(v string) { draft.CheckDigit = v; refresh() }),
						}}),
						AdminFormSection(AdminFormSectionProps{ID: "worker-id-reserved", Title: props.Text("worker_ids.excluded"), Fields: []ui.Node{
							workerIDTextField(props.I18nProps, "worker-excluded", props.Text("worker_ids.excluded"), props.Text("worker_ids.excluded_help"), draft.ExcludedRanges, 160, props.Validation.ForField("worker-excluded"), func(v string) { draft.ExcludedRanges = v; refresh() }),
						}}),
					),
				),
				html.Div(html.Props{Class: "worker-id-actions sticky-actions", DataAttr: html.DataAttribute{Name: "unsaved", Value: map[bool]string{true: "true", false: "false"}[unsaved]}}, html.Button(html.Props{Class: "button primary", Type: "submit", Disabled: !props.Editable || !unsaved || !workerIDNumbersValid(draft), Raw: map[string]any{"aria-describedby": "worker-id-status"}}, ui.Text(props.Text("worker_ids.save"))), html.P(html.Props{ID: "worker-id-status", Class: "muted", Raw: map[string]any{"role": "status", "aria-live": "polite"}}, ui.Text(statusText)), workerIDUnsavedNotice(props.I18nProps, unsaved)),
			),
			ui.CreateElement(WorkerIDPreview, WorkerIDPreviewProps{I18nProps: props.I18nProps, Policy: previewPolicy, Source: current.Source, ChangedFields: changedFields, Unsaved: unsaved}),
		),
	)
}

type WorkerIDPreviewProps struct {
	I18nProps
	Policy        WorkerIDPolicy
	Source        WorkerIDPolicy
	ChangedFields []string
	Unsaved       bool
}

func WorkerIDPreview(props WorkerIDPreviewProps) ui.Node {
	examples := make([]ui.Node, 0, len(props.Policy.Previews))
	for _, value := range props.Policy.Previews {
		examples = append(examples, html.Li(html.Props{}, html.Code(html.Props{}, ui.Text(value))))
	}
	if len(examples) == 0 {
		examples = append(examples, html.Li(html.Props{}, ui.Text(props.Text("worker_ids.preview_unavailable"))))
	}
	return html.Aside(html.Props{Class: "surface worker-id-preview", DataAttr: html.DataAttribute{Name: "unsaved", Value: map[bool]string{true: "true", false: "false"}[props.Unsaved]}},
		html.H2(html.Props{}, ui.Text(props.Text("worker_ids.preview"))),
		html.P(html.Props{Class: "muted"}, ui.Text(props.Text("worker_ids.preview_help"))),
		html.Ul(html.Props{Class: "worker-id-examples"}, examples...),
		workerIDChangeSummary(props),
		html.Tag("dl", html.Props{Class: "worker-id-state"},
			html.Div(html.Props{}, html.Tag("dt", html.Props{}, ui.Text(props.Text("worker_ids.next"))), html.Tag("dd", html.Props{}, ui.Text(strconv.FormatInt(props.Policy.NextSequence, 10)))),
			html.Div(html.Props{}, html.Tag("dt", html.Props{}, ui.Text(props.Text("worker_ids.issued"))), html.Tag("dd", html.Props{}, ui.Text(strconv.FormatInt(props.Policy.IssuedCount, 10)))),
		),
		html.P(html.Props{Class: "callout"}, ui.Text(props.Text("worker_ids.non_reuse"))),
	)
}

func workerIDUnsavedNotice(i18n I18nProps, unsaved bool) ui.Node {
	if !unsaved {
		return html.Span(html.Props{Class: "muted", DataAttr: html.DataAttribute{Name: "save-state", Value: "saved"}}, ui.Text(i18n.Text("worker_ids.saved")))
	}
	return html.Span(html.Props{Class: "status warning", DataAttr: html.DataAttribute{Name: "save-state", Value: "unsaved"}, Raw: map[string]any{"role": "status"}}, ui.Text(i18n.Text("worker_ids.unsaved")))
}

func workerIDChangeSummary(props WorkerIDPreviewProps) ui.Node {
	if len(props.ChangedFields) == 0 {
		return html.P(html.Props{Class: "worker-id-change-summary", DataAttr: html.DataAttribute{Name: "diff-state", Value: "unchanged"}}, ui.Text(props.Text("worker_ids.no_unsaved")))
	}
	items := make([]ui.Node, 0, len(props.ChangedFields))
	for _, field := range props.ChangedFields {
		items = append(items, html.Li(html.Props{}, ui.Text(field)))
	}
	return html.Div(html.Props{Class: "worker-id-change-summary", DataAttr: html.DataAttribute{Name: "diff-state", Value: "changed"}}, html.Strong(html.Props{}, ui.Text(props.Text("worker_ids.changes_to_apply"))), html.Ul(html.Props{}, items...))
}

type workerIDOption struct{ Value, Label string }

func workerIDTextField(i18n I18nProps, id, label, help, value string, max int, issue *ValidationIssue, update func(string)) ui.Node {
	return ValidationInput(ValidationInputProps{I18nProps: i18n, ID: id, Type: "text", Label: label, Help: help, Value: value, MaxLength: max, Issue: issue, OnInput: ui.UseEvent(func(event ui.InputEvent) { update(event.GetValue()) })})
}

func workerIDNumberField(i18n I18nProps, id, label, help, value string, min, max int64, issue *ValidationIssue, update func(string)) ui.Node {
	return ValidationInput(ValidationInputProps{I18nProps: i18n, ID: id, Type: "number", Label: label, Help: help, Value: value, Min: strconv.FormatInt(min, 10), Max: strconv.FormatInt(max, 10), Required: true, Issue: issue, OnInput: ui.UseEvent(func(event ui.InputEvent) {
		update(event.GetValue())
	})})
}

func workerIDSelect(id, label, selected string, values []workerIDOption, update func(string)) ui.Node {
	options := make([]ui.Node, 0, len(values))
	for _, value := range values {
		options = append(options, html.Option(html.Props{Value: value.Value, Selected: value.Value == selected}, ui.Text(value.Label)))
	}
	p := html.Props{ID: id}
	p.OnChange = ui.UseEvent(func(event ui.InputEvent) { update(event.GetValue()) })
	return ui.CreateElement(LabeledControl, LabeledControlProps{For: id, Label: label, Control: html.Select(p, options...)})
}

func workerIDSeparatorOptions(locale LocaleContext) []workerIDOption {
	if strings.HasPrefix(locale.Resolved, "de") {
		return []workerIDOption{{"-", "Bindestrich ( - )"}, {"/", "Schrägstrich ( / )"}, {".", "Punkt ( . )"}, {"", "Kein Trennzeichen"}}
	}
	if strings.HasPrefix(locale.Resolved, "ar") {
		return []workerIDOption{{"-", "شرطة ( - )"}, {"/", "شرطة مائلة ( / )"}, {".", "نقطة ( . )"}, {"", "بدون فاصل"}}
	}
	return []workerIDOption{{"-", "Dash ( - )"}, {"/", "Slash ( / )"}, {".", "Dot ( . )"}, {"", "No separator"}}
}

func workerIDOptions(locale LocaleContext, kind string) []workerIDOption {
	de := strings.HasPrefix(locale.Resolved, "de")
	ar := strings.HasPrefix(locale.Resolved, "ar")
	switch kind {
	case "padding":
		if de {
			return []workerIDOption{{"true", "Mit führenden Nullen"}, {"false", "Natürliche Breite"}}
		}
		if ar {
			return []workerIDOption{{"true", "ملء بأصفار بادئة"}, {"false", "عرض الرقم الطبيعي"}}
		}
		return []workerIDOption{{"true", "Pad with leading zeroes"}, {"false", "Natural number width"}}
	case "year":
		if de {
			return []workerIDOption{{"NONE", "Kein Jahr"}, {"YY", "Zweistelliges Jahr"}, {"YYYY", "Vierstelliges Jahr"}}
		}
		if ar {
			return []workerIDOption{{"NONE", "بدون سنة"}, {"YY", "سنة من رقمين"}, {"YYYY", "سنة من أربعة أرقام"}}
		}
		return []workerIDOption{{"NONE", "Do not include year"}, {"YY", "Two-digit year"}, {"YYYY", "Four-digit year"}}
	case "unit":
		if de {
			return []workerIDOption{{"false", "Keine Einheit"}, {"true", "Organisationseinheit einschließen"}}
		}
		if ar {
			return []workerIDOption{{"false", "بدون وحدة"}, {"true", "تضمين رمز الوحدة التنظيمية"}}
		}
		return []workerIDOption{{"false", "Do not include unit"}, {"true", "Include organization-unit code"}}
	default:
		if de {
			return []workerIDOption{{"NONE", "Keine Prüfziffer"}, {"LUHN_MOD10", "Luhn-Mod-10-Prüfziffer"}}
		}
		if ar {
			return []workerIDOption{{"NONE", "بدون رقم تحقق"}, {"LUHN_MOD10", "رقم تحقق Luhn mod-10"}}
		}
		return []workerIDOption{{"NONE", "No check digit"}, {"LUHN_MOD10", "Luhn mod-10 check digit"}}
	}
}

func saveWorkerIDPolicy(save func(WorkerIDPolicy), draft *WorkerIDPolicy) ui.Handler {
	if save == nil {
		return ui.Handler{}
	}
	return ui.UseEvent(func(event ui.FormEvent) { event.PreventDefault(); save(*draft) })
}
