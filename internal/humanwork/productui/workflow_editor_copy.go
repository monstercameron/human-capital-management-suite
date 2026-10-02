package productui

import "github.com/monstercameron/human-capital-management-suite/internal/experience/localize"

// workflowEditorMessages is the reviewed English copy for the draft editor.
// It is merged into the default catalog, so coverage tooling counts every key
// here against the other locales.
//
// The effect, reversal, lock, overlay and source vocabularies had no catalog
// entries at all: the editor fell back to a title-cased token, which reads as
// plausible English and so passed every "no missing key marker" check while
// showing "Internal Mutation" to German and Arabic authors.
func workflowEditorMessages() map[string]localize.Message {
	return parseCopyTable(workflowEditorEnglishCopy)
}

// workflowEditorTranslations returns the editor's copy for one non-default
// locale. Arabic carries all six plural categories; the two-form shortcut it
// replaces produced "2 خطوة".
func workflowEditorTranslations(locale string) map[string]localize.Message {
	switch locale {
	case "de-DE":
		return parseCopyTable(workflowEditorGermanCopy)
	case "ar":
		return parseCopyTable(workflowEditorArabicCopy)
	default:
		return nil
	}
}
