package timesession

import "testing"

func TestVersion(t *testing.T) {
	if got := Version(); got != 1 {
		t.Fatalf("Version() = %d, want 1", got)
	}
}

func TestExplain(t *testing.T) {
	session := Session{Tenant: "acme", Worker: "w1", Assignment: "a1", SessionID: "s1", State: StateOpen,
		Segments: []Segment{{Kind: SegmentWork}}, OpenExceptions: []Exception{{Kind: ExceptionMissingOut}}, Revision: 3}

	got := Explain(session)
	want := Explanation{SessionID: "s1", Tenant: "acme", Worker: "w1", Assignment: "a1", State: StateOpen,
		SegmentCount: 1, OpenExceptionCount: 1, Revision: 3, HasPendingAutoOut: false}
	if got != want {
		t.Fatalf("Explain() = %+v, want %+v", got, want)
	}

	// The zero session explains as the zero state rather than failing.
	if zero := Explain(Session{}); zero.State != "" || zero.SegmentCount != 0 {
		t.Fatalf("Explain(zero) = %+v, want zero explanation", zero)
	}

	session.PendingAutoOut = &AutoOutGrace{}
	if got := session.Explain(); !got.HasPendingAutoOut {
		t.Fatalf("Explain() with pending grace: HasPendingAutoOut = false, want true")
	}
}
