package agentrun

import (
	"context"
	"errors"
	"testing"
)

func TestTodo_AGENTUX_009_TaskAgentIdentity(t *testing.T) {
	agent := TaskAgentIdentity{ID: "policy-helper", DisplayName: "Policy Helper", Version: "2"}
	ctx, err := WithTaskAgentIdentity(context.Background(), agent)
	if err != nil {
		t.Fatal(err)
	}
	got := taskAgentIdentityFromContext(ctx)
	if got == nil || *got != agent {
		t.Fatalf("identity = %+v", got)
	}
	got.DisplayName = "changed"
	if *taskAgentIdentityFromContext(ctx) != agent {
		t.Fatal("context identity was aliased")
	}
	for _, invalid := range []TaskAgentIdentity{{}, {ID: " persona", DisplayName: "Name", Version: "1"}, {ID: "persona", DisplayName: "", Version: "1"}} {
		if _, err := WithTaskAgentIdentity(context.Background(), invalid); !errors.Is(err, ErrTaskAgentInvalid) {
			t.Fatalf("invalid identity %+v = %v", invalid, err)
		}
	}
}
