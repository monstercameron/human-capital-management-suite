//go:build js && wasm

package productui

import (
	"syscall/js"
	"testing"
)

func TestUXBlindQVisibleDialogSkipsClosedDisclosure(t *testing.T) {
	for _, tc := range []struct {
		name          string
		closedDetails bool
		transientRoot bool
		closedPopover bool
		want          bool
	}{
		{name: "dialog inside closed details", closedDetails: true, want: false},
		{name: "dialog inside transient disclosure", transientRoot: true, want: false},
		{name: "dialog outside disclosure", want: true},
		// TestIssue3596: the Chat composer's emoji shell is a closed popover with
		// role=dialog and no hidden attribute; it is not drawn and must not count.
		{name: "closed popover dialog", closedPopover: true, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var callbacks []js.Func
			defer func() {
				for _, callback := range callbacks {
					callback.Release()
				}
			}()

			dialog := js.Global().Get("Object").New()
			closest := js.FuncOf(func(_ js.Value, args []js.Value) any {
				if len(args) != 1 {
					return js.Null()
				}
				selector := args[0].String()
				if selector == "details[data-hcm-transient-popover]" && tc.transientRoot ||
					selector == "details:not([open])" && tc.closedDetails {
					return js.Global().Get("Object").New()
				}
				return js.Null()
			})
			callbacks = append(callbacks, closest)
			dialog.Set("closest", closest)
			hasAttribute := js.FuncOf(func(_ js.Value, args []js.Value) any {
				return len(args) == 1 && args[0].String() == "popover" && tc.closedPopover
			})
			// No popover in these fixtures is open.
			matches := js.FuncOf(func(js.Value, []js.Value) any { return false })
			callbacks = append(callbacks, hasAttribute, matches)
			dialog.Set("hasAttribute", hasAttribute)
			dialog.Set("matches", matches)

			dialogs := js.Global().Get("Array").New()
			dialogs.Call("push", dialog)
			doc := js.Global().Get("Object").New()
			querySelectorAll := js.FuncOf(func(js.Value, []js.Value) any { return dialogs })
			callbacks = append(callbacks, querySelectorAll)
			doc.Set("querySelectorAll", querySelectorAll)

			if got := uxblindQVisibleDialog(doc); got != tc.want {
				t.Fatalf("visible dialog = %t, want %t", got, tc.want)
			}
		})
	}
}
