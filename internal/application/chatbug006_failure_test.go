package application

import (
	"fmt"
	"testing"
)

// A terminal run failure keeps its own retry decision through the durable
// post-failure classification, however many layers wrap it.
func TestTodo_CHATBUG_006_RunFailurePreserved(t *testing.T) {
	for _, test := range []struct {
		name  string
		err   error
		code  string
		retry bool
	}{
		{"daily limit is not retryable", &PersonaRunFailure{Code: "MODEL_LIMIT", Retryable: false, kind: ErrPersonaRunModelFailure}, "MODEL_UNAVAILABLE", false},
		{"timeout is retryable", &PersonaRunFailure{Code: "MODEL_TIMEOUT", Retryable: true, kind: ErrPersonaRunModelFailure}, "MODEL_UNAVAILABLE", true},
		{"context unavailable is not retryable", &PersonaRunFailure{Code: "CONTEXT_UNAVAILABLE", Retryable: false, kind: ErrPersonaRunExecutorUnavailable}, "EXECUTION_UNAVAILABLE", false},
		{"rejected output never retries", &PersonaRunFailure{Code: "OUTPUT_REJECTED", Retryable: true, kind: ErrPersonaRunOutputRejected}, "OUTPUT_REJECTED", false},
		{"failure without a kind is unavailable", &PersonaRunFailure{Code: "MODEL_TIMEOUT", Retryable: true}, "EXECUTION_UNAVAILABLE", true},
	} {
		code, retry := personaPostFailureClassification(fmt.Errorf("invoke: %w", test.err))
		if code != test.code || retry != test.retry {
			t.Fatalf("%s: classification=%s %v want=%s %v", test.name, code, retry, test.code, test.retry)
		}
	}
}
