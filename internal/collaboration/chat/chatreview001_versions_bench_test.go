package chat

import "testing"

var chatReview001VersionSink string

// CHATREVIEW-001: DisplayVersionLabel sits on render paths (version cells,
// chips, cited documents). The benchmark pins its per-call cost across the
// regex-hoist optimization.
func BenchmarkChatReview001_DisplayVersionLabel(b *testing.B) {
	labels := []string{"v2.4.1", "2.4.1-preview.1+build.02", "1", "version 3", "v", "private-version-id", ""}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chatReview001VersionSink = DisplayVersionLabel(labels[i%len(labels)])
	}
}
