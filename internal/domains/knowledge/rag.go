package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrChunkRejected is the KNOW-004 seeded-defect sentinel. Uncited,
	// stale, injected or invalidated knowledge presented for RAG publication
	// must fail with this error carrying field, state and version.
	ErrChunkRejected = errors.New("KNOW_004_REJECTED")
)

// Instruction-taint screening lives in taint.go: both this gate and the
// KNOW-006 freshness gate share one injectable detector
// (DetectInstructionTaint) that reuses the agentsecurity contract. The
// markers below moved there so the two gates cannot disagree.

// audienceRank orders chunk audiences from widest to narrowest. A chunk may
// narrow but never broaden the source article audience.
var audienceRank = map[string]int{
	"PUBLIC": 0, "EMPLOYEES": 1, "MANAGERS": 2,
}

// ChunkRequest is one RAG derivative proposed for publication. TaintedSource
// marks pipeline-detected injection; Invalidated marks source retraction.
type ChunkRequest struct {
	TenantID      string
	Article       ArticleRevision
	Text          string
	Audience      string
	Purpose       string
	At            time.Time
	ChunkRef      string
	TaintedSource bool
	Invalidated   bool
}

// Chunk is the published, cited derivative. It preserves article identity,
// citations, classification, taint verdict and effective scope.
type Chunk struct {
	ChunkRef       string
	ArticleID      string
	Revision       uint64
	Text           string
	Citations      []SourceRef
	Classification string
	Tainted        bool
	EffectiveFrom  time.Time
	EffectiveTo    time.Time
	Audience       string
	Digest         string
}

// ChunkRejection is the stable KNOW-004 failure shape.
type ChunkRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *ChunkRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrChunkRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the KNOW_004_REJECTED sentinel to errors.Is.
func (r *ChunkRejection) Unwrap() error { return ErrChunkRejected }

func chunkReject(field, state, version, reason string) error {
	return &ChunkRejection{Field: field, State: state, Version: version, Reason: reason}
}

// hostile keeps its name and shape for compatibility: it is the shared
// default detector with the error suppressed for boolean call sites. New
// code should prefer DetectInstructionTaint, which surfaces failures.
func hostile(text string) bool {
	hit, _ := DetectInstructionTaint(text)
	return hit
}

// PublishChunk validates and freezes one RAG derivative. It is pure: chunks
// are values, and publication persists nothing by itself. Instruction-taint
// screening runs the shared default detector; use PublishChunkWithDetector
// to inject one.
func PublishChunk(req ChunkRequest) (Chunk, error) {
	return PublishChunkWithDetector(req, DetectInstructionTaint)
}

// PublishChunkWithDetector validates and freezes one RAG derivative with an
// injectable instruction-taint detector. A nil detector selects the shared
// default; a failing detector fails closed and the chunk is refused.
func PublishChunkWithDetector(req ChunkRequest, detect InstructionDetector) (Chunk, error) {
	const version = "knowledge-chunk/v1"
	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.ChunkRef) == "" {
		return Chunk{}, chunkReject("chunk.identity", "MISSING", version, "chunk needs tenant and chunk reference")
	}
	if strings.TrimSpace(req.Text) == "" {
		return Chunk{}, chunkReject("chunk.text", "MISSING", version, "chunk text is required")
	}
	a := req.Article
	if strings.TrimSpace(a.ArticleID) == "" || a.Revision == 0 {
		return Chunk{}, chunkReject("article.identity", "MISSING", version, "chunk needs a source article revision")
	}
	if len(a.SourceRefs) == 0 {
		return Chunk{}, chunkReject("article.source_refs", "UNCITED", version, fmt.Sprintf("article %q has no citations", a.ArticleID))
	}
	if a.Supersession != nil {
		return Chunk{}, chunkReject("article.supersession", "STALE", version, fmt.Sprintf("article %q was superseded", a.ArticleID))
	}
	if req.TaintedSource {
		return Chunk{}, chunkReject("article.provenance", "INJECTED", version, fmt.Sprintf("article %q arrived from an untrusted source", a.ArticleID))
	}
	if req.Invalidated {
		return Chunk{}, chunkReject("article.validity", "INVALIDATED", version, fmt.Sprintf("article %q was invalidated by its source", a.ArticleID))
	}
	if req.At.IsZero() {
		return Chunk{}, chunkReject("chunk.at", "MISSING", version, "publication time is required")
	}
	from := a.EffectiveInterval.EffectiveFrom.Time()
	if req.At.Before(from) {
		return Chunk{}, chunkReject("article.effective", "STALE", version, "article is not yet effective")
	}
	var effectiveTo time.Time
	if to := a.EffectiveInterval.EffectiveTo; to.IsSet() {
		effectiveTo = to.Time()
		if !req.At.Before(effectiveTo) {
			return Chunk{}, chunkReject("article.effective", "STALE", version, "article is past its effective window")
		}
	}
	detector := defaultDetector(detect)
	tainted, err := detector(req.Text)
	if err != nil {
		return Chunk{}, chunkReject("chunk.text", "HOSTILE", version, "instruction-taint detector failure: "+err.Error())
	}
	if tainted {
		return Chunk{}, chunkReject("chunk.text", "HOSTILE", version, "chunk carries an instruction-smuggling marker")
	}
	want, ok := audienceRank[strings.ToUpper(req.Audience)]
	have, ok2 := audienceRank[strings.ToUpper(a.AudienceScope)]
	if !ok || !ok2 {
		return Chunk{}, chunkReject("chunk.audience", "INVALID", version, "chunk and article audiences must be declared")
	}
	if want < have {
		return Chunk{}, chunkReject("chunk.audience", "BROADENED", version, "chunk cannot broaden access beyond the source article")
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		req.TenantID, a.ArticleID, fmt.Sprintf("%d", a.Revision),
		req.Text, strings.ToUpper(req.Audience), req.Purpose,
	}, "\x00")))
	return Chunk{
		ChunkRef:       req.ChunkRef,
		ArticleID:      a.ArticleID,
		Revision:       a.Revision,
		Text:           req.Text,
		Citations:      append([]SourceRef(nil), a.SourceRefs...),
		Classification: a.Classification,
		Tainted:        false,
		EffectiveFrom:  from,
		EffectiveTo:    effectiveTo,
		Audience:       strings.ToUpper(req.Audience),
		Digest:         "sha256:" + hex.EncodeToString(sum[:]),
	}, nil
}
