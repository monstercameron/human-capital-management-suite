package application

import "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"

// displayVersion is the legacy sequence display until publication supplies a
// sealed semantic label. The sequence remains the immutable citation identity.
func displayVersion(sequence uint64) string {
	return chat.DisplayVersion(sequence)
}
