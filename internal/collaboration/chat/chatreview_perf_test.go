package chat

import "testing"

// The legacy citation fallback compiled its static version-suffix pattern on
// every call. This benchmark pins the cost the hoisted pattern must beat.
// (DisplayVersionLabel is benchmarked by the versions lane in
// chatreview001_versions_bench_test.go.)
func BenchmarkAgentLegacyCitationSection(b *testing.B) {
	body := "See the leave policy (Leave policy, v1.2.0, section 3) for details."
	for i := 0; i < b.N; i++ {
		agentLegacyCitationSection(body, "Leave policy (version 4)")
	}
}
