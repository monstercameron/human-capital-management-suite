package search

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_REV_026_01(t *testing.T) {
	e := envelope(t)
	query := "Ada Lovelace"
	e.QueryDigest = QueryDigest(query)
	var sourceTenant values.TenantId
	runtime := Runtime{
		Source: CandidateSourceFunc(func(_ context.Context, got QueryEnvelope) ([]Candidate, error) {
			sourceTenant = got.Tenant
			return []Candidate{candidate(t, searchID1, "Ada Lovelace engineer")}, nil
		}),
		Authorizer: AuthorizerFunc(func(_ context.Context, got QueryEnvelope, c Candidate) (AuthorizationDecision, error) {
			return allowDecision(c), nil
		}),
		Ranker: LexicalRanker{},
	}
	results, proof, err := runtime.Search(context.Background(), RuntimeRequest{Envelope: e, QueryText: query})
	if err != nil {
		t.Fatal(err)
	}
	if sourceTenant != e.Tenant || len(results) != 1 || results[0].Subject.Id != searchID1 {
		t.Fatalf("runtime did not return tenant-scoped authorized hit: sourceTenant=%q results=%+v", sourceTenant, results)
	}
	if results[0].SourceDigest == "" || results[0].AuthorizationEvidence == "" || proof.EnvelopeDigest != e.Digest() {
		t.Fatalf("runtime omitted source or authorization evidence: results=%+v proof=%+v", results, proof)
	}
}

func TestTodo_REV_026_01_Integration(t *testing.T) {
	e := envelope(t)
	query := "Ada Lovelace"
	e.QueryDigest = QueryDigest(query)
	var authorized, ranked int
	runtime := Runtime{
		Source: CandidateSourceFunc(func(context.Context, QueryEnvelope) ([]Candidate, error) {
			return []Candidate{candidate(t, searchID2, "Ada Lovelace hidden"), candidate(t, searchID1, "Ada Lovelace visible")}, nil
		}),
		Authorizer: AuthorizerFunc(func(_ context.Context, _ QueryEnvelope, c Candidate) (AuthorizationDecision, error) {
			authorized++
			return AuthorizationDecision{Allowed: c.Subject.Id == searchID1, PolicyDigest: e.PolicyDigest, Evidence: "decision:" + c.Subject.Id}, nil
		}),
		Ranker: QueryRankerFunc(func(_ context.Context, _ QueryEnvelope, gotQuery string, candidates []Candidate) ([]RankedCandidate, error) {
			if gotQuery != query {
				t.Fatalf("ranker query = %q, want %q", gotQuery, query)
			}
			ranked = len(candidates)
			return (LexicalRanker{}).RankQuery(context.Background(), e, gotQuery, candidates)
		}),
	}
	results, _, err := runtime.Search(context.Background(), RuntimeRequest{Envelope: e, QueryText: query})
	if err != nil {
		t.Fatal(err)
	}
	if authorized != 2 || ranked != 1 || len(results) != 1 || results[0].Subject.Id != searchID1 {
		t.Fatalf("source/authz/ranking path mismatch: authorized=%d ranked=%d results=%+v", authorized, ranked, results)
	}
}

func TestTodo_REV_026_01_Security(t *testing.T) {
	e := envelope(t)
	query := "sensitive worker"
	e.QueryDigest = QueryDigest(query)
	var authorizedIDs []string
	rankCalls := 0
	runtime := Runtime{
		// Even an overbroad index adapter cannot pass a foreign tenant's record
		// into the authorization port.
		Source: CandidateSourceFunc(func(context.Context, QueryEnvelope) ([]Candidate, error) {
			foreign := candidate(t, searchID2, "sensitive worker foreign")
			foreign.Subject.Tenant = "other-tenant"
			return []Candidate{foreign, candidate(t, searchID1, "sensitive worker denied")}, nil
		}),
		Authorizer: AuthorizerFunc(func(_ context.Context, _ QueryEnvelope, c Candidate) (AuthorizationDecision, error) {
			authorizedIDs = append(authorizedIDs, c.Subject.Id)
			return AuthorizationDecision{Allowed: false, PolicyDigest: e.PolicyDigest, Evidence: "suppressed"}, nil
		}),
		Ranker: QueryRankerFunc(func(context.Context, QueryEnvelope, string, []Candidate) ([]RankedCandidate, error) {
			rankCalls++
			return nil, nil
		}),
	}
	results, proof, err := runtime.Search(context.Background(), RuntimeRequest{Envelope: e, QueryText: query})
	if err != nil {
		t.Fatal(err)
	}
	if len(authorizedIDs) != 1 || authorizedIDs[0] != searchID1 || rankCalls != 1 || len(results) != 0 {
		t.Fatalf("unauthorized or foreign record reached a visible path: authz=%v rankCalls=%d results=%+v", authorizedIDs, rankCalls, results)
	}
	if strings.Contains(proof.SuppressedDigest, searchID1) || strings.Contains(proof.SuppressedDigest, searchID2) {
		t.Fatalf("suppression proof disclosed a subject identity: %+v", proof)
	}
	// A mismatched query digest fails before source access, avoiding a caller
	// changing query text while retaining evidence for another search.
	e.QueryDigest = "sha256:another-query"
	sourceCalls := 0
	runtime.Source = CandidateSourceFunc(func(context.Context, QueryEnvelope) ([]Candidate, error) {
		sourceCalls++
		return nil, nil
	})
	if _, _, err := runtime.Search(context.Background(), RuntimeRequest{Envelope: e, QueryText: query}); err != ErrInvalidRequest || sourceCalls != 0 {
		t.Fatalf("mismatched query digest err=%v sourceCalls=%d", err, sourceCalls)
	}
}
