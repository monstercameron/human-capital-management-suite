package chatrewrite

import "testing"

// protect and restore recompile the protected-span pattern on every call.
// This benchmark pins the cost the shared pattern must beat.
func BenchmarkProtectRestore(b *testing.B) {
	draft := "Please finish the 3 tasks by 2026-01-05; see https://intranet.example/t/9 and mail ana@example.com about the $200 invoice"
	for i := 0; i < b.N; i++ {
		p, err := protect(draft)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := p.restore(p.text); err != nil {
			b.Fatal(err)
		}
	}
}
