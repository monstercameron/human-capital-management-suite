package chatbarrier

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func mustRule(t *testing.T, a, b string) Rule {
	t.Helper()
	r, err := NewRule("r-"+a+b, a, b, "admin", time.Unix(1, 0))
	if err != nil {
		t.Fatalf("NewRule(%q,%q): %v", a, b, err)
	}
	return r
}

func TestIssue3783_RuleIsOrderedAndCaseFolded(t *testing.T) {
	r1, r2 := mustRule(t, "Sales", "Advisory"), mustRule(t, " advisory ", "SALES")
	if r1.GroupA != r2.GroupA || r1.GroupB != r2.GroupB || r1.GroupA != "advisory" || r1.GroupB != "sales" {
		t.Fatalf("pair not normalized: %+v %+v", r1, r2)
	}
	if r1.LabelA != "Advisory" || r1.LabelB != "Sales" {
		t.Fatalf("labels must follow the keys: %+v", r1)
	}
}

func TestIssue3783_RuleRefusesBadInput(t *testing.T) {
	long := strings.Repeat("x", MaxGroupRunes+1)
	for name, c := range map[string][3]string{
		"blank a":    {"", "Sales", "id"},
		"blank b":    {"Sales", "  ", "id"},
		"same group": {"Sales", " sales", "id"},
		"markup":     {"<b>Sales</b>", "Advisory", "id"},
		"control":    {"Sales\x00", "Advisory", "id"},
		"too long":   {long, "Advisory", "id"},
		"no id":      {"Sales", "Advisory", ""},
	} {
		if _, err := NewRule(c[2], c[0], c[1], "admin", time.Now()); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
	}
	if _, err := NewRule("id", "Sales", "Advisory", "", time.Now()); !errors.Is(err, ErrInvalid) {
		t.Errorf("a rule needs an author: %v", err)
	}
}

func TestIssue3783_CheckDeniesPairInEitherDirection(t *testing.T) {
	rules := Rules{mustRule(t, "Sales", "Advisory")}
	sales, advisory, hr := Member{"s1", "Sales"}, Member{"a1", "advisory"}, Member{"h1", "People"}
	if _, bad := rules.Check([]Member{sales}, []Member{advisory}); !bad {
		t.Fatal("advisory joining a sales room must be denied")
	}
	if _, bad := rules.Check([]Member{advisory}, []Member{sales}); !bad {
		t.Fatal("sales joining an advisory room must be denied")
	}
	if _, bad := rules.Check([]Member{sales, advisory}, []Member{hr}); bad {
		t.Fatal("an unrelated person may join, even to a room that already breaks the rule")
	}
	if _, bad := rules.Check([]Member{sales}, []Member{{"s2", "SALES"}}); bad {
		t.Fatal("two people of one group are not separated")
	}
	if _, bad := rules.Check([]Member{sales}, []Member{{"x", ""}}); bad {
		t.Fatal("a person with no group is on no side")
	}
}

func TestIssue3783_CheckCoversPeopleJoiningTogether(t *testing.T) {
	rules := Rules{mustRule(t, "Sales", "Advisory")}
	v, bad := rules.Check(nil, []Member{{"s1", "Sales"}, {"h1", "People"}, {"a1", "Advisory"}})
	if !bad || v.Rule.LabelA != "Advisory" || v.Rule.LabelB != "Sales" {
		t.Fatalf("a new conversation must not start with both sides: %+v %v", v, bad)
	}
	if _, bad := rules.Check(nil, []Member{{"s1", "Sales"}}); bad {
		t.Fatal("one person alone breaks nothing")
	}
	if _, bad := (Rules{}).Check([]Member{{"s1", "Sales"}}, []Member{{"a1", "Advisory"}}); bad {
		t.Fatal("no rules, no barrier")
	}
}

func TestIssue3783_CheckIgnoresTheSamePersonOnBothSides(t *testing.T) {
	rules := Rules{mustRule(t, "Sales", "Advisory")}
	if _, bad := rules.Check([]Member{{"p", "Sales"}}, []Member{{"p", "Advisory"}}); bad {
		t.Fatal("a person already in the room is not their own opposite")
	}
}
