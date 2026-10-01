package agentsystem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
)

const (
	peerExtractionSchemaID      = "persona.peer-context"
	peerExtractionSchemaVersion = "1"
	maxPeerExtractionBytes      = 4096
)

// PersonaPeerExtractor converts peer-authored chat posts into one bounded,
// schema-bound extraction. The raw post is never returned to the planner.
type PersonaPeerExtractor struct {
	extractor *agentsecurity.QuarantinedExtractor
}

// NewPersonaPeerExtractor constructs a peer extractor around the same narrow
// model seam used for other quarantined content.
func NewPersonaPeerExtractor(model agentsecurity.QuarantineModel) (*PersonaPeerExtractor, error) {
	extractor, err := agentsecurity.NewQuarantinedExtractor(model)
	if err != nil {
		return nil, err
	}
	return &PersonaPeerExtractor{extractor: extractor}, nil
}

// NewPersonaPeerExtractor binds the extractor to this platform's tool-less
// quarantine gateway and the invocation's audit/model request template.
func (p *Platform) NewPersonaPeerExtractor(template agentmodel.Request) (*PersonaPeerExtractor, error) {
	if p == nil || p.quarantine == nil {
		return nil, ErrNotConfigured
	}
	return NewPersonaPeerExtractor(&quarantineModel{platform: p, template: template})
}

// Extract applies AGENT2-015 to a single peer post. It refuses recognizable
// instruction payloads before model invocation and verifies the digest again
// before returning the typed result to the persona context builder.
func (p *PersonaPeerExtractor) Extract(ctx context.Context, request agentinvoke.PeerExtractionRequest) (agentinvoke.PeerExtraction, error) {
	if p == nil || p.extractor == nil || strings.TrimSpace(request.PostID) == "" || strings.TrimSpace(request.AuthorID) == "" || strings.TrimSpace(request.Content) == "" {
		return agentinvoke.PeerExtraction{}, fmt.Errorf("%w: peer post identity and content are required", ErrInvalid)
	}
	digest := peerContentDigest(request.Content)
	if request.Digest != digest {
		return agentinvoke.PeerExtraction{}, fmt.Errorf("%w: peer post digest does not match content", ErrInvalid)
	}
	instruction, err := agentsecurity.DefaultInstructionDetector(request.Content)
	if err != nil || instruction || peerInstructionPayload(request.Content) {
		return agentinvoke.PeerExtraction{}, fmt.Errorf("%w: peer post contains instruction-shaped content", agentsecurity.ErrQuarantined)
	}
	extraction, err := p.extractor.Extract(ctx, agentsecurity.QuarantineRequest{
		Source: agentsecurity.SourceChat, SourceID: "chat-post:" + request.PostID,
		Content: request.Content,
		Schema: agentsecurity.ExtractionSchema{
			ID: peerExtractionSchemaID, Version: peerExtractionSchemaVersion,
			Fields: []agentsecurity.ExtractionField{{Name: "peer_summary", Type: "string", Required: true}},
		},
	})
	if err != nil {
		return agentinvoke.PeerExtraction{}, err
	}
	if extraction.SourceDigest != digest || len(extraction.Values) != 1 || extraction.Values[0].Name != "peer_summary" {
		return agentinvoke.PeerExtraction{}, fmt.Errorf("%w: peer extraction is not bound to its declared source", agentsecurity.ErrQuarantined)
	}
	var summary string
	if err := json.Unmarshal(extraction.Values[0].Value, &summary); err != nil || strings.TrimSpace(summary) == "" || len(summary) > maxPeerExtractionBytes || containsLinkPayload(summary) {
		return agentinvoke.PeerExtraction{}, fmt.Errorf("%w: peer extraction is empty, oversized, or contains a link", agentsecurity.ErrQuarantined)
	}
	return agentinvoke.PeerExtraction{
		SchemaID: extraction.SchemaID, SchemaVersion: extraction.SchemaVersion,
		Values: map[string]string{"peer_summary": summary}, SourceDigest: extraction.SourceDigest,
	}, nil
}

func peerContentDigest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func peerInstructionPayload(content string) bool {
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

func containsLinkPayload(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"http://", "https://", "www.", "![", "]("} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
