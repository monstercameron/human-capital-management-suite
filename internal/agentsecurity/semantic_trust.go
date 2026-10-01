package agentsecurity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type SourceKind string

const (
	SourceUser      SourceKind = "user"
	SourceResume    SourceKind = "resume"
	SourceEmail     SourceKind = "email"
	SourceDocument  SourceKind = "document"
	SourceWeb       SourceKind = "web"
	SourceChat      SourceKind = "chat"
	SourceConnector SourceKind = "connector"
	SourceMCP       SourceKind = "mcp"
)

type TrustLevel string

const (
	TrustCanonical TrustLevel = "CANONICAL_FACT"
	TrustHuman     TrustLevel = "AUTHORIZED_HUMAN_ASSERTION"
	TrustDocument  TrustLevel = "UNTRUSTED_DOCUMENT"
	TrustWeb       TrustLevel = "UNTRUSTED_WEB_CONTENT"
	TrustModel     TrustLevel = "MODEL_DERIVED"
)

type TaintLabel string

const (
	TaintCanonical TaintLabel = "CANONICAL_FACT"
	TaintHuman     TaintLabel = "HUMAN_ASSERTION"
	TaintExternal  TaintLabel = "EXTERNAL_UNTRUSTED"
	TaintDerived   TaintLabel = "AGENT_DERIVED"
	TaintTool      TaintLabel = "TOOL_DERIVED"
)

type AssertionKind string

const (
	KindFact        AssertionKind = "FACT"
	KindObservation AssertionKind = "OBSERVATION"
	KindClaim       AssertionKind = "CLAIM"
	KindInference   AssertionKind = "INFERENCE"
)

type Citation struct {
	SourceID string
	Location string
	Digest   string
}

// Datum is opaque evidence minted by ToolGateway. Callers cannot supply trust,
// taint, provenance, detector status, or a canonical classification.
type Datum struct {
	content    string
	trust      TrustLevel
	taint      []TaintLabel
	provenance []string
	kind       AssertionKind
	citation   Citation
	supporting []Citation
}
type Detector func(string) (bool, error)
type AnswerPart struct {
	Kind      AssertionKind
	Text      string
	Trust     TrustLevel
	Taint     []TaintLabel
	Citations []Citation
}
type Answer struct{ Parts []AnswerPart }

var (
	ErrInvalidDatum    = errors.New("invalid agent datum")
	ErrQuarantined     = errors.New("agent datum quarantined")
	ErrMissingCitation = errors.New("agent datum requires a valid citation")
	ErrAuthority       = errors.New("semantic authority required")
)

// WithDetector returns an independent gateway. Nil is a configuration fault,
// not permission to fall back or skip semantic inspection.
func (g *ToolGateway) WithDetector(detector Detector) *ToolGateway {
	if g == nil {
		return nil
	}
	c := *g
	c.tools = cloneDescriptors(g.tools)
	c.semanticSeal = &c
	if detector == nil {
		c.detector = func(string) (bool, error) { return false, errors.New("nil detector") }
	} else {
		c.detector = detector
	}
	return &c
}

// Observe admits non-authoritative content. Facts are deliberately unavailable
// for resumes, email, documents, web content, and human prompts.
func (g *ToolGateway) Observe(source SourceKind, content string, kind AssertionKind, citation Citation) (Datum, error) {
	if g == nil || strings.TrimSpace(content) == "" || !source.valid() || !kind.valid() || kind == KindFact {
		return Datum{}, ErrInvalidDatum
	}
	trust := trustForSource(source)
	return Datum{content: content, trust: trust, taint: []TaintLabel{taintForTrust(trust)}, provenance: []string{citation.SourceID}, kind: kind, citation: citation}, nil
}

// FactFromTool is the sole canonical-fact constructor. The result must be the
// exact output of this gateway's deterministic validation boundary.
func (g *ToolGateway) FactFromTool(result TypedResult, content string, citation Citation) (Datum, error) {
	if g == nil || g.semanticSeal == nil || result.semanticSeal != g.semanticSeal || result.semanticReceipt == "" || !containsString(result.Taint, string(TaintCanonical)) {
		return Datum{}, ErrAuthority
	}
	receipt, err := digestValidatedResult(result)
	if err != nil || receipt != result.semanticReceipt {
		return Datum{}, ErrAuthority
	}
	validatedContent, ok := result.Value.(string)
	if !ok || content != validatedContent {
		return Datum{}, ErrAuthority
	}
	if !containsString(result.Provenance, citationProof(citation)) || strings.TrimSpace(content) == "" {
		return Datum{}, ErrMissingCitation
	}
	return Datum{content: content, trust: TrustCanonical, taint: toTaint(result.Taint), provenance: cloneStrings(result.Provenance), kind: KindFact, citation: citation}, nil
}

// Infer monotonically carries every upstream taint and provenance label.
func (g *ToolGateway) Infer(content string, inputs ...Datum) (Datum, error) {
	if g == nil || strings.TrimSpace(content) == "" || len(inputs) == 0 {
		return Datum{}, ErrInvalidDatum
	}
	d := Datum{content: content, trust: TrustModel, kind: KindInference, taint: []TaintLabel{TaintDerived}}
	for _, in := range inputs {
		if err := g.validateSemanticDatum(in); err != nil {
			return Datum{}, err
		}
		d.taint = joinTaint(d.taint, in.taint...)
		d.provenance = joinStrings(d.provenance, in.provenance...)
		if in.kind == KindInference {
			d.supporting = joinCitations(d.supporting, in.supporting...)
		} else {
			d.supporting = joinCitations(d.supporting, in.citation)
		}
	}
	return d, nil
}

