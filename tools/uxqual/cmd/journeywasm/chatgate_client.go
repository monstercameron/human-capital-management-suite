package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

type chatgateClientRequest struct {
	Reviews                                                                  []chatgate.ReviewDecision
	Conversation, Action, Key, Version, SubmissionID, Person, Reason, Locale string
	ExpectedRevision, SubmissionRevision                                     uint64
	Definition                                                               chatgate.Definition
	Answers                                                                  map[string]json.RawMessage
}

// chatgateListAction asks for the list of gates the person may know of. It is
// the client's own name for the read; the server takes it as GET ?list=1.
const chatgateListAction = "list"

type chatgateClientReply struct {
	View   *chatui.GateView  `json:"view"`
	Result json.RawMessage   `json:"result"`
	Error  string            `json:"error"`
	Fields map[string]string `json:"fields"`
}

func chatgateValues(d chatgate.Definition, values map[string][]string) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	for _, f := range d.Fields {
		xs, ok := values[f.ID]
		if !ok {
			continue
		}
		var v any
		switch f.Kind {
		case "multiple_choice":
			v = xs
		case "boolean", "acknowledgement":
			v = len(xs) > 0 && xs[0] == "true"
		default:
			if len(xs) == 0 {
				continue
			}
			if xs[0] == "" && !f.Required {
				continue
			}
			v = xs[0]
		}
		b, e := json.Marshal(v)
		if e != nil {
			return nil, e
		}
		out[f.ID] = b
	}
	return out, nil
}
func chatgateEditedDefinition(d chatgate.Definition, values map[string]string, checks map[string]bool) (chatgate.Definition, error) {
	d.Fields = append([]chatgate.Field(nil), d.Fields...)
	d.Purpose = values["gate-purpose"]
	d.Mode = values["gate-mode"]
	if date := values["gate-answer-by"]; date != "" {
		deadline, e := time.Parse("2006-01-02", date)
		if e != nil {
			return d, chatgate.ErrInvalid
		}
		d.AnswerBy = deadline
	}
	for i := range d.Fields {
		prefix := "gate-editor-" + strconv.Itoa(i) + "-"
		f := &d.Fields[i]
		f.Label = values[prefix+"label"]
		f.Help = values[prefix+"help"]
		f.Purpose = values[prefix+"purpose"]
		f.Kind = values[prefix+"kind"]
		if class := values[prefix+"class"]; class != "" {
			f.DataClass = class
		}
		if f.Kind == "acknowledgement" {
			var ref []string
			if json.Unmarshal([]byte(values[prefix+"document"]), &ref) != nil || len(ref) != 2 {
				return d, chatgate.ErrInvalid
			}
			f.DocumentID, f.DocumentVersion = ref[0], ref[1]
		}
		f.Required = checks[prefix+"required"]
		f.Visibility.Administrators = checks[prefix+"administrators"]
		f.Visibility.Members = checks[prefix+"members"]
		if consumers, ok := values[prefix+"consumers"]; ok {
			var selected []string
			if json.Unmarshal([]byte(consumers), &selected) != nil {
				return d, chatgate.ErrInvalid
			}
			f.Visibility.Consumers = selected
		}
		n, e := strconv.Atoi(values[prefix+"days"])
		if e != nil || n < 1 {
			return d, chatgate.ErrInvalid
		}
		f.RetentionDays = n
		f.Options = chatgateLines(values[prefix+"options"])
	}
	if d.Mode == "rule" {
		// The rule is the one the editor shows (chatui.GateRuleEdit); a rule the
		// service would refuse is refused here.
		return chatui.GateRuleApply(d, chatgateRuleEdit(values, checks))
	}
	d.Rules = nil
	return d, nil
}

