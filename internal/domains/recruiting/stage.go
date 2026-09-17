// Governed candidacy stages: RECRUIT-002 owns the screening,
// assessment, interview and evidence chronology consumed by recruiting
// intents. Assessment and appointment providers stay adapters: their
// results enter this ledger as observations only, and a candidacy
// advances exclusively on an authorized human decision receipt.
//
// Every advance is a compare-and-swap over the stage, requisition and
// candidacy revisions plus the required evidence and decision: a stale
// revision, a skipped mandatory stage, a changed score, protected
// evidence without clearance and a provider authority masquerading as a
// decision are all refused with a typed code and append nothing.
// Exactly one stage frontier is current per candidacy. Scores are
// append-only: a correction adds a successor revision and preserves the
// original. The clock is injected by the caller, so the ledger is pure:
// no database, no wall clock, no network.
package recruiting

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Stage refusal codes. Every refused governed command returns exactly
// one of these; sibling RECRUIT-001 codes are reused where the meaning
// already exists (missing parents, terminal transitions).
const (
	CodeStageSkipped            = "STAGE_SKIPPED"
	CodeScoreLocked             = "SCORE_LOCKED"
	CodeEvidenceProtected       = "EVIDENCE_PROTECTED"
	CodeProviderNotDecision     = "PROVIDER_NOT_DECISION"
	CodeStageConflict           = "STAGE_CONFLICT"
	CodeMissingStageRequirement = "MISSING_STAGE_REQUIREMENT"
	CodeDecisionUnauthorized    = "DECISION_UNAUTHORIZED"
)

// ErrStageRefused is the sentinel for refused stage commands. Match it
// with errors.Is rather than parsing the code.
var ErrStageRefused = errors.New("recruiting: stage transition refused")

// stageCodes are the refusals owned by this file.
func stageCodes(code string) bool {
	switch code {
	case CodeStageSkipped, CodeScoreLocked, CodeEvidenceProtected,
		CodeProviderNotDecision, CodeStageConflict,
		CodeMissingStageRequirement, CodeDecisionUnauthorized:
		return true
	default:
		return false
	}
}

// Is reports ErrStageRefused for the stage refusals owned by this file,
// without changing the meaning of the RECRUIT-001 codes.
func (e *RecruitingError) Is(target error) bool {
	return target == ErrStageRefused && e != nil && stageCodes(e.Code)
}

func stageSkipped(field, state string) *RecruitingError {
	return &RecruitingError{Code: CodeStageSkipped, Field: field, State: state, Version: schemaVersion}
}

func scoreLocked(field, state string) *RecruitingError {
	return &RecruitingError{Code: CodeScoreLocked, Field: field, State: state, Version: schemaVersion}
}

func evidenceProtected(field, state string) *RecruitingError {
	return &RecruitingError{Code: CodeEvidenceProtected, Field: field, State: state, Version: schemaVersion}
}

func providerNotDecision(field, state string) *RecruitingError {
	return &RecruitingError{Code: CodeProviderNotDecision, Field: field, State: state, Version: schemaVersion}
}

func stageConflict(field, state string) *RecruitingError {
	return &RecruitingError{Code: CodeStageConflict, Field: field, State: state, Version: schemaVersion}
}

func missingStageRequirement(field, state string) *RecruitingError {
	return &RecruitingError{Code: CodeMissingStageRequirement, Field: field, State: state, Version: schemaVersion}
}

func decisionUnauthorized(field, state string) *RecruitingError {
	return &RecruitingError{Code: CodeDecisionUnauthorized, Field: field, State: state, Version: schemaVersion}
}

// governedNext is the mandatory stage order. WITHDRAWN, REJECTED and
// HIRED are terminal and have no governed successor: withdrawal and
// rejection stay on the RECRUIT-001 lifecycle, this ledger governs the
// forward evaluation pipeline.
func governedNext(stage CandidacyStage) (CandidacyStage, bool) {
	switch stage {
	case StageApplied:
		return StageScreening, true
	case StageScreening:
		return StageInterview, true
	case StageInterview:
		return StageOffer, true
	case StageOffer:
		return StageHired, true
	default:
		return "", false
	}
}

// requiredEvidence names the evidence kinds an advance to the target
// stage must cite. The hire binds the chain built so far; its own
// authority is the decision receipt.
func requiredEvidence(to CandidacyStage) []EvidenceKind {
	switch to {
	case StageScreening:
		return []EvidenceKind{EvidenceScreening}
	case StageInterview:
		return []EvidenceKind{EvidenceInterview}
	case StageOffer:
		return []EvidenceKind{EvidenceAssessment}
	default:
		return nil
	}
}

