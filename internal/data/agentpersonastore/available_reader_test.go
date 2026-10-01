package agentpersonastore

import "testing"

func TestNormalizeAvailableInstallations(t *testing.T) {
	got := normalizeAvailableInstallations([]AvailableInstallation{{PersonaID: " persona-a ", PersonaVersion: 1, InstallationID: " install-a ", ConversationID: " room-a "}, {}, {PersonaID: "persona-a", PersonaVersion: 1, InstallationID: "install-a", ConversationID: "room-a"}, {PersonaID: "persona-b", PersonaVersion: 2, InstallationID: "install-b", ConversationID: "room-b"}})
	if len(got) != 2 || got[0].PersonaID != "persona-a" || got[0].InstallationID != "install-a" || got[1].PersonaID != "persona-b" {
		t.Fatalf("normalized installations = %v, want two exact tuples", got)
	}
	if got := normalizeAvailableInstallations(nil); len(got) != 0 {
		t.Fatalf("nil installations = %v, want empty", got)
	}
}
