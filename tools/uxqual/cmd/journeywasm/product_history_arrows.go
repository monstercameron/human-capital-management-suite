package main

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"

// productHistoryArrows is the part of the history props the header's two
// arrows render from: whether each one is enabled. The shell compares it
// before re-rendering for a history refresh, so a push that leaves both
// arrows as they were (every chat room switch after the first) costs
// nothing.
type productHistoryArrows struct {
	back, forward bool
}

func productHistoryArrowsOf(props productui.HistoryNavigationProps) productHistoryArrows {
	return productHistoryArrows{
		back:    props.CanGoBack && props.GoBack != nil,
		forward: props.CanGoForward && props.GoForward != nil,
	}
}
