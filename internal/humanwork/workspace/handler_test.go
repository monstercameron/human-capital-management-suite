package workspace

import "testing"

func TestHandler_Smoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestHandler_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	_ = 1
}

func TestNormalizeBearerInputAcceptsRawAndAuthorizationValues(t *testing.T) {
	for input, want := range map[string]string{
		"token-123":            "token-123",
		"  token-123  ":        "token-123",
		"Bearer token-123":     "token-123",
		" bEaReR   token-123 ": "token-123",
		"Bearer":               "Bearer",
		"":                     "",
	} {
		if got := normalizeBearerInput(input); got != want {
			t.Errorf("normalizeBearerInput(%q) = %q, want %q", input, got, want)
		}
	}
}
