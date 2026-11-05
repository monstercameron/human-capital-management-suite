package application

// agentUX082UnionPlacementConversations appends the conversations where the
// agent is already placed to the preparation's targets, in stable order. A
// renewal reinstalls every placement at the new version; without the union the
// install loop covers only the spec's targets and a placement an owner added
// elsewhere stays on the lapsed version (AGENTUX-082: the first renewal on
// real data moved two of Policy Helper's four placements).
func agentUX082UnionPlacementConversations(targets, existing []string) []string {
	seen := make(map[string]bool, len(targets)+len(existing))
	out := make([]string, 0, len(targets)+len(existing))
	for _, conversation := range targets {
		if conversation == "" || seen[conversation] {
			continue
		}
		seen[conversation] = true
		out = append(out, conversation)
	}
	for _, conversation := range existing {
		if conversation == "" || seen[conversation] {
			continue
		}
		seen[conversation] = true
		out = append(out, conversation)
	}
	return out
}