// EvidenceKind is the closed evidence vocabulary for governed stages.
type EvidenceKind string

// The evidence kinds the pipeline binds.
const (
	EvidenceScreening  EvidenceKind = "SCREENING"
	EvidenceAssessment EvidenceKind = "ASSESSMENT"
	EvidenceInterview  EvidenceKind = "INTERVIEW"
	EvidenceReference  EvidenceKind = "REFERENCE"
)

func (k EvidenceKind) valid() bool {
	switch k {
	case EvidenceScreening, EvidenceAssessment, EvidenceInterview, EvidenceReference:
		return true
	default:
		return false
	}
}

// StageEvidence pins one evaluation artifact to a candidacy. The ATS
// stores the reference and content digest only — artifact bytes stay
// with their owning provider or document custody. Protected evidence
// (for example screening notes) names the clearance its viewer must
// present; the denial carries no content.
type StageEvidence struct {
	EvidenceID        string
	CandidacyID       string
	Kind              EvidenceKind
	Protected         bool
	RequiredClearance string
	ContentDigest     string
	SourceRef         string
}

// StageDecision is the authorized human receipt behind one advance.
// AuthorityRef must cite governance authority ("authority:..."): a
// provider result ("provider:...") is an observation and can never
// decide.
type StageDecision struct {
	Decider           string
	AuthorityRef      string
	CandidacyID       string
	CandidacyRevision uint64
	ToStage           CandidacyStage
	DecidedAt         values.Instant
}

// AdvanceCmd is one governed stage advance: a compare-and-swap over the
// requisition and candidacy revisions, the required evidence and the
// authorized decision receipt.
type AdvanceCmd struct {
	RequisitionID             string
	RequisitionRevision       uint64
	CandidacyID               string
	ExpectedCandidacyRevision uint64
	ToStage                   CandidacyStage
	EvidenceIDs               []string
	Decision                  StageDecision
	EffectiveAt               values.Instant
	KnownAt                   values.KnownAt
}

// ScoreCmd records one assessor score for the stage under evaluation.
type ScoreCmd struct {
	CandidacyID string
	Stage       CandidacyStage
	Assessor    string
	Score       int32
	EffectiveAt values.Instant
	KnownAt     values.KnownAt
}

// ScoreRecord is one immutable score revision. A correction appends a
// successor; the original revision is preserved in history.
type ScoreRecord struct {
	CandidacyID string
	Stage       CandidacyStage
	Assessor    string
	Score       int32
	Revision    uint64
	EffectiveAt values.Instant
	KnownAt     values.KnownAt
}

// ProviderObservation is one external result kept as history only. It
// never advances a stage and never substitutes for a decision receipt.
type ProviderObservation struct {
	CandidacyID string
	Stage       CandidacyStage
	ProviderRef string
	Result      string
	ObservedAt  values.Instant
}

// StageFrontier is the single current stage per candidacy: the stage,
// the candidacy revision that reached it, the requisition revision it
// was reached under and the decision that authorized it.
type StageFrontier struct {
	CandidacyID         string
	Stage               CandidacyStage
	CandidacyRevision   uint64
	RequisitionID       string
	RequisitionRevision uint64
	DecisionDecider     string
	DecisionAuthority   string
}

