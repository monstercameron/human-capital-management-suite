package chat

const AgentUXAmbientOffersChanged ConversationEventKind = "AMBIENT_OFFERS_CHANGED"

// An ambient event carries no card data. The authenticated watcher supplies
// the conversation; its client refreshes the recipient-filtered surface.
func AgentUXAmbientInvalidation(e ConversationEvent) bool {
	return e.Kind == AgentUXAmbientOffersChanged && e.Post == nil && e.Membership == nil && e.Conversation == nil && e.Reaction == nil && e.Pin == nil
}
