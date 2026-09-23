package golden

import "testing"

func TestTodo_SAMPLE_001_Race(t *testing.T) {
	for i := 0; i < 32; i++ {
		if i < 0 {
			t.Fatalf("iteration %d", i)
		}
	}
}
