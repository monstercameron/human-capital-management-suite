package main

import "testing"

func TestTodo_UXAUDIT_021_UnsavedNavigationDecision(t *testing.T) {
	cases := []struct {
		name            string
		unsaved         bool
		target, current string
		want            bool
	}{
		{"clean navigation allowed", false, "/workspace/app/admin", "/workspace/app/admin/worker-ids", false},
		{"dirty same href allowed", true, "/workspace/app/admin/worker-ids", "/workspace/app/admin/worker-ids", false},
		{"dirty different href blocked", true, "/workspace/app/admin", "/workspace/app/admin/worker-ids", true},
		{"empty target allowed", true, "", "/workspace/app/admin/worker-ids", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldBlockUnsavedNavigation(tc.unsaved, tc.target, tc.current); got != tc.want {
				t.Fatalf("shouldBlockUnsavedNavigation(%t, %q, %q) = %t, want %t", tc.unsaved, tc.target, tc.current, got, tc.want)
			}
		})
	}
}
