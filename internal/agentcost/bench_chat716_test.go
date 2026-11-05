package agentcost

import (
	"testing"
	"time"
)

func BenchmarkAdmit(b *testing.B) {
	clock := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	owners := fakeOwners{
		owners: map[string]bool{"t1/policy/olive": true},
		admins: map[string]bool{"t1/admin": true},
	}
	gate, err := NewGate(owners, &fakeNotifier{}, time.UTC, func() time.Time { return clock })
	if err != nil {
		b.Fatal(err)
	}
	if err := gate.Set("olive", Limit{TenantID: "t1", AgentID: "policy", MaxRuns: 1000000, MaxSpendMicros: 250_000_000}); err != nil {
		b.Fatal(err)
	}
	if err := gate.Set("olive", Limit{TenantID: "t1", AgentID: "policy", ConversationID: "general", MaxRuns: 1000000}); err != nil {
		b.Fatal(err)
	}
	subject := policy("general")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if decision := gate.Admit(subject); !decision.Allowed {
			b.Fatalf("admit refused: %+v", decision)
		}
	}
}
