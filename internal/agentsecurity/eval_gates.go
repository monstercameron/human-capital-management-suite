package agentsecurity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// EvalFixture is one immutable safety/grounding/tool-selection check. All
// three signals must pass: a single failure grounds the release.
type EvalFixture struct {
	Name              string
	SafetyPass        bool
	GroundingPass     bool
	ToolSelectionPass bool
}

// Release pins the exact prompt, model and tool versions under evaluation.
// Any version change is a different release and must re-pass evaluation.
type Release struct {
	Agent          string
	AgentBuild     string
	AgentVersion   string
	PersonaID      string
	PersonaVersion string
	InstallationID string
	Model          string
	ModelDigest    string
	Tool           string
	ToolVersion    uint32
	Prompt         string
	PromptHash     string
}

// EvalRun is the immutable evaluation record gating publication.
type EvalRun struct {
	Release  Release
	Fixtures []EvalFixture
	Passed   bool
	Sequence uint64
	Digest   string
}

func evalDigest(release Release, fixtures []EvalFixture, passed bool, sequence uint64) string {
	names := make([]string, 0, len(fixtures))
	votes := make(map[string]string, len(fixtures))
	for _, fixture := range fixtures {
		names = append(names, fixture.Name)
		votes[fixture.Name] = fmt.Sprintf("safety=%v grounding=%v tool=%v", fixture.SafetyPass, fixture.GroundingPass, fixture.ToolSelectionPass)
	}
	sort.Strings(names)
	ordered := make([]string, 0, len(names))
	for _, name := range names {
		ordered = append(ordered, name+":"+votes[name])
	}
	bound := struct {
		Release  Release  `json:"release"`
		Fixtures []string `json:"fixtures"`
		Passed   bool     `json:"passed"`
		Sequence uint64   `json:"sequence"`
	}{release, ordered, passed, sequence}
	encoded, _ := json.Marshal(bound)
	sum := sha256.Sum256(append([]byte("hcm-next-agent-eval/v1\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Evaluator runs immutable eval runs under a monotonic sequence. No clock:
type Evaluator struct {
	mu       sync.Mutex
	sequence uint64
	runs     map[string]EvalRun
}

// NewEvaluator starts an empty evaluator.
func NewEvaluator() *Evaluator {
	return &Evaluator{runs: make(map[string]EvalRun)}
}

// Evaluate executes every fixture and seals the immutable run. Empty
// fixture sets never pass: unevaluated releases do not deploy.
func (e *Evaluator) Evaluate(release Release, fixtures []EvalFixture) (EvalRun, error) {
	if e == nil {
		return EvalRun{}, refusal(RefusalInvalid, "evaluator", "nil evaluator")
	}
	if strings.TrimSpace(release.Agent) == "" || strings.TrimSpace(release.Model) == "" || strings.TrimSpace(release.Tool) == "" || release.ToolVersion == 0 || strings.TrimSpace(release.PromptHash) == "" {
		return EvalRun{}, refusal(RefusalInvalid, "release", "agent, model, tool, tool version and prompt hash are required")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	passed := len(fixtures) > 0
	seen := make(map[string]bool, len(fixtures))
	for _, fixture := range fixtures {
		if strings.TrimSpace(fixture.Name) == "" || seen[fixture.Name] {
			return EvalRun{}, refusal(RefusalInvalid, "fixtures", "fixture names must be unique and non-empty")
		}
		seen[fixture.Name] = true
		if !fixture.SafetyPass || !fixture.GroundingPass || !fixture.ToolSelectionPass {
			passed = false
		}
	}
	e.sequence++
	run := EvalRun{Release: release, Fixtures: append([]EvalFixture(nil), fixtures...), Passed: passed, Sequence: e.sequence}
	run.Digest = evalDigest(release, run.Fixtures, passed, run.Sequence)
	e.runs[run.Digest] = run
	return run, nil
}

// Verify recomputes the run seal.
func (run EvalRun) Verify() error {
	if run.Digest == "" || evalDigest(run.Release, run.Fixtures, run.Passed, run.Sequence) != run.Digest {
		return refusal(RefusalOutput, "eval_run", "evaluation seal is broken")
	}
	return nil
}

// Publisher gates deployment on passing, sealed eval runs.
type Publisher struct {
	mu        sync.Mutex
	published map[string]EvalRun
}

// NewPublisher starts an empty publication gate.
func NewPublisher() *Publisher {
	return &Publisher{published: make(map[string]EvalRun)}
}

// Publish deploys one release behind its passing sealed run. Failed runs,
// broken seals and re-publication are refused.
func (p *Publisher) Publish(run EvalRun) error {
	if p == nil {
		return refusal(RefusalInvalid, "publisher", "nil publisher")
	}
	if err := run.Verify(); err != nil {
		return err
	}
	if !run.Passed {
		return refusal(RefusalCapability, "eval_run", "failed evaluation cannot deploy")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, dup := p.published[run.Digest]; dup {
		return refusal(RefusalInvalid, "eval_run", "release is already published")
	}
	p.published[run.Digest] = run
	return nil
}

// Published reports whether the sealed run is deployed.
func (p *Publisher) Published(digest string) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.published[digest]
	return ok
}

// DisableScope selects leases by the conjunction of its non-empty fields.
// An empty scope matches nothing; AllAI explicitly selects every lease.
type DisableScope struct {
	Agent          string
	AgentVersion   string
	PersonaID      string
	PersonaVersion string
	InstallationID string
	Model          string
	Tool           string
	Tenant         string
	AllAI          bool
}

// Lease is one revocable write-capable call binding.
type Lease struct {
	ID             string
	Agent          string
	AgentVersion   string
	PersonaID      string
	PersonaVersion string
	InstallationID string
	Model          string
	Tool           string
	Tenant         string
	WriteCapable   bool
	revoked        bool
}

// Fallback is the deterministic non-AI continuation returned when AI
// capability is disabled. Non-AI HCM always remains available.
type Fallback struct {
	Reason         string
	NonAIAvailable bool
}

// KillSwitch revokes leases by scope and fences later work with the
// deterministic fallback. RunStep serializes a step start with revocation.
type KillSwitch struct {
	mu       sync.Mutex
	leases   map[string]*Lease
	disabled []DisableScope
}

// NewKillSwitch starts an empty kill switch.
func NewKillSwitch() *KillSwitch {
	return &KillSwitch{leases: make(map[string]*Lease)}
}

// Grant registers one in-flight call. Duplicate lease IDs are refused.
func (k *KillSwitch) Grant(lease Lease) error {
	if k == nil {
		return refusal(RefusalInvalid, "kill_switch", "nil kill switch")
	}
	if strings.TrimSpace(lease.ID) == "" {
		return refusal(RefusalInvalid, "lease", "lease id is required")
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, dup := k.leases[lease.ID]; dup {
		return refusal(RefusalInvalid, "lease", "duplicate lease")
	}
	for _, scope := range k.disabled {
		if scopeMatches(scope, &lease) {
			return refusal(RefusalEffectClass, "lease", "disabled scope cannot grant a new lease")
		}
	}
	held := lease
	k.leases[lease.ID] = &held
	return nil
}

func scopeMatches(scope DisableScope, lease *Lease) bool {
	if scope.AllAI {
		return true
	}
	selected := false
	if scope.Agent != "" {
		selected = true
		if scope.Agent != lease.Agent {
			return false
		}
	}
	if scope.AgentVersion != "" {
		selected = true
		if scope.AgentVersion != lease.AgentVersion {
			return false
		}
	}
	if scope.PersonaID != "" {
		selected = true
		if scope.PersonaID != lease.PersonaID {
			return false
		}
	}
	if scope.PersonaVersion != "" {
		selected = true
		if scope.PersonaVersion != lease.PersonaVersion {
			return false
		}
	}
	if scope.InstallationID != "" {
		selected = true
		if scope.InstallationID != lease.InstallationID {
			return false
		}
	}
	if scope.Model != "" {
		selected = true
		if scope.Model != lease.Model {
			return false
		}
	}
	if scope.Tool != "" {
		selected = true
		if scope.Tool != lease.Tool {
			return false
		}
	}
	if scope.Tenant != "" {
		selected = true
		if scope.Tenant != lease.Tenant {
			return false
		}
	}
	return selected
}

// Disable revokes every lease in scope and returns the deterministic
// fallback. Once it returns, later step starts for matching leases are
// refused, and non-AI HCM stays available.
func (k *KillSwitch) Disable(scope DisableScope) (revoked int, fallback Fallback) {
	fallback = Fallback{Reason: "ai capability disabled", NonAIAvailable: true}
	if k == nil {
		return 0, fallback
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if scope != (DisableScope{}) {
		known := false
		for _, disabled := range k.disabled {
			if disabled == scope {
				known = true
				break
			}
		}
		if !known {
			k.disabled = append(k.disabled, scope)
		}
	}
	for _, lease := range k.leases {
		if !lease.revoked && scopeMatches(scope, lease) {
			lease.revoked = true
			revoked++
		}
	}
	return revoked, fallback
}

// Check admits one lease use. Revoked leases are refused with the
// deterministic fallback. Call before beginning a new unit of work.
func (k *KillSwitch) Check(id string) (Fallback, error) {
	if k == nil {
		return Fallback{}, refusal(RefusalInvalid, "kill_switch", "nil kill switch")
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	lease, ok := k.leases[id]
	if !ok {
		return Fallback{}, refusal(RefusalCapability, "lease", "unknown lease")
	}
	if lease.revoked {
		return Fallback{Reason: "lease revoked", NonAIAvailable: true}, refusal(RefusalEffectClass, "lease", "revoked lease cannot invoke")
	}
	return Fallback{Reason: "ai admitted", NonAIAvailable: true}, nil
}

// Invoke is the legacy name for Check.
func (k *KillSwitch) Invoke(id string) (Fallback, error) {
	return k.Check(id)
}

// RunStep starts one bounded unit of work only while its lease is active.
// The callback runs under the switch lock, so Disable and the step start
// have a single order: a running callback completes before Disable returns,
// or Disable returns first and the callback never starts. Callers must keep
// the callback to one step and must not call KillSwitch methods from it.
func (k *KillSwitch) RunStep(id string, step func() error) (Fallback, error) {
	if k == nil {
		return Fallback{}, refusal(RefusalInvalid, "kill_switch", "nil kill switch")
	}
	if step == nil {
		return Fallback{}, refusal(RefusalInvalid, "step", "step is required")
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	lease, ok := k.leases[id]
	if !ok {
		return Fallback{}, refusal(RefusalCapability, "lease", "unknown lease")
	}
	if lease.revoked {
		return Fallback{Reason: "lease revoked", NonAIAvailable: true}, refusal(RefusalEffectClass, "lease", "revoked lease cannot start a step")
	}
	if err := step(); err != nil {
		return Fallback{Reason: "step failed", NonAIAvailable: true}, err
	}
	return Fallback{Reason: "ai admitted", NonAIAvailable: true}, nil
}

// AIIncident links the exact versions, inputs, outputs and tool attempts
// behind one AI failure for audit.
type AIIncident struct {
	Release        Release
	InputDigest    string
	OutputDigest   string
	ToolAttempts   []string
	IncidentDigest string
}

// RecordIncident seals one incident over exact versioned material.
func RecordIncident(release Release, inputDigest, outputDigest string, attempts []string) (AIIncident, error) {
	if strings.TrimSpace(release.Agent) == "" || strings.TrimSpace(release.Model) == "" || strings.TrimSpace(inputDigest) == "" || strings.TrimSpace(outputDigest) == "" {
		return AIIncident{}, refusal(RefusalInvalid, "incident", "release, input digest and output digest are required")
	}
	bound := struct {
		Release      Release  `json:"release"`
		InputDigest  string   `json:"input_digest"`
		OutputDigest string   `json:"output_digest"`
		Attempts     []string `json:"attempts"`
	}{release, inputDigest, outputDigest, append([]string(nil), attempts...)}
	encoded, err := json.Marshal(bound)
	if err != nil {
		return AIIncident{}, refusal(RefusalInvalid, "incident", "incident material is not canonical")
	}
	sum := sha256.Sum256(append([]byte("hcm-next-ai-incident/v1\x00"), encoded...))
	return AIIncident{Release: release, InputDigest: inputDigest, OutputDigest: outputDigest, ToolAttempts: append([]string(nil), attempts...), IncidentDigest: "sha256:" + hex.EncodeToString(sum[:])}, nil
}
