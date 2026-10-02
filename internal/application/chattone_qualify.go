package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	modelopenai "github.com/monstercameron/human-capital-management-suite/internal/agentmodel/openai"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
)

// ErrChattoneNotQualified means a model did not pass the writing-style suite.
var ErrChattoneNotQualified = errors.New("application: model did not pass the chat writing style suite")

// ChattoneAgentVersionDigest is the version a model's evaluation is bound to:
// the exact instruction text a rewrite is made with. A deployment whose
// evaluation names another version is not eligible, so changing the
// instructions requires a new evaluation.
func ChattoneAgentVersionDigest() string {
	return "hcm-chat-writing-style/" + chatrewrite.InstructionDigest()[:16]
}

// ChattoneQualificationCase is one labelled draft and the style asked of it.
// Hostile lists words that must not survive a rewrite; Facts lists text that
// must survive exactly.
type ChattoneQualificationCase struct {
	ID      string   `json:"id"`
	Style   string   `json:"style"`
	Draft   string   `json:"draft"`
	Hostile []string `json:"hostile,omitempty"`
	Facts   []string `json:"facts,omitempty"`
}

type chattoneDraftLabel struct {
	draft   string
	hostile []string
	facts   []string
}

// chattoneSuiteDrafts are synthetic drafts: angry, rude, sarcastic or shouting
// messages with the facts a rewrite must keep. None is real workplace text.
var chattoneSuiteDrafts = []chattoneDraftLabel{
	{"You idiot, the deploy really broke prod again. @dana needs to fix 42 files by 2026-10-01. Please do it NOW.", []string{"idiot"}, []string{"@dana", "42", "2026-10-01"}},
	{"This report is garbage and nobody read the 3 pages of notes at https://example.com/notes before sending it.", []string{"garbage"}, []string{"3", "https://example.com/notes"}},
	{"I will not approve the $12,500 budget until Finance explains the gap. Stop wasting my time.", []string{"wasting my time"}, []string{"$12,500", "Finance"}},
	{"Why on earth is the build still red? Fix it or I will escalate to @morgan today.", nil, []string{"@morgan"}},
	{"Shut up about the schedule already. The launch is 2026-11-03 and that is final.", []string{"shut up"}, []string{"2026-11-03"}},
	{"Your pathetic excuse for a status update missed all 4 blockers on the board.", []string{"pathetic"}, []string{"4"}},
	{"Fix the typo on slide 7 before the meeting at 14:30, you useless clown.", []string{"useless", "clown"}, []string{"7", "14:30"}},
	{"No, this is not ready. The tests are a joke and the 12 failing cases prove it.", []string{"a joke"}, []string{"12"}},
	{"Du Idiot, das Deployment ist schon wieder kaputt. Bitte repariere es bis 2026-10-01.", []string{"idiot"}, []string{"2026-10-01"}},
	{"Eres un inútil, el informe tiene 5 errores otra vez y @lucia lo sabía.", []string{"inútil"}, []string{"5", "@lucia"}},
}

// ChattoneQualificationSuite is the labelled suite every writing-style model is
// evaluated on: each draft, in each of the three default styles.
func ChattoneQualificationSuite() []ChattoneQualificationCase {
	out := make([]ChattoneQualificationCase, 0, len(chattoneSuiteDrafts)*3)
	for i, d := range chattoneSuiteDrafts {
		for _, style := range chatrewrite.DefaultStyles() {
			out = append(out, ChattoneQualificationCase{ID: fmt.Sprintf("draft-%02d-%s", i+1, style.ID), Style: style.ID, Draft: d.draft, Hostile: d.hostile, Facts: d.facts})
		}
	}
	return out
}

// ChattoneCaseResult is one case's verdict. It holds a digest of the output,
// never the output.
type ChattoneCaseResult struct {
	ID           string `json:"id"`
	Style        string `json:"style"`
	Passed       bool   `json:"passed"`
	Reason       string `json:"reason,omitempty"`
	OutputDigest string `json:"output_digest,omitempty"`
}

// ChattoneQualification is the record of one evaluation run.
type ChattoneQualification struct {
	SuiteDigest  string                   `json:"suite_digest"`
	AgentVersion string                   `json:"agent_version"`
	Model        agentmodel.ModelIdentity `json:"model"`
	MeasuredAt   time.Time                `json:"measured_at"`
	Passed       bool                     `json:"passed"`
	CostMicros   int64                    `json:"cost_micros"`
	Cases        []ChattoneCaseResult     `json:"cases"`
}

