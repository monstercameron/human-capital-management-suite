package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
)

const (
	personaPeerSchemaID      = "persona.peer-context"
	personaPeerSchemaVersion = "1"
	maxPersonaPeerSummary    = 4096
)

var (
	// ErrPersonaPeerInvalid identifies an invalid peer extraction request.
	ErrPersonaPeerInvalid = errors.New("application: invalid persona peer extraction")
	// ErrPersonaPeerQuarantined identifies peer content that cannot safely reach planning.
	ErrPersonaPeerQuarantined = errors.New("application: persona peer content quarantined")
)

// PersonaPeerExtractor adapts the typed quarantine gateway to persona
// invocation context. Raw peer content never appears in its returned values.
type PersonaPeerExtractor struct {
	extractor *agentsecurity.QuarantinedExtractor
}

// NewPersonaPeerExtractor constructs a fail-closed extractor. A nil model is
// rejected instead of falling back to an untyped or local model path.
func NewPersonaPeerExtractor(model agentsecurity.QuarantineModel) (*PersonaPeerExtractor, error) {
	extractor, err := agentsecurity.NewQuarantinedExtractor(model)
	if err != nil {
		return nil, err
	}
	return &PersonaPeerExtractor{extractor: extractor}, nil
}

var _ agentinvoke.PeerExtractor = (*PersonaPeerExtractor)(nil)

// Extract returns the only schema-bound value permitted into persona context.
// The source digest is checked before and after model extraction; the summary
// cannot name a recipient or carry an outbound URL.
func (p *PersonaPeerExtractor) Extract(ctx context.Context, request agentinvoke.PeerExtractionRequest) (agentinvoke.PeerExtraction, error) {
	evidence, err := p.ExtractEvidence(ctx, request)
	if err != nil {
		return agentinvoke.PeerExtraction{}, err
	}
	value, err := peerSummary(evidence)
	if err != nil {
		return agentinvoke.PeerExtraction{}, err
	}
	return agentinvoke.PeerExtraction{
		SchemaID: evidence.SchemaID, SchemaVersion: evidence.SchemaVersion,
		Values: map[string]string{"peer_summary": value}, SourceDigest: evidence.SourceDigest,
	}, nil
}

// ExtractEvidence retains the quarantine evidence for callers that need to
// carry external taint, provenance, citations, and source digest forward.
func (p *PersonaPeerExtractor) ExtractEvidence(ctx context.Context, request agentinvoke.PeerExtractionRequest) (agentsecurity.QuarantineExtraction, error) {
	if p == nil || p.extractor == nil || strings.TrimSpace(request.PostID) == "" || strings.TrimSpace(request.AuthorID) == "" || strings.TrimSpace(request.Content) == "" {
		return agentsecurity.QuarantineExtraction{}, fmt.Errorf("%w: peer post identity and content are required", ErrPersonaPeerInvalid)
	}
	digest := personaPeerDigest(request.Content)
	if request.Digest != digest {
		return agentsecurity.QuarantineExtraction{}, fmt.Errorf("%w: peer post digest does not match content", ErrPersonaPeerInvalid)
	}
	instruction, detectorErr := agentsecurity.DefaultInstructionDetector(request.Content)
	if detectorErr != nil || instruction || personaPeerInstruction(request.Content) {
		return agentsecurity.QuarantineExtraction{}, fmt.Errorf("%w: peer post contains instruction-shaped content", ErrPersonaPeerQuarantined)
	}
	extraction, err := p.extractor.Extract(ctx, agentsecurity.QuarantineRequest{
		Source: agentsecurity.SourceChat, SourceID: "chat-post:" + request.PostID, Content: request.Content,
		Schema: agentsecurity.ExtractionSchema{ID: personaPeerSchemaID, Version: personaPeerSchemaVersion,
			Fields: []agentsecurity.ExtractionField{{Name: "peer_summary", Type: "string", Required: true}}},
	})
	if err != nil {
		return agentsecurity.QuarantineExtraction{}, err
	}
	if extraction.SourceDigest != digest || extraction.SchemaID != personaPeerSchemaID || extraction.SchemaVersion != personaPeerSchemaVersion {
		return agentsecurity.QuarantineExtraction{}, fmt.Errorf("%w: extraction is not bound to the declared peer source", ErrPersonaPeerQuarantined)
	}
	if _, err := peerSummary(extraction); err != nil {
		return agentsecurity.QuarantineExtraction{}, err
	}
	return extraction, nil
}

func peerSummary(extraction agentsecurity.QuarantineExtraction) (string, error) {
	if len(extraction.Values) != 1 || extraction.Values[0].Name != "peer_summary" {
		return "", fmt.Errorf("%w: extraction must contain exactly peer_summary", ErrPersonaPeerQuarantined)
	}
	var summary string
	if err := json.Unmarshal(extraction.Values[0].Value, &summary); err != nil || strings.TrimSpace(summary) == "" || len(summary) > maxPersonaPeerSummary || personaPeerLink(summary) {
		return "", fmt.Errorf("%w: peer summary is empty, oversized, or contains a link", ErrPersonaPeerQuarantined)
	}
	return summary, nil
}

func personaPeerDigest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func personaPeerInstruction(content string) bool {
	lower := strings.ToLower(content)
	for _, marker := range []string{
		"post everyone's salary", "post everyones salary", "fake approval", "approved by the channel owner",
		"ignore previous instructions", "system message:", "developer message:", "reveal your prompt",
		"execute this tool", "follow these instructions", "@comp-analyst:",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func personaPeerLink(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"http://", "https://", "www.", "![", "]("} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
