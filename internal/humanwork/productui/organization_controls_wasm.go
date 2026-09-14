//go:build js && wasm

package productui

import "syscall/js"

// bindOrganizationBrowseControls enhances the SSR-native organization view.
// It changes only already-rendered disclosure nodes and preserves the current
// scroll offset while density is changed, so keyboard focus and reading
// position survive the interaction.
func bindOrganizationBrowseControls() {
	doc := js.Global().Get("document")
	buttons := doc.Call("querySelectorAll", "[data-organization-density], [data-organization-action]")
	for i := 0; i < buttons.Get("length").Int(); i++ {
		button := buttons.Call("item", i)
		listener := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			event := args[0]
			target := event.Get("currentTarget")
			org := target.Call("closest", ".org")
			if !org.Truthy() {
				return nil
			}
			if action := target.Call("getAttribute", "data-organization-action").String(); action != "" {
				open := action == "expand-all"
				details := org.Call("querySelectorAll", "details.organization-unit-disclosure, details.ownership-reports")
				for j := 0; j < details.Get("length").Int(); j++ {
					details.Call("item", j).Set("open", open)
				}
				return nil
			}
			if density := target.Call("getAttribute", "data-organization-density").String(); density != "" {
				scroller := doc.Get("scrollingElement")
				top := scroller.Get("scrollTop")
				org.Get("classList").Call("remove", "organization-density-compact", "organization-density-comfortable", "organization-density-spacious")
				org.Get("classList").Call("add", "organization-density-"+density)
				peers := org.Call("querySelectorAll", "[data-organization-density]")
				for j := 0; j < peers.Get("length").Int(); j++ {
					peer := peers.Call("item", j)
					peer.Set("ariaPressed", peer.Call("getAttribute", "data-organization-density").String() == density)
				}
				scroller.Set("scrollTop", top)
			}
			return nil
		})
		button.Call("addEventListener", "click", listener)
	}
}
