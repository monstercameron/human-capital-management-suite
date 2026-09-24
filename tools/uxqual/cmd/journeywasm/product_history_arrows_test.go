package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// A history refresh re-renders the shell only when an arrow would change.
// The comparison must see exactly what the arrows render: an arrow is
// enabled only when it can move and has an action to call.
func TestProductHistoryArrowsFollowRenderedState(t *testing.T) {
	noop := func() {}
	cases := []struct {
		name  string
		props productui.HistoryNavigationProps
		want  productHistoryArrows
	}{
		{"server render", productui.HistoryNavigationProps{}, productHistoryArrows{}},
		{"back only", productui.HistoryNavigationProps{CanGoBack: true, GoBack: noop, GoForward: noop}, productHistoryArrows{back: true}},
		{"both", productui.HistoryNavigationProps{CanGoBack: true, CanGoForward: true, GoBack: noop, GoForward: noop}, productHistoryArrows{back: true, forward: true}},
		{"can move without an action", productui.HistoryNavigationProps{CanGoBack: true, CanGoForward: true}, productHistoryArrows{}},
	}
	for _, c := range cases {
		if got := productHistoryArrowsOf(c.props); got != c.want {
			t.Fatalf("%s: arrows %+v, want %+v", c.name, got, c.want)
		}
	}
	// A second push in the same room keeps Back enabled and Forward
	// disabled: equal signatures, so the shell does not re-render.
	first := productHistoryArrowsOf(productui.HistoryNavigationProps{CanGoBack: true, GoBack: noop, GoForward: noop})
	second := productHistoryArrowsOf(productui.HistoryNavigationProps{CanGoBack: true, GoBack: func() {}, GoForward: func() {}})
	if first != second {
		t.Fatal("fresh closures for the same arrow state must not force a shell re-render")
	}
}