// BuildAnswer is the semantic enforcement point for answer emission.
func (g *ToolGateway) BuildAnswer(data []Datum) (Answer, error) {
	if g == nil || g.detector == nil {
		return Answer{}, fmt.Errorf("%w: detector unavailable", ErrQuarantined)
	}
	a := Answer{Parts: make([]AnswerPart, 0, len(data))}
	for _, d := range data {
		if err := g.validateSemanticDatum(d); err != nil {
			return Answer{}, err
		}
		p := AnswerPart{Kind: d.kind, Text: d.content, Trust: d.trust, Taint: append([]TaintLabel(nil), d.taint...)}
		if d.kind == KindInference {
			p.Citations = append([]Citation(nil), d.supporting...)
		} else {
			p.Citations = []Citation{d.citation}
		}
		a.Parts = append(a.Parts, p)
	}
	return a, nil
}

func (g *ToolGateway) validateSemanticDatum(d Datum) error {
	if g == nil || g.detector == nil {
		return fmt.Errorf("%w: detector unavailable", ErrQuarantined)
	}
	if d.content == "" || !d.kind.valid() || d.trust == "" || len(d.taint) == 0 || len(d.provenance) == 0 {
		return ErrInvalidDatum
	}
	instruction, err := g.detector(d.content)
	if err != nil {
		return fmt.Errorf("%w: detector failure", ErrQuarantined)
	}
	if instruction && d.trust != TrustCanonical {
		return ErrQuarantined
	}
	if d.kind == KindFact && d.trust != TrustCanonical {
		return ErrAuthority
	}
	if d.kind != KindInference {
		if !validCitation(d) {
			return ErrMissingCitation
		}
		return nil
	}
	if len(d.supporting) == 0 {
		return ErrMissingCitation
	}
	for _, c := range d.supporting {
		if c.SourceID == "" || c.Location == "" || c.Digest == "" || !containsString(d.provenance, c.SourceID) {
			return ErrMissingCitation
		}
	}
	return nil
}

func DefaultInstructionDetector(content string) (bool, error) {
	lower := strings.ToLower(content)
	for _, m := range []string{"ignore previous instructions", "system message", "developer message", "reveal your prompt", "execute this tool", "follow these instructions"} {
		if strings.Contains(lower, m) {
			return true, nil
		}
	}
	return false, nil
}
func (s SourceKind) valid() bool {
	return s == SourceUser || s == SourceResume || s == SourceEmail || s == SourceDocument || s == SourceWeb || s == SourceChat || s == SourceConnector || s == SourceMCP
}
func (k AssertionKind) valid() bool {
	return k == KindFact || k == KindObservation || k == KindClaim || k == KindInference
}
func trustForSource(s SourceKind) TrustLevel {
	if s == SourceUser {
		return TrustHuman
	}
	if s == SourceWeb {
		return TrustWeb
	}
	return TrustDocument
}
func taintForTrust(t TrustLevel) TaintLabel {
	if t == TrustHuman {
		return TaintHuman
	}
	if t == TrustCanonical {
		return TaintCanonical
	}
	return TaintExternal
}
func validCitation(d Datum) bool {
	return d.citation.SourceID != "" && d.citation.Location != "" && d.citation.Digest == digestContent(d.content) && containsString(d.provenance, d.citation.SourceID)
}
func citationProof(c Citation) string {
	encoded, _ := json.Marshal(struct {
		SourceID string `json:"source_id"`
		Location string `json:"location"`
		Digest   string `json:"digest"`
	}{c.SourceID, c.Location, c.Digest})
	sum := sha256.Sum256(append([]byte("hcm-next-agent-citation/v1\x00"), encoded...))
	return "citation:sha256:" + hex.EncodeToString(sum[:])
}
func digestContent(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}
func joinTaint(v []TaintLabel, add ...TaintLabel) []TaintLabel {
	seen := map[TaintLabel]bool{}
	out := make([]TaintLabel, 0, len(v)+len(add))
	for _, x := range append(v, add...) {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
func joinStrings(v []string, add ...string) []string {
	out := cloneStrings(v)
	for _, x := range add {
		if x != "" && !containsString(out, x) {
			out = append(out, x)
		}
	}
	return out
}
func joinCitations(v []Citation, add ...Citation) []Citation {
	out := append([]Citation(nil), v...)
	seen := map[string]bool{}
	for _, c := range out {
		seen[citationTuple(c)] = true
	}
	for _, c := range add {
		key := citationTuple(c)
		if !seen[key] {
			seen[key] = true
			out = append(out, c)
		}
	}
	return out
}
func citationTuple(c Citation) string {
	encoded, _ := json.Marshal(struct {
		SourceID string `json:"source_id"`
		Location string `json:"location"`
		Digest   string `json:"digest"`
	}{c.SourceID, c.Location, c.Digest})
	return string(encoded)
}
func toTaint(v []string) []TaintLabel {
	out := make([]TaintLabel, 0, len(v))
	for _, x := range v {
		out = joinTaint(out, TaintLabel(x))
	}
	return out
}
func cloneDescriptors(in map[string]ToolDescriptor) map[string]ToolDescriptor {
	out := make(map[string]ToolDescriptor, len(in))
	for k, v := range in {
		v.DataScope = cloneStrings(v.DataScope)
		out[k] = v
	}
	return out
}
