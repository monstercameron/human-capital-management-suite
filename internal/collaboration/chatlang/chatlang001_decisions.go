package chatlang

// CHATLANG-001: the decisions about how Chat is translated, as a checked-in
// record the code and its tests answer to. It states what was decided and what
// has not been measured yet; it does not claim a measurement that was not made.
// Owner decision of 2026-10-02: "the language, stt and writing style can use
// openai via schemaflux".

// EngineFamily is one of the three ways text can be translated that the research
// compares.
type EngineFamily string

const (
	// FamilyLanguageModel is a general language model through the governed model route.
	FamilyLanguageModel EngineFamily = "language_model"
	// FamilyService is a dedicated translation service.
	FamilyService EngineFamily = "translation_service"
	// FamilyLocalModel is an open translation model run inside the deployment.
	FamilyLocalModel EngineFamily = "local_model"
)

// CandidateStatus is how far a candidate got.
type CandidateStatus string

const (
	// StatusDefault is the engine the product translates with.
	StatusDefault CandidateStatus = "default"
	// StatusNotBuilt means no engine of this kind is built; the port admits one.
	StatusNotBuilt CandidateStatus = "not_built"
)

// Candidate is one engine considered.
type Candidate struct {
	Family EngineFamily    `json:"family"`
	Name   string          `json:"name"`
	Status CandidateStatus `json:"status"`
	// Leaves is where the text goes when this candidate is used.
	Leaves string `json:"leaves"`
	// Open names what is not yet known and is checked before the candidate is built.
	Open string `json:"open"`
}

// Decisions is the record.
type Decisions struct {
	Candidates []Candidate `json:"candidates"`
	// Default is the engine and model the product asks for; the model is
	// configurable and never substituted.
	DefaultEngine string `json:"default_engine"`
	DefaultModel  string `json:"default_model"`
	// Fallback is the order of what happens when an engine cannot serve a message.
	Fallback []string `json:"fallback"`
	// Detection says where the language of a message is found.
	Detection []string `json:"detection"`
	// Quality says what is checked before a translation is shown.
	Quality []string `json:"quality"`
	// Cost says how translation is metered and limited.
	Cost []string `json:"cost"`
	// Classes are the data classes text may leave the deployment in, and
	// Conditions what must hold before it does.
	Classes    []string `json:"classes"`
	Conditions []string `json:"conditions"`
	// Budgets are the latency budgets of CHATLANG-003, in milliseconds.
	LivePercentile95Millis int `json:"live_p95_ms"`
	PagePercentile95Millis int `json:"page_p95_ms"`
	// NotMeasured lists the research that needs a live run or reviewers.
	NotMeasured []string `json:"not_measured"`
}

// ResearchDecisions returns the record.
func ResearchDecisions() Decisions {
	return Decisions{
		Candidates: []Candidate{
			{FamilyLanguageModel, "openai gpt-6-luna through SchemaFlux", StatusDefault, "the model provider, under the workspace's approved terms", "quality per pair on the labelled chat set; p50 and p95 latency; price per million characters"},
			{FamilyService, "DeepL, Google Cloud Translation, Azure Translator, Amazon Translate", StatusNotBuilt, "the service provider, under its own terms", "glossary and formality support, retention and training use, sub-processors, price, quality per pair"},
			{FamilyLocalModel, "MADLAD-400, NLLB-200", StatusNotBuilt, "nowhere: the text stays in the deployment", "each model's licence for commercial use, checked first; quality per pair; hardware needed"},
		},
		DefaultEngine: "openai",
		DefaultModel:  "gpt-6-luna",
		Fallback:      []string{"openai", "the message as written"},
		Detection: []string{
			"the writer's own correction, when there is one",
			"the local detector at send, in the deployment, under 2 ms (CHATLANG-002)",
			"for a message with no recorded language, the local detector when a reader first opens it",
			"the engine's own finding in the same call that translates, which wins when it differs from the record",
			"text with no language of its own (a name, a number, an emoji, code) is recorded as no language and never sent",
		},
		Quality: []string{
			"mentions, links, addresses, code, numbers, dates and glossary terms are taken out before the call and restored after, and every one must come back exactly once",
			"the model states whether the translation keeps the meaning; a translation it does not vouch for is never shown",
			"a failed check is asked once more, then the message stays as written",
			"a pair is offered only after the quality gate of CHATLANG-006 passes for it",
		},
		Cost: []string{
			"one translation per message revision per language, kept for the life of the revision",
			"every call writes one usage line with tokens and the cost priced from the approved schedule",
			"a monthly limit per workspace: history pauses at it, new messages follow, the administrator is told",
		},
		Classes: []string{"public", "internal"},
		Conditions: []string{
			"the workspace has turned translation on and allows an outside service",
			"the channel has not turned translation off or barred outside services",
			"the model terms, region and credential were approved for the translation purpose",
			"the text is not one the message filters would mask",
		},
		LivePercentile95Millis: 700,
		PagePercentile95Millis: 2000,
		NotMeasured: []string{
			"quality on 300 labelled messages per pair, by metric and by bilingual reviewers",
			"latency at the 50th and 95th percentile against the live model",
			"price per million characters and projected monthly cost at 50, 500 and 5,000 people",
			"the dedicated services and the local models, against the same set",
			"language identification accuracy under ten words, local classifier against the engine",
		},
	}
}
