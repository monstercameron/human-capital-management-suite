package chatui

// authorIsKnownPerson reports whether the model knows id as a person: the
// signed-in reader, a worker in the session's directory, or a member that is
// not an agent. An author the model does not know as a person is not one.
func (m Model) authorIsKnownPerson(id string) bool {
	if id == "" {
		return false
	}
	if id == m.CurrentUser {
		return true
	}
	for _, person := range m.SearchDirectory {
		if person.ID == id {
			return true
		}
	}
	for _, member := range m.Members {
		if member.ID == id && !member.Agent {
			return true
		}
	}
	return false
}

// unattestedAnnouncementIdentity draws a stored announcement as an agent's
// message from the first render, before the room's agent directory has said
// which agent wrote it, and when that directory never arrives. Its author is
// not a person the model knows, so the body is an announcement and never text
// to print. The name is a neutral agent label until the directory attests the
// author; a person's post with the same body is never given this identity.
func unattestedAnnouncementIdentity(model Model, message Message) Message {
	if message.PersonaActor != nil || model.authorIsKnownPerson(message.AuthorID) || message.AuthorID == "" {
		return message
	}
	if _, ok := DecodeAnnouncementMessageBody(message.Body); !ok {
		return message
	}
	message.PersonaActor = &PersonaActor{PersonaID: message.AuthorID, AgentID: message.AuthorID, Trusted: true}
	// CHATBUG-088: before the directory has said who wrote it the author line is
	// a neutral mark, never a name; once it has, an author it did not list keeps
	// the generic label.
	if agentDirectoryArrived(model) {
		message.Author = agentReplyFallback(model.Locale, "chat.agent.name", "Agent")
	} else {
		message.Author = ""
	}
	return message
}
