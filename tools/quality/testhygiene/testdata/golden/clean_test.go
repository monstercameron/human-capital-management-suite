package golden

import (
	"sync"
	"testing"
)

func TestTodo_SAMPLE_003_Race(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
		}()
	}
	wg.Wait()
}

func TestTodo_SAMPLE_004_Golden(t *testing.T) {
	if got, want := 2+2, 4; got != want {
		t.Fatalf("got %d want %d", got, want)
	}
}
