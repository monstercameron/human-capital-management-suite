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
		field := values["gate-rule-field"]
		allowed := chatgateLines(values["gate-rule-values"])
		reason := strings.TrimSpace(values["gate-rule-reason"])
		if field == "" || len(allowed) == 0 || reason == "" {
			return d, chatgate.ErrInvalid
		}
		d.Rules = []chatgate.Rule{{When: chatgate.Expression{Operator: "in", Field: field, Values: allowed}, Outcome: "admitted", Reason: reason}, {When: chatgate.Expression{Operator: "not", Children: []chatgate.Expression{{Operator: "in", Field: field, Values: allowed}}}, Outcome: "declined", Reason: reason}}
	} else {
		d.Rules = nil
	}
	return d, nil
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
