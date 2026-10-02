package chat

import "testing"

func TestAgentUXQuality_SemanticVersions(t *testing.T) {
	for _, pair := range [][2]string{{"1", "v1.0.0"}, {"v2", "v2.0.0"}, {"version 3", "v3.0.0"}, {"v2.4.1", "v2.4.1"}, {"2.4.1", "v2.4.1"}, {"v2.4.1-preview.1", "v2.4.1-preview.1"}, {"2.4.1-preview.1+build.02", "v2.4.1-preview.1+build.02"}, {"2.4.1+build.02", "v2.4.1+build.02"}, {"v2.4.1-01", ""}, {"v2.4.1-preview..1", ""}, {"private-version-id", ""}, {"", ""}} {
		if got := DisplayVersionLabel(pair[0]); got != pair[1] {
			t.Fatalf("%q displayed %q, want %q", pair[0], got, pair[1])
		}
	}
	if DisplayVersion(42) != "v42.0.0" {
		t.Fatal("legacy sequence formatter changed")
	}
}
