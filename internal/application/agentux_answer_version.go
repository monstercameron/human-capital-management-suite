package application

import "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"

// displayVersion is the legacy sequence display until publication supplies a
// sealed semantic label. The sequence remains the immutable citation identity.
func displayVersion(sequence uint64) string {
	return chat.DisplayVersion(sequence)
}

// displayVersionLabel prefers a sealed semantic label and falls back to the
// legacy sequence display when the version predates labels (AGENTUX-065).
func displayVersionLabel(label string, sequence uint64) string {
	if display := chat.DisplayVersionLabel(label); display != "" {
		return display
	}
	return chat.DisplayVersion(sequence)
}
