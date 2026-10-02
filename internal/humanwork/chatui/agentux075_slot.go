package chatui

import "strconv"

// AGENTUX-075 part 3: the working message of a question and its answer (or its
// failure) are one element in one place. They carry one key, taken from the
// question and the place of the run among that question's runs, so that when the
// answer arrives the page replaces the working message in the same slot, without
// a row inserted or removed around it. A reload draws the same key, so the order
// holds. The key never depends on the run's identifier, which a waiting card
// that the page drew before the server reported ("pending:<question>") does not
// yet have.

// agentUX075SlotKey is the key of the reply slot of the ordinal-th run of a question.
func agentUX075SlotKey(postID string, ordinal int) string {
	return "agent-reply:" + postID + ":" + strconv.Itoa(ordinal)
}

// agentUX075Ordinal is the place of the invocation at index among the runs the
// page holds for the same question.
func agentUX075Ordinal(model Model, index int) int {
	if index < 0 || index >= len(model.PersonaInvocations) {
		return 0
	}
	postID, ordinal := model.PersonaInvocations[index].PostID, 0
	for _, earlier := range model.PersonaInvocations[:index] {
		if earlier.PostID == postID {
			ordinal++
		}
	}
	return ordinal
}

// agentUX075AnswerSlot is the reply slot of the invocation whose answer the
// message messageID is, in the person's own conversation with an agent (where the
// answer is an ordinary message of the agent's). ok is false for any other
// message.
func agentUX075AnswerSlot(model Model, messageID string) (string, bool) {
	if messageID == "" || !model.selected().Agent {
		return "", false
	}
	for index, invocation := range model.PersonaInvocations {
		projection := invocation.Projection
		if projection.DurablePostID != messageID || invocation.PostID == "" || projection.ViewerID != projection.InvokerID || (model.CurrentUser != "" && projection.ViewerID != model.CurrentUser) {
			continue
		}
		return agentUX075SlotKey(invocation.PostID, agentUX075Ordinal(model, index)), true
	}
	return "", false
}

// agentUX075MessageKey is the key of a timeline message: the reply slot of the
// run it answers, or its own.
func agentUX075MessageKey(model Model, messageID string) string {
	if slot, ok := agentUX075AnswerSlot(model, messageID); ok {
		return slot
	}
	return "message:" + messageID
}

// agentUX075AnswerOnPage reports whether the answer of an invocation is already
// on the page as a message of the agent's own conversation, so that its message
// carries the slot and the working row has nothing left to hold.
func agentUX075AnswerOnPage(model Model, invocation PersonaThreadInvocation) bool {
	answer := invocation.Projection.DurablePostID
	if answer == "" || !model.selected().Agent {
		return false
	}
	for _, message := range model.Messages {
		if message.ID == answer {
			return true
		}
	}
	return false
}
