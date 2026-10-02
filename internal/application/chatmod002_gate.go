package application

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// checkGateText runs the free text a person types into a channel gate (the
// answers an applicant submits, and the reason a reviewer gives an applicant)
// through the workspace's language filters before the gate sees it. The
// refusal names the answer field, so the form can say which one.
func (s integrate2GateSurface) checkGateText(ctx context.Context, r ChatgateRequest) error {
	if s.Filter == nil {
		return nil
	}
	type text struct{ field, value string }
	var texts []text
	switch r.Action {
	case "submit":
		keys := make([]string, 0, len(r.Answers))
		for key := range r.Answers {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			for _, value := range gateAnswerStrings(r.Answers[key]) {
				texts = append(texts, text{key, value})
			}
		}
	case "admit", "decline", "bulk-admit", "bulk-decline":
		texts = append(texts, text{"reason", r.Reason})
	}
	if len(texts) == 0 {
		return nil
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil {
		return chatgate.ErrDenied
	}
	principal := chat.Principal{TenantID: p.Tenant().String(), SubjectID: p.Subject()}
	// A gate guards a channel, never a direct conversation, and an applicant may not
	// be able to read the channel yet: no lookup, so the answer reveals nothing
	// about a conversation the caller cannot see.
	c := chat.Conversation{ID: r.Conversation, TenantID: principal.TenantID, Kind: chat.PublicChannel}
	for _, t := range texts {
		if strings.TrimSpace(t.value) == "" {
			continue
		}
		if err := s.Filter.CheckFilterText(ctx, principal, c, t.value); err != nil {
			return chatgate.FieldError{Field: t.field, Cause: err}
		}
	}
	return nil
}

// gateAnswerStrings returns every string inside one JSON answer: a text answer
// is one string, a multiple choice is a list of them.
func gateAnswerStrings(raw json.RawMessage) []string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch typed := v.(type) {
		case string:
			out = append(out, typed)
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				walk(typed[key])
			}
		}
	}
	walk(value)
	return out
}
