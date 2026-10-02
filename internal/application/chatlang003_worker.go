package application

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatlangAttempts is how many times one job asks the engine before the
// rendering is discarded: a model that lost a placeholder once is asked once more.
const chatlangAttempts = 2

// chatlangPermanent remembers, in process, the jobs that retrying cannot help.
// The producer, authority and ledger mark a job; the job store reads the mark
// when the worker reports the failure, and ends the job for good.
type chatlangPermanent struct {
	mu      sync.Mutex
	reasons map[string]string
}

func chatlangJobKey(r chatrender.Rendering) string {
	return r.Tenant + "\x00" + r.Message + "\x00" + strconv.FormatUint(r.Revision, 10) + "\x00" + string(r.Tone) + "\x00" + r.Language
}
func (p *chatlangPermanent) mark(r chatrender.Rendering, reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.reasons == nil {
		p.reasons = map[string]string{}
	}
	p.reasons[chatlangJobKey(r)] = reason
}
func (p *chatlangPermanent) take(r chatrender.Rendering) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	reason, ok := p.reasons[chatlangJobKey(r)]
	delete(p.reasons, chatlangJobKey(r))
	return reason, ok
}

// chatlangJobs is the worker's job store: the chat store, except that a job
// marked permanent is ended instead of requeued.
type chatlangJobs struct {
	*chatstore.Store
	perm *chatlangPermanent
}

func (j chatlangJobs) FailRendering(ctx context.Context, job chatrender.Job) error {
	if reason, ok := j.perm.take(job.Request); ok {
		return j.Store.FailRenderingPermanently(ctx, job, reason)
	}
	return j.Store.FailRendering(ctx, job)
}

// ChatlangScreen decides whether text may be sent to an engine, and whether an
// engine's answer may be shown, under the workspace's message filters. A
// message the filters would mask for readers is never sent out, and a
// translation the filters would mask is never shown.
type ChatlangScreen interface {
	Clean(ctx context.Context, facts chatstore.ChatlangJobFacts, text string) (bool, error)
}

// ChatlangFilterScreen applies the workspace's message filters.
type ChatlangFilterScreen struct{ Filters *chatfilter.Service }

func (s ChatlangFilterScreen) Clean(ctx context.Context, f chatstore.ChatlangJobFacts, text string) (bool, error) {
	if s.Filters == nil {
		return false, chatlang.ErrUnavailable
	}
	// The author's own exemptions are deliberately not applied: text that any
	// reader would see masked does not leave the deployment.
	result, err := s.Filters.Evaluate(ctx, chatfilter.Input{Tenant: f.Tenant, Channel: f.Conversation, Subject: f.Author, Body: text, Direct: f.Direct}, false)
	if err != nil {
		return false, err
	}
	return result.Masked == text && result.Refusal() == nil, nil
}

// ChatlangProducer is the translation rendering producer: the engine port
// behind the rendering jobs of CHATRENDER-001. It protects what must not be
// translated, calls the engine, checks and restores, and writes one usage line
// per call.
type ChatlangProducer struct {
	Engine chatlang.Engine
	Store  *chatstore.Store
	Screen ChatlangScreen
	perm   *chatlangPermanent
	Now    func() time.Time
}

func (p *ChatlangProducer) usage(ctx context.Context, in chatrender.Rendering, response chatlang.Response, outcome string) error {
	provider, model := response.Provider, response.Model
	if provider == "" {
		provider, model = "unknown", "unknown"
	}
	at := time.Now().UTC()
	if p.Now != nil {
		at = p.Now().UTC()
	}
	return p.Store.RecordChatlangUsage(ctx, chatstore.ChatlangUsage{Tenant: in.Tenant, Message: in.Message, Revision: in.Revision, Language: in.Language, Provider: provider, Model: model,
		InstructionDigest: chatlangInstructionDigest(response), InputTokens: response.InputTokens, OutputTokens: response.OutputTokens, CostMicros: response.CostMicros, Outcome: outcome, At: at})
}

