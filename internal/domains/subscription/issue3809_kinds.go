package subscription

// Chat event kinds share the managed subscription authorization and delivery
// contract. Payload schemas are registered by the chat webhook publisher in the
// application layer.
const (
	// EventChatMessagePosted reports a message committed to an allowed public channel.
	EventChatMessagePosted EventKind = "chat.message.posted"
	// EventChatApprovalDecided reports the decision on an approval raised in an allowed public channel.
	EventChatApprovalDecided EventKind = "chat.approval.decided"
)
