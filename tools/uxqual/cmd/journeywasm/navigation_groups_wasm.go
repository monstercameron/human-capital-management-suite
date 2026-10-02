//go:build js && wasm

package main

import (
	"net/url"
	"strings"
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// useTabletNavigationPreference keeps the CSS tablet default and an explicit
// nav=expanded route in agreement after software navigation. The server has
// already emitted the correct class for full loads; the effect repairs only
// the long-lived shell that the history router keeps mounted.
func useTabletNavigationPreference(rawQuery string) {
	ui.UseEffectOf(func() func() {
		values, err := url.ParseQuery(strings.TrimPrefix(rawQuery, "?"))
		if err != nil {
			return nil
		}
		shell := js.Global().Get("document").Call("querySelector", ".app-shell")
		if !shell.Truthy() {
			return nil
		}
		classes := shell.Get("classList")
		if values.Get("nav") == "expanded" {
			classes.Call("add", "nav-expanded")
		} else {
			classes.Call("remove", "nav-expanded")
		}
		return nil
	}, rawQuery)
}

type browserNavigationGroupController struct {
	state    map[string]bool
	save     func(map[string]bool)
	listener js.Func
	bound    bool
}

func newBrowserNavigationGroupController(save func(map[string]bool)) *browserNavigationGroupController {
	return &browserNavigationGroupController{state: map[string]bool{}, save: save}
}

func (c *browserNavigationGroupController) Load(state map[string]bool) {
	if c == nil {
		return
	}
	c.state = make(map[string]bool, len(state))
	for key, open := range state {
		if validNavigationGroupKey(key) {
			c.state[key] = open
		}
	}
}

func (c *browserNavigationGroupController) Bind() {
	if c == nil || c.bound {
		return
	}
	c.bound = true
	c.listener = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		if !target.Truthy() || target.Get("nodeType").Int() != 1 {
			return nil
		}
		summary := target.Call("closest", "summary")
		if !summary.Truthy() {
			return nil
		}
		group := summary.Get("parentElement")
		if !group.Truthy() || group.Get("tagName").String() != "DETAILS" || group.Call("hasAttribute", "data-hcm-nav-force-open").Bool() {
			return nil
		}
		key := strings.TrimSpace(domAttribute(group, "data-hcm-nav-group"))
		if !validNavigationGroupKey(key) {
			return nil
		}
		// A summary click is dispatched before the browser applies the native
		// details toggle. Read the state on the next task so we persist the
		// resulting value rather than the value the user just changed.
		var save js.Func
		save = js.FuncOf(func(js.Value, []js.Value) any {
			defer save.Release()
			c.state[key] = group.Get("open").Bool()
			if c.save != nil {
				c.save(c.State())
			}
			return nil
		})
		js.Global().Call("setTimeout", save, 0)
		return nil
	})
	js.Global().Get("document").Call("addEventListener", "click", c.listener)
}

func (c *browserNavigationGroupController) State() map[string]bool {
	if c == nil {
		return nil
	}
	state := make(map[string]bool, len(c.state))
	for key, open := range c.state {
		state[key] = open
	}
	return state
}
