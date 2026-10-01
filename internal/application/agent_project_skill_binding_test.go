package application

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

func TestTodo_AGENT_049_Registration(t *testing.T) {
	caps := capability.NewRegistry()
	skills := agentskills.NewRegistry(caps)
	pin, err := BindAgentProjectSkill(caps, skills, &AgentProjectSkill{})
	if err != nil || pin.ID != AgentProjectSkillID || pin.Version != 1 || pin.Digest == "" {
		t.Fatalf("project skill registration = %+v, err=%v", pin, err)
	}
	if _, err := BindAgentProjectSkill(caps, skills, &AgentProjectSkill{}); err == nil {
		t.Fatal("duplicate project skill registration succeeded")
	}
	if _, err := BindAgentProjectSkill(nil, skills, &AgentProjectSkill{}); !errors.Is(err, errAgentProjectSkillRegistration) {
		t.Fatalf("nil registry error=%v", err)
	}
}
