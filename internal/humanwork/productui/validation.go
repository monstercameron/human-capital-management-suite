package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ValidationSeverity is a presentation severity. The domain/workflow service
// remains authoritative for whether a value is valid; the UI only presents
// the already-authorized finding.
type ValidationSeverity string

const (
	ValidationSeverityError   ValidationSeverity = "error"
	ValidationSeverityWarning ValidationSeverity = "warning"
)

// ValidationIssue is the display-safe projection of one server or local
// constraint finding. MessageKey is preferred because it keeps copy localized;
// Message is an already-safe fallback supplied by the owning service.
type ValidationIssue struct {
	_          incomparable
	FieldID    string
	Code       string
	MessageKey string
	Message    string
	Severity   ValidationSeverity
}

// ValidationState is intentionally presentation-only. It carries no
// authority, policy, or mutation semantics and can be populated only after
// the owning service has filtered the result for the current principal.
type ValidationState struct {
	Issues              []ValidationIssue
	SubmissionAttempted bool
}

func isValidationError(issue ValidationIssue) bool {
	return issue.Severity == "" || issue.Severity == ValidationSeverityError
}

func (state ValidationState) Errors() []ValidationIssue {
	if len(state.Issues) == 0 {
		return nil
	}
	result := make([]ValidationIssue, 0, len(state.Issues))
	for _, issue := range state.Issues {
		if isValidationError(issue) {
			result = append(result, issue)
		}
	}
	return result
}

func (state ValidationState) ForField(fieldID string) *ValidationIssue {
	fieldID = strings.TrimSpace(fieldID)
	for index := range state.Issues {
		issue := &state.Issues[index]
		if strings.TrimSpace(issue.FieldID) == fieldID && isValidationError(*issue) {
			return issue
		}
	}
	return nil
}

// ValidationInputProps describes the stable, native control boundary used by
// production forms. The component emits all field/error relationships in one
// place so pages cannot accidentally drift on aria-invalid or descriptions.
type ValidationInputProps struct {
	I18nProps
	ID        string
	Name      string
	Type      string
	Label     string
	Help      string
	Value     string
	Pattern   string
	Max       string
	Min       string
	MaxLength int
	Required  bool
	ReadOnly  bool
	Disabled  bool
	Issue     *ValidationIssue
	OnInput   ui.Handler
}

// ValidationInput renders an input with a deterministic accessible name,
// description, invalid state, and localized error text. Empty help/error
// content is omitted so aria references never point at inert nodes.
func ValidationInput(props ValidationInputProps) ui.Node {
	id := strings.TrimSpace(props.ID)
	if id == "" {
		panic("productui: ValidationInput requires a stable ID")
	}
	errorID := id + "-error"
	helpID := id + "-help"
	input := html.Props{
		ID: id, Name: props.Name, Type: props.Type, Value: props.Value,
		Pattern: props.Pattern, Min: props.Min, Max: props.Max,
		MaxLength: props.MaxLength, Required: props.Required,
		ReadOnly: props.ReadOnly, Disabled: props.Disabled, OnInput: props.OnInput,
		Aria: map[string]string{},
	}
	if strings.TrimSpace(props.Help) != "" {
		input.Aria["describedby"] = helpID
	}
	if props.Required {
		input.Aria["required"] = "true"
	}
	if props.Issue != nil {
		input.Aria["invalid"] = "true"
		input.Aria["errormessage"] = errorID
		if input.Aria["describedby"] != "" {
			input.Aria["describedby"] += " " + errorID
		} else {
			input.Aria["describedby"] = errorID
		}
	}
	if len(input.Aria) == 0 {
		input.Aria = nil
	}
	children := []ui.Node{
		html.Span(html.Props{Class: "validation-field-label"}, ui.Text(props.Label)),
		html.Input(input),
	}
	if strings.TrimSpace(props.Help) != "" {
		children = append(children, html.Small(html.Props{ID: helpID, Class: "validation-field-help"}, ui.Text(props.Help)))
	}
	if props.Issue != nil {
		children = append(children, validationErrorNode(props.I18nProps, errorID, *props.Issue))
	}
	return html.Label(html.Props{For: id, Class: validationFieldClass(props.Issue)}, children...)
}

