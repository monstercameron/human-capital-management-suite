package application

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chattoneRewordContext is how many of the conversation's earlier messages the
// model reads, for register only.
const chattoneRewordContext = 4

// ChattoneRewordFacts is the part of the chat store the producer reads: the
// message a job is for and the messages before it.
type ChattoneRewordFacts interface {
	ChatlangJobFacts(ctx context.Context, tenant, post string, revision uint64) (chatstore.ChatlangJobFacts, error)
	ChatlangPreviousBodies(ctx context.Context, tenant, post string, n int) ([]string, error)
}

// ChattoneRewordProducer is the "reworded" kind of the rendering registry
// (chatrender.Reword). It produces a rendering from the original message with
// the one structured rewrite call, never stores it as the body, and fails
// closed: whatever it cannot do, the reader is given the message as written.
type ChattoneRewordProducer struct {
	Facts    ChattoneRewordFacts
	Reworder *chatrewrite.Reworder
	// Model is the identity recorded on the rendering; empty selects the default
	// model of the operation. It records what produced the text, it does not
	// choose the model.
	Model string
}

// Registration is the producer's entry in a rendering registry. The worker that
// runs rendering jobs registers it beside the translation producer; rewording
// always runs first when both are asked for.
func (p *ChattoneRewordProducer) Registration() chatrender.Registration {
	return chatrender.Registration{Kind: chatrender.Reword, Producer: p}
}

func (p *ChattoneRewordProducer) Produce(ctx context.Context, in chatrender.Rendering) (chatrender.Rendering, error) {
	if p == nil || p.Facts == nil || p.Reworder == nil {
		return chatrender.Rendering{}, chatrender.ErrUnavailable
	}
	facts, err := p.Facts.ChatlangJobFacts(ctx, in.Tenant, in.Message, in.Revision)
	if err != nil {
		return chatrender.Rendering{}, err
	}
	// A direct message is offered to the writer and never imposed on the reader.
	if facts.Direct {
		return chatrender.Rendering{}, chatrender.ErrDenied
	}
	original := chatui.AuthoredReaderText(facts.Body)
	var recent []string
	if bodies, err := p.Facts.ChatlangPreviousBodies(ctx, in.Tenant, in.Message, chattoneRewordContext); err == nil {
		for _, body := range bodies {
			// Earlier messages that would themselves read as abusive are not sent.
			if text := chatui.AuthoredReaderText(body); text != "" && chatrewrite.ScreenHeat(text) != chatrewrite.HeatAbusive {
				recent = append(recent, text)
			}
		}
	}
	result, err := p.Reworder.Reword(ctx, chatrewrite.RewordRequest{Identity: chatrewrite.Identity{Tenant: in.Tenant, Person: facts.Author, Conversation: facts.Conversation}, Text: original, Context: recent})
	if err != nil {
		return chatrender.Rendering{}, err
	}
	switch result.Outcome {
	case chatrewrite.RewordDone:
	case chatrewrite.RewordHardFilter:
		// The workspace's hard filters decide an abusive message, not this kind.
		return chatrender.Rendering{}, chatrender.ErrDenied
	default:
		return chatrender.Rendering{}, chatrender.ErrInvalid
	}
	model := p.Model
	if model == "" {
		model = agentmodel.ChattoneDefaultModel
	}
	in.Text = result.Text
	in.Tone = chatrender.Reworded
	in.Producer = chatrender.ProducerIdentity{Provider: "openai", Model: model, InstructionDigest: chatrewrite.InstructionDigest(), GlossaryVersion: "none"}
	in.Checks = chatrender.Checks{Meaning: true, Placeholders: true}
	in.Confidence = 0.99
	return in, nil
}

var _ chatrender.Producer = (*ChattoneRewordProducer)(nil)

// NewChattoneReworder puts the reword service together over the same governed
// model call, content policy, outbound verifier and meaning check the
// writing-style service uses. It keeps its own call counter: the platform's
// budget ledger behind the model call is what limits spend.
func NewChattoneReworder(model chatrewrite.Model, policy chatrewrite.Policy, decision chatrewrite.HeatDecision, now func() time.Time) (*chatrewrite.Reworder, error) {
	if isNilPersonaOutputPort(model) || isNilPersonaOutputPort(policy) {
		return nil, errors.Join(chatrewrite.ErrUnavailable, errors.New("a model and a content policy are required"))
	}
	verifier, err := NewChattoneOutboundVerifier()
	if err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	service := &chatrewrite.Service{Model: model, Policy: policy, Meaning: ChattoneMeaningGuard{}, Outbound: verifier, Ledger: chatrewrite.NewMemoryLedger(1 << 20), Now: now}
	var port chatrewrite.HeatDecision
	if !isNilPersonaOutputPort(decision) {
		port = decision
	}
	return &chatrewrite.Reworder{Service: service, Decision: port}, nil
}