// Evaluation is the evidence a deployment records for the model.
func (q ChattoneQualification) Evaluation() agentmodel.ModelEvaluation {
	return agentmodel.ModelEvaluation{AgentVersionDigest: q.AgentVersion, SuiteDigest: q.SuiteDigest, Passed: q.Passed}
}

func chattoneSuiteDigest(cases []ChattoneQualificationCase) string {
	encoded, _ := json.Marshal(struct {
		Cases       []ChattoneQualificationCase `json:"cases"`
		Instruction string                      `json:"instruction"`
	}{cases, chatrewrite.InstructionDigest()})
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// RunChattoneQualification runs every case through the real rewrite service,
// so the placeholder protection, the meaning guard, the length bounds and the
// outbound verifier all apply as they do in service, and then checks what the
// service cannot: that the facts survive verbatim, that no hostile word
// remains, that a rewrite differs from the draft, and that a "concise" rewrite
// is no longer than the draft. The run passes only if every case does. The
// service's model is the one under evaluation; its policy should allow the
// synthetic drafts.
func RunChattoneQualification(ctx context.Context, service *chatrewrite.Service, identity chatrewrite.Identity, model agentmodel.ModelIdentity, now func() time.Time) ChattoneQualification {
	cases := ChattoneQualificationSuite()
	q := ChattoneQualification{SuiteDigest: chattoneSuiteDigest(cases), AgentVersion: ChattoneAgentVersionDigest(), Model: model, MeasuredAt: now().UTC(), Passed: true}
	for _, c := range cases {
		result := ChattoneCaseResult{ID: c.ID, Style: c.Style}
		output, err := service.Rewrite(ctx, chatrewrite.Request{Identity: identity, Draft: c.Draft, StyleID: c.Style, Context: []string{"Keep replies short and direct"}})
		switch {
		case err != nil:
			result.Reason = "the service refused: " + err.Error()
		default:
			result.OutputDigest = "sha256:" + hex.EncodeToString(func() []byte { s := sha256.Sum256([]byte(output)); return s[:] }())
			result.Reason = chattoneCaseFailure(c, output)
			result.Passed = result.Reason == ""
		}
		if !result.Passed {
			q.Passed = false
		}
		q.Cases = append(q.Cases, result)
	}
	return q
}

func chattoneCaseFailure(c ChattoneQualificationCase, output string) string {
	lower := strings.ToLower(output)
	for _, fact := range c.Facts {
		if !strings.Contains(output, fact) {
			return "a fact did not survive: " + fact
		}
	}
	for _, word := range c.Hostile {
		if strings.Contains(lower, strings.ToLower(word)) {
			return "hostile wording remains: " + word
		}
	}
	if strings.TrimSpace(output) == strings.TrimSpace(c.Draft) {
		return "the rewrite is the draft"
	}
	if c.Style == "concise" && utf8.RuneCountInString(output) > utf8.RuneCountInString(c.Draft) {
		return "a concise rewrite is longer than the draft"
	}
	return ""
}

// chattoneLiveModel evaluates one provider model directly: it is the model
// under test, called without a deployment's routing because no deployment
// qualifies it yet. It sends only the suite's synthetic drafts.
type chattoneLiveModel struct {
	adapter  agentmodel.ModelAdapter
	profile  agentmodel.ModelProfile
	limits   agentmodel.ModelLimits
	timeout  time.Duration
	region   string
	retain   string
	training agentmodel.ProcessingUse
	logging  agentmodel.ProcessingUse
	mu       sync.Mutex
	cost     int64
}

func (m *chattoneLiveModel) Rewrite(ctx context.Context, p chatrewrite.Prompt) (string, error) {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", chatrewrite.ErrUnavailable
	}
	task, _, _ := ChattoneModelEvidenceProfile()
	request := agentmodel.ModelRequest{ContractVersion: agentmodel.ContractVersion, TaskProfile: task.ID, ModelProfile: m.profile.ID,
		Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleSystem, Content: p.Instruction}, {Role: agentmodel.RoleUser, Content: p.Data}},
		Output:   agentmodel.OutputConstraint{Mode: agentmodel.OutputText}, Deadline: time.Now().Add(m.timeout), Limits: m.limits, TraceID: "chattone-qualify-" + hex.EncodeToString(nonce[:]),
		Processing: agentmodel.ProcessingPolicy{Residency: m.region, Retention: m.retain, TrainingUse: m.training, Logging: m.logging}}
	callCtx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	result, err := m.adapter.Invoke(callCtx, request)
	m.mu.Lock()
	m.cost += result.Usage.CostMicros
	m.mu.Unlock()
	if err != nil || result.Failure != nil || result.Refusal != nil || result.Finish != agentmodel.FinishComplete || len(result.ToolProposals) != 0 {
		return "", chatrewrite.ErrUnavailable
	}
	return result.Text, nil
}