// chatgateRuleEdit reads the rule editor's fields: the subject, the answers
// ticked among the known ones and the others typed one per line, the reason,
// and what happens to everyone else.
func chatgateRuleEdit(values map[string]string, checks map[string]bool) chatui.GateRuleEdit {
	edit := chatui.GateRuleEdit{Subject: values["gate-rule-field"], Reason: values["gate-rule-reason"], Else: values["gate-rule-else"]}
	for i := 0; ; i++ {
		id := "gate-rule-choice-" + strconv.Itoa(i)
		value, ok := values[id]
		if !ok {
			break
		}
		if checks[id] {
			edit.Values = append(edit.Values, value)
		}
	}
	edit.Values = append(edit.Values, chatgateLines(values["gate-rule-values"])...)
	return edit
}

// chatgateDraftDefinition is the builder's fields as a draft to draw from,
// whether or not its rule is finished. ok is false when a field of a question
// cannot be read at all.
func chatgateDraftDefinition(d chatgate.Definition, values map[string]string, checks map[string]bool) (chatgate.Definition, bool) {
	edited, err := chatgateEditedDefinition(d, values, checks)
	if err == nil {
		return edited, true
	}
	if values["gate-mode"] != "rule" {
		return d, false
	}
	// Everything but the rule, then the rule as it stands.
	withoutRule := map[string]string{}
	for key, value := range values {
		withoutRule[key] = value
	}
	withoutRule["gate-mode"] = "review"
	edited, err = chatgateEditedDefinition(d, withoutRule, checks)
	if err != nil {
		return d, false
	}
	edited.Mode = "rule"
	return chatui.GateRuleDraft(edited, chatgateRuleEdit(values, checks)), true
}

// chatgateJoins reads the server's list of gates into what the page keeps by
// conversation. ok is false for a list that cannot be read; the page then keeps
// what it had.
func chatgateJoins(raw json.RawMessage) (map[string]chatui.GateJoin, bool) {
	var rows []struct {
		Conversation   string    `json:"conversation"`
		Questions      int       `json:"questions"`
		Purpose        string    `json:"purpose"`
		ChannelPurpose string    `json:"channel_purpose"`
		Mode           string    `json:"mode"`
		AnswerAgain    bool      `json:"answer_again"`
		AnswerBy       time.Time `json:"answer_by"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &rows) != nil {
		return nil, false
	}
	out := make(map[string]chatui.GateJoin, len(rows))
	for _, row := range rows {
		if row.Conversation == "" || row.Questions <= 0 {
			continue
		}
		out[row.Conversation] = chatui.GateJoin{Questions: row.Questions, Purpose: row.Purpose, ChannelPurpose: row.ChannelPurpose, Mode: row.Mode, AnswerAgain: row.AnswerAgain, AnswerBy: row.AnswerBy}
	}
	return out, true
}

// chatgateKnownUngated reports whether the page already knows a conversation
// has no gate: the list of gates was read and does not hold it. Joining it
// then needs no question to the gate service first.
func chatgateKnownUngated(joins map[string]chatui.GateJoin, conversation string) bool {
	if joins == nil {
		return false
	}
	_, gated := joins[conversation]
	return !gated
}

// chatgateSubmitProblems checks the answers of a form before they are sent and
// returns, for each question that is wrong, the sentence that says so.
func chatgateSubmitProblems(locale string, d chatgate.Definition, answers map[string][]string) map[string]string {
	values, err := chatgateValues(d, answers)
	if err != nil {
		return nil
	}
	problems := map[string]string{}
	for field, key := range chatui.GateAnswerProblems(d, values) {
		problems[field] = chatui.GateText(locale, key)
	}
	return problems
}
func chatgateLines(s string) []string {
	out := []string{}
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}
func chatgateViewDefinition(v chatui.GateView) chatgate.Definition {
	if v.Administrator && v.Gate.Draft != nil {
		return *v.Gate.Draft
	}
	for _, d := range v.Gate.Versions {
		if d.Version.String() == v.Gate.Current {
			return d
		}
	}
	return chatgate.Definition{Mode: "review"}
}
func chatgateSuggestedVersion(v chatui.GateView) string {
	for _, old := range v.Gate.Versions {
		if old.Version.String() == v.Gate.Current {
			return chatgate.RequiredBump(old, chatgateViewDefinition(v)).String()
		}
	}
	return "1.0.0"
}
