package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
)

var (
	ErrNoCandidateSource = errors.New("search: candidate source is required")
	ErrInvalidRequest    = errors.New("search: invalid runtime request")
)

// QueryDigest returns the stable evidence digest callers place in the
// QueryEnvelope for the exact submitted query text.
func QueryDigest(query string) string {
	sum := sha256.Sum256([]byte(query))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// CandidateSource returns the tenant-scoped indexed records available to a
// query plan. Implementations must apply the tenant predicate at the storage
// boundary; Runtime checks it again before authorization.
type CandidateSource interface {
	Candidates(context.Context, QueryEnvelope) ([]Candidate, error)
}

type CandidateSourceFunc func(context.Context, QueryEnvelope) ([]Candidate, error)

func (f CandidateSourceFunc) Candidates(ctx context.Context, e QueryEnvelope) ([]Candidate, error) {
	return f(ctx, e)
}

// RuntimeRequest carries query text only for the ranker. It is never copied
// into results or proof. QueryDigest must bind this text in the caller's
// already-authorized envelope.
type RuntimeRequest struct {
	Envelope  QueryEnvelope
	QueryText string
}

// QueryRanker is the running search ranker port. Like Ranker, it sees only
// candidates that passed authorization.
type QueryRanker interface {
	RankQuery(context.Context, QueryEnvelope, string, []Candidate) ([]RankedCandidate, error)
}

type QueryRankerFunc func(context.Context, QueryEnvelope, string, []Candidate) ([]RankedCandidate, error)

func (f QueryRankerFunc) RankQuery(ctx context.Context, e QueryEnvelope, q string, c []Candidate) ([]RankedCandidate, error) {
	return f(ctx, e, q, c)
}

// Runtime binds an indexed candidate source, the authorization prefilter,
// and a query-aware ranker into one callable path for a transport or worker.
type Runtime struct {
	Source     CandidateSource
	Authorizer Authorizer
	Ranker     QueryRanker
}

func (r Runtime) Search(ctx context.Context, req RuntimeRequest) ([]Result, Proof, error) {
	if ctx == nil || ctx.Err() != nil || strings.TrimSpace(req.QueryText) == "" || strings.TrimSpace(req.QueryText) != req.QueryText {
		return nil, Proof{}, ErrInvalidRequest
	}
	if err := req.Envelope.Validate(); err != nil {
		return nil, Proof{}, err
	}
	if req.Envelope.QueryDigest != QueryDigest(req.QueryText) {
		return nil, Proof{}, ErrInvalidRequest
	}
	if r.Source == nil {
		return nil, Proof{}, ErrNoCandidateSource
	}
	if r.Authorizer == nil {
		return nil, Proof{}, ErrNoAuthorizer
	}
	if r.Ranker == nil {
		return nil, Proof{}, ErrNoRanker
	}
	candidates, err := r.Source.Candidates(ctx, req.Envelope)
	if err != nil {
		return nil, Proof{}, err
	}
	// A broken or overbroad index adapter must not cause cross-tenant content to
	// be sent to authorization or ranking. Drop it without returning identities.
	tenantScoped := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Subject.Tenant == req.Envelope.Tenant {
			tenantScoped = append(tenantScoped, candidate)
		}
	}
	ranker := RankerFunc(func(ctx context.Context, e QueryEnvelope, candidates []Candidate) ([]RankedCandidate, error) {
		return r.Ranker.RankQuery(ctx, e, req.QueryText, candidates)
	})
	return Execute(ctx, req.Envelope, tenantScoped, r.Authorizer, ranker)
}

// LexicalRanker is a small production-ready exact/fuzzy ranker for indexed
// text. More specialized rankers can be injected behind QueryRanker.
type LexicalRanker struct{}

func (LexicalRanker) RankQuery(ctx context.Context, e QueryEnvelope, query string, candidates []Candidate) ([]RankedCandidate, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, ErrInvalidRequest
	}
	terms := searchTerms(query)
	if len(terms) == 0 {
		return nil, ErrInvalidRequest
	}
	ranked := make([]RankedCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		text := strings.ToLower(candidate.SearchText)
		score := int64(0)
		if e.Mode == ModeExact {
			if strings.Contains(text, strings.ToLower(strings.TrimSpace(query))) {
				score = int64(len(terms) * 100)
			}
		} else if e.Mode == ModeFuzzy {
			words := searchTerms(text)
			for _, term := range terms {
				best := 0
				for _, word := range words {
					if word == term {
						best = 100
						break
					}
					if len(term) >= 3 && (strings.Contains(word, term) || strings.Contains(term, word)) && best < 60 {
						best = 60
					}
				}
				score += int64(best)
			}
		} else {
			return nil, ErrInvalidRequest
		}
		if score > 0 {
			ranked = append(ranked, RankedCandidate{Subject: candidate.Subject, Score: score})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			return ranked[i].Subject.String() < ranked[j].Subject.String()
		}
		return ranked[i].Score > ranked[j].Score
	})
	if len(ranked) > e.MaxResults {
		ranked = ranked[:e.MaxResults]
	}
	return ranked, nil
}

func searchTerms(value string) []string {
	return strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
}
