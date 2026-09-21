package providerwire

import (
	"net/http"
	"strings"
	"testing"
)

const parent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

func TestValidCorrelationID(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"corr-1", true},
		{strings.Repeat("a", MaxCorrelationIDLen), true},
		{strings.Repeat("a", MaxCorrelationIDLen+1), false},
		{"", false},
		{"has space", false},
		{"crlf\r\nX-Evil: 1", false},
		{"tab\t", false},
		{"nonasciié", false},
	} {
		if got := ValidCorrelationID(tc.in); got != tc.want {
			t.Errorf("ValidCorrelationID(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestValidTraceParent(t *testing.T) {
	if got, ok := ValidTraceParent(parent); !ok || got != parent {
		t.Fatalf("ValidTraceParent(valid) = %q, %v", got, ok)
	}
	for _, bad := range []string{
		"",
		"garbage",
		"01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01",
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-02",
		parent + "\r\nX-Evil: 1",
	} {
		if got, ok := ValidTraceParent(bad); ok || got != "" {
			t.Errorf("ValidTraceParent(%q) = %q, %v; want rejection", bad, got, ok)
		}
	}
}

func TestChildTraceParentKeepsTraceAndFlagsWithNewSpan(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 16; i++ {
		child, ok := ChildTraceParent(parent)
		if !ok {
			t.Fatal("ChildTraceParent rejected a valid parent")
		}
		if _, ok := ValidTraceParent(child); !ok {
			t.Fatalf("child %q is not a valid traceparent", child)
		}
		parts := strings.Split(child, "-")
		if parts[1] != "4bf92f3577b34da6a3ce929d0e0e4736" || parts[3] != "01" {
			t.Fatalf("child %q changed the trace id or flags", child)
		}
		if parts[2] == "00f067aa0ba902b7" {
			t.Fatalf("child %q reused the parent span id", child)
		}
		seen[parts[2]] = true
	}
	if len(seen) < 16 {
		t.Fatalf("child span ids repeated: %d distinct of 16", len(seen))
	}
	unsampled, ok := ChildTraceParent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00")
	if !ok || !strings.HasSuffix(unsampled, "-00") {
		t.Fatalf("unsampled child = %q, %v", unsampled, ok)
	}
	if _, ok := ChildTraceParent("bogus"); ok {
		t.Fatal("ChildTraceParent accepted a malformed parent")
	}
}

func TestFromHeaderAndApplyEcho(t *testing.T) {
	in := http.Header{}
	in.Set(TraceParentHeader, parent)
	in.Set(CorrelationIDHeader, "corr-42")
	c := FromHeader(in)
	if c.TraceParent != parent || c.CorrelationID != "corr-42" || c.IsZero() {
		t.Fatalf("FromHeader = %+v", c)
	}
	out := http.Header{}
	c.ApplyEcho(out)
	if out.Get(CorrelationIDHeader) != "corr-42" {
		t.Fatalf("echoed correlation = %q", out.Get(CorrelationIDHeader))
	}
	echo := out.Get(TraceParentHeader)
	if !strings.HasPrefix(echo, "00-4bf92f3577b34da6a3ce929d0e0e4736-") || echo == parent {
		t.Fatalf("echoed traceparent = %q, want a child of %q", echo, parent)
	}

	bad := http.Header{}
	bad.Set(TraceParentHeader, "00-zz-zz-01")
	bad.Set(CorrelationIDHeader, strings.Repeat("x", MaxCorrelationIDLen+1))
	c = FromHeader(bad)
	if !c.IsZero() {
		t.Fatalf("malformed inbound values kept: %+v", c)
	}
	out = http.Header{}
	c.ApplyEcho(out)
	if len(out) != 0 {
		t.Fatalf("malformed values echoed: %v", out)
	}
}
