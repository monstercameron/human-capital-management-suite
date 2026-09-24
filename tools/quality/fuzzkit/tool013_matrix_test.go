package fuzzkit

import (
	"fmt"
	"strings"
	"testing"
)

// TestTodo_TOOL_013_Property proves successful parsing preserves every
// canonical field and rejects a non-canonical spelling of sequence values.
func TestTodo_TOOL_013_Property(t *testing.T) {
	for _, raw := range SeedCorpus() {
		env, err := ParseEnvelope(raw)
		if err != nil {
			continue
		}
		rebuilt := fmt.Sprintf("%s:%s:%d:%s", env.Tenant, env.Kind, env.Sequence, env.Decimal)
		if rebuilt != raw {
			t.Errorf("successful parse did not round-trip: %q became %q", raw, rebuilt)
		}
	}
	for _, raw := range []string{"tenant-1:kind:01:12.34", "tenant-1:kind:+1:12.34"} {
		if _, err := ParseEnvelope(raw); err == nil {
			t.Errorf("accepted non-canonical sequence %q", raw)
		}
	}
}

// TestTodo_TOOL_013_Golden pins the shared malformed-input corpus. Both the
// planted-defect and corrected parser must continue exercising the same
// shape, including the decimal input that exposed the original panic.
func TestTodo_TOOL_013_Golden(t *testing.T) {
	want := "tenant-1:promotion:1:12.34\ntenant-2:compensation:42:0.01\n\n:::\ntenant-1:kind:1:\ntenant-1:kind:1\ntenant-1:kind:1:12.34:extra\n:kind:1:12.34\ntenant-1::1:12.34\ntenant-1:kind:not-a-number:12.34\ntenant-1:kind:1.5:12.34\ntenant-1:kind:1:notadecimal\ntenant-1:kind:1:12.34.56\ntenant-1:kind:1:."
	if got := strings.Join(SeedCorpus(), "\n"); got != want {
		t.Fatalf("TOOL-013 seed corpus bytes changed:\n%s", got)
	}
}

// TestTodo_TOOL_013_Security rejects identifier injection, tenant confusion,
// and non-decimal payloads before an envelope can be treated as canonical.
func TestTodo_TOOL_013_Security(t *testing.T) {
	for _, raw := range []string{
		"tenant-1:kind:1:12.34:other",
		"tenant-1\" OR 1=1:kind:1:12.34",
		"tenant-1/../tenant-2:kind:1:12.34",
		"tenant-1:kind\nadmin:1:12.34",
		"tenant-1:kind:1:12x34",
		"tenant-1:kind:1:12.34e2",
	} {
		if _, err := ParseEnvelope(raw); err == nil {
			t.Errorf("accepted malformed or injected envelope %q", raw)
		}
	}
}
