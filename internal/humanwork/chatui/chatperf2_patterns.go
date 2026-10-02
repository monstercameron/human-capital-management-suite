package chatui

import (
	"regexp"
	"strings"
)

// CHATBUG-014: every message drawn is searched for the references it may hold:
// documents, journeys, project tasks, shared messages. Each search is a regular
// expression of alternatives (an absolute address, or one of this product's own
// paths or tokens), and Go's regexp has no shortcut for such a pattern: it
// tries a match at every position of the text. Profiled in the browser, that
// was the largest single cost of drawing a conversation, about a third of the
// time, and almost every message holds none of these.
//
// Each alternative starts with fixed text, so a body without any of those
// texts cannot match and is not searched at all.

// chatperf2Pattern is a regular expression together with the fixed texts one
// of which every match of it contains. The finders below answer "no match" for
// text that holds none of them, which is what the expression itself would
// answer; every other method is the expression's own.
type chatperf2Pattern struct {
	*regexp.Regexp
	needles []string
}

// chatperf2Literals pairs an expression with the fixed texts its matches
// contain. needles must name one for every alternative of the expression:
// a match that contains none of them would be lost.
func chatperf2Literals(expression *regexp.Regexp, needles ...string) *chatperf2Pattern {
	return &chatperf2Pattern{Regexp: expression, needles: needles}
}

// possible reports whether text holds any of the fixed texts.
func (p *chatperf2Pattern) possible(text string) bool {
	for _, needle := range p.needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

// FindAllStringIndex is the expression's own, skipped for text that cannot
// match.
func (p *chatperf2Pattern) FindAllStringIndex(text string, n int) [][]int {
	if !p.possible(text) {
		return nil
	}
	return p.Regexp.FindAllStringIndex(text, n)
}

// FindAllString is the expression's own, skipped for text that cannot match.
func (p *chatperf2Pattern) FindAllString(text string, n int) []string {
	if !p.possible(text) {
		return nil
	}
	return p.Regexp.FindAllString(text, n)
}
