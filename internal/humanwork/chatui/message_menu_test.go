package chatui

import "testing"

func TestMessageMenuKeyboardRoving(t *testing.T) {
	for _, tc := range []struct {
		key     string
		current int
		want    int
	}{
		{"ArrowDown", -1, 0},
		{"ArrowDown", 2, 0},
		{"ArrowUp", 0, 2},
		{"ArrowUp", -1, 2},
		{"Home", 2, 0},
		{"End", 0, 2},
	} {
		if got := messageMenuNextIndex(tc.key, tc.current, 3); got != tc.want {
			t.Errorf("%s from %d: got %d, want %d", tc.key, tc.current, got, tc.want)
		}
	}
}
