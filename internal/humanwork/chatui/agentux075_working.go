package chatui

import "strings"

// AGENTUX-075: the line of a working message says what the agent is doing now.
// The server reports each step of the run as a typed kind and, where one
// applies, the name of the thing it works on; this table turns the kind into the
// sentence the person reads, in their language. A kind it does not know leaves
// the generic line in place, so an unfamiliar word or an internal name never
// reaches the page.

// agentUX075ElapsedAfter is the number of seconds after which the working message
// shows how long the agent has been at it, and offers Stop to the asker.
const agentUX075ElapsedAfter = 10

// agentUX075Copy holds the working-step lines. Keys are "chat.agent.step.<kind>".
// {name} is a document title or the subject of a search; it is isolated for
// direction where it is put in.
var agentUX075Copy = map[string]map[string]string{
	"en-US": {
		"chat.agent.step.reading_question":  "Reading the question…",
		"chat.agent.step.searching":         "Searching documents…",
		"chat.agent.step.searching_for":     "Searching documents for {name}…",
		"chat.agent.step.reading_document":  "Reading {name}…",
		"chat.agent.step.reading_documents": "Reading the documents it found…",
		"chat.agent.step.writing":           "Writing the answer…",
		"chat.agent.step.working":           "Working on it…",
	},
	"de-DE": {
		"chat.agent.step.reading_question":  "Die Frage wird gelesen…",
		"chat.agent.step.searching":         "Dokumente werden durchsucht…",
		"chat.agent.step.searching_for":     "Dokumente werden nach {name} durchsucht…",
		"chat.agent.step.reading_document":  "{name} wird gelesen…",
		"chat.agent.step.reading_documents": "Die gefundenen Dokumente werden gelesen…",
		"chat.agent.step.writing":           "Die Antwort wird geschrieben…",
		"chat.agent.step.working":           "Wird bearbeitet…",
	},
	"ar": {
		"chat.agent.step.reading_question":  "جارٍ قراءة السؤال…",
		"chat.agent.step.searching":         "جارٍ البحث في المستندات…",
		"chat.agent.step.searching_for":     "جارٍ البحث في المستندات عن {name}…",
		"chat.agent.step.reading_document":  "جارٍ قراءة {name}…",
		"chat.agent.step.reading_documents": "جارٍ قراءة المستندات التي وُجدت…",
		"chat.agent.step.writing":           "جارٍ كتابة الإجابة…",
		"chat.agent.step.working":           "جارٍ العمل على ذلك…",
	},
}

func agentUX075Text(locale, key string) string {
	language := "en-US"
	switch lower := strings.ToLower(locale); {
	case strings.HasPrefix(lower, "de"):
		language = "de-DE"
	case strings.HasPrefix(lower, "ar"):
		language = "ar"
	}
	// The shared resolution refuses an empty, unlocalized or placeholder text and
	// falls back to English.
	return chatbug039Text(key, agentUX075Copy[language][key], agentUX075Copy["en-US"][key])
}

// agentUX075Named puts a name into a line, isolated so that a title in another
// script or direction does not reorder the words around it.
func agentUX075Named(locale, key, name string) string {
	return strings.ReplaceAll(agentUX075Text(locale, key), "{name}", "⁨"+name+"⁩")
}

// agentUX075Stage names the stage a server activity word belongs to, or "" for
// one that is not recognized. It serves a server that reports only the coarse
// activity.
func agentUX075Stage(activity string) string {
	switch strings.ToLower(strings.TrimSpace(activity)) {
	case "admission", "preparing context", "reading the question":
		return "reading_question"
	case "preparing answer", "searching":
		return "searching"
	case "reading sources", "reading documents":
		return "reading_documents"
	case "validating answer", "delivering answer", "writing the answer":
		return "writing"
	}
	return ""
}

// agentUX075StepLine is the sentence for the activity word a server without step
// kinds reported, or "" when there is none to show. A run that names the document
// it is reading ("Reading <title>") shows that title.
func agentUX075StepLine(locale, activity string) string {
	if title, found := strings.CutPrefix(strings.TrimSpace(activity), "Reading "); found && agentUX075Stage(activity) == "" && strings.TrimSpace(title) != "" {
		return agentUX075Named(locale, "chat.agent.step.reading_document", strings.TrimSpace(title))
	}
	if stage := agentUX075Stage(activity); stage != "" {
		return agentUX075Text(locale, "chat.agent.step."+stage)
	}
	return ""
}

// agentUX075ProgressLine is the working line for a run: from the step kind the
// server reported, else from its activity word, else "" (the generic line).
func agentUX075ProgressLine(locale string, progress PersonaProgressProps) string {
	subject := strings.TrimSpace(progress.StepSubject)
	switch kind := strings.TrimSpace(progress.StepKind); kind {
	case "reading_document":
		if subject != "" {
			return agentUX075Named(locale, "chat.agent.step.reading_document", subject)
		}
		return agentUX075Text(locale, "chat.agent.step.reading_documents")
	case "searching":
		if subject != "" {
			return agentUX075Named(locale, "chat.agent.step.searching_for", subject)
		}
		return agentUX075Text(locale, "chat.agent.step.searching")
	case "reading_question", "reading_documents", "writing", "working":
		return agentUX075Text(locale, "chat.agent.step."+kind)
	}
	return agentUX075StepLine(locale, progress.Activity)
}
