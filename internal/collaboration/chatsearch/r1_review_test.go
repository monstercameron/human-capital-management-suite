package chatsearch

import (
	"errors"
	"testing"
)

// A channel or person filter with nothing after its "#" or "@" used to parse to
// an empty filter: the chip was shown but the search quietly ran in every room
// and for every author. Like every other known operator with no value, it
// fails closed.
func TestR1_ReviewEmptyScopeFilterFailsClosed(t *testing.T) {
	for _, query := range []string{`budget in:#`, `budget from:@`, `in:# from:@x`, `from:"@"`} {
		if _, err := Parse(query); !errors.Is(err, ErrInvalid) {
			t.Errorf("Parse(%q) error = %v, want ErrInvalid", query, err)
		}
	}
	for _, query := range []string{`budget in:#general`, `budget from:@Full`, `budget in:general`} {
		if _, err := Parse(query); err != nil {
			t.Errorf("Parse(%q) error = %v, want none", query, err)
		}
	}
}

// A NUL byte cannot be stored or compared by the database, so it used to reach
// every source and come back as "content sources unavailable" with a 200. It is
// an invalid request.
func TestR1_ReviewNULInQueryIsInvalid(t *testing.T) {
	if _, err := Parse("bud\x00get"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Parse with NUL error = %v, want ErrInvalid", err)
	}
}