// Produce implements chatrender.Producer for the translated kind.
func (p *ChatlangProducer) Produce(ctx context.Context, in chatrender.Rendering) (chatrender.Rendering, error) {
	if p == nil || p.Engine == nil || p.Store == nil || p.Screen == nil || p.perm == nil {
		return chatrender.Rendering{}, chatrender.ErrUnavailable
	}
	source, target := chatrender.Language(in.SourceLanguage), chatrender.Language(in.Language)
	// An engine that detects (the structured one) can place a message whose
	// language is not recorded; one that does not cannot translate it.
	detecting := false
	if d, ok := p.Engine.(interface{ Detects() bool }); ok {
		detecting = d.Detects()
	}
	if source == "" || (source == "und" && !detecting) || source == target || !chatrender.Supported(target) {
		p.perm.mark(in, "nothing to translate")
		return chatrender.Rendering{}, chatrender.ErrInvalid
	}
	facts, err := p.Store.ChatlangJobFacts(ctx, in.Tenant, in.Message, in.Revision)
	if err != nil {
		return chatrender.Rendering{}, err
	}
	original := chatui.AuthoredReaderText(facts.Body)
	workspace, err := p.Store.ChatlangWorkspace(ctx, in.Tenant)
	if err != nil {
		return chatrender.Rendering{}, err
	}
	glossary, _, err := p.Store.ChatlangGlossary(ctx, in.Tenant)
	if err != nil {
		return chatrender.Rendering{}, err
	}
	protected := chatlang.Protect(original, glossary, target)
	if !protected.HasLetters() {
		p.perm.mark(in, "nothing to translate")
		return chatrender.Rendering{}, chatlang.ErrNothingToTranslate
	}
	context := p.context(ctx, in, facts)
	request := chatlang.Request{Tenant: in.Tenant, Message: in.Message, Revision: in.Revision, Source: source, Target: target, Formality: workspace.Formality[target], Text: protected.Text(), Context: context}
	var restored string
	var response chatlang.Response
	var failures []string
	attempts := 0
	for attempts < chatlangAttempts {
		attempts++
		response, err = p.Engine.Translate(ctx, request)
		if err != nil {
			if lineErr := p.usage(ctx, in, chatlang.Response{}, "failed"); lineErr != nil {
				return chatrender.Rendering{}, lineErr
			}
			if errors.Is(err, chatlang.ErrBudget) || errors.Is(err, chatlang.ErrBarred) {
				p.perm.mark(in, err.Error())
			}
			return chatrender.Rendering{}, err
		}
		// The engine found the text already in the reader's language, or in none:
		// there is nothing to show. The call is still a usage line.
		if language := chatrender.Language(response.DetectedSource); response.DetectedSource != "" && (language == target || language == "und") {
			if lineErr := p.usage(ctx, in, response, "discarded"); lineErr != nil {
				return chatrender.Rendering{}, lineErr
			}
			p.perm.mark(in, "nothing to translate")
			return chatrender.Rendering{}, chatlang.ErrNothingToTranslate
		}
		if response.MeaningChecked && !response.MeaningPreserved {
			// The model itself does not vouch for the meaning: not shown, and asked once more.
			failures = []string{"the engine does not vouch for the meaning"}
		} else {
			failures = protected.Verify(response.Text)
			if len(failures) == 0 {
				restored, failures, err = protected.Restore(response.Text)
			}
		}
		if len(failures) == 0 {
			break
		}
		// A call that cost money and gave nothing usable is still a line.
		if lineErr := p.usage(ctx, in, response, "discarded"); lineErr != nil {
			return chatrender.Rendering{}, lineErr
		}
	}
	if len(failures) > 0 {
		p.perm.mark(in, "checks failed")
		return chatrender.Rendering{}, fmt.Errorf("%w: %v", chatrender.ErrInvalid, failures)
	}
	clean, err := p.Screen.Clean(ctx, facts, restored)
	if err != nil {
		return chatrender.Rendering{}, err
	}
	if !clean {
		_ = p.usage(ctx, in, response, "discarded")
		p.perm.mark(in, "translation filtered")
		return chatrender.Rendering{}, chatrender.ErrDenied
	}
	if err = p.usage(ctx, in, response, "translated"); err != nil {
		return chatrender.Rendering{}, err
	}
	confidence := response.Confidence
	if confidence <= 0 || confidence > 1 {
		confidence = 0.9
	}
	if attempts > 1 {
		confidence = 0.6
	}
	in.Text = restored
	in.Producer = chatrender.ProducerIdentity{Provider: response.Provider, Model: response.Model, InstructionDigest: chatlangInstructionDigest(response), GlossaryVersion: "glossary-v" + strconv.FormatInt(glossary.Version, 10)}
	in.Checks = chatrender.Checks{Meaning: true, Placeholders: true}
	in.Confidence = confidence
	return in, nil
}

