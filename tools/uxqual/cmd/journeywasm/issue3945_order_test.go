package main

import "testing"

func TestIssue3945_ControlQueryAddsReplacesAndRemovesTheOrder(t *testing.T) {
	for _, tc := range []struct{ name, query, order, want string }{
		{"oldest added, words and filters kept", `deadline kind:message`, "oldest", `deadline kind:message sort:"oldest"`},
		{"oldest replaces newest", `deadline sort:newest`, "oldest", `deadline sort:"oldest"`},
		{"back to the default removes the word", `deadline sort:oldest in:#design`, "", `deadline in:#design`},
		{"default stays default", `deadline`, "", `deadline`},
	} {
		got, err := chatsearchControlQuery(tc.query, map[string]string{"order": tc.order}, nil)
		if err != nil || got != tc.want {
			t.Fatalf("%s: got %q (%v), want %q", tc.name, got, err, tc.want)
		}
	}
}
