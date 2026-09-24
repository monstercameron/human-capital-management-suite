package rolloutplan

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestTodo_ROLLOUT_003_Golden(t *testing.T) {
	ledger := newTestActivationLedger(t)
	receipt, err := ledger.Activate(activationRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	explanation, err := ledger.Explain("canary", "sub:1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(struct {
		Receipt     ActivationReceipt     `json:"receipt"`
		Explanation ActivationExplanation `json:"explanation"`
	}{receipt, explanation}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const goldenPath = "testdata/activation_golden.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}
