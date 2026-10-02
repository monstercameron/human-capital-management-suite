package chat

import "strings"

type AgentAnswerFailure struct {
	Class, Sentence, NextStep string
	Retryable                 bool
}

// AgentAnswerFailureFor is the single closed mapping shared by Chat and the
// invocation boundary. Technical codes and provider details never become copy.
func AgentAnswerFailureFor(locale, agent, code string) AgentAnswerFailure {
	class := "unavailable"
	for _, row := range []struct{ class, codes string }{
		{"permission", "ADMISSION_REFUSED,INVALID_POST,NOT_CANONICAL,AUTHOR_NOT_HUMAN,AUTHOR_NOT_MEMBER,AUTHOR_NOT_IN_AUDIENCE,OUT_OF_SCOPE,PERMISSION_DENIED,AUTHORITY_REFUSED,GRANT_DENIED,CONTEXT_UNAVAILABLE,TOOL_POLICY_UNAVAILABLE,TOOL_PROPOSAL_UNAVAILABLE,TOOL_ADMISSION_FAILED,DELIVERY_AUDIENCE_DENIED,authority,grant,audience,tool_scope,delivery_audience"},
		{"nothing", "NOTHING_FOUND,NO_DOCUMENTS,NO_DOCUMENT,NO_RESULTS,NO_RESULT"},
		{"unavailable", "PERSONA_NOT_INSTALLED,PERSONA_NOT_CURRENT,PERSONA_SUSPENDED,NO_EFFECTIVE_SKILLS,MODEL_BINDING_INVALID,TOOL_EXECUTION_FAILED,MODEL_ROUTE_UNAVAILABLE,MODEL_NOT_CONFIGURED,VERSION_NOT_RUNNABLE,ADMISSION_UNAVAILABLE,EXECUTION_UNAVAILABLE,OUTPUT_REJECTED,OUTPUT_BINDING_INVALID,MODEL_RESULT_INVALID,installation,model_route,output_grounding,output_schema"},
		{"timeout", "MODEL_TIMEOUT,TIMEOUT,TIMED_OUT,EXPIRED,DEADLINE_EXCEEDED,deadline"},
		{"limit", "LIMIT_REACHED,BUDGET_EXCEEDED,MODEL_LIMIT,DAILY_LIMIT_REACHED,limit_exceeded,budget"},
		{"interrupted", "ANSWER_INTERRUPTED,LEASE_LOST,RESTART_INTERRUPTED"},
		{"stopped", "CANCELLED,STOPPED,ADMIN_STOPPED,stopped"},
		{"service", "MODEL_UNAVAILABLE,MODEL_REFUSED_OR_INCOMPLETE,DELIVERY_FAILED,DELIVERY_RECEIPT_INVALID,INVOCATION_FAILED,model_call,model_output,tool_call,delivery_write"},
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
		"timeout":     {"{agent} took too long to answer.", "Try again.", "true"},
		"limit":       {"{agent} has reached the limit for this conversation.", "Try again after the limit resets.", "false"},
		"interrupted": {"{agent}'s answer was interrupted.", "Try again.", "true"},
		"stopped":     {"{agent}'s answer was stopped.", "Send a new question when you are ready.", "false"},
		"service":     {"{agent} could not answer because the service had a problem.", "Try again.", "true"},
	}
	if strings.HasPrefix(locale, "de") {
		copy = map[string][3]string{
			"permission":  {"{agent} kann diese Anfrage hier nicht beantworten.", "Fragen Sie nach einem Dokument, das der Agent in dieser Unterhaltung lesen darf.", "false"},
			"nothing":     {"{agent} hat dazu nichts in den hier lesbaren Dokumenten gefunden.", "Nennen Sie das Dokument.", "false"},
			"unavailable": {"{agent} ist in dieser Unterhaltung gerade nicht verfügbar.", "Bitten Sie die verantwortliche Person, die Einrichtung zu prüfen.", "false"},
			"timeout":     {"{agent} hat zu lange für die Antwort gebraucht.", "Versuchen Sie es erneut.", "true"},
			"limit":       {"{agent} hat das Limit für diese Unterhaltung erreicht.", "Versuchen Sie es nach dem Zurücksetzen des Limits erneut.", "false"},
			"interrupted": {"Die Antwort von {agent} wurde unterbrochen.", "Versuchen Sie es erneut.", "true"},
			"stopped":     {"Die Antwort von {agent} wurde gestoppt.", "Senden Sie eine neue Frage, wenn Sie bereit sind.", "false"},
			"service":     {"{agent} konnte wegen eines Dienstproblems nicht antworten.", "Versuchen Sie es erneut.", "true"},
		}
	} else if strings.HasPrefix(locale, "ar") {
		copy = map[string][3]string{
			"permission":  {"لا يمكن لـ {agent} الإجابة عن هذا الطلب هنا.", "اسأل عن مستند يمكنه قراءته في هذه المحادثة.", "false"},
			"nothing":     {"لم يجد {agent} معلومات عن ذلك في المستندات التي يمكنه قراءتها هنا.", "حاول تسمية المستند.", "false"},
			"unavailable": {"{agent} غير متاح في هذه المحادثة الآن.", "اطلب من المسؤول عنه التحقق من إعداداته.", "false"},
			"timeout":     {"استغرق {agent} وقتًا طويلًا للإجابة.", "حاول مرة أخرى.", "true"},
			"limit":       {"وصل {agent} إلى الحد المسموح به لهذه المحادثة.", "حاول بعد إعادة تعيين الحد.", "false"},
			"interrupted": {"انقطعت إجابة {agent}.", "حاول مرة أخرى.", "true"},
			"stopped":     {"تم إيقاف إجابة {agent}.", "أرسل سؤالًا جديدًا عندما تكون مستعدًا.", "false"},
			"service":     {"تعذر على {agent} الإجابة بسبب مشكلة في الخدمة.", "حاول مرة أخرى.", "true"},
		}
	}
	selected := copy[class]
	return AgentAnswerFailure{Class: class, Sentence: strings.ReplaceAll(selected[0], "{agent}", agent), NextStep: selected[1], Retryable: selected[2] == "true"}
}
