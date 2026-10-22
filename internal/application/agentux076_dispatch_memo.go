package application

import (
	"context"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

// AGENTUX-076 (AGENTUX-028): the outbound verifier is asked about every field of
// one model request, one field at a time, and each ask used to rebuild everything
// the answer rests on: the admission record, the persona and manifest, the model
// route, the thread, the reference documents and the facts block. A request of
// eight fields did that eight times, a few database reads each.
//
// One request is one verification. The facts the fields are checked against are
// built once per request and kept on the request's context, which ends with the
// request; the next request, and the next step of the run, builds them again.

type openAIModelDispatchMemoKey struct{}

// openAIModelDispatchMemo is what one dispatch's field checks share.
type openAIModelDispatchMemo struct {
	mu      sync.Mutex
	ready   bool
	binding openAIModelDispatchBinding
	record  agentrun.Record
	route   PersonaRunModelRoute
	fields  map[string]personaAuthoritativeModelField
}

// withOpenAIModelDispatchMemo starts the memo for one dispatch.
func withOpenAIModelDispatchMemo(ctx context.Context) context.Context {
	return context.WithValue(ctx, openAIModelDispatchMemoKey{}, &openAIModelDispatchMemo{})
}

func openAIModelDispatchMemoFrom(ctx context.Context) *openAIModelDispatchMemo {
	memo, _ := ctx.Value(openAIModelDispatchMemoKey{}).(*openAIModelDispatchMemo)
	return memo
}

// get returns what an earlier field check of this dispatch built, for the same
// run and step.
func (m *openAIModelDispatchMemo) get(binding openAIModelDispatchBinding) (agentrun.Record, PersonaRunModelRoute, map[string]personaAuthoritativeModelField, bool) {
	if m == nil {
		return agentrun.Record{}, PersonaRunModelRoute{}, nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.ready || m.binding != binding {
		return agentrun.Record{}, PersonaRunModelRoute{}, nil, false
	}
	return m.record, m.route, m.fields, true
}

func (m *openAIModelDispatchMemo) put(binding openAIModelDispatchBinding, record agentrun.Record, route PersonaRunModelRoute, fields map[string]personaAuthoritativeModelField) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.ready, m.binding, m.record, m.route, m.fields = true, binding, record, route, fields
	m.mu.Unlock()
}
