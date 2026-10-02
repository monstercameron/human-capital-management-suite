package chatfilter

import (
	"regexp"
	"regexp/syntax"
	"strings"
)

// Pattern limits. Go's regexp is RE2: matching is linear in the input whatever
// the pattern, with no lookaround and no backreferences (they fail to compile).
// The limits below keep an administrator's pattern small and plain.
const (
	maxPatternBytes   = 256
	maxPatternsInRule = 32
	maxPatternRepeats = 8
)

// compilePattern accepts only plain RE2 patterns: not empty-matching, not
// longer than 256 bytes, not more than 32 per rule, with no unbounded
// wildcard (.* .+) and no nested unbounded repetition such as (a+)+ or (a*)*.
func compilePattern(d Definition) (Matcher, error) {
	if len(d.Match) > maxPatternsInRule {
		return nil, ErrInvalid
	}
	var patterns []*regexp.Regexp
	for _, p := range d.Match {
		if len(p) == 0 || len(p) > maxPatternBytes || strings.Contains(p, "(?") {
			return nil, ErrInvalid
		}
		tree, err := syntax.Parse(p, syntax.Perl)
		if err != nil || minWidth(tree) == 0 || !plainRepetition(tree, false, new(int)) {
			return nil, ErrInvalid
		}
		r, err := regexp.Compile(p)
		if err != nil || r.MatchString("") {
			return nil, ErrInvalid
		}
		patterns = append(patterns, r)
	}
	return func(in Input) []Span {
		var out []Span
		for _, p := range patterns {
			for _, m := range p.FindAllStringIndex(in.Body, -1) {
				if m[1] > m[0] {
					out = append(out, Span{m[0], m[1]})
				}
			}
		}
		return out
	}, nil
}

func unbounded(re *syntax.Regexp) bool {
	return re.Op == syntax.OpStar || re.Op == syntax.OpPlus || re.Op == syntax.OpRepeat && re.Max == -1
}

// plainRepetition rejects an unbounded repetition inside another one and an
// unbounded repetition of the any-character wildcard.
func plainRepetition(re *syntax.Regexp, inside bool, repeats *int) bool {
	if re.Op == syntax.OpRepeat {
		if *repeats++; *repeats > maxPatternRepeats {
			return false
		}
	}
	rep := unbounded(re)
	if rep {
		if inside {
			return false
		}
		sub := re.Sub[0]
		for sub.Op == syntax.OpCapture {
			sub = sub.Sub[0]
		}
		if sub.Op == syntax.OpAnyChar || sub.Op == syntax.OpAnyCharNotNL {
			return false
		}
	}
	for _, sub := range re.Sub {
		if !plainRepetition(sub, inside || rep, repeats) {
			return false
		}
	}
	return true
}

// minWidth is the length of the shortest string the pattern matches.
func minWidth(re *syntax.Regexp) int {
	switch re.Op {
	case syntax.OpLiteral:
		return len(re.Rune)
	case syntax.OpCharClass, syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return 1
	case syntax.OpCapture, syntax.OpPlus:
		return minWidth(re.Sub[0])
	case syntax.OpRepeat:
		return re.Min * minWidth(re.Sub[0])
	case syntax.OpConcat:
		total := 0
		for _, sub := range re.Sub {
			total += minWidth(sub)
		}
		return total
	case syntax.OpAlternate:
		least := -1
		for _, sub := range re.Sub {
			if w := minWidth(sub); least < 0 || w < least {
				least = w
			}
		}
		return least
	}
	return 0
}
