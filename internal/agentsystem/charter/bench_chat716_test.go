package agentcharter

import "testing"

func BenchmarkResolve(b *testing.B) {
	input := agent010Input()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Resolve(input); err != nil {
			b.Fatal(err)
		}
	}
}
