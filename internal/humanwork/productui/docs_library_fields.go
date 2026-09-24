package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Element IDs of the Docs library's inline folder-name fields.
const (
	docsFolderNewID    = "docs-folder-new"
	docsFolderRenameID = "docs-folder-rename"
)

type docsFolderNameFieldProps struct {
	ID, Seed, Placeholder string
	OnInput               ui.Handler
}

// docsFolderNameField is an uncontrolled folder-name box. It renders no
// value prop, so no render of the library -- however late -- can write an
// older string back over what the person typed; the owner keeps the text
// in a ref fed by OnInput. Seed is the starting text (the current name when
// renaming), written into the DOM once when the field mounts.
func docsFolderNameField(props docsFolderNameFieldProps) ui.Node {
	seeded := ui.UseRef(false)
	ui.UseLayoutEffect(func() func() {
		if !seeded.Get() {
			seeded.Set(true)
			if props.Seed != "" {
				setDocsFieldValue(props.ID, props.Seed)
			}
		}
		return nil
	})
	return html.Input(html.Props{ID: props.ID, MaxLength: 80, Placeholder: props.Placeholder, OnInput: props.OnInput, AutoComplete: "off"})
}
