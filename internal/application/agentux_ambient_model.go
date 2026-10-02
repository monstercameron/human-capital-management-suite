package application

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strconv"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// Like the support-email planner, this port must use an admitted run's pinned
// model route and budget, with sourceKey as its immutable model-step identity.
// It may propose a draft; it never owns task creation or reminder scheduling.
type AgentUXAmbientRunSource struct {
	Kind, Tenant, Conversation, Post, Author, Parent, ReadDigest string
	Revision                                                     uint64
}

type AgentUXAmbientRunPlanner interface {
	PlanAmbientMessage(context.Context, AgentUXAmbientRunSource, string, string, []agentmodel.ModelMessage) ([]byte, error)
}

type AgentUXAmbientModelAdapter struct{ Planner AgentUXAmbientRunPlanner }

func AgentUXAmbientModelMessages(agent string, input AgentUXAmbientModelInput) ([]agentmodel.ModelMessage, error) {
	m := input.Message
	if !input.Untrusted || input.SourceKind != "CHAT_MESSAGE" || AgentUXAmbientInstructions(agent) == "" || m.ID == "" || m.Tenant == "" || m.Conversation == "" || m.Author == "" || m.Revision == 0 || !AgentUXAmbientScreen(m.Body, agent) || len(input.ThreadParent) > 8000 || !utf8.ValidString(input.ThreadParent) || m.Parent == "" && input.ThreadParent != "" {
		return nil, ErrAgentUXAmbientDenied
	}
	raw, err := json.Marshal(struct {
		Message string `json:"message"`
		Parent  string `json:"thread_parent,omitempty"`
		Zone    string `json:"author_time_zone"`
	}{m.Body, input.ThreadParent, m.Zone})
	if err != nil {
		return nil, err
	}
	return []agentmodel.ModelMessage{
		{Role: agentmodel.RoleDeveloper, Content: AgentUXAmbientInstructions(agent)},
		{Role: agentmodel.RoleUser, Content: agentDocumentReferenceDataBegin + "\n" + string(raw) + "\n" + agentDocumentReferenceDataEnd},
	}, nil
}

func (a AgentUXAmbientModelAdapter) ExtractAmbient(ctx context.Context, agent string, input AgentUXAmbientModelInput) (AgentUXAmbientProposal, error) {
	var p AgentUXAmbientProposal
	if a.Planner == nil {
		return p, ErrAgentUXAmbientDenied
	}
	messages, err := AgentUXAmbientModelMessages(agent, input)
	if err != nil {
		return p, err
	}
	key := agentUXAmbientID(input.Message.Tenant, input.Message.ID, agent, strconv.FormatUint(input.Message.Revision, 10))
	m := input.Message
	source := AgentUXAmbientRunSource{Kind: "CHAT_MESSAGE", Tenant: m.Tenant, Conversation: m.Conversation, Post: m.ID, Author: m.Author, Parent: m.Parent, Revision: m.Revision, ReadDigest: personaRunBytesDigest([]byte(messages[1].Content))}
	raw, err := a.Planner.PlanAmbientMessage(ctx, source, agent, key, messages)
	if err != nil {
		return p, err
	}
	if len(raw) == 0 || len(raw) > 8192 || !utf8.Valid(raw) {
		return p, ErrAgentUXAmbientInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 7 {
		return p, ErrAgentUXAmbientInvalid
	}
	for _, name := range []string{"kind", "title", "owner", "date", "clock", "event", "explicit"} {
		if value, found := fields[name]; !found || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return p, ErrAgentUXAmbientInvalid
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&p) != nil || decoder.Decode(&struct{}{}) != io.EOF || len(p.Title) > 1000 || len(p.Owner) > 150 || len(p.Date) > 64 || len(p.Clock) > 16 || p.Kind != "" && p.Kind != "TASK" && p.Kind != "REMINDER" || p.Event != "" && p.Event != "DEADLINE" && p.Event != "MEETING" {
		return AgentUXAmbientProposal{}, ErrAgentUXAmbientInvalid
	}
	return p, nil
}
