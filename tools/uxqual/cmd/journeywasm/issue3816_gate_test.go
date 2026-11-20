package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

func TestIssue3816_GateResultsOpenTheGateForm(t *testing.T) {
	for kind, want := range map[chatsearch.Kind]bool{
		chatsearch.GateQuestion: true,
		chatsearch.GateAnswer:   true,
		chatsearch.Message:      false,
		chatsearch.Conversation: false,
		chatsearch.Saved:        false,
	} {
		if got := chatsearchOpensGate(kind); got != want {
			t.Errorf("%s opens the gate form = %v, want %v", kind, got, want)
		}
	}
}
