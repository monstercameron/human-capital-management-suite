package productui

// issue3974GeneralPurpose is the General agent's description. It points at the
// document picker ("add them below") only when the page draws one; otherwise it
// says plainly that the agent does not look in company documents.
func issue3974GeneralPurpose(locale LocaleContext, snapshot AgentSnapshot) string {
	if snapshot.DocumentHubAvailable {
		return locale.Text("agents.general_agent_purpose_documents")
	}
	return locale.Text("agents.general_agent_purpose")
}
