package main

import "testing"

func TestShellFix_DOMAttribute(t *testing.T) {
	for _, test := range []struct {
		name     string
		value    string
		isString bool
		want     string
	}{
		{name: "string", value: "agent-42", isString: true, want: "agent-42"},
		{name: "null", value: "<null>", isString: false, want: ""},
		{name: "undefined", value: "<undefined>", isString: false, want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizedDOMAttribute(test.value, test.isString); got != test.want {
				t.Fatalf("normalized DOM attribute = %q, want %q", got, test.want)
			}
		})
	}
}

func TestShellFix_TabletNavigationPreference(t *testing.T) {
	if !shellfixNavigationExpanded("locale=de-DE&nav=expanded") {
		t.Fatal("explicit expansion did not override the tablet default")
	}
	for _, rawQuery := range []string{"", "nav=collapsed", "nav=invalid"} {
		if shellfixNavigationExpanded(rawQuery) {
			t.Fatalf("non-expanded route %q overrode the tablet default", rawQuery)
		}
	}
}
