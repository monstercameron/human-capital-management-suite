package main

import (
	"strings"
	"testing"
)

func TestTodo_AGENTUX_053_CleanupCommand(t *testing.T) {
	command, rest := splitCommand([]string{agentDemoCleanupCommand, "-profile", "local-dev"})
	if command != agentDemoCleanupCommand || len(rest) != 2 {
		t.Fatalf("command=%q rest=%q", command, rest)
	}
	if agentDemoCleanupCommand == agentDemoCommand {
		t.Fatal("the cleanup is the preparation command")
	}
	spec := spec(command, rest)
	var names []string
	for _, field := range spec.ConfigFields {
		names = append(names, field.Name)
	}
	if strings.Join(names, ",") != "profile,tenant,database-url,chat-database-url" {
		t.Fatalf("cleanup flags %v: it needs the core and chat databases and nothing else", names)
	}
	if got := formatAgentDemoCleanup(nil); !strings.HasPrefix(got, "Nothing to archive") {
		t.Fatalf("empty receipt %q", got)
	}
	if got := formatAgentDemoCleanup([]string{"a", "b", "c"}); !strings.Contains(got, "Archived 3 old verification posts") {
		t.Fatalf("receipt %q", got)
	}
}