func validationFieldClass(issue *ValidationIssue) string {
	if issue == nil {
		return "validation-field"
	}
	return "validation-field has-error"
}

func validationErrorNode(i18n I18nProps, id string, issue ValidationIssue) ui.Node {
	// The summary is the form's single assertive announcement. Keeping inline
	// errors as ordinary described text prevents assistive technology from
	// announcing every error twice while preserving field-level discovery.
	return html.Small(html.Props{ID: id, Class: "validation-field-error"}, ui.Text(validationIssueText(i18n, issue)))
}

func validationIssueText(i18n I18nProps, issue ValidationIssue) string {
	if key := strings.TrimSpace(issue.MessageKey); key != "" {
		message := i18n.Text(key)
		// LocaleContext intentionally exposes unresolved keys for catalog QA. A
		// validation surface must fail closed to reviewed copy instead of
		// shipping a raw key to an employee or screen reader.
		if !strings.HasPrefix(message, "⟦") && !strings.HasSuffix(message, "⟧") {
			return message
		}
	}
	if message := strings.TrimSpace(issue.Message); message != "" && !looksLikeMessageKey(message) {
		// ui.Text escapes service-provided text at the rendering boundary. Keys
		// are rejected here because exposing one is neither useful nor localized.
		return message
	}
	return i18n.Text("validation.generic_error")
}

func looksLikeMessageKey(message string) bool {
	if strings.ContainsAny(message, " \t\r\n") || strings.Count(message, ".") == 0 {
		return false
	}
	for _, part := range strings.Split(message, ".") {
		if part == "" {
			return false
		}
		for _, r := range part {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
				return false
			}
		}
	}
	return true
}

// ValidationSummaryProps keeps the component reusable without global IDs.
// FieldIDs is the set of rendered controls that may safely receive fragment
// links; unknown service field identifiers remain readable, but unlinked.
type ValidationSummaryProps struct {
	I18nProps
	ID       string
	Issues   []ValidationIssue
	FieldIDs []string
	Ref      ui.DOMRef
}

// ValidationSummary presents the first actionable correction target in DOM
// order. The negative tabindex is deliberate: a submitting controller may
// focus this summary, while normal keyboard order remains label -> control.
func ValidationSummary(props ValidationSummaryProps) ui.Node {
	id := strings.TrimSpace(props.ID)
	if id == "" {
		panic("productui: ValidationSummary requires a stable ID")
	}
	errors := ValidationState{Issues: props.Issues}.Errors()
	if len(errors) == 0 {
		return html.Div(html.Props{ID: id, Class: "validation-summary-empty", Hidden: true})
	}
	items := make([]ui.Node, 0, len(errors))
	for _, issue := range errors {
		message := validationIssueText(props.I18nProps, issue)
		fieldID := strings.TrimSpace(issue.FieldID)
		if fieldID == "" || !containsValidationField(props.FieldIDs, fieldID) {
			items = append(items, html.Li(html.Props{}, ui.Text(message)))
			continue
		}
		items = append(items, html.Li(html.Props{}, html.A(html.Props{Href: "#" + fieldID}, ui.Text(message))))
	}
	summaryProps := html.WithProps(html.Props{ID: id, Class: "validation-summary", TabIndex: -1, Raw: map[string]any{
		"role": "alert", "aria-live": "assertive", "aria-atomic": "true", "data-validation-focus": "first-error",
	}}, html.Ref(props.Ref))
	return html.Section(summaryProps,
		html.H2(html.Props{}, ui.Text(props.Text("validation.summary_title"))),
		html.P(html.Props{Class: "validation-summary-intro"}, ui.Text(props.Text("validation.summary_intro"))),
		html.Ul(html.Props{}, items...),
	)
}

func containsValidationField(fieldIDs []string, fieldID string) bool {
	for _, candidate := range fieldIDs {
		if candidate == fieldID {
			return true
		}
	}
	return false
}
