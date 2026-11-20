package main

// issue3899RefocusDelays are the moments, in milliseconds after the first
// focus move, at which the browser client checks that the focus put on the
// first new search result survived the page's follow-up redraws.
var issue3899RefocusDelays = []int{60, 160, 400}

// issue3899ShouldRefocus says whether focus must be put on the first new
// result again. A second redraw of the results (recent searches, avatars)
// can replace the control that was just focused and drop focus to the page
// body; that is put right. Focus a person moved elsewhere on purpose is
// left alone.
func issue3899ShouldRefocus(activeIsBody, targetPresent bool) bool {
	return activeIsBody && targetPresent
}
