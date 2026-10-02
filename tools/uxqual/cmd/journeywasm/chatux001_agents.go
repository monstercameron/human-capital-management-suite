package main

// CHATUX-001: the header subtitle's agent count. The count of the last good
// agent read is kept per conversation, outside the roster the read fills, so a
// roster that a listing refresh replaced, or that a failed read cleared, does not
// take the agents out of "Public · 18 members · 2 agents".

// chatux001WithAgentCount returns counts with conversation set to n. The map is
// copied, not edited: models share the map they were copied from.
func chatux001WithAgentCount(counts map[string]int, conversation string, n int) map[string]int {
	if conversation == "" || counts[conversation] == n {
		return counts
	}
	out := make(map[string]int, len(counts)+1)
	for id, count := range counts {
		out[id] = count
	}
	out[conversation] = n
	return out
}
