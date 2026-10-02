package main

// personaEditorFirstField selects the first field a person can type in when
// the editor opens. The form begins with hidden inputs and the read-only agent
// address; focusing the first input of any kind left focus where it was.
const personaEditorFirstField = "input:not([type=hidden]):not([readonly]):not([disabled]),select:not([disabled]),textarea:not([disabled])"

// personaEditorField is one field of the open editor: what it holds now and
// what it held when the editor opened.
type personaEditorField struct {
	Value, Initial string
}

// personaEditorDirty reports whether anything in the editor differs from what
// it opened with, counting the documents listed as well as the fields.
func personaEditorDirty(fields []personaEditorField, documents, initialDocuments int) bool {
	if documents != initialDocuments {
		return true
	}
	for _, field := range fields {
		if field.Value != field.Initial {
			return true
		}
	}
	return false
}

// personaEditorClose decides what Cancel or Escape does. An untouched editor
// closes at once. A changed one closes only after the person agrees to lose
// the changes; ask is called only then.
func personaEditorClose(dirty bool, ask func() bool) bool {
	if !dirty {
		return true
	}
	return ask != nil && ask()
}

// personaEditorEscapeCloses reports whether Escape belongs to the editor. A
// list open inside it (document results, the mention menu) takes Escape first
// and closes itself, leaving the editor open.
func personaEditorEscapeCloses(key string, listOpen, alreadyHandled bool) bool {
	return key == "Escape" && !listOpen && !alreadyHandled
}
