package main

// rolloutPreviewFirstConversations returns the conversations a staged rollout
// updates first. The server refuses a preview that names none. One selected
// conversation is its own first step; with several, the chosen ones are kept
// and the first selected stands in when none is chosen.
func rolloutPreviewFirstConversations(selected, chosen []string) []string {
	if len(selected) == 0 {
		return chosen
	}
	kept := make([]string, 0, len(chosen))
	for _, id := range chosen {
		for _, candidate := range selected {
			if id == candidate {
				kept = append(kept, id)
				break
			}
		}
	}
	if len(kept) > 0 {
		return kept
	}
	return []string{selected[0]}
}
