package golden

import (
	"os"
	"testing"
)

func TestTodo_SAMPLE_002_Golden(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.golden")
	if err != nil {
		t.Skipf("golden file missing: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("empty golden file")
	}
}
