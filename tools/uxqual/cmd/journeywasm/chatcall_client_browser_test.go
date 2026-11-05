package main

import (
	"strings"
	"testing"
)

// TestTodo_CHATCALL_004_Browser pins the acceptance contract the throwaway
// two-context prototype must satisfy: two browser contexts with synthetic
// devices calling each other through the review cell under injected
// impairment. It runs in the native suite like the other browser-contract
// tests; its assertions are pure contract checks, so the same test also
// compiles under the wasm build. The prototype run itself is recorded in
// the contract's status, which is NOT_RUN until it reports.
func TestTodo_CHATCALL_004_Browser(t *testing.T) {
	contract := CallPrototypeContract()
	if contract.Contexts != 2 {
		t.Fatalf("prototype contexts = %d, want two calling each other", contract.Contexts)
	}
	joined := strings.Join(contract.SyntheticDevices, " ")
	if !strings.Contains(joined, "--use-fake-device-for-media-stream") || !strings.Contains(joined, "--use-fake-ui-for-media-stream") {
		t.Fatalf("synthetic devices = %q, want the fake-device flags", joined)
	}
	if len(contract.Impairment) == 0 {
		t.Fatal("the contract injects no network impairment")
	}
	wantPass := []string{
		"signalling completes over the tunnel",
		"audio flows both ways at analyser level",
		"hang-up releases every track and closes the connection",
		"a second call reuses no state from the first",
	}
	if len(contract.Pass) != len(wantPass) {
		t.Fatalf("pass criteria = %v", contract.Pass)
	}
	for i, want := range wantPass {
		if contract.Pass[i] != want {
			t.Fatalf("pass criterion %d = %q, want %q", i, contract.Pass[i], want)
		}
	}
	if contract.Status != "NOT_RUN" {
		t.Fatalf("status = %q, want NOT_RUN until the two-context run reports", contract.Status)
	}
	if !strings.Contains(RenderCallClient(), "NOT_RUN") {
		t.Fatal("the rendered record hides the pending prototype")
	}
}
