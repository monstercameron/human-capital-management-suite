package chat

import "strings"

type AgentAnswerFailure struct {
	Class, Sentence, NextStep string
	Retryable                 bool
}

// AgentAnswerTitleOnlyCode is the outcome of a run whose reply was nothing but
// the name of a document (or a citation of one). The reply check refuses such
// a reply; the run ends with this code so the card can say what happened.
const AgentAnswerTitleOnlyCode = "ANSWER_TITLE_ONLY"

// AgentAnswerFailureFor is the single closed mapping shared by Chat and the
// invocation boundary. Technical codes and provider details never become copy.
func AgentAnswerFailureFor(locale, agent, code string) AgentAnswerFailure {
	class := "unavailable"
	for _, row := range []struct{ class, codes string }{
		{"permission", "ADMISSION_REFUSED,INVALID_POST,NOT_CANONICAL,AUTHOR_NOT_HUMAN,AUTHOR_NOT_MEMBER,AUTHOR_NOT_IN_AUDIENCE,OUT_OF_SCOPE,PERMISSION_DENIED,AUTHORITY_REFUSED,GRANT_DENIED,CONTEXT_UNAVAILABLE,TOOL_POLICY_UNAVAILABLE,TOOL_PROPOSAL_UNAVAILABLE,TOOL_ADMISSION_FAILED,DELIVERY_AUDIENCE_DENIED,authority,grant,audience,tool_scope,delivery_audience"},
		{"nothing", "NOTHING_FOUND,NO_DOCUMENTS,NO_DOCUMENT,NO_RESULTS,NO_RESULT"},
		{"unavailable", "PERSONA_NOT_INSTALLED,PERSONA_NOT_CURRENT,PERSONA_SUSPENDED,NO_EFFECTIVE_SKILLS,MODEL_BINDING_INVALID,TOOL_EXECUTION_FAILED,MODEL_ROUTE_UNAVAILABLE,MODEL_NOT_CONFIGURED,VERSION_NOT_RUNNABLE,ADMISSION_UNAVAILABLE,EXECUTION_UNAVAILABLE,MODEL_RESULT_INVALID,installation,model_route"},
		// CHATBUG-049: an answer the agent wrote and the reply checks refused is
		// not an agent that is unavailable. The agent was there and answered; what
		// it wrote was not shown, and the card says so. A reply that was nothing
		// but a document's name has a sentence of its own.
		// AGENTUX-075: a server with no model at all says so, in its own sentence.
		{"no_model", "SERVER_HAS_NO_MODEL"},
		{"rejected", "OUTPUT_REJECTED,OUTPUT_BINDING_INVALID,output_grounding,output_schema"},
		{"title_only", AgentAnswerTitleOnlyCode},
		{"timeout", "MODEL_TIMEOUT,TIMEOUT,TIMED_OUT,EXPIRED,DEADLINE_EXCEEDED,deadline"},
		{"limit", "LIMIT_REACHED,BUDGET_EXCEEDED,MODEL_LIMIT,limit_exceeded,budget," + AgentMentionLimitCode},
		// AGENTCOST-006: an owner's daily spend limit has a sentence of its own, which says when it resets.
		{"daily_limit", "DAILY_LIMIT_REACHED"},
		{"interrupted", "ANSWER_INTERRUPTED,LEASE_LOST,RESTART_INTERRUPTED"},
		{"stopped", "CANCELLED,STOPPED,ADMIN_STOPPED,stopped"},
		// AGENTUX-051: INVOCATION_FAILED, a failure whose cause is not known, is not in
		// this row. It is not retryable, so it takes the default "unavailable"
		// sentence and never promises that trying again helps.
		{"service", "MODEL_UNAVAILABLE,MODEL_REFUSED_OR_INCOMPLETE,DELIVERY_FAILED,DELIVERY_RECEIPT_INVALID,model_call,model_output,tool_call,delivery_write"},
	} {
		for _, candidate := range strings.Split(row.codes, ",") {
			if strings.EqualFold(strings.TrimSpace(code), candidate) {
				class = row.class
				break
			}
		}
	}
	copy := map[string][3]string{
		"permission":  {"{agent} cannot answer this request here.", "Ask about a document it can read in this conversation.", "false"},
		"nothing":     {"{agent} found nothing about this in the documents it can read here.", "Try naming the document.", "false"},
		"unavailable": {"{agent} is not available in this conversation right now.", "Ask the person who looks after it to check its setup.", "false"},
		"no_model":    {"{agent} cannot answer because no model is configured on this server.", "Ask an administrator to connect a model.", "false"},
		"rejected":    {"{agent} wrote an answer that did not pass its checks, so it is not shown.", "Ask again, or ask about one thing at a time.", "false"},
		"title_only":  {"{agent} answered with only the name of a document, so the answer is not shown.", "Ask again, or ask what the document says.", "false"},
		"timeout":     {"{agent} took too long to answer.", "Try again.", "true"},
		"limit":       {"{agent} has reached the limit for this conversation.", "Try again after the limit resets.", "false"},
		"daily_limit": {"{agent} reached today's limit. It resets at 00:00.", "Try again tomorrow, or ask the person who looks after it to raise the limit.", "false"},
		"interrupted": {"{agent}'s answer was interrupted.", "Try again.", "true"},
		"stopped":     {"{agent}'s answer was stopped.", "Send a new question when you are ready.", "false"},
		"service":     {"{agent} could not answer because the service had a problem.", "Try again.", "true"},
	}
	if strings.HasPrefix(locale, "de") {
		copy = map[string][3]string{
			"permission":  {"{agent} kann diese Anfrage hier nicht beantworten.", "Fragen Sie nach einem Dokument, das der Agent in dieser Unterhaltung lesen darf.", "false"},
			"nothing":     {"{agent} hat dazu nichts in den hier lesbaren Dokumenten gefunden.", "Nennen Sie das Dokument.", "false"},
			"unavailable": {"{agent} ist in dieser Unterhaltung gerade nicht verfügbar.", "Bitten Sie die verantwortliche Person, die Einrichtung zu prüfen.", "false"},
			"no_model":    {"{agent} kann nicht antworten, weil auf diesem Server kein Modell eingerichtet ist.", "Bitten Sie eine Administratorin oder einen Administrator, ein Modell zu verbinden.", "false"},
			"rejected":    {"Die Antwort von {agent} hat die Prüfung nicht bestanden und wird deshalb nicht angezeigt.", "Fragen Sie erneut oder stellen Sie eine Frage nach der anderen.", "false"},
			"title_only":  {"{agent} hat nur mit dem Namen eines Dokuments geantwortet; die Antwort wird deshalb nicht angezeigt.", "Fragen Sie erneut oder fragen Sie, was im Dokument steht.", "false"},
			"timeout":     {"{agent} hat zu lange für die Antwort gebraucht.", "Versuchen Sie es erneut.", "true"},
			"limit":       {"{agent} hat das Limit für diese Unterhaltung erreicht.", "Versuchen Sie es nach dem Zurücksetzen des Limits erneut.", "false"},
			"daily_limit": {"{agent} hat das heutige Limit erreicht. Es wird um 00:00 Uhr zurückgesetzt.", "Versuchen Sie es morgen erneut oder bitten Sie die verantwortliche Person, das Limit zu erhöhen.", "false"},
			"interrupted": {"Die Antwort von {agent} wurde unterbrochen.", "Versuchen Sie es erneut.", "true"},
			"stopped":     {"Die Antwort von {agent} wurde gestoppt.", "Senden Sie eine neue Frage, wenn Sie bereit sind.", "false"},
			"service":     {"{agent} konnte wegen eines Dienstproblems nicht antworten.", "Versuchen Sie es erneut.", "true"},
		}
	} else if strings.HasPrefix(locale, "ar") {
		copy = map[string][3]string{
			"permission":  {"لا يمكن لـ {agent} الإجابة عن هذا الطلب هنا.", "اسأل عن مستند يمكنه قراءته في هذه المحادثة.", "false"},
			"nothing":     {"لم يجد {agent} معلومات عن ذلك في المستندات التي يمكنه قراءتها هنا.", "حاول تسمية المستند.", "false"},
			"unavailable": {"{agent} غير متاح في هذه المحادثة الآن.", "اطلب من المسؤول عنه التحقق من إعداداته.", "false"},
			"no_model":    {"لا يمكن لـ {agent} الإجابة لأنه لم يتم إعداد أي نموذج على هذا الخادم.", "اطلب من أحد المسؤولين ربط نموذج.", "false"},
			"rejected":    {"لم تجتز إجابة {agent} الفحص، لذلك لم تُعرض.", "اسأل مرة أخرى، أو اسأل عن أمر واحد في كل مرة.", "false"},
			"title_only":  {"أجاب {agent} باسم مستند فقط، لذلك لم تُعرض الإجابة.", "اسأل مرة أخرى، أو اسأل عمّا يقوله المستند.", "false"},
			"timeout":     {"استغرق {agent} وقتًا طويلًا للإجابة.", "حاول مرة أخرى.", "true"},
			"limit":       {"وصل {agent} إلى الحد المسموح به لهذه المحادثة.", "حاول بعد إعادة تعيين الحد.", "false"},
			"daily_limit": {"بلغ {agent} حد اليوم. يُعاد تعيينه عند 00:00.", "حاول غداً، أو اطلب من المسؤول عنه رفع الحد.", "false"},
			"interrupted": {"انقطعت إجابة {agent}.", "حاول مرة أخرى.", "true"},
			"stopped":     {"تم إيقاف إجابة {agent}.", "أرسل سؤالًا جديدًا عندما تكون مستعدًا.", "false"},
			"service":     {"تعذر على {agent} الإجابة بسبب مشكلة في الخدمة.", "حاول مرة أخرى.", "true"},
		}
	}
	selected := copy[class]
	return AgentAnswerFailure{Class: class, Sentence: strings.ReplaceAll(selected[0], "{agent}", agent), NextStep: selected[1], Retryable: selected[2] == "true"}
}
