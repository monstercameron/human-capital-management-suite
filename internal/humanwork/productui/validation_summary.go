// Package productui resolves the accessible validation
// summary model. The summary component announces errors
// with an assertive live region; this resolution is its
// governed content: errors only, in state order, with the
// component's exact fail-closed copy rule and links only
// to registered fields. Entry text resolves through the
// same helper the component renders, so the model and the
// announcement can never drift. A clean state resolves to
// an empty model that renders hidden.
package productui

import "strings"

// ValidationSummaryEntry is one announced summary item:
// its resolved text with the field it links to, if the
// field is registered for fragment links.
type ValidationSummaryEntry struct {
	Text    string
	FieldID string
	Linked  bool
}

// ValidationSummaryModel is the resolved summary content:
// its catalog title and intro with one entry per error.
// Empty marks a clean state with no title, intro, or
// entries.
type ValidationSummaryModel struct {
	Title   string
	Intro   string
	Entries []ValidationSummaryEntry
	Empty   bool
}

// ValidationFieldState is the inline counterpart to a summary entry. It is
// resolved from the same validation issue text and registered field allowlist
// used by ValidationSummary, ensuring a field never renders a stale or
// unreviewed service key beside its control.
type ValidationFieldState struct {
	FieldID string
	Invalid bool
	Message string
}

// ResolveValidationFields resolves one inline state per registered field, in
// the supplied control order. At most the first error for a field is shown;
// warnings do not mark the control invalid. Unknown issue fields remain in the
// page-level summary but cannot attach to a control that was not rendered.
func ResolveValidationFields(locale LocaleContext, state ValidationState, fieldIDs []string) []ValidationFieldState {
	i18n := I18nProps{Locale: locale}
	result := make([]ValidationFieldState, 0, len(fieldIDs))
	errors := state.Errors()
	for _, rawID := range fieldIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		field := ValidationFieldState{FieldID: id}
		for _, issue := range errors {
			if strings.TrimSpace(issue.FieldID) != id {
				continue
			}
			field.Invalid = true
			field.Message = validationIssueText(i18n, issue)
			break
		}
		result = append(result, field)
	}
	return result
}

// ResolveValidationSummary resolves the summary model for
// one validation state and the rendered controls that may
// receive fragment links. Unknown service field
// identifiers stay readable but unlinked. Inputs are never
// mutated.
func ResolveValidationSummary(locale LocaleContext, state ValidationState, fieldIDs []string) ValidationSummaryModel {
	errors := state.Errors()
	if len(errors) == 0 {
		return ValidationSummaryModel{Empty: true}
	}
	i18n := I18nProps{Locale: locale}
	summary := ValidationSummaryModel{
		Title:   locale.Text("validation.summary_title"),
		Intro:   locale.Text("validation.summary_intro"),
		Entries: make([]ValidationSummaryEntry, 0, len(errors)),
	}
	for _, issue := range errors {
		fieldID := strings.TrimSpace(issue.FieldID)
		summary.Entries = append(summary.Entries, ValidationSummaryEntry{
			Text:    validationIssueText(i18n, issue),
			FieldID: fieldID,
			Linked:  fieldID != "" && containsValidationField(fieldIDs, fieldID),
		})
	}
	return summary
}