// context returns the previous few messages the engine reads so a short reply
// is understood. Anything the filters would mask is left out; a failure to
// read context leaves it empty rather than failing the translation.
func (p *ChatlangProducer) context(ctx context.Context, in chatrender.Rendering, facts chatstore.ChatlangJobFacts) []string {
	bodies, err := p.Store.ChatlangPreviousBodies(ctx, in.Tenant, in.Message, chatlang.MaxContext)
	if err != nil {
		return nil
	}
	var out []string
	for _, body := range bodies {
		text := chatui.AuthoredReaderText(body)
		if clean, err := p.Screen.Clean(ctx, facts, text); err == nil && clean && text != "" {
			out = append(out, text)
		}
	}
	return out
}

// ChatlangAuthority rechecks, before the engine is called and again before the
// rendering is stored, that the message still exists at this revision, that the
// workspace and channel still allow translation into this language by this
// engine, and that the filters would not mask the original.
type ChatlangAuthority struct {
	Governance *ChatlangGovernance
	Screen     ChatlangScreen
	perm       *chatlangPermanent
}

func (a ChatlangAuthority) AuthorizeRenderingJob(ctx context.Context, job chatrender.Job) error {
	r := job.Request
	if a.Governance == nil || a.Governance.Store == nil || a.Screen == nil || a.perm == nil {
		return chatrender.ErrUnavailable
	}
	facts, err := a.Governance.Store.ChatlangJobFacts(ctx, r.Tenant, r.Message, r.Revision)
	if err != nil {
		return err
	}
	reason, err := a.Governance.Permit(ctx, r.Tenant, facts.Conversation, chatrender.Language(r.Language))
	if err != nil {
		return err
	}
	if reason != chatlang.Allowed {
		a.perm.mark(r, string(reason))
		return fmt.Errorf("%w: %s", chatrender.ErrDenied, reason)
	}
	// CHATLANG-006: a pair the quality gate withholds is not translated.
	if a.Governance.PairDecision(r.SourceLanguage, r.Language) == chatlang.GateWithhold {
		a.perm.mark(r, "quality gate")
		return fmt.Errorf("%w: quality gate", chatrender.ErrDenied)
	}
	clean, err := a.Screen.Clean(ctx, facts, chatui.AuthoredReaderText(facts.Body))
	if err != nil {
		return err
	}
	if !clean {
		a.perm.mark(r, "original filtered")
		return chatrender.ErrDenied
	}
	return nil
}

// ChatlangUsageLedger reserves against the workspace's monthly limit before a
// call. The usage lines themselves are written by the producer in the same step
// as each engine call, so no caller can forget one; this ledger only refuses a
// call once the month's spend has reached the limit, and quietly: the job ends
// and the reader keeps the original.
type ChatlangUsageLedger struct {
	Governance *ChatlangGovernance
	perm       *chatlangPermanent
}

func (l ChatlangUsageLedger) ReserveRendering(ctx context.Context, job chatrender.Job) (string, error) {
	r := job.Request
	if l.Governance == nil || r.Message == "" {
		return "", chatrender.ErrInvalid
	}
	spent, limit, err := l.Governance.Budget(ctx, r.Tenant)
	if err != nil {
		return "", err
	}
	if spent >= limit {
		l.perm.mark(r, "monthly limit reached")
		return "", chatlang.ErrBudget
	}
	return "chatlang:" + r.Tenant + ":" + r.Message + ":" + strconv.FormatUint(r.Revision, 10) + ":" + r.Language + ":" + uuid.NewString(), nil
}
func (ChatlangUsageLedger) RecordRendering(_ context.Context, reference string, _ chatrender.Rendering) error {
	if reference == "" {
		return chatrender.ErrInvalid
	}
	return nil
}

