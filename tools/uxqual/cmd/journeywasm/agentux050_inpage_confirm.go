package main

import "strings"

// ownerActionDestructive reports whether an owner-controls action cannot be
// taken back, so its in-page confirmation (AGENTUX-050) uses the danger style.
func ownerActionDestructive(action string) bool {
	switch action {
	case "stop", "quarantine", "delete", "revoke", "retire":
		return true
	}
	return false
}

// personaCommandDestructive is the same for an agent administration command.
func personaCommandDestructive(command string) bool {
	switch strings.ToUpper(command) {
	case "RETIRE", "SUSPEND", "DISABLE", "DELETE", "REJECT", "QUARANTINE":
		return true
	}
	return false
}
