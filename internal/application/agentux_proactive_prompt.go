package application

// agentAnnouncementFormatMessage states the shape the reply validator accepts.
// Without it a model writing an announcement from a document reaches for a
// "Source:" line, a bracketed title or a link, each of which the validator
// refuses, so the announcement fails after the model has already been paid.
const agentAnnouncementFormatMessage = "Write the announcement as a plain chat message that everyone in the channel will read. " +
	"Use short sentences; put each item of a list on its own line starting with a hyphen. " +
	"How to cite here: end the message with one plain sentence naming the document in parentheses, for example (2026 holiday guide). Do not cite after each line. " +
	"Never write the word \"Source\" or \"Sources\", never use square brackets, web addresses or markup of any kind: the product lists the documents under your message itself, and a message that contains any of these is discarded. " +
	"Say only what the provided documents say. Today is the date in the goal, in TimeZone. For upcoming, coming up, next or remaining items, include only dates on or after Today. Compare each date to Today before answering; never include Labor Day on September 7 when Today is October 1. Leave past dates out unless the instruction explicitly asks for history."
