package agentmodel

import (
	"encoding/json"
	"testing"
)

func BenchmarkPricingCost(b *testing.B) {
	schedule, err := NewPricingSchedule(PricingSchedule{
		Version: "v1", Authority: "bench", Signature: "bench",
		Entries: []PricingEntry{{
			Identity:                     ModelIdentity{ProviderID: "openai", ModelID: "m", Version: "v"},
			InputMicrosPerMillionTokens:  1000,
			OutputMicrosPerMillionTokens: 3000,
		}},
	})
	if err != nil {
		b.Fatal(err)
	}
	selection := ModelSelection{ProfileID: "p", ProfileDigest: schedule.Digest, Identity: ModelIdentity{ProviderID: "openai", ModelID: "m", Version: "v"}}
	usage := ModelUsage{InputTokens: 1200, OutputTokens: 300, TotalTokens: 1500}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := schedule.Cost(selection, usage); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeChatlangTranslation(b *testing.B) {
	result := ModelResult{
		ContractVersion: ContractVersion,
		Provider:        ModelIdentity{ProviderID: "openai", ModelID: "m", Version: "v"},
		Finish:          FinishComplete,
		Structured:      json.RawMessage(`{"source_language":"de","text":"Guten Morgen","meaning_preserved":true}`),
		Usage:           ModelUsage{InputTokens: 10, OutputTokens: 10, TotalTokens: 20},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodeChatlangTranslation(result); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkChatlangTranslationSchema(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ChatlangTranslationSchema(); err != nil {
			b.Fatal(err)
		}
	}
}
