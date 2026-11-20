package chatui

// Issues #3837 and #3682: a select whose first option was the chosen one showed
// its last option in the browser (Hindi for "Written in" and for the reading
// language), although the rendered markup chose the right one.
//
// The cause is not which option the render marks. On the commit after a subtree
// is first mounted, the reconciler can place the same nodes again: for a select
// that is one `select.append(option0, option1, ...)` holding the options the
// select already has. The browser carries that out by taking the options out
// one at a time and then putting them back together. Each time the chosen option
// leaves a single-choice select, the browser chooses the next one, so every
// option comes back marked chosen and the last one wins. Measured in the served
// app with the setters and append instrumented: nothing wrote `value`,
// `selectedIndex` or `selected`; the choice moved from index 0 to the last index
// inside that one append call.
//
// selectReappendKeepsChoice decides whether the option chosen before an append
// must be chosen again after it. It must when the append moves options the
// select already owns. It must not for a select that allows several choices
// (the browser leaves those alone), for a select with nothing chosen, for a
// first mount (nothing is moved), or when the append brings a new option that
// arrives chosen: that one is the render's own choice and it wins.
func selectReappendKeepsChoice(multiple bool, chosenIndex int, movesOwnChild, bringsChosenOption bool) bool {
	return !multiple && chosenIndex >= 0 && movesOwnChild && !bringsChosenOption
}
