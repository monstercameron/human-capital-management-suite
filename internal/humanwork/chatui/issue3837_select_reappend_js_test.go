//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

// issue3837Select is a single-choice select that follows the browser's rules
// for which option is chosen: taking the chosen option out chooses the first one
// left, an option that comes in chosen takes the choice from the others, and
// choosing one option un-chooses the rest. Those rules are what turn one
// append of a select's own options into "the last option is chosen".
type issue3837Select struct {
	el      js.Value
	options js.Value
	funcs   []js.Func
}

func (s *issue3837Select) release() {
	for _, f := range s.funcs {
		f.Release()
	}
}

func (s *issue3837Select) fn(f func(this js.Value, args []js.Value) any) js.Func {
	wrapped := js.FuncOf(f)
	s.funcs = append(s.funcs, wrapped)
	return wrapped
}

func (s *issue3837Select) anyChosen() bool {
	for i := 0; i < s.options.Length(); i++ {
		if s.options.Index(i).Get("chosen").Bool() {
			return true
		}
	}
	return false
}

// option makes a detached option. `selected` is an accessor, as in a browser:
// setting it chooses this option and no other of its select.
func (s *issue3837Select) option(value string) js.Value {
	option := js.Global().Get("Object").New()
	option.Set("value", value)
	option.Set("chosen", false)
	option.Set("parentNode", js.Null())
	get := s.fn(func(this js.Value, _ []js.Value) any { return this.Get("chosen").Bool() })
	set := s.fn(func(this js.Value, args []js.Value) any {
		on := len(args) > 0 && args[0].Truthy()
		this.Set("chosen", on)
		if on && this.Get("parentNode").Equal(s.el) {
			for i := 0; i < s.options.Length(); i++ {
				if other := s.options.Index(i); !other.Equal(this) {
					other.Set("chosen", false)
				}
			}
		}
		return nil
	})
	js.Global().Get("Object").Call("defineProperty", option, "selected", map[string]any{"get": get, "set": set})
	return option
}

// browserAppend is the browser's `append(...nodes)`: every node is first moved
// out of its parent, then they are inserted together in the order given.
func (s *issue3837Select) browserAppend() js.Func {
	return s.fn(func(this js.Value, args []js.Value) any {
		for _, arg := range args {
			if !arg.Get("parentNode").Equal(this) {
				continue
			}
			s.options.Call("splice", s.options.Call("indexOf", arg), 1)
			arg.Set("parentNode", js.Null())
			if arg.Get("chosen").Bool() && !s.anyChosen() && s.options.Length() > 0 {
				s.options.Index(0).Set("chosen", true)
			}
		}
		for _, arg := range args {
			s.options.Call("push", arg)
			arg.Set("parentNode", this)
			if arg.Get("chosen").Bool() {
				arg.Set("selected", true)
			}
		}
		if !s.anyChosen() && s.options.Length() > 0 {
			s.options.Index(0).Set("chosen", true)
		}
		return js.Undefined()
	})
}

func newIssue3837Select(t *testing.T, guarded bool, values ...string) (*issue3837Select, []js.Value) {
	t.Helper()
	s := &issue3837Select{el: js.Global().Get("Object").New(), options: js.Global().Get("Array").New()}
	t.Cleanup(s.release)
	s.el.Set("tagName", "SELECT")
	s.el.Set("multiple", false)
	s.el.Set("options", s.options)
	index := s.fn(func(js.Value, []js.Value) any {
		for i := 0; i < s.options.Length(); i++ {
			if s.options.Index(i).Get("chosen").Bool() {
				return i
			}
		}
		return -1
	})
	js.Global().Get("Object").Call("defineProperty", s.el, "selectedIndex", map[string]any{"get": index})
	s.el.Set("contains", s.fn(func(_ js.Value, args []js.Value) any {
		return len(args) > 0 && args[0].Get("parentNode").Equal(s.el)
	}))
	appendFn := s.browserAppend()
	if guarded {
		guard := selectAppendKeepingChoice(appendFn.Value)
		s.funcs = append(s.funcs, guard)
		appendFn = guard
	}
	s.el.Set("append", appendFn)
	options := make([]js.Value, 0, len(values))
	for _, value := range values {
		options = append(options, s.option(value))
	}
	return s, options
}

func issue3837Args(options []js.Value) []any {
	args := make([]any, 0, len(options))
	for _, option := range options {
		args = append(args, option)
	}
	return args
}

