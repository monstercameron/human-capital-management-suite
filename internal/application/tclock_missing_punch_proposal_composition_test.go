package application

import (
	"errors"
	"testing"
)

func TestTodo_TCLOCK011_MissingPunchProposalBinderRequiresDurableFacts(t *testing.T) {
	_, err := NewMissingPunchProposalStartBinder(MissingPunchProposalBinderConfig{})
	if !errors.Is(err, errMissingPunchProposalConfig) {
		t.Fatalf("empty composition error = %v, want configuration refusal", err)
	}
}