// ChattoneQualifyConfig is what a live evaluation needs: an approved base
// deployment (the provider, model identity, pricing and credential scopes to
// qualify the task on) and the provider key.
type ChattoneQualifyConfig struct {
	Base          PersonaModelDeployment
	APIKey        string
	MaxLatency    time.Duration
	MaxCostMicros int64
	Now           func() time.Time
}

// QualifyChattoneModel evaluates the base deployment's first model on the
// suite, calling the provider directly with synthetic drafts only. It returns
// the record of the run. When every case passes it also returns the approval
// document for the task, built with the run's own evidence; otherwise it
// returns ErrChattoneNotQualified and no document.
func QualifyChattoneModel(ctx context.Context, cfg ChattoneQualifyConfig) (ChattoneQualification, PersonaModelDeployment, error) {
	if strings.TrimSpace(cfg.APIKey) == "" || cfg.Now == nil || len(cfg.Base.Profiles) == 0 {
		return ChattoneQualification{}, PersonaModelDeployment{}, fmt.Errorf("%w: a provider key, a clock and a base deployment are required", errChattoneBinding)
	}
	pricing, _, err := cfg.Base.validate()
	if err != nil {
		return ChattoneQualification{}, PersonaModelDeployment{}, err
	}
	template := cfg.Base.Profiles[0]
	var terms *agentegress.ProviderTerms
	for i := range cfg.Base.Terms {
		if cfg.Base.Terms[i].ModelProfile == template.ID {
			terms = &cfg.Base.Terms[i]
		}
	}
	if terms == nil || len(terms.AllowedRegions) == 0 {
		return ChattoneQualification{}, PersonaModelDeployment{}, fmt.Errorf("%w: the template profile has no provider terms", errChattoneBinding)
	}
	selection := agentmodel.ModelSelection{ProfileID: template.ID, ProfileDigest: template.ProfileDigest, Identity: template.Identity}
	provider, err := modelopenai.New(modelopenai.Config{APIKey: cfg.APIKey, BaseURL: cfg.Base.BaseURL, ModelProfile: template.ID, Identity: template.Identity, PreflightInputTokens: false})
	if err != nil {
		return ChattoneQualification{}, PersonaModelDeployment{}, err
	}
	priced, err := agentmodel.NewPricedAdapter(provider, selection, pricing)
	if err != nil {
		return ChattoneQualification{}, PersonaModelDeployment{}, err
	}
	limits := agentmodel.ModelLimits{MaxInputTokens: chattoneMaxInputTokens, MaxOutputTokens: chattoneMaxOutputTokens, MaxCostMicros: cfg.MaxCostMicros}
	for limits.MaxOutputTokens > 500 && pricing.Validate(ctx, selection, limits) != nil {
		limits.MaxOutputTokens -= 250
	}
	live := &chattoneLiveModel{adapter: priced, profile: template, limits: limits, timeout: minDuration(cfg.MaxLatency, chattoneRequestTimeout), region: terms.AllowedRegions[0],
		retain: fmt.Sprintf("%s:%d", terms.Retention.Mode, int64(terms.Retention.MaxAge)), training: terms.TrainingUse, logging: terms.Logging}
	verifier, err := NewChattoneOutboundVerifier()
	if err != nil {
		return ChattoneQualification{}, PersonaModelDeployment{}, err
	}
	registry := chatrewrite.NewRegistry()
	service := &chatrewrite.Service{Registry: registry, Model: live, Policy: chattoneAllowAllPolicy{}, Meaning: ChattoneMeaningGuard{}, Outbound: verifier, Ledger: chatrewrite.NewMemoryLedger(1000), Now: cfg.Now}
	q := RunChattoneQualification(ctx, service, chatrewrite.Identity{Tenant: "qualification", Person: "qualification", Conversation: "qualification"}, template.Identity, cfg.Now)
	live.mu.Lock()
	q.CostMicros = live.cost
	live.mu.Unlock()
	if !q.Passed {
		return q, PersonaModelDeployment{}, ErrChattoneNotQualified
	}
	dep, err := NewChattoneDeployment(cfg.Base, q.Evaluation(), cfg.MaxLatency, cfg.MaxCostMicros)
	return q, dep, err
}

// chattoneAllowAllPolicy admits the suite's synthetic drafts; the evaluation is
// of the model, not of any workspace's filters.
type chattoneAllowAllPolicy struct{}

func (chattoneAllowAllPolicy) Accept(context.Context, chatrewrite.Identity, string) (bool, error) {
	return true, nil
}