var issue3837Written = []string{"", "en", "de", "fr", "es", "pt", "ar", "ja", "hi"}

// TestIssue3837_ReappendedOptionsKeepAnyLanguageChosen is the search page's
// "Written in" select: Any language is chosen on mount, the reconciler appends
// the same nine options again, and Any language must still be chosen. The
// unguarded select shows what the browser did before: Hindi.
func TestIssue3837_ReappendedOptionsKeepAnyLanguageChosen(t *testing.T) {
	plain, plainOptions := newIssue3837Select(t, false, issue3837Written...)
	plainOptions[0].Set("selected", true)
	plain.el.Call("append", issue3837Args(plainOptions)...)
	if got := plain.el.Get("selectedIndex").Int(); got != 0 {
		t.Fatalf("mount chose option %d, want 0", got)
	}
	plain.el.Call("append", issue3837Args(plainOptions)...)
	if got := plain.el.Get("selectedIndex").Int(); got != len(issue3837Written)-1 {
		t.Fatalf("the browser model chose option %d after a second append, want the last one: the defect is not reproduced", got)
	}

	guarded, options := newIssue3837Select(t, true, issue3837Written...)
	options[0].Set("selected", true)
	guarded.el.Call("append", issue3837Args(options)...)
	guarded.el.Call("append", issue3837Args(options)...)
	if got := guarded.el.Get("selectedIndex").Int(); got != 0 {
		t.Fatalf("after its options were appended again the select chose option %d (%q), want 0 (Any language)", got, guarded.options.Index(got).Get("value").String())
	}
	if got := guarded.options.Length(); got != len(issue3837Written) {
		t.Fatalf("the select holds %d options, want %d", got, len(issue3837Written))
	}
	for i, want := range issue3837Written {
		if got := guarded.options.Index(i).Get("value").String(); got != want {
			t.Fatalf("option %d is %q, want %q: the append changed the order", i, got, want)
		}
	}
}

// TestIssue3682_ReappendedOptionsKeepTheReadingLanguage is the reading-language
// select of Chat preferences: English is first and chosen for a person who reads
// English, and a language the person picked stays picked as well.
func TestIssue3682_ReappendedOptionsKeepTheReadingLanguage(t *testing.T) {
	languages := []string{"en", "de", "fr", "es", "pt", "ar", "ja", "hi"}
	for _, chosen := range []int{0, 2} {
		s, options := newIssue3837Select(t, true, languages...)
		options[0].Set("selected", true)
		s.el.Call("append", issue3837Args(options)...)
		options[chosen].Set("selected", true)
		s.el.Call("append", issue3837Args(options)...)
		if got := s.el.Get("selectedIndex").Int(); got != chosen {
			t.Fatalf("reading language moved to %q, want %q", languages[got], languages[chosen])
		}
	}
}

// TestIssue3837_SelectGuardLeavesOtherAppendsToTheBrowser covers what the guard
// must not change: a first mount, a new option that arrives chosen, and a select
// that allows several choices.
func TestIssue3837_SelectGuardLeavesOtherAppendsToTheBrowser(t *testing.T) {
	s, options := newIssue3837Select(t, true, "a", "b", "c")
	options[1].Set("selected", true)
	s.el.Call("append", issue3837Args(options)...)
	if got := s.el.Get("selectedIndex").Int(); got != 1 {
		t.Fatalf("first mount chose option %d, want the one rendered as chosen (1)", got)
	}

	arriving := s.option("d")
	arriving.Set("selected", true)
	s.el.Call("append", issue3837Args(append(options, arriving))...)
	if got := s.options.Index(s.el.Get("selectedIndex").Int()).Get("value").String(); got != "d" {
		t.Fatalf("a new option that arrived chosen lost to %q", got)
	}

	several, many := newIssue3837Select(t, true, "a", "b", "c")
	several.el.Set("multiple", true)
	several.el.Call("append", issue3837Args(many)...)
	owner, chosen := selectChoiceToKeep(several.el, many)
	if owner.Truthy() || chosen.Truthy() {
		t.Fatal("a select that allows several choices was given one choice to keep")
	}

	// Text and other non-element arguments reach the browser untouched.
	owner, chosen = selectChoiceToKeep(js.ValueOf("text"), []js.Value{js.ValueOf("more")})
	if owner.Truthy() || chosen.Truthy() {
		t.Fatal("a text receiver was treated as a select")
	}
}
