package main

import (
	"reflect"
	"testing"
)

// The rollout preview was sent with no "update first" conversation and the
// server refused it with 400, for one selected conversation and for several.
func TestRolloutPreviewAlwaysNamesTheConversationsUpdatedFirst(t *testing.T) {
	for name, tc := range map[string]struct{ selected, chosen, want []string }{
		"one selected, none chosen":        {[]string{"a"}, nil, []string{"a"}},
		"several selected, none chosen":    {[]string{"a", "b"}, nil, []string{"a"}},
		"several selected, one chosen":     {[]string{"a", "b"}, []string{"b"}, []string{"b"}},
		"a chosen one is no longer picked": {[]string{"a", "b"}, []string{"c"}, []string{"a"}},
		"nothing selected":                 {nil, nil, nil},
	} {
		if got := rolloutPreviewFirstConversations(tc.selected, tc.chosen); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}