// ChatlangRuntime runs translation jobs for the served tenants.
type ChatlangRuntime struct {
	Worker      chatrender.Worker
	Tenants     []string
	Lease       time.Duration
	Poll        time.Duration
	Concurrency int
	Logger      interface{ Error(string, ...any) }
}

// NewChatlangRuntime puts the worker together over the chat store, the
// governance, an engine and the filters.
func NewChatlangRuntime(store *chatstore.Store, governance *ChatlangGovernance, engine chatlang.Engine, screen ChatlangScreen, now func() time.Time, tenants []string, extra ...chatrender.Registration) (*ChatlangRuntime, error) {
	if store == nil || governance == nil || engine == nil || screen == nil {
		return nil, chatrender.ErrUnavailable
	}
	perm := &chatlangPermanent{}
	producer := &ChatlangProducer{Engine: engine, Store: store, Screen: screen, perm: perm, Now: now}
	// extra registers other kinds beside the translation producer (the reword
	// producer of CHATTONE, which runs first when both are asked for).
	registry, err := chatrender.NewRegistry(append([]chatrender.Registration{{Kind: chatrender.Translate, Producer: producer}}, extra...)...)
	if err != nil {
		return nil, err
	}
	return &ChatlangRuntime{
		Worker: chatrender.Worker{Jobs: chatlangJobs{Store: store, perm: perm}, Registry: registry,
			Authority: ChatlangAuthority{Governance: governance, Screen: screen, perm: perm}, Usage: ChatlangUsageLedger{Governance: governance, perm: perm}},
		Tenants: tenants, Lease: time.Minute, Poll: 100 * time.Millisecond, Concurrency: 8,
	}, nil
}

// Drain runs jobs for one tenant until none is waiting and returns how many it
// ran. A job's own failure is not an error here: the worker has already
// recorded it and the reader falls back to the original.
func (r *ChatlangRuntime) Drain(ctx context.Context, tenant string) (int, error) {
	ran := 0
	for ctx.Err() == nil {
		err := r.Worker.RunOne(ctx, tenant, r.Lease)
		if errors.Is(err, dbport.ErrNoRows) {
			return ran, nil
		}
		if errors.Is(err, chatrender.ErrUnavailable) && r.Worker.Jobs == nil {
			return ran, err
		}
		ran++
	}
	return ran, ctx.Err()
}

// Run polls until the context ends. Several loops per tenant claim jobs with
// row locks that skip each other, so a page of history translates in parallel.
func (r *ChatlangRuntime) Run(ctx context.Context) error {
	if r == nil {
		return chatrender.ErrUnavailable
	}
	poll := r.Poll
	if poll <= 0 {
		poll = 100 * time.Millisecond
	}
	var wg sync.WaitGroup
	for _, tenant := range r.Tenants {
		for i := 0; i < max(r.Concurrency, 1); i++ {
			wg.Add(1)
			// The first loop polls at the base interval and each further loop a
			// little slower, so an idle workspace costs a few queries a second
			// and a busy one still has every loop working.
			go func(idle time.Duration) {
				defer wg.Done()
				for ctx.Err() == nil {
					err := r.Worker.RunOne(ctx, tenant, r.Lease)
					wait := time.Duration(0)
					switch {
					case err == nil:
					case errors.Is(err, dbport.ErrNoRows):
						wait = idle
					default:
						// The job's own failure has been recorded; pause briefly so
						// a failing dependency is not retried in a hot loop.
						wait = 50 * time.Millisecond
					}
					if wait > 0 {
						select {
						case <-ctx.Done():
						case <-time.After(wait):
						}
					}
				}
			}(poll * time.Duration(i+1))
		}
	}
	wg.Wait()
	return ctx.Err()
}

// chatlangInstructionDigest is the digest of the instruction behind a response:
// the engine's own when it says which, otherwise the text instruction.
func chatlangInstructionDigest(response chatlang.Response) string {
	if response.InstructionDigest != "" {
		return response.InstructionDigest
	}
	return chatlang.InstructionDigest()
}
