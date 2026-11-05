package productui

import "testing"

var chatReview001BenchSink string

// chatReview001BenchTable mirrors a mid-size agent copy table. The Inline
// benchmark rebuilds it on every lookup, the pattern the agent surfaces used
// before the shared package-level tables (agentux_copy.go); the Shared
// benchmark resolves it through the helper every file now uses.
var chatReview001BenchTable = map[string][3]string{
	"key_a": {"a en", "a de", "a ar"},
	"key_b": {"b en", "b de", "b ar"},
	"key_c": {"c en", "c de", "c ar"},
	"key_d": {"d en", "d de", "d ar"},
	"key_e": {"e en", "e de", "e ar"},
	"key_f": {"f en", "f de", "f ar"},
	"key_g": {"g en", "g de", "g ar"},
	"key_h": {"h en", "h de", "h ar"},
}

func BenchmarkChatReview001_CopyTableInline(b *testing.B) {
	locale := ResolveProductLocale("de-DE")
	keys := []string{"key_a", "key_b", "key_c", "key_d", "key_e", "key_f", "key_g", "key_h"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy := map[string][3]string{
			"key_a": {"a en", "a de", "a ar"},
			"key_b": {"b en", "b de", "b ar"},
			"key_c": {"c en", "c de", "c ar"},
			"key_d": {"d en", "d de", "d ar"},
			"key_e": {"e en", "e de", "e ar"},
			"key_f": {"f en", "f de", "f ar"},
			"key_g": {"g en", "g de", "g ar"},
			"key_h": {"h en", "h de", "h ar"},
		}
		values, ok := copy[keys[i%len(keys)]]
		if !ok {
			b.Fatal("missing key")
		}
		index := 0
		switch locale.Resolved {
		case "de-DE":
			index = 1
		case "ar":
			index = 2
		}
		chatReview001BenchSink = values[index]
	}
}

func BenchmarkChatReview001_CopyTableShared(b *testing.B) {
	locale := ResolveProductLocale("de-DE")
	keys := []string{"key_a", "key_b", "key_c", "key_d", "key_e", "key_f", "key_g", "key_h"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chatReview001BenchSink = agentuxCopyText(chatReview001BenchTable, locale, keys[i%len(keys)])
	}
}

var chatReview001AssetSink string

// BenchmarkChatReview001_ControlAssetFresh re-hashes the control sheet on
// every linked render, the pattern renderDocument used before the cached
// control-sheet asset; Cached serves the precomputed asset.
func BenchmarkChatReview001_ControlAssetFresh(b *testing.B) {
	body := ControlStylesheet()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chatReview001AssetSink = NewStylesheetAsset(ControlStylesheetAssetPrefix, body).LinkElement()
	}
}

func BenchmarkChatReview001_ControlAssetCached(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		chatReview001AssetSink = ControlStylesheetAsset().LinkElement()
	}
}