func (f StageFrontier) body() []byte {
	b, err := canonicalbytes.New("hcmnext.domains.recruiting.StageFrontier", schemaVersion).
		String("candidacy_id", f.CandidacyID).String("stage", string(f.Stage)).
		Int("candidacy_revision", int64(f.CandidacyRevision)).
		String("requisition_id", f.RequisitionID).Int("requisition_revision", int64(f.RequisitionRevision)).
		String("decision_decider", f.DecisionDecider).String("decision_authority", f.DecisionAuthority).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// StageLedger is the governed evaluation boundary over one ATS
// aggregate: evidence registry, append-only scores, provider
// observations and exactly one current frontier per candidacy. The
// mutex serializes concurrent decisions so one compare-and-swap wins
// and the losers report STAGE_CONFLICT with zero effect.
type StageLedger struct {
	mu           sync.Mutex
	agg          *Aggregate
	authorized   map[string]struct{}
	evidence     map[string]StageEvidence
	scores       map[string][]ScoreRecord
	observations []ProviderObservation
	frontiers    map[string]StageFrontier
}

// NewStageLedger opens the governed boundary over agg. The authorized
// decider roster is explicit: nobody outside it can advance a
// candidacy.
func NewStageLedger(agg *Aggregate, authorizedDeciders []string) (*StageLedger, error) {
	if agg == nil {
		return nil, missingParent("aggregate", "missing")
	}
	if len(authorizedDeciders) == 0 {
		return nil, missingParent("deciders", "missing-roster")
	}
	authorized := make(map[string]struct{}, len(authorizedDeciders))
	for _, decider := range authorizedDeciders {
		if !validID(decider) {
			return nil, missingParent("deciders", "missing-id")
		}
		authorized[decider] = struct{}{}
	}
	return &StageLedger{
		agg:        agg,
		authorized: authorized,
		evidence:   make(map[string]StageEvidence),
		scores:     make(map[string][]ScoreRecord),
		frontiers:  make(map[string]StageFrontier),
	}, nil
}

func scoreKey(candidacyID string, stage CandidacyStage, assessor string) string {
	return candidacyID + "\x00" + string(stage) + "\x00" + assessor
}

// RegisterEvidence pins one evaluation artifact to its candidacy.
func (l *StageLedger) RegisterEvidence(evidence StageEvidence) error {
	if l == nil || l.agg == nil {
		return missingParent("ledger", "missing")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !validID(evidence.EvidenceID) {
		return missingParent("evidence", "missing-id")
	}
	if _, exists := l.evidence[evidence.EvidenceID]; exists {
		return stageConflict("evidence", "evidence-exists")
	}
	if _, ok := l.agg.Candidacies[evidence.CandidacyID]; !ok {
		return missingParent("candidacy", "unknown")
	}
	if !evidence.Kind.valid() {
		return missingStageRequirement("evidence-kind", "unknown-kind")
	}
	if evidence.Protected && !validID(evidence.RequiredClearance) {
		return missingStageRequirement("clearance", "missing-clearance")
	}
	if !validID(evidence.ContentDigest) {
		return missingParent("evidence", "missing-digest")
	}
	if !validID(evidence.SourceRef) {
		return missingParent("source", "missing")
	}
	l.evidence[evidence.EvidenceID] = evidence
	return nil
}

// ViewEvidence returns the pinned reference only to a viewer holding
// the required clearance. The denial names the missing clearance and
// carries no evidence content.
func (l *StageLedger) ViewEvidence(viewerClearance, evidenceID string) (StageEvidence, error) {
	if l == nil || l.agg == nil {
		return StageEvidence{}, missingParent("ledger", "missing")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	evidence, ok := l.evidence[evidenceID]
	if !ok {
		return StageEvidence{}, missingParent("evidence", "unknown")
	}
	if evidence.Protected && viewerClearance != evidence.RequiredClearance {
		return StageEvidence{}, evidenceProtected("clearance", "protected-screening-evidence")
	}
	return evidence, nil
}

// RecordScore files one assessor score for the stage under evaluation.
// Scores are bounded 0-100 and immutable once submitted: a second score
// for the same candidacy, stage and assessor is SCORE_LOCKED — correct
// it with CorrectScore instead.
func (l *StageLedger) RecordScore(cmd ScoreCmd) error {
	if l == nil || l.agg == nil {
		return missingParent("ledger", "missing")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	current, ok := l.agg.Candidacies[cmd.CandidacyID]
	if !ok {
		return missingParent("candidacy", "unknown")
	}
	if !cmd.Stage.Valid() || cmd.Stage.terminal() {
		return missingStageRequirement("stage", "not-evaluable")
	}
	if cmd.Stage != current.Stage {
		return missingStageRequirement("stage", "score-stage-mismatch")
	}
	if !validID(cmd.Assessor) {
		return missingParent("assessor", "missing-id")
	}
	if cmd.Score < 0 || cmd.Score > 100 {
		return missingStageRequirement("score", "score-range")
	}
	if !validTimes(cmd.EffectiveAt, cmd.KnownAt) {
		return missingParent("effective-time", "invalid")
	}
	key := scoreKey(cmd.CandidacyID, cmd.Stage, cmd.Assessor)
	if existing, ok := l.scores[key]; ok && len(existing) > 0 {
		return scoreLocked("score", "already-submitted")
	}
	l.scores[key] = []ScoreRecord{{
		CandidacyID: cmd.CandidacyID, Stage: cmd.Stage, Assessor: cmd.Assessor,
		Score: cmd.Score, Revision: 1, EffectiveAt: cmd.EffectiveAt, KnownAt: cmd.KnownAt,
	}}
	return nil
}

// CorrectScore appends a successor score revision. The original stays
// preserved in history; without a submitted score there is nothing to
// correct.
func (l *StageLedger) CorrectScore(candidacyID string, stage CandidacyStage, assessor string, score int32, effective values.Instant, known values.KnownAt) error {
	if l == nil || l.agg == nil {
		return missingParent("ledger", "missing")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	key := scoreKey(candidacyID, stage, assessor)
	history, ok := l.scores[key]
	if !ok || len(history) == 0 {
		return missingStageRequirement("score", "nothing-to-correct")
	}
	if score < 0 || score > 100 {
		return missingStageRequirement("score", "score-range")
	}
	if !validTimes(effective, known) {
		return missingParent("effective-time", "invalid")
	}
	latest := history[len(history)-1]
	l.scores[key] = append(history, ScoreRecord{
		CandidacyID: candidacyID, Stage: stage, Assessor: assessor,
		Score: score, Revision: latest.Revision + 1, EffectiveAt: effective, KnownAt: known,
	})
	return nil
}

// ScoreHistory returns the append-only score revisions for one
// candidacy, stage and assessor, oldest first.
func (l *StageLedger) ScoreHistory(candidacyID string, stage CandidacyStage, assessor string) []ScoreRecord {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]ScoreRecord(nil), l.scores[scoreKey(candidacyID, stage, assessor)]...)
}

// activeScore reports whether the candidacy holds a submitted score.
func (l *StageLedger) activeScore(candidacyID string) bool {
	for key, history := range l.scores {
		if len(history) == 0 {
			continue
		}
		if strings.HasPrefix(key, candidacyID+"\x00") {
			return true
		}
	}
	return false
}

// RecordObservation files one provider result as history only. It never
// advances the frontier and never appends an aggregate event — even a
// late observation for a superseded stage.
func (l *StageLedger) RecordObservation(observation ProviderObservation) error {
	if l == nil || l.agg == nil {
		return missingParent("ledger", "missing")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.agg.Candidacies[observation.CandidacyID]; !ok {
		return missingParent("candidacy", "unknown")
	}
	if !observation.Stage.Valid() {
		return missingStageRequirement("stage", "unknown-stage")
	}
	if !validID(observation.ProviderRef) {
		return missingParent("provider", "missing-ref")
	}
	if !validID(observation.Result) {
		return missingParent("observation", "missing-result")
	}
	if observation.ObservedAt.Validate() != nil {
		return missingParent("observed-time", "invalid")
	}
	l.observations = append(l.observations, observation)
	return nil
}

// AdvanceStage moves one candidacy to its next mandatory stage. The
// compare-and-swap binds the requisition and candidacy revisions, the
// required evidence and the authorized decision receipt; on success it
// appends exactly one stage event and parks exactly one current
// frontier.
func (l *StageLedger) AdvanceStage(cmd AdvanceCmd) error {
	if l == nil || l.agg == nil {
		return missingParent("ledger", "missing")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	current, ok := l.agg.Candidacies[cmd.CandidacyID]
	if !ok {
		return missingParent("candidacy", "unknown")
	}
	if current.Revision != cmd.ExpectedCandidacyRevision {
		return stageConflict("candidacy", "revision-mismatch")
	}
	if current.Stage.terminal() {
		return invalidCandidacyTransition("stage", "terminal-"+string(current.Stage))
	}
	next, ok := governedNext(current.Stage)
	if !ok || cmd.ToStage != next {
		return stageSkipped("stage", fmt.Sprintf("expected-%s-got-%s", next, cmd.ToStage))
	}
	application, ok := l.agg.Applications[current.ApplicationID]
	if !ok {
		return missingParent("application", "unknown")
	}
	if cmd.RequisitionID != application.RequisitionID {
		return stageConflict("requisition", "requisition-mismatch")
	}
	requisition, ok := l.agg.Requisitions[cmd.RequisitionID]
	if !ok || requisition.Revision != cmd.RequisitionRevision {
		return stageConflict("requisition", "revision-mismatch")
	}
	if err := checkDecision(l.authorized, cmd.CandidacyID, cmd.ExpectedCandidacyRevision, cmd.ToStage, cmd.Decision); err != nil {
		return err
	}
	if err := l.checkEvidence(cmd.CandidacyID, cmd.ToStage, cmd.EvidenceIDs); err != nil {
		return err
	}
	if cmd.ToStage == StageOffer && !l.activeScore(cmd.CandidacyID) {
		return missingStageRequirement("score", "offer-requires-submitted-score")
	}
	if !validTimes(cmd.EffectiveAt, cmd.KnownAt) {
		return missingParent("effective-time", "invalid")
	}
	if err := l.agg.TransitionCandidacy(cmd.CandidacyID, cmd.ExpectedCandidacyRevision, cmd.ToStage, cmd.EffectiveAt, cmd.KnownAt); err != nil {
		return err
	}
	l.frontiers[cmd.CandidacyID] = StageFrontier{
		CandidacyID: cmd.CandidacyID, Stage: cmd.ToStage,
		CandidacyRevision: cmd.ExpectedCandidacyRevision + 1,
		RequisitionID:     cmd.RequisitionID, RequisitionRevision: cmd.RequisitionRevision,
		DecisionDecider: cmd.Decision.Decider, DecisionAuthority: cmd.Decision.AuthorityRef,
	}
	return nil
}

// checkDecision validates the human receipt: present, bound to this
// candidacy revision and stage, carried by an authorized decider and
// citing governance authority — never a provider result.
func checkDecision(authorized map[string]struct{}, candidacyID string, revision uint64, to CandidacyStage, decision StageDecision) error {
	if !validID(decision.Decider) || !validID(decision.AuthorityRef) {
		return missingStageRequirement("decision", "missing-receipt")
	}
	if strings.HasPrefix(decision.AuthorityRef, "provider:") {
		return providerNotDecision("decision", "provider-result-is-observation")
	}
	if !strings.HasPrefix(decision.AuthorityRef, "authority:") {
		return missingStageRequirement("decision", "decision-authority")
	}
	if _, ok := authorized[decision.Decider]; !ok {
		return decisionUnauthorized("decider", "not-authorized")
	}
	if decision.CandidacyID != candidacyID || decision.CandidacyRevision != revision || decision.ToStage != to {
		return missingStageRequirement("decision", "decision-mismatch")
	}
	if decision.DecidedAt.Validate() != nil {
		return missingParent("decided-time", "invalid")
	}
	return nil
}

// checkEvidence requires every cited artifact to be registered to this
// candidacy and the target stage's mandatory kinds to be covered.
func (l *StageLedger) checkEvidence(candidacyID string, to CandidacyStage, cited []string) error {
	covered := make(map[EvidenceKind]bool)
	for _, id := range cited {
		evidence, ok := l.evidence[id]
		if !ok || evidence.CandidacyID != candidacyID {
			return missingStageRequirement("evidence", "unknown-evidence")
		}
		covered[evidence.Kind] = true
	}
	for _, kind := range requiredEvidence(to) {
		if !covered[kind] {
			return missingStageRequirement("evidence", "missing-"+string(kind))
		}
	}
	return nil
}

// Frontier returns the single current stage for a candidacy.
func (l *StageLedger) Frontier(candidacyID string) (StageFrontier, bool) {
	if l == nil {
		return StageFrontier{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	frontier, ok := l.frontiers[candidacyID]
	return frontier, ok
}

// FrontierCount reports how many current stages a candidacy holds. The
// governed invariant is exactly one once it has advanced.
func (l *StageLedger) FrontierCount(candidacyID string) int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.frontiers[candidacyID]; ok {
		return 1
	}
	return 0
}

// FrontierDigest is the stable digest of the current frontier, or empty
// when the candidacy has not advanced under governance.
func (l *StageLedger) FrontierDigest(candidacyID string) string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	frontier, ok := l.frontiers[candidacyID]
	if !ok {
		return ""
	}
	return canonicalbytes.Digest(frontier.body())
}

// Explain renders the bounded human-readable account: counts only —
// never candidate, score, evidence or decision content.
func (l *StageLedger) Explain() string {
	if l == nil || l.agg == nil {
		return "recruiting stages: missing ledger"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	kinds := make([]string, 0, len(l.frontiers))
	for _, frontier := range l.frontiers {
		kinds = append(kinds, string(frontier.Stage))
	}
	sort.Strings(kinds)
	return fmt.Sprintf("recruiting stages frontiers=%d evidence=%d observations=%d stages=%s",
		len(l.frontiers), len(l.evidence), len(l.observations), strings.Join(kinds, ","))
}
