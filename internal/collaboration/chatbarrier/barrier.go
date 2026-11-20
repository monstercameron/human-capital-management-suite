// Package chatbarrier holds the pure rules behind Chat's information barriers
// (issue 3783): a deny-list of group pairs that may never share a
// conversation, such as Sales and Advisory.
//
// A group is the name of a person's team in the people directory. The package
// reads no database and no clock: callers hand it the current rules and the
// groups of the people involved, and it answers whether a person may join.
// Nothing here names the other side of a refusal to a caller; a rule's labels
// are for the administrators who write it.
package chatbarrier

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MaxGroupRunes bounds a group name, the same limit the directory's team names
// are held to in a form.
const MaxGroupRunes = 80

// ErrInvalid is returned for a rule that cannot be stored: a blank group, a
// group paired with itself, a name that is too long or carries markup or
// control characters.
var ErrInvalid = errors.New("chatbarrier: invalid rule")

// Rule keeps two groups apart. GroupA and GroupB are comparison keys in
// ascending order, so the same pair is one rule whichever side is named first.
// LabelA and LabelB keep the spelling the administrator typed, in the same order.
type Rule struct {
	ID        string
	GroupA    string
	GroupB    string
	LabelA    string
	LabelB    string
	CreatedBy string
	CreatedAt time.Time
}

// Member is one person as a barrier check sees them: who they are and the group
// the directory puts them in. A person with no group is on no side of any rule.
type Member struct {
	Subject string
	Group   string
}

// Key is the comparison form of a group name: trimmed, inner whitespace
// collapsed and case folded, so "Sales", " sales " and "SALES" are one group.
func Key(group string) string {
	return strings.ToLower(strings.Join(strings.Fields(group), " "))
}

// Label is the display form of a group name: trimmed, inner whitespace collapsed.
func Label(group string) string { return strings.Join(strings.Fields(group), " ") }

func validGroup(group string) bool {
	label := Label(group)
	if label == "" || utf8.RuneCountInString(label) > MaxGroupRunes {
		return false
	}
	for _, r := range label {
		if unicode.IsControl(r) || strings.ContainsRune("<>&\"'`", r) {
			return false
		}
	}
	return true
}

// NewRule builds a rule for the pair of groups. The pair is stored in key
// order; naming the same group twice, or a group that is not a plain name, is
// refused.
func NewRule(id, groupA, groupB, createdBy string, at time.Time) (Rule, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(createdBy) == "" || !validGroup(groupA) || !validGroup(groupB) {
		return Rule{}, ErrInvalid
	}
	keyA, keyB := Key(groupA), Key(groupB)
	if keyA == keyB {
		return Rule{}, ErrInvalid
	}
	labelA, labelB := Label(groupA), Label(groupB)
	if keyA > keyB {
		keyA, keyB, labelA, labelB = keyB, keyA, labelB, labelA
	}
	return Rule{ID: id, GroupA: keyA, GroupB: keyB, LabelA: labelA, LabelB: labelB, CreatedBy: createdBy, CreatedAt: at}, nil
}

// Rules is the set of barriers in force for one workspace.
type Rules []Rule

// Separates reports whether a rule keeps the two groups apart. An empty group
// is never kept apart from anyone.
func (rs Rules) Separates(a, b string) bool {
	_, ok := rs.find(a, b)
	return ok
}

func (rs Rules) find(a, b string) (Rule, bool) {
	keyA, keyB := Key(a), Key(b)
	if keyA == "" || keyB == "" || keyA == keyB {
		return Rule{}, false
	}
	if keyA > keyB {
		keyA, keyB = keyB, keyA
	}
	for _, r := range rs {
		if r.GroupA == keyA && r.GroupB == keyB {
			return r, true
		}
	}
	return Rule{}, false
}

// Violation names the rule a join would break and the first pair it would put
// in one conversation.
type Violation struct {
	Rule    Rule
	Joining Member
	Other   Member
}

// Check reports whether letting the joining people into a conversation that
// already holds the present people would put two separated groups together. A
// pair that is already together before the join is not counted, so one old
// breach never blocks an unrelated addition. People who are joining together
// are checked against each other as well, which is what a new conversation
// needs. It returns false when nothing is in the way.
func (rs Rules) Check(present, joining []Member) (Violation, bool) {
	if len(rs) == 0 {
		return Violation{}, false
	}
	for i, j := range joining {
		for _, p := range present {
			if p.Subject == j.Subject {
				continue
			}
			if rule, ok := rs.find(j.Group, p.Group); ok {
				return Violation{Rule: rule, Joining: j, Other: p}, true
			}
		}
		for _, k := range joining[i+1:] {
			if k.Subject == j.Subject {
				continue
			}
			if rule, ok := rs.find(j.Group, k.Group); ok {
				return Violation{Rule: rule, Joining: j, Other: k}, true
			}
		}
	}
	return Violation{}, false
}
